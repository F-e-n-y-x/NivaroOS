package jwt

import (
	"crypto/ecdsa"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/labstack/echo/v5"
)

func strict(t *testing.T, on bool) {
	t.Helper()
	old := middleware.LocalStrictPath
	middleware.LocalStrictPath = filepath.Join(t.TempDir(), "strict")
	t.Cleanup(func() { middleware.LocalStrictPath = old })
	if on {
		if err := os.WriteFile(middleware.LocalStrictPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func keys(t *testing.T) (*ecdsa.PrivateKey, func() (*ecdsa.PublicKey, error)) {
	t.Helper()
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return priv, func() (*ecdsa.PublicKey, error) { return pub, nil }
}

// From a browser (a remote peer), so local-automation trust never applies.
func get(h http.Handler, target string, hdr map[string]string) int {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = "192.168.1.20:5555"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// L5: in strict mode a ?token= only opens WebSockets (which can't send a
// header); media links use a ticket.
func TestQueryTokenOnlyForWebSocketsWhenStrict(t *testing.T) {
	priv, pk := keys(t)
	tok, err := GetAccessToken("bob", priv, 1)
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.Use(JWT(pk))
	e.GET("/v1/x", func(c *echo.Context) error { return c.String(200, "ok") })

	strict(t, false)
	if c := get(e, "/v1/x?token="+tok, nil); c != 200 {
		t.Fatalf("legacy ?token=: %d, want 200", c)
	}
	strict(t, true)
	if c := get(e, "/v1/x?token="+tok, nil); c != 401 {
		t.Fatalf("strict ?token= on a plain GET: %d, want 401", c)
	}
	if c := get(e, "/v1/x?token="+tok, map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}); c != 200 {
		t.Fatalf("strict ?token= on a WebSocket: %d, want 200", c)
	}
	if c := get(e, "/v1/x", map[string]string{"Authorization": tok}); c != 200 {
		t.Fatalf("header token: %d, want 200", c)
	}
}

func TestMediaTicket(t *testing.T) {
	strict(t, true)
	priv, pk := keys(t)
	who := &Claims{Username: "bob", ID: 7, SessionID: "s1"}
	dirT, err := IssueTicket(priv, who, TicketDir, "/DATA/Photos")
	if err != nil {
		t.Fatal(err)
	}
	urlT, err := IssueTicket(priv, who, TicketURL, "/v1/vm-sidecar/vms/win 11/screenshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IssueTicket(priv, who, TicketURL, "/v1/sys/debug"); err == nil {
		t.Fatal("a ticket for a non-media URL was issued")
	}
	if _, err := IssueTicket(priv, who, TicketDir, "DATA/rel"); err == nil {
		t.Fatal("a ticket for a relative dir was issued")
	}

	var gotID int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, c, _ := ValidateRequest(r, RequestToken(r), pk)
		if !ok {
			w.WriteHeader(401)
			return
		}
		gotID = c.ID
	})
	cases := []struct {
		target string
		want   int
	}{
		{"/v1/image?type=thumbnail&path=/DATA/Photos/a.jpg&mt=" + dirT, 200},
		{"/v1/file?path=/DATA/Photos/a.jpg&mt=" + dirT, 200},
		{"/v1/file/stream?path=/DATA/Photos/a.mkv&mt=" + dirT, 200},
		{"/v1/file?path=/DATA/Photos/sub/a.jpg&mt=" + dirT, 401},
		{"/v1/file?path=/DATA/Photos/../../etc/shadow&mt=" + dirT, 401},
		{"/v1/file?path=/DATA/Photos&mt=" + dirT, 401},
		{"/v1/file?path=/DATA/Other/a.jpg&mt=" + dirT, 401},
		{"/v1/folder/size?path=/DATA/Photos/a&mt=" + dirT, 401},
		{"/v1/file/content?path=/DATA/Photos/a.txt&mt=" + dirT, 401},
		{"/vms/win%2011/screenshot?mt=" + urlT, 401}, // not under the sidecar's mount
		{"/v1/vm-sidecar/vms/win%2011/screenshot?mt=" + urlT, 200},
		{"/v1/vm-sidecar/vms/other/screenshot?mt=" + urlT, 401},
		{"/v1/file?path=/DATA/Photos/a.jpg&token=" + dirT, 401}, // a ticket is no token
	}
	for _, c := range cases {
		if got := get(h, c.target, nil); got != c.want {
			t.Errorf("GET %s: %d, want %d", c.target, got, c.want)
		}
	}
	if gotID != 7 {
		t.Errorf("ticket user id %d, want 7", gotID)
	}
	// Never as a bearer token, and never for a write.
	if ok, _, _ := Validate(dirT, pk); ok {
		t.Error("a ticket passed Validate (would work as an Authorization header)")
	}
	r := httptest.NewRequest(http.MethodDelete, "/v1/file?path=/DATA/Photos/a.jpg&mt="+dirT, nil)
	if ok, _, _ := ValidateRequest(r, RequestToken(r), pk); ok {
		t.Error("a ticket allowed a DELETE")
	}
}

// Unused, a ticket lasts TicketLifetime; while a video keeps using it
// (range requests on every seek) it stays valid.
func TestMediaTicketExpiry(t *testing.T) {
	priv, pk := keys(t)
	who := &Claims{Username: "bob", ID: 7}
	t0 := time.Now()
	defer func() { ticketNow = time.Now }()
	at := func(d time.Duration) { ticketNow = func() time.Time { return t0.Add(d) } }
	ok := func(tk string) bool {
		r := httptest.NewRequest(http.MethodGet, "/v1/file?path=/DATA/a.mkv&mt="+tk, nil)
		v, _, _ := ValidateRequest(r, RequestToken(r), pk)
		return v
	}

	idle, _ := IssueTicket(priv, who, TicketDir, "/DATA")
	busy, _ := IssueTicket(priv, who, TicketDir, "/DATA")
	at(TicketLifetime - time.Minute)
	if !ok(busy) {
		t.Fatal("fresh ticket refused")
	}
	at(TicketLifetime + time.Minute)
	if ok(idle) {
		t.Error("unused ticket still valid after TicketLifetime")
	}
	if !ok(busy) {
		t.Error("ticket in use expired")
	}
	at(3*TicketLifetime + time.Minute)
	if ok(busy) {
		t.Error("ticket idle for longer than TicketLifetime still valid")
	}
}
