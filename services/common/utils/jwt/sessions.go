package jwt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
)

// Account-wide session revocation across services.
//
// revocation.go ends one session (its "sid"); this ends every session of
// an account at once - a password change, "sign out everywhere", a
// deleted account - including sessions nothing on the server knows the
// id of.
//
// An access token is signed by the user service and checked by every
// service with the public key alone, so on its own it stays valid for its
// whole lifetime: a password change, "sign out everywhere" or a deleted
// account used to end a session only in the user service, while core,
// app-management, local-storage and the rest kept accepting the token for
// up to 3 h.
//
// Every token now carries its account's token generation ("gen"). The user
// service owns the generations (o_users.token_generation) and publishes all
// of them in a root-owned file in the runtime directory. Revoking an
// account's sessions increments its generation and rewrites the file, and
// Validate - the check every service's auth middleware runs - rejects a
// token whose account isn't listed (deleted) or whose generation is older.
//
// Validate stats the file on every call and re-reads it only when it
// changed (the user service replaces it atomically), so a revocation is
// seen by every service on its next request: no TTL window, no message-bus
// dependency, no network call.

// Token lifetimes (GetSessionTokens / IssueSessionTokens).
const (
	AccessTokenLifetime  = 3 * time.Hour
	RefreshTokenLifetime = 7 * 24 * time.Hour
)

// SessionsFilename is the published session state, in the runtime path.
const SessionsFilename = "user-sessions.json"

// Reasons an account's sessions ended (UserSession.Reason, and the reason
// of the *SessionRevokedError Validate returns).
const (
	ReasonPasswordChanged     = "password_changed"
	ReasonSignedOutEverywhere = "signed_out_everywhere"
	ReasonAccountDeleted      = "account_deleted"
)

// SessionState is the published file.
type SessionState struct {
	Version int                    `json:"version"`
	Users   map[string]UserSession `json:"users"` // key: user id
}

// UserSession is one account's session rule.
type UserSession struct {
	// Generation every valid token of this account carries.
	Generation int64 `json:"gen"`
	// ValidAfter (unix seconds) also rejects tokens issued earlier. It is
	// the older per-account cut-off (set by password changes before token
	// generations existed) and still ends the sessions it ended.
	ValidAfter int64 `json:"valid_after,omitempty"`
	// Reason the generation last changed, for the 401 a client gets.
	Reason string `json:"reason,omitempty"`
}

// Allows reports whether a token with these claims is still a live session.
func (u UserSession) Allows(c *Claims) bool {
	if c == nil || c.Generation != u.Generation {
		return false
	}
	if u.ValidAfter > 0 && (c.IssuedAt == nil || c.IssuedAt.Unix() < u.ValidAfter) {
		return false
	}
	return true
}

type sessionCache struct {
	mu    sync.Mutex
	path  string
	fi    os.FileInfo
	state *SessionState
	err   error
}

var sessions = &sessionCache{path: defaultSessionsPath()}

// defaultSessionsPath is the runtime file, except in a test binary: a test
// never depends on this host's accounts (one that wants the check calls
// SetSessionsPath).
func defaultSessionsPath() string {
	if testing.Testing() {
		return ""
	}
	return filepath.Join(constants.DefaultRuntimePath, SessionsFilename)
}

// SetSessionsPath points Validate at another session file (tests, or a
// non-default runtime path). It returns the previous path.
func SetSessionsPath(p string) string {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	old := sessions.path
	sessions.path, sessions.fi, sessions.state, sessions.err = p, nil, nil, nil
	return old
}

// SessionsPath is the file Validate reads.
func SessionsPath() string {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return sessions.path
}

// load returns the current state; nil (and no error) when nothing has been
// published yet.
func (s *sessionCache) load() (*SessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil, nil
	}
	fi, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.fi, s.state, s.err = nil, nil, nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session state: %w", err)
	}
	if s.fi != nil && os.SameFile(s.fi, fi) && fi.ModTime().Equal(s.fi.ModTime()) && fi.Size() == s.fi.Size() {
		return s.state, s.err
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return s.state, nil // keep the last good state (or none)
	}
	var st SessionState
	if err := json.Unmarshal(raw, &st); err != nil || st.Users == nil {
		// Never a lockout: a damaged file (the user service rewrites it
		// atomically, so this is disk damage or a hand edit) keeps the last
		// good state, or none - signature-only, as before this existed -
		// until the user service republishes it (within 30 s).
		s.fi = fi
		return s.state, nil
	}
	s.fi = fi
	s.state = &st
	return s.state, nil
}

// CheckSession reports whether claims belong to a live session. Before the
// user service has published any state (first start after an upgrade from
// a version without it) only the signature counts, as before; so it does
// when the file is damaged and no good state was read before (no lockout).
func CheckSession(c *Claims) error {
	st, err := sessions.load()
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	u, ok := st.Users[strconv.Itoa(c.ID)]
	if !ok {
		return &SessionRevokedError{Reason: ReasonAccountDeleted}
	}
	if !u.Allows(c) {
		return &SessionRevokedError{Reason: u.Reason}
	}
	return nil
}

// WriteSessionState atomically replaces the state file at path (written by
// the user service only). It is not secret - user ids and counters - but
// only root may write it.
func WriteSessionState(path string, st SessionState) error {
	if st.Users == nil {
		st.Users = map[string]UserSession{}
	}
	if st.Version == 0 {
		st.Version = 1
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(path); err == nil && string(cur) == string(raw) {
		return nil // unchanged: don't make every service re-read it
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// A rename within the same second on a coarse-mtime filesystem still
	// changes the inode, which load compares too.
	_ = os.Chtimes(path, time.Now(), time.Now())
	return nil
}
