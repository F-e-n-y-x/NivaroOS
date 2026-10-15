package route

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
)

func enabledTLS(t *testing.T, redirect bool) *service.TLS {
	tl := &service.TLS{
		Dir: t.TempDir() + "/tls", Port: "0", Handler: http.NotFoundHandler(),
		SANs: func() ([]string, []net.IP) {
			return []string{"nas", "nas.local"}, []net.IP{net.ParseIP("192.168.1.20")}
		},
	}
	t.Cleanup(tl.Stop)
	if err := tl.Set(true, redirect); err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestHTTPSRedirect(t *testing.T) {
	tl := enabledTLS(t, true)
	_, port, _ := net.SplitHostPort(tl.Addr())
	h := HTTPSRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }), tl)

	nav := map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
	for _, c := range []struct {
		name, method, host, peer string
		hdr                      map[string]string
		want                     string // "" = passed through
	}{
		{"page load", "GET", "192.168.1.20", "192.168.1.50:5000", nav, "https://192.168.1.20:" + port + "/apps?x=1"},
		{"page load by name", "GET", "nas.local:80", "192.168.1.50:5000", nav, "https://nas.local:" + port + "/apps?x=1"},
		{"api call (app, fetch)", "GET", "192.168.1.20", "192.168.1.50:5000", map[string]string{"Sec-Fetch-Mode": "cors"}, ""},
		{"no fetch metadata (Android app, curl)", "GET", "192.168.1.20", "192.168.1.50:5000", nil, ""},
		{"iframe", "GET", "192.168.1.20", "192.168.1.50:5000", map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, ""},
		{"form post", "POST", "192.168.1.20", "192.168.1.50:5000", nav, ""},
		{"name the cert lacks", "GET", "nas.example.com", "192.168.1.50:5000", nav, ""},
		{"relay on this box (cloudflared/tailscale serve)", "GET", "box.example.com", "127.0.0.1:5000", nav, ""},
	} {
		r := httptest.NewRequest(c.method, "http://"+c.host+"/apps?x=1", nil)
		r.Host, r.RemoteAddr = c.host, c.peer
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := ""
		if w.Code == http.StatusFound {
			got = w.Header().Get("Location")
		} else if w.Code != 299 {
			t.Errorf("%s: status %d", c.name, w.Code)
		}
		if got != c.want {
			t.Errorf("%s: redirect %q, want %q", c.name, got, c.want)
		}
	}

	// Redirect switch off (https still on): nothing redirects.
	if err := tl.Set(true, false); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://192.168.1.20/", nil)
	r.RemoteAddr = "192.168.1.50:5000"
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 299 {
		t.Fatalf("redirected with the switch off: %d", w.Code)
	}
}

func TestTLSManagementRoutes(t *testing.T) {
	defer setup(t)(t)
	// Public status + CA + trust page; the switch needs a token.
	w := httptest.NewRecorder()
	_router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/gateway/tls", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("no TLS: %d", w.Code)
	}

	_state.TLS = enabledTLS(t, false)
	w = httptest.NewRecorder()
	_router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/gateway/tls", nil))
	var res struct{ Data service.TLSStatus }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || !res.Data.Enabled || res.Data.Fingerprint == "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	_router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/gateway/tls/ca.crt", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-x509-ca-cert" || w.Body.Len() < 100 {
		t.Fatalf("ca.crt: %d %s", w.Code, w.Header())
	}

	w = httptest.NewRecorder()
	_router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/gateway/tls/trust", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), res.Data.Fingerprint) {
		t.Fatalf("trust page: %d", w.Code)
	}

	// PUT from a browser (not local automation) without a token: refused.
	r := httptest.NewRequest("PUT", "/v1/gateway/tls", strings.NewReader(`{"enabled":false}`))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.168.1.50:5000"
	w = httptest.NewRecorder()
	_router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
		t.Fatalf("unauthenticated PUT: %d", w.Code)
	}
	if !_state.TLS.Status().Enabled {
		t.Fatal("switched off without a token")
	}
}

func TestTrustShortcut(t *testing.T) {
	w := httptest.NewRecorder()
	NewGatewayRoute(nil).GetRoute().ServeHTTP(w, httptest.NewRequest("GET", "/trust", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/v1/gateway/tls/trust" {
		t.Fatalf("%d %s", w.Code, w.Header())
	}
}
