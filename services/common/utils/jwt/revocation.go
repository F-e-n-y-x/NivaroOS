package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Session revocation.
//
// Every login starts a session: its access and refresh tokens carry the same
// random "sid" claim, and a refresh keeps it. Ending a session early - a
// companion phone removed from the server's device list, for one - adds its
// sid to revoked_sessions.json; from then on Validate (the access-token check
// of every service's auth middleware) and the user service's refresh refuse
// every token of that session, the ones already handed out included.
//
// The list is one small JSON file on the host, written by whichever service
// ends a session (under an exclusive flock, replaced atomically) and read by
// all of them; a reader re-reads it only when its size or mtime changed, and
// looks at most once a second. Entries are kept for longer than any token of
// the session can live (the refresh token's 7 days), then pruned.

// RevokedSessionsPath is where the list lives. Not in /var/lib/nivaroos/db:
// that directory is world-writable, and whoever can replace the file can
// un-revoke a session.
var RevokedSessionsPath = "/var/lib/nivaroos/revoked_sessions.json"

// revokedTTL is how long an entry is kept: past it, every token that carried
// the sid has expired anyway.
const revokedTTL = 8 * 24 * time.Hour

// RevokedSession is one ended session.
type RevokedSession struct {
	// Reason says why, for the client: ReasonCompanionRemoved lets the app
	// show "this phone was removed" instead of a plain "session ended".
	Reason    string    `json:"reason"`
	RevokedAt time.Time `json:"revoked_at"`
}

// ReasonCompanionRemoved: the session belonged to a companion phone that was
// removed from the server's device list.
const ReasonCompanionRemoved = "companion_removed"

// ErrSessionRevoked is returned (wrapped in *SessionRevokedError) for a
// token whose session was ended.
var ErrSessionRevoked = errors.New("session revoked")

// SessionRevokedError carries the reason a session was ended.
type SessionRevokedError struct{ Reason string }

func (e *SessionRevokedError) Error() string {
	if e.Reason == "" {
		return ErrSessionRevoked.Error()
	}
	return ErrSessionRevoked.Error() + ": " + e.Reason
}

func (e *SessionRevokedError) Unwrap() error { return ErrSessionRevoked }

// RevocationReason returns the reason of a revoked-session error from
// Validate, and whether err is one.
func RevocationReason(err error) (string, bool) {
	var re *SessionRevokedError
	if errors.As(err, &re) {
		return re.Reason, true
	}
	return "", false
}

// NewSessionID returns a random session id for a new login.
func NewSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on Linux; a session without an id just
		// can't be revoked individually.
		return ""
	}
	return hex.EncodeToString(buf)
}

var revoked struct {
	mu        sync.Mutex
	path      string
	checkedAt time.Time
	modTime   time.Time
	size      int64
	entries   map[string]RevokedSession
}

// SessionRevoked reports whether session sid was ended, and why.
func SessionRevoked(sid string) (RevokedSession, bool) {
	if sid == "" {
		return RevokedSession{}, false
	}
	revoked.mu.Lock()
	defer revoked.mu.Unlock()
	refreshRevokedLocked(false)
	rec, ok := revoked.entries[sid]
	return rec, ok
}

// refreshRevokedLocked re-reads the list when it changed. Unless force, the
// file is looked at no more than once a second.
func refreshRevokedLocked(force bool) {
	now := time.Now()
	if revoked.path == RevokedSessionsPath && !force && now.Sub(revoked.checkedAt) < time.Second {
		return
	}
	revoked.checkedAt = now
	fi, err := os.Stat(RevokedSessionsPath)
	if err != nil {
		// No file: nothing was ever revoked (or it was pruned away).
		revoked.path, revoked.entries = RevokedSessionsPath, nil
		revoked.modTime, revoked.size = time.Time{}, 0
		return
	}
	if revoked.path == RevokedSessionsPath && fi.ModTime().Equal(revoked.modTime) && fi.Size() == revoked.size {
		return
	}
	entries, err := readRevoked(RevokedSessionsPath)
	if err != nil {
		// A file being replaced can't be torn (rename is atomic), so this is
		// a damaged file: keep what was known rather than forget it.
		return
	}
	revoked.path, revoked.entries = RevokedSessionsPath, entries
	revoked.modTime, revoked.size = fi.ModTime(), fi.Size()
}

func readRevoked(path string) (map[string]RevokedSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]RevokedSession{}, nil
		}
		return nil, err
	}
	entries := map[string]RevokedSession{}
	if len(data) == 0 {
		return entries, nil
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// RevokeSessions ends the given sessions: every access and refresh token that
// carries one of their ids stops working, in every service, within a second.
func RevokeSessions(sids []string, reason string) error {
	var ids []string
	for _, s := range sids {
		if s != "" {
			ids = append(ids, s)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	path := RevokedSessionsPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Writers (any service) take turns on a lock file next to the list;
	// readers never lock - they only ever see a whole file.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	entries, err := readRevoked(path)
	if err != nil {
		// A damaged list is replaced; losing its old entries is safer than
		// being unable to revoke anything ever again.
		entries = map[string]RevokedSession{}
	}
	now := time.Now()
	for sid, rec := range entries {
		if now.Sub(rec.RevokedAt) > revokedTTL {
			delete(entries, sid)
		}
	}
	for _, sid := range ids {
		entries[sid] = RevokedSession{Reason: reason, RevokedAt: now}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".revoked_sessions-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
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
	// Session ids are not credentials; every service must be able to read
	// the list whatever user it runs as.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("saving revoked sessions: %w", err)
	}
	// This process sees its own revocation at once.
	revoked.mu.Lock()
	refreshRevokedLocked(true)
	revoked.mu.Unlock()
	return nil
}
