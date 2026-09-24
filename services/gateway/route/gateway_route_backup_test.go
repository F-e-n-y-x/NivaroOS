package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

func TestBackupRouting(t *testing.T) {
	for p, want := range map[string]bool{
		"/v1/backup":                        true,
		"/v1/backup/health":                 true,
		"/v1/backup/jobs/3/runs":            true,
		"/v1/backupx":                       false,
		"/v1/backu":                         false,
		"/v1/schedules":                     false,
		"/v1/download-station/backup/files": false,
	} {
		if got := isBackupPath(p); got != want {
			t.Errorf("isBackupPath(%q)=%v want %v", p, got, want)
		}
	}
}

// The service trusts the local automation header, so the gateway must
// strip a client-supplied copy before proxying and set it only for a
// same-host non-browser caller.
func TestBackupRouteLocalAutomationHeader(t *testing.T) {
	var got []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+" "+r.Header.Get(nivaroos_middleware.LocalAutomationHeader))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	saved := backupProxy.Director
	defer func() { backupProxy.Director = saved }()
	backupProxy.Director = func(r *http.Request) {
		saved(r)
		r.URL.Host = backend.Listener.Addr().String()
	}

	mux := NewGatewayRoute(nil).GetRoute()
	send := func(remote string, hdr map[string]string) {
		req := httptest.NewRequest(http.MethodGet, "/v1/backup/runs?status=running", nil)
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
	// A remote client forging the header.
	send("192.0.2.7:5000", map[string]string{nivaroos_middleware.LocalAutomationHeader: "1"})
	// A browser tab on this host.
	send("127.0.0.1:5000", map[string]string{"Origin": "http://localhost", nivaroos_middleware.LocalAutomationHeader: "1"})
	// The installer's curl on this host.
	send("127.0.0.1:5000", nil)

	want := []string{"/v1/backup/runs ", "/v1/backup/runs ", "/v1/backup/runs 1"}
	if len(got) != len(want) {
		t.Fatalf("backend saw %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: backend saw %q, want %q (path kept, header only for local automation)", i, got[i], want[i])
		}
	}
}

func TestBackupRouteUnavailable(t *testing.T) {
	saved := backupProxy.Director
	defer func() { backupProxy.Director = saved }()
	backupProxy.Director = func(r *http.Request) {
		saved(r)
		r.URL.Host = "127.0.0.1:1" // nothing listens here
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/backup/health", nil)
	req.RemoteAddr = "192.0.2.7:5000"
	rec := httptest.NewRecorder()
	NewGatewayRoute(nil).GetRoute().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502 so the UI treats the module as not installed", rec.Code)
	}
}
