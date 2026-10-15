package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

// Pins what echo v4 sent (for a page of this same host) so the v5 migration
// stays invisible to clients:
// CORS headers and the {"message": ...} error body.
func TestCorsAndErrorShape(t *testing.T) {
	e := echo.New()
	e.Use(Cors())
	e.GET("/bad", func(c *echo.Context) error { return echo.NewHTTPError(http.StatusBadRequest, "nope") })
	e.GET("/boom", func(c *echo.Context) error { return http.ErrAbortHandler })

	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		return w
	}

	w := do(http.MethodOptions, "/bad", map[string]string{"Origin": "http://example.com:5173", "Access-Control-Request-Method": "GET"})
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://example.com:5173" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Methods") != "POST,GET,OPTIONS,PUT,DELETE" ||
		w.Header().Get("Access-Control-Max-Age") != "172800" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	for path, want := range map[string]struct {
		code int
		body string
	}{
		"/bad":     {400, `{"message":"nope"}`},
		"/boom":    {500, `{"message":"Internal Server Error"}`},
		"/missing": {404, `{"message":"Not Found"}`},
	} {
		w := do(http.MethodGet, path, map[string]string{"Origin": "http://example.com:5173"})
		if w.Code != want.code || strings.TrimSpace(w.Body.String()) != want.body || w.Header().Get("Access-Control-Allow-Origin") != "http://example.com:5173" {
			t.Errorf("%s: %d %q %v", path, w.Code, w.Body.String(), w.Header())
		}
	}
}

func TestQuietRequest(t *testing.T) {
	withProc(t, fakeProc(t))
	ok := echo_middleware.RequestLoggerValues{Status: 200, Latency: time.Millisecond}
	req := func(remote string, hdr ...string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/x", nil)
		r.RemoteAddr = remote
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		return r
	}
	slow, bad, failed := ok, ok, ok
	slow.Latency = 2 * time.Second
	bad.Status = 404
	failed.Error = errors.New("x")
	for name, c := range map[string]struct {
		r    *http.Request
		v    echo_middleware.RequestLoggerValues
		want bool
	}{
		"internal ok":    {req("127.0.0.1:5000"), ok, true},
		"internal v6 ok": {req("[::1]:5000"), ok, true},
		"internal slow":  {req("127.0.0.1:5000"), slow, false},
		"internal 404":   {req("127.0.0.1:5000"), bad, false},
		"internal error": {req("127.0.0.1:5000"), failed, false},
		"via gateway":    {req("127.0.0.1:5000", "X-Forwarded-For", "100.82.15.11"), ok, false},
		// tailscaled relays from loopback; its headers must keep it logged.
		"via tailscale serve": {req("127.0.0.1:5000", "X-Forwarded-Host", "box.tail1.ts.net", "X-Forwarded-Proto", "https", "Tailscale-User-Login", "owner@example.com"), ok, false},
		"local browser":       {req("127.0.0.1:5000", "Origin", "http://localhost"), ok, false},
		"remote ok":           {req("192.168.1.9:5000"), ok, false},
	} {
		if got := quietRequest(c.r, c.v); got != c.want {
			t.Errorf("%s: quiet=%v, want %v", name, got, c.want)
		}
	}
}

// A userspace tailscaled re-dials tailnet peers from 127.0.0.1: their
// requests must reach the access log.
func TestQuietRequestUserspaceTailscale(t *testing.T) {
	withProc(t, fakeProc(t, fakeProcess{pid: "7", comm: "tailscaled", args: []string{"tailscaled", "--tun=userspace-networking"}}))
	r := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	if quietRequest(r, echo_middleware.RequestLoggerValues{Status: 200, Latency: time.Millisecond}) {
		t.Fatal("loopback request under userspace tailscaled was not logged")
	}
}
