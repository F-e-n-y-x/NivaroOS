package jwt

import (
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func useTempSessions(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), SessionsFilename)
	old := SetSessionsPath(p)
	t.Cleanup(func() { SetSessionsPath(old) })
	return p
}

func publish(t *testing.T, path string, users map[string]UserSession) {
	t.Helper()
	if err := WriteSessionState(path, SessionState{Users: users}); err != nil {
		t.Fatal(err)
	}
}

func TestTestBinariesNeverReadTheHostsSessions(t *testing.T) {
	if defaultSessionsPath() != "" {
		t.Fatal("a test binary reads the host's session state by default")
	}
}

func TestNothingPublishedMeansSignatureOnly(t *testing.T) {
	useTempSessions(t) // path set, file absent (user service not upgraded yet)
	priv, pub, _ := GenerateKeyPair()
	access, _, _ := IssueSessionTokens("alice", priv, 1, NewSessionID(), 0)
	if ok, _, err := Validate(access, func() (*ecdsa.PublicKey, error) { return pub, nil }); !ok || err != nil {
		t.Fatalf("rejected before any state was published: %v", err)
	}
}

func TestRevokingAnAccountEndsEverySessionEverywhereAtOnce(t *testing.T) {
	path := useTempSessions(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	publish(t, path, map[string]UserSession{"1": {Generation: 0}, "2": {Generation: 0}})

	phone, _, _ := IssueSessionTokens("alice", priv, 1, NewSessionID(), 0)
	web, _, _ := IssueSessionTokens("alice", priv, 1, NewSessionID(), 0)
	legacy, _ := GetAccessToken("alice", priv, 1) // no sid, no gen
	bob, _, _ := IssueSessionTokens("bob", priv, 2, NewSessionID(), 0)
	for _, tok := range []string{phone, web, legacy, bob} {
		if ok, _, err := Validate(tok, key); !ok {
			t.Fatalf("valid token refused: %v", err)
		}
	}

	// alice changes her password: generation 1.
	publish(t, path, map[string]UserSession{"1": {Generation: 1, Reason: ReasonPasswordChanged}, "2": {Generation: 0}})
	for _, tok := range []string{phone, web, legacy} {
		ok, _, err := Validate(tok, key)
		if ok {
			t.Fatal("a session from before the password change still works")
		}
		if r, _ := RevocationReason(err); r != ReasonPasswordChanged {
			t.Fatalf("reason %q, want %q (%v)", r, ReasonPasswordChanged, err)
		}
	}
	if ok, _, err := Validate(bob, key); !ok {
		t.Fatalf("another account's session ended too: %v", err)
	}
	// The session that made the change got tokens of the new generation.
	fresh, _, _ := IssueSessionTokens("alice", priv, 1, NewSessionID(), 1)
	if ok, _, err := Validate(fresh, key); !ok {
		t.Fatalf("new-generation token refused: %v", err)
	}
}

func TestADeletedAccountsTokensAreRefused(t *testing.T) {
	path := useTempSessions(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	publish(t, path, map[string]UserSession{"1": {}})
	tok, _, _ := IssueSessionTokens("carol", priv, 3, NewSessionID(), 0)
	ok, _, err := Validate(tok, key)
	if ok {
		t.Fatal("token of an account that doesn't exist accepted")
	}
	if r, _ := RevocationReason(err); r != ReasonAccountDeleted {
		t.Fatalf("reason %q", r)
	}
	if res := UnauthorizedResult(err); res.Message != "this account no longer exists" {
		t.Fatalf("401 body %+v", res)
	}
}

func TestValidAfterStillEndsOlderSessions(t *testing.T) {
	path := useTempSessions(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	old, _, _ := IssueSessionTokens("alice", priv, 1, "", 0)
	publish(t, path, map[string]UserSession{"1": {ValidAfter: time.Now().Add(time.Hour).Unix()}})
	if ok, _, _ := Validate(old, key); ok {
		t.Fatal("a token issued before valid_after was accepted")
	}
}

// No lockout: a damaged state file never refuses everyone. With a good
// state read before, that one keeps applying; with none, signature only.
func TestADamagedStateFileNeverLocksEveryoneOut(t *testing.T) {
	path := useTempSessions(t)
	priv, pub, _ := GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	tok, _, _ := IssueSessionTokens("alice", priv, 1, "", 0)
	os.WriteFile(path, []byte("{not json"), 0o644)
	if ok, _, err := Validate(tok, key); !ok {
		t.Fatalf("locked out by a damaged state file: %v", err)
	}
	// A good state, then damage: the good state still applies.
	publish(t, path, map[string]UserSession{"1": {Generation: 1, Reason: ReasonPasswordChanged}})
	if ok, _, _ := Validate(tok, key); ok {
		t.Fatal("revoked token accepted")
	}
	os.WriteFile(path, []byte("garbage, longer than before"), 0o644)
	if ok, _, _ := Validate(tok, key); ok {
		t.Fatal("damage un-revoked a session")
	}
	fresh, _, _ := IssueSessionTokens("alice", priv, 1, "", 1)
	if ok, _, err := Validate(fresh, key); !ok {
		t.Fatalf("current session locked out by damage: %v", err)
	}
	// Removed file (e.g. /run cleared at boot before the user service
	// republishes): signature only - nobody locked out.
	os.Remove(path)
	if ok, _, err := Validate(fresh, key); !ok {
		t.Fatalf("locked out with no state file: %v", err)
	}
}

func TestWriteSessionStateIsAtomicAndSkipsNoChange(t *testing.T) {
	path := useTempSessions(t)
	publish(t, path, map[string]UserSession{"1": {Generation: 2}})
	fi1, _ := os.Stat(path)
	if fi1.Mode().Perm() != 0o644 {
		t.Fatalf("mode %o", fi1.Mode().Perm())
	}
	publish(t, path, map[string]UserSession{"1": {Generation: 2}})
	fi2, _ := os.Stat(path)
	if !os.SameFile(fi1, fi2) || !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatal("an unchanged state was rewritten")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	// Readers during rewrites only ever see a whole file.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for g := int64(0); ; g++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = WriteSessionState(path, SessionState{Users: map[string]UserSession{"1": {Generation: g % 2}}})
		}
	}()
	c := &Claims{ID: 1}
	for i := 0; i < 2000; i++ {
		if err := CheckSession(c); err != nil {
			if _, ok := RevocationReason(err); !ok {
				close(stop)
				wg.Wait()
				t.Fatalf("reader saw a torn file: %v", err)
			}
		}
	}
	close(stop)
	wg.Wait()
}
