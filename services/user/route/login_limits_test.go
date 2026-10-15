package route

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func (w *world) loginFrom(ip, name, pw string) int {
	raw, _ := json.Marshal(map[string]string{"username": name, "password": pw})
	req := httptest.NewRequest(http.MethodPost, "/v1/users/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = net.JoinHostPort(ip, "5000")
	rec := httptest.NewRecorder()
	w.users.ServeHTTP(rec, req)
	return rec.Code
}

// M2: one bad login every 12 s from the internet kept the owner's name
// bucket empty, so the owner couldn't sign in from anywhere. Now internet
// failures only limit the internet; LAN and tailnet still get in.
func TestInternetGuessingDoesntLockOutTheLANOrTailnet(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "owner", "right password")
	for i := 0; i < 30; i++ {
		w.loginFrom("203.0.113."+strconv.Itoa(i), "owner", "guess")
	}
	if got := w.loginFrom("203.0.113.200", "owner", "guess"); got != http.StatusTooManyRequests {
		t.Fatalf("31st guess from a fresh internet address: %d, want 429 (the internet shares one per-name budget)", got)
	}
	for _, ip := range []string{"192.168.10.20", "100.101.102.103", "fd7a:115c:a1e0::1"} {
		if got := w.loginFrom(ip, "owner", "right password"); got != http.StatusOK {
			t.Fatalf("owner from %s locked out by internet guessing: %d", ip, got)
		}
	}
}

// The account list was public: the internet got the name to guess.
func TestTheAccountListNeedsAToken(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "owner", "right password")
	if rec := w.call(t, w.users, http.MethodGet, "/v1/users/name", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /v1/users/name: %d %s", rec.Code, rec.Body)
	}
	tok := w.login(t, "owner", "right password")
	if rec := w.call(t, w.users, http.MethodGet, "/v1/users/name", tok.access, nil); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("owner")) {
		t.Fatalf("signed-in GET /v1/users/name: %d %s", rec.Code, rec.Body)
	}
}
