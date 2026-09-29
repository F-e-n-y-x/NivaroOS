package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

func TestFansRouting(t *testing.T) {
	for p, want := range map[string]bool{
		"/v1/fans":           true,
		"/v1/fans/health":    true,
		"/v1/fans/status":    true,
		"/v1/fansx":          false,
		"/v1/fan":            false,
		"/v1/sys/fans":       false,
		"/v1/backup/v1/fans": false,
	} {
		if got := isFansPath(p); got != want {
			t.Errorf("isFansPath(%q)=%v want %v", p, got, want)
		}
	}
}

// The path is kept and the local automation header is only vouched for a
// same-host non-browser caller (the service treats that as admin).
func TestFansRouteLocalAutomationHeader(t *testing.T) {
	var got []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+" "+r.Header.Get(nivaroos_middleware.LocalAutomationHeader))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	saved := fansProxy.Director
	defer func() { fansProxy.Director = saved }()
	fansProxy.Director = func(r *http.Request) {
		saved(r)
		r.URL.Host = backend.Listener.Addr().String()
	}

	mux := NewGatewayRoute(nil).GetRoute()
	send := func(remote string, hdr map[string]string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/fans/auto", nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	send("192.0.2.7:5000", map[string]string{nivaroos_middleware.LocalAutomationHeader: "1"})
	send("127.0.0.1:5000", map[string]string{"Origin": "http://localhost", nivaroos_middleware.LocalAutomationHeader: "1"})
	send("127.0.0.1:5000", nil)

	want := []string{"/v1/fans/auto ", "/v1/fans/auto ", "/v1/fans/auto 1"}
	if len(got) != len(want) {
		t.Fatalf("backend saw %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: backend saw %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFansRouteUnavailable(t *testing.T) {
	saved := fansProxy.Director
	defer func() { fansProxy.Director = saved }()
	fansProxy.Director = func(r *http.Request) {
		saved(r)
		r.URL.Host = "127.0.0.1:1"
	}
	rec := httptest.NewRecorder()
	NewGatewayRoute(nil).GetRoute().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/fans/status", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}
