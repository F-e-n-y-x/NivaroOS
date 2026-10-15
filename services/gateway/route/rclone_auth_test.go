package route

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// L8: rclone's OAuth callback reaches `rclone authorize` (127.0.0.1:53682)
// through the dashboard's own port - local-storage used to listen on 53682
// on every LAN, docker, libvirt and Tailscale address, all the time.
// Whatever holds loopback 53682 can be any local user, so its pages get a
// sandboxed origin and never see the dashboard's credentials.
func TestRcloneAuthProxy(t *testing.T) {
	type seen struct{ uri, auth, cookie string }
	got := make(chan seen, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Cookie")}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<p>Success!</p>")
	}))
	defer backend.Close()
	p := newRcloneAuthProxy(strings.TrimPrefix(backend.URL, "http://"))

	r := httptest.NewRequest(http.MethodGet, "/v1/rclone-auth/?state=s&code=c", nil)
	r.Header.Set("Authorization", "secret-token")
	r.Header.Set("Cookie", "a=b")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	s := <-got
	if s.uri != "/?state=s&code=c" || s.auth != "" || s.cookie != "" {
		t.Errorf("rclone saw %+v, want /?state=s&code=c without credentials", s)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("CSP %q, want sandbox", csp)
	}

	w = httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/rclone-auth/", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", w.Code)
	}

	backend.Close()
	w = httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/rclone-auth/auth?state=s", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "rclone") {
		t.Errorf("no rclone running: %d %q", w.Code, w.Body)
	}

	for path, want := range map[string]bool{"/v1/rclone-auth": true, "/v1/rclone-auth/auth": true, "/v1/rclone-authx": false, "/v1/rclone": false} {
		if isRcloneAuthPath(path) != want {
			t.Errorf("isRcloneAuthPath(%q) != %v", path, want)
		}
	}
}
