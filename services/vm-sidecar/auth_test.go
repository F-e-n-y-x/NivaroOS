package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopbackAutomation(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    bool
	}{
		{"loopback v4, no browser headers", "127.0.0.1:5000", nil, true},
		{"loopback v6, no browser headers", "[::1]:5000", nil, true},
		{"loopback with Origin", "127.0.0.1:5000", map[string]string{"Origin": "http://127.0.0.1"}, false},
		{"loopback with Sec-Fetch-Site", "127.0.0.1:5000", map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{"loopback with Sec-Fetch-Site none", "127.0.0.1:5000", map[string]string{"Sec-Fetch-Site": "none"}, false},
		{"LAN address", "192.168.1.20:5000", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/vms", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			if got := isLoopbackAutomation(r); got != c.want {
				t.Fatalf("isLoopbackAutomation = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRequireAuth_BrowserOnLoopbackStillNeedsToken(t *testing.T) {
	called := false
	h := requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }), t.TempDir())

	r := httptest.NewRequest(http.MethodGet, "/vms", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called || w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without calling the handler, got %d (called=%v)", w.Code, called)
	}

	r = httptest.NewRequest(http.MethodGet, "/vms", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Fatalf("expected non-browser loopback automation to pass through, got %d", w.Code)
	}
}

func TestSameHostOrigin(t *testing.T) {
	cases := []struct {
		host, origin string
		want         bool
	}{
		{"192.168.1.5:28641", "http://192.168.1.5", true},
		{"192.168.1.5:28641", "https://192.168.1.5:8443", true},
		{"nas.local:28641", "http://NAS.local:80", true},
		{"[fe80::1]:28641", "http://[fe80::1]:8080", true},
		{"192.168.1.5:28641", "http://192.168.1.6", false},
		{"192.168.1.5:28641", "http://192.168.1.5.evil.example", false},
		{"192.168.1.5:28641", "file://192.168.1.5", false},
		{"192.168.1.5:28641", "null", false},
		{"192.168.1.5:28641", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/vms", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := sameHostOrigin(r); got != c.want {
			t.Errorf("host %q origin %q: got %v, want %v", c.host, c.origin, got, c.want)
		}
	}
}

func TestWithCORS_EchoesOnlySameHostOrigin(t *testing.T) {
	h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/vms", nil)
	r.Host = "10.0.0.2:28641"
	r.Header.Set("Origin", "http://10.0.0.2:8080")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://10.0.0.2:8080" {
		t.Errorf("expected same-host origin echoed back, got %q", got)
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Errorf("expected Vary: Origin, got %q", w.Header().Get("Vary"))
	}

	r = httptest.NewRequest(http.MethodOptions, "/vms", nil)
	r.Host = "10.0.0.2:28641"
	r.Header.Set("Origin", "http://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no ACAO for a foreign origin, got %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("expected no preflight allowance for a foreign origin")
	}
}

func TestConsoleCheckOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true}, // mobile app / non-browser clients
		{"http://10.0.0.2:8080", true},
		{"http://evil.example", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/vms/x/console", nil)
		r.Host = "10.0.0.2:28641"
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := consoleUpgrader.CheckOrigin(r); got != c.want {
			t.Errorf("origin %q: got %v, want %v", c.origin, got, c.want)
		}
	}
}
