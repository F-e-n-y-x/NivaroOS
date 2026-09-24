package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func req(remote string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://10.0.0.5/v1/sys/hardware", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestIsLocalAutomation(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    bool
	}{
		{"direct loopback v4", "127.0.0.1:5555", nil, true},
		{"direct loopback v6", "[::1]:5555", nil, true},
		{"lan peer", "192.168.1.20:5555", nil, false},
		{"lan peer spoofing XFF", "192.168.1.20:5555", map[string]string{"X-Forwarded-For": "127.0.0.1"}, false},
		{"lan peer spoofing X-Real-IP", "192.168.1.20:5555", map[string]string{"X-Real-IP": "127.0.0.1"}, false},
		{"proxied via gateway (browser/mobile)", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "192.168.1.20"}, false},
		{"proxied, XFF claims loopback", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "127.0.0.1"}, false},
		{"browser on host", "127.0.0.1:5555", map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		{"page with origin", "127.0.0.1:5555", map[string]string{"Origin": "http://evil.example"}, false},
		{"gateway-vouched cli", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "127.0.0.1", LocalAutomationHeader: "1"}, true},
		{"gateway-vouched but browser", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "127.0.0.1", LocalAutomationHeader: "1", "Origin": "http://x"}, false},
		{"lan peer with forged mark", "192.168.1.20:5555", map[string]string{LocalAutomationHeader: "1"}, false},
	}
	for _, c := range cases {
		if got := IsLocalAutomation(req(c.remote, c.headers)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestMarkLocalAutomation(t *testing.T) {
	r := req("192.168.1.20:1", map[string]string{LocalAutomationHeader: "1"})
	MarkLocalAutomation(r)
	if r.Header.Get(LocalAutomationHeader) != "" {
		t.Fatal("forged mark from LAN peer must be stripped")
	}
	r = req("127.0.0.1:1", map[string]string{LocalAutomationHeader: "1", "X-Forwarded-For": "1.2.3.4"})
	MarkLocalAutomation(r)
	if r.Header.Get(LocalAutomationHeader) != "" {
		t.Fatal("request from an outer reverse proxy must not be marked")
	}
	r = req("127.0.0.1:1", nil)
	MarkLocalAutomation(r)
	if r.Header.Get(LocalAutomationHeader) != "1" {
		t.Fatal("direct local automation should be marked")
	}
}

func TestMatchRoute(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/v1/sys/wsterm", "/v1/sys/wsterm", true},
		{"/v1/sys/wsterm", "/v1/sys/wsterm/", true},
		{"/v1/sys/wsterm", "/v1/sys/wstermx", false},
		{"/v1/container/:id/terminal", "/v1/container/abc123/terminal", true},
		{"/v1/container/:id/terminal", "/v1/container//terminal", false},
		{"/v1/container/:id/terminal", "/v1/container/abc/logs", false},
		{"/v1/container/:id/terminal", "/v1/container/abc/terminal/x", false},
		{"/v1/files/*", "/v1/files/a/b", true},
	}
	for _, c := range cases {
		if got := MatchRoute(c.pattern, c.path); got != c.want {
			t.Errorf("MatchRoute(%q,%q)=%v want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestLocalAutomationSkipperNeverSkip(t *testing.T) {
	e := echo.New()
	skip := LocalAutomationSkipper("/v1/sys/wsterm", "/v1/container/:id/terminal")
	for path, want := range map[string]bool{
		"/v1/sys/hardware":           true,
		"/v1/sys/wsterm":             false,
		"/v1/container/abc/terminal": false,
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:4000"
		c := e.NewContext(r, httptest.NewRecorder())
		if got := skip(c); got != want {
			t.Errorf("%s: skip=%v want %v", path, got, want)
		}
	}
}

func TestCheckWebSocketOrigin(t *testing.T) {
	cases := []struct {
		host, origin string
		want         bool
	}{
		{"192.168.1.10", "", true},
		{"192.168.1.10", "http://192.168.1.10:8080", true},
		{"nas.local:80", "https://NAS.local", true},
		{"[fe80::1]:80", "http://[fe80::1]:3000", true},
		{"192.168.1.10", "http://evil.example", false},
		{"192.168.1.10", "null", false},
		{"192.168.1.10", "file://192.168.1.10", false},
		{"127.0.0.1:80|nas.example.com", "https://nas.example.com", true},
		{"127.0.0.1:80|nas.example.com", "https://evil.example", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		host, fwd, _ := strings.Cut(c.host, "|")
		r.Host = host
		if fwd != "" {
			r.Header.Set("X-Forwarded-Host", fwd)
		}
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := CheckWebSocketOrigin(r); got != c.want {
			t.Errorf("host=%q origin=%q: got %v want %v", c.host, c.origin, got, c.want)
		}
	}
}
