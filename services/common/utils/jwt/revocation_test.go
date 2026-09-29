package jwt

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v4"
)

func useTempRevocationList(t *testing.T) string {
	t.Helper()
	old := RevokedSessionsPath
	RevokedSessionsPath = filepath.Join(t.TempDir(), "revoked_sessions.json")
	t.Cleanup(func() {
		RevokedSessionsPath = old
		revoked.mu.Lock()
		revoked.path, revoked.entries, revoked.checkedAt = "", nil, time.Time{}
		revoked.mu.Unlock()
	})
	return RevokedSessionsPath
}

func TestSessionTokensCarryTheSessionID(t *testing.T) {
	useTempRevocationList(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	sid := NewSessionID()
	if len(sid) != 32 {
		t.Fatalf("session id %q", sid)
	}
	access, refresh, err := GetSessionTokens("alice", priv, 7, sid)
	if err != nil {
		t.Fatal(err)
	}
	ok, claims, err := Validate(access, key)
	if !ok || err != nil || claims.SessionID != sid || claims.ID != 7 {
		t.Fatalf("access: ok=%v err=%v claims=%+v", ok, err, claims)
	}
	rc, err := ParseToken(refresh, key)
	if err != nil || rc.SessionID != sid || rc.Issuer != "refresh" {
		t.Fatalf("refresh: %v %+v", err, rc)
	}
}

func TestRevokedSessionIsRefusedAtOnceOthersKeepWorking(t *testing.T) {
	path := useTempRevocationList(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	phone, web := NewSessionID(), NewSessionID()
	phoneAccess, _, _ := GetSessionTokens("alice", priv, 1, phone)
	webAccess, _, _ := GetSessionTokens("alice", priv, 1, web)
	legacy, _ := GetAccessToken("alice", priv, 1) // no sid

	// Read once, so the reader has a cached (empty) list.
	if ok, _, _ := Validate(phoneAccess, key); !ok {
		t.Fatal("phone token refused before revocation")
	}
	if err := RevokeSessions([]string{phone, ""}, ReasonCompanionRemoved); err != nil {
		t.Fatal(err)
	}
	ok, _, err := Validate(phoneAccess, key)
	if ok {
		t.Fatal("revoked session's access token still accepted")
	}
	if reason, is := RevocationReason(err); !is || reason != ReasonCompanionRemoved {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrSessionRevoked) {
		t.Fatal("not ErrSessionRevoked")
	}
	if ok, _, err := Validate(webAccess, key); !ok {
		t.Fatalf("another session of the same user was ended too: %v", err)
	}
	if ok, _, err := Validate(legacy, key); !ok {
		t.Fatalf("token without a session id refused: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("list must be readable by every service: %v %v", err, fi.Mode())
	}
	res := UnauthorizedResult(&SessionRevokedError{Reason: ReasonCompanionRemoved})
	if d, _ := res.Data.(map[string]string); d["reason"] != ReasonCompanionRemoved {
		t.Fatalf("401 body %+v", res)
	}
}

// Another process (core) revokes; this one (another service) notices within
// its one-second re-check, without a restart.
func TestRevocationWrittenElsewhereIsPickedUp(t *testing.T) {
	path := useTempRevocationList(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	sid := NewSessionID()
	access, _, _ := GetSessionTokens("bob", priv, 2, sid)
	if ok, _, _ := Validate(access, key); !ok {
		t.Fatal("refused before revocation")
	}
	data, _ := json.Marshal(map[string]RevokedSession{sid: {Reason: "x", RevokedAt: time.Now()}})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	revoked.mu.Lock()
	revoked.checkedAt = time.Now().Add(-2 * time.Second) // the second has passed
	revoked.mu.Unlock()
	if ok, _, _ := Validate(access, key); ok {
		t.Fatal("revocation by another process not seen")
	}
}

func TestOldEntriesArePrunedAndConcurrentWritersKeepAll(t *testing.T) {
	path := useTempRevocationList(t)
	old := map[string]RevokedSession{"ancient": {RevokedAt: time.Now().Add(-9 * 24 * time.Hour)}}
	data, _ := json.Marshal(old)
	os.WriteFile(path, data, 0o644)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := RevokeSessions([]string{string(rune('a' + i))}, "r"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := readRevoked(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["ancient"]; ok {
		t.Fatal("entry older than any token was kept")
	}
	if len(entries) != 8 {
		t.Fatalf("lost a concurrent revocation: %d entries", len(entries))
	}
}

// The claim travels under its short, standard-looking name.
func TestSidClaimName(t *testing.T) {
	priv, _, _ := GenerateKeyPair()
	access, _, _ := GetSessionTokens("c", priv, 3, "abc")
	tok, _, err := new(jwtlib.Parser).ParseUnverified(access, jwtlib.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Claims.(jwtlib.MapClaims)["sid"] != "abc" {
		t.Fatalf("claims %v", tok.Claims)
	}
}
