package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/codegen/message_bus"
	"github.com/F-e-n-y-x/NivaroOS/services/user/common"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model/system_model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Pairing a phone by code
//
// A signed-in user asks for a one-time code (Settings > Pair a phone, shown
// with a QR of the server's addresses + the code); the app trades it for a
// normal session of that user. The code is the only secret: 8 characters
// of a 32-letter alphabet (2^40), valid PairCodeLifetime, used once. Only
// its SHA-256 is kept, in memory (a restart ends every pending code), and
// it is compared in constant time. Guessing is limited per client address
// (pairLimiter) and per code: every wrong guess counts against every live
// code, and a code with pairMaxFailures against it is burnt - so a spread
// of addresses still gets pairMaxFailures tries at a 2^40 space per code.

const (
	PairCodeLifetime = 5 * time.Minute
	pairCodeLen      = 8
	pairAlphabet     = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I O 0 1
	pairMaxFailures  = 10
	// A dead code is remembered this long, to answer "used"/"expired"
	// rather than "wrong".
	pairRemember = 10 * time.Minute

	PairNotifyEvent = "user-service:pairing:notify"
)

// Reasons a redeem is refused (data.reason).
const (
	PairInvalid = "invalid"
	PairExpired = "expired"
	PairUsed    = "used"
	PairTooMany = "too_many_attempts"
	pairOK      = ""
)

type pairCode struct {
	hash     [32]byte
	userID   int
	expires  time.Time
	used     bool
	failures int
}

type pairStore struct {
	mu    sync.Mutex
	codes []*pairCode
	now   func() time.Time
}

var (
	pairings = &pairStore{now: time.Now}
	// Redeem attempts per client address, issues per user: the login
	// limiter's buckets (5 at once, then 5 a minute).
	pairLimiter  = &loginLimits{buckets: map[string]*rate.Limiter{}, seen: map[string]time.Time{}}
	issueLimiter = &loginLimits{buckets: map[string]*rate.Limiter{}, seen: map[string]time.Time{}}
)

// normalizePairCode drops what people type around a code (spaces, the
// dash it is shown with) and lower case.
func normalizePairCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r != ' ' && r != '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func newPairCode() (string, error) {
	buf := make([]byte, pairCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, x := range buf {
		buf[i] = pairAlphabet[x&31] // 256 % 32 == 0: uniform
	}
	return string(buf), nil
}

// prune drops codes dead for longer than pairRemember. Caller holds mu.
func (s *pairStore) prune(now time.Time) {
	live := s.codes[:0]
	for _, c := range s.codes {
		if now.Before(c.expires.Add(pairRemember)) {
			live = append(live, c)
		}
	}
	s.codes = live
}

// issue makes userID's code, replacing any earlier one of that user.
func (s *pairStore) issue(userID int) (string, time.Time, error) {
	code, err := newPairCode()
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	for _, c := range s.codes {
		if c.userID == userID && !c.used {
			c.used = true // superseded
		}
	}
	exp := now.Add(PairCodeLifetime)
	s.codes = append(s.codes, &pairCode{hash: sha256.Sum256([]byte(code)), userID: userID, expires: exp})
	return code, exp, nil
}

// redeem uses code once: the user it was issued to, or why not.
func (s *pairStore) redeem(code string) (int, string) {
	code = normalizePairCode(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	if len(code) != pairCodeLen {
		return 0, PairInvalid
	}
	h := sha256.Sum256([]byte(code))
	var hit *pairCode
	for _, c := range s.codes { // every entry, no early exit
		if subtle.ConstantTimeCompare(c.hash[:], h[:]) == 1 {
			hit = c
		}
	}
	switch {
	case hit == nil:
		for _, c := range s.codes {
			if !c.used && now.Before(c.expires) {
				c.failures++
			}
		}
		return 0, PairInvalid
	case hit.used:
		return 0, PairUsed
	case !now.Before(hit.expires):
		return 0, PairExpired
	case hit.failures >= pairMaxFailures:
		hit.used = true
		return 0, PairTooMany
	}
	hit.used = true
	return hit.userID, pairOK
}

// PostPairCode issues a pairing code for the signed-in user.
//
//	POST /v1/users/current/pair -> {code, expires_at (unix s), lifetime (s)}
func PostPairCode(c *gin.Context) {
	user := service.MyService.User().GetUserAllInfoById(c.GetHeader("user_id"))
	if user.Id == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	if !issueLimiter.allow("user:" + strconv.Itoa(user.Id)) {
		c.JSON(common_err.TOO_MANY_REQUEST, model.Result{Success: common_err.TOO_MANY_REQUEST, Message: "Too many pairing codes - wait a minute", Data: gin.H{"reason": PairTooMany}})
		return
	}
	code, exp, err := pairings.issue(user.Id)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{
		"code": code, "expires_at": exp.Unix(), "lifetime": int(PairCodeLifetime.Seconds()),
	}})
}

var pairMessages = map[string]string{
	PairInvalid: "That code is wrong - check it and try again",
	PairExpired: "That code has expired - show a new one in Settings",
	PairUsed:    "That code was already used - show a new one in Settings",
	PairTooMany: "Too many attempts - wait a minute, then show a new code in Settings",
}

func pairRefused(c *gin.Context, reason string) {
	status := http.StatusBadRequest
	switch reason {
	case PairExpired, PairUsed:
		status = http.StatusGone
	case PairTooMany:
		status = http.StatusTooManyRequests
	}
	c.JSON(status, model.Result{Success: status, Message: pairMessages[reason], Data: gin.H{"reason": reason}})
}

// PostPairRedeem trades a pairing code for a new session of its user - the
// same answer as a sign-in (data.token, data.user).
//
//	POST /v1/users/pair {"code": "ABCD-EFGH", "device": "Pixel 9"}
func PostPairRedeem(c *gin.Context) {
	var req struct {
		Code   string `json:"code"`
		Device string `json:"device"`
	}
	_ = c.ShouldBindJSON(&req)
	if !pairLimiter.allow("ip:" + c.ClientIP()) {
		pairRefused(c, PairTooMany)
		return
	}
	id, reason := pairings.redeem(req.Code)
	if reason != pairOK {
		pairRefused(c, reason)
		return
	}
	user := service.MyService.User().GetUserAllInfoById(strconv.Itoa(id))
	if user.Id == 0 { // deleted since the code was shown
		pairRefused(c, PairInvalid)
		return
	}
	privateKey, _ := service.MyService.User().GetKeyPair()
	access, refresh, err := jwt.IssueSessionTokens(user.Username, privateKey, user.Id, jwt.NewSessionID(), user.TokenGeneration)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	notifyPaired(user.Username, req.Device, c.ClientIP())
	user.Password = ""
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{
		"token": system_model.VerifyInformation{AccessToken: access, RefreshToken: refresh, ExpiresAt: time.Now().Add(jwt.AccessTokenLifetime).Unix()},
		"user":  user,
	}})
}

// pairedProps is the notification-feed event for a pairing (the feed's
// ":notify" contract, services/message-bus/service/notification_classify.go).
func pairedProps(username, device, ip string) map[string]string {
	device = strings.TrimSpace(device)
	if r := []rune(device); len(r) > 64 {
		device = string(r[:64])
	}
	if device == "" {
		device = "A phone"
	}
	return map[string]string{
		"title":    "A phone was paired",
		"message":  device + " signed in to " + username + " with a pairing code (from " + ip + ")",
		"level":    "info",
		"category": "system",
	}
}

// notifyPaired is a test seam; the bus is best-effort.
var notifyPaired = func(username, device, ip string) {
	bus := service.MyService.MessageBus()
	if bus == nil {
		return
	}
	go func() {
		// The bus keeps event types in /run: registered again before each
		// publish (an upsert), in case it restarted since this service did.
		et := []message_bus.EventType{{Name: PairNotifyEvent, SourceID: common.SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}}}
		if _, err := bus.RegisterEventTypesWithResponse(context.Background(), et); err != nil {
			logger.Error("registering the pairing notification", zap.Error(err))
		}
		if _, err := bus.PublishEventWithResponse(context.Background(), common.SERVICENAME, PairNotifyEvent, pairedProps(username, device, ip)); err != nil {
			logger.Error("publishing the pairing notification", zap.Error(err))
		}
	}()
}
