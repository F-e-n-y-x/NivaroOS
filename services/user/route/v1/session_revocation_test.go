package v1

import (
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
)

func tokensOf(t *testing.T, body []byte, refresh bool) (access, refreshTok string) {
	t.Helper()
	var res struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatal(err)
	}
	if refresh {
		var d struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		json.Unmarshal(res.Data, &d)
		return d.AccessToken, d.RefreshToken
	}
	var d struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
	}
	json.Unmarshal(res.Data, &d)
	return d.Token.AccessToken, d.Token.RefreshToken
}

func sidOf(t *testing.T, tok string) string {
	t.Helper()
	_, pub := service.MyService.User().GetKeyPair()
	c, err := jwt.ParseToken(tok, func() (*ecdsa.PublicKey, error) { return pub, nil })
	if err != nil {
		t.Fatal(err)
	}
	return c.SessionID
}

// Each sign-in is its own session; a refresh stays in it; once the session
// is revoked (its companion phone was removed) its refresh token is refused
// with the reason, while the user's other sessions carry on.
func TestLoginSessionsCanBeEndedOneByOne(t *testing.T) {
	old := jwt.RevokedSessionsPath
	jwt.RevokedSessionsPath = filepath.Join(t.TempDir(), "revoked_sessions.json")
	t.Cleanup(func() { jwt.RevokedSessionsPath = old })

	f := withAuthRoutes(t)
	setPassword(t, f.alice, "pw for alice")
	login := func() (string, string) {
		// Other tests' failed logins share the process-wide limiter.
		loginLimiter.reset("ip:192.168.1.50", "user:alice")
		w := post(f, "/v1/users/login", map[string]string{"username": "alice", "password": "pw for alice"})
		if w.Code != http.StatusOK {
			t.Fatalf("login: %d %s", w.Code, w.Body)
		}
		return tokensOf(t, w.Body.Bytes(), false)
	}
	phoneAccess, phoneRefresh := login()
	_, webRefresh := login()
	phone := sidOf(t, phoneAccess)
	if phone == "" || phone != sidOf(t, phoneRefresh) {
		t.Fatal("a login's access and refresh tokens must share one session id")
	}
	if phone == sidOf(t, webRefresh) {
		t.Fatal("two logins share a session")
	}

	w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": phoneRefresh})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	a2, r2 := tokensOf(t, w.Body.Bytes(), true)
	if sidOf(t, a2) != phone || sidOf(t, r2) != phone {
		t.Fatal("a refresh left its session")
	}

	if err := jwt.RevokeSessions([]string{phone}, jwt.ReasonCompanionRemoved); err != nil {
		t.Fatal(err)
	}
	w = post(f, "/v1/users/refresh", map[string]string{"refresh_token": r2})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session refreshed: %d", w.Code)
	}
	var res struct {
		Data map[string]string `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Data["reason"] != jwt.ReasonCompanionRemoved {
		t.Fatalf("401 without the reason: %s", w.Body)
	}
	if w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": webRefresh}); w.Code != http.StatusOK {
		t.Fatalf("the web session ended too: %d", w.Code)
	}
}

// A refresh token from before sessions had ids joins a session on refresh.
func TestALegacyRefreshTokenGetsASession(t *testing.T) {
	f := withAuthRoutes(t)
	priv, _ := service.MyService.User().GetKeyPair()
	legacy, _ := jwt.GetRefreshToken("alice", priv, f.alice.Id)
	w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": legacy})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	a, r := tokensOf(t, w.Body.Bytes(), true)
	if s := sidOf(t, a); s == "" || s != sidOf(t, r) {
		t.Fatalf("no session after refresh: %q", s)
	}
}
