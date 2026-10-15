package service

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// The user service owns every account's token generation and publishes
// them (jwt.SessionsFilename in the runtime path) for every service's
// Validate. The database is the source of truth: the file is rewritten
// after each change, at start, and every sessionsRepublishEvery in case
// anything changed the database behind this process's back.

var (
	sessionsMu   sync.Mutex
	sessionsFile string // "" (tests using NewUserService alone): publish nothing
)

// sessionsRepublishEvery bounds how long a change made outside this
// process (the recovery command) takes to reach the other services.
const sessionsRepublishEvery = 30 * time.Second

// SetSessionsFile sets where the session state is published.
func SetSessionsFile(path string) {
	sessionsMu.Lock()
	sessionsFile = path
	sessionsMu.Unlock()
}

// SessionRule is the published rule of one account - what every service
// applies to its tokens. The user service applies the same to the
// database row, so its own checks never wait for the file.
func SessionRule(u model.UserDBModel) jwt.UserSession {
	return jwt.UserSession{Generation: u.TokenGeneration, ValidAfter: u.TokensValidAfter, Reason: u.TokenRevokeReason}
}

// SessionAllows reports whether claims are a live session of account u
// (u.Id 0: the account doesn't exist).
func SessionAllows(u model.UserDBModel, c *jwt.Claims) bool {
	return u.Id != 0 && c != nil && c.ID == u.Id && SessionRule(u).Allows(c)
}

func (u *userService) RevokeSessions(id int, reason string) (int64, error) {
	var gen int64
	err := u.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.UserDBModel{}).Where("id = ?", id).Updates(map[string]interface{}{
			"token_generation":    gorm.Expr("token_generation + 1"),
			"token_revoke_reason": reason,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("no such account")
		}
		return tx.Model(&model.UserDBModel{}).Where("id = ?", id).Pluck("token_generation", &gen).Error
	})
	if err != nil {
		return 0, err
	}
	if err := u.PublishSessions(); err != nil {
		// The database already refuses the old sessions here (and the
		// refresh everywhere); the other services see it on the retry.
		logger.Error("publishing revoked sessions", zap.Error(err))
	}
	return gen, nil
}

func (u *userService) PublishSessions() error {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if sessionsFile == "" {
		return nil
	}
	var users []model.UserDBModel
	if err := u.db.Select("id", "token_generation", "tokens_valid_after", "token_revoke_reason").Find(&users).Error; err != nil {
		return err
	}
	st := jwt.SessionState{Version: 1, Users: make(map[string]jwt.UserSession, len(users))}
	for _, x := range users {
		st.Users[strconv.Itoa(x.Id)] = SessionRule(x)
	}
	return jwt.WriteSessionState(sessionsFile, st)
}

func (u *userService) publishSessions() {
	if err := u.PublishSessions(); err != nil {
		logger.Error("publishing session state", zap.Error(err))
	}
}

// KeepSessionsPublished publishes now and then every
// sessionsRepublishEvery (an unchanged state isn't rewritten).
func KeepSessionsPublished(us UserService, stop <-chan struct{}) {
	publish := func() {
		if err := us.PublishSessions(); err != nil {
			logger.Error("publishing session state", zap.Error(err))
		}
	}
	publish()
	t := time.NewTicker(sessionsRepublishEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			publish()
		}
	}
}

// refreshReuseGrace: the refresh token just exchanged still works this long,
// for clients racing themselves (two tabs, two app engines) or retrying a
// refresh whose answer was lost (a phone's background job may only try
// again minutes later). It gets a token of the current id - the session
// doesn't fork, and a copy used later than this still ends it.
const refreshReuseGrace = 10 * time.Minute

func (u *userService) StartRefresh(sid string, userID int) (string, error) {
	jti := jwt.NewSessionID()
	now := time.Now()
	err := u.db.Transaction(func(tx *gorm.DB) error {
		// Sessions not refreshed for longer than a refresh token lives are over.
		if err := tx.Where("rotated_at < ?", now.Add(-jwt.RefreshTokenLifetime-24*time.Hour).Unix()).Delete(&model.RefreshSessionModel{}).Error; err != nil {
			return err
		}
		return tx.Save(&model.RefreshSessionModel{SessionID: sid, UserID: userID, JTI: jti, RotatedAt: now.Unix()}).Error
	})
	return jti, err
}

func (u *userService) RotateRefresh(sid string, userID int, presented string) (string, bool, error) {
	if sid == "" {
		return "", true, nil
	}
	var next string
	var reused bool
	now := time.Now()
	err := u.db.Transaction(func(tx *gorm.DB) error {
		var r model.RefreshSessionModel
		err := tx.Where("sid = ?", sid).Take(&r).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			// A session from before refresh tokens rotated: start now.
			next = jwt.NewSessionID()
			return tx.Create(&model.RefreshSessionModel{SessionID: sid, UserID: userID, JTI: next, PrevJTI: presented, RotatedAt: now.Unix()}).Error
		case err != nil:
			return err
		case r.UserID != userID:
			reused = true
			return nil
		case presented != "" && presented == r.JTI:
			next = jwt.NewSessionID()
			return tx.Model(&r).Updates(map[string]interface{}{"jti": next, "prev_jti": presented, "rotated_at": now.Unix()}).Error
		case presented == r.PrevJTI && now.Sub(time.Unix(r.RotatedAt, 0)) < refreshReuseGrace:
			next = r.JTI
			return nil
		}
		reused = true
		return tx.Delete(&r).Error
	})
	return next, reused, err
}
