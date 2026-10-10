package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCrashReports(t *testing.T) {
	old := crashReportsDir
	crashReportsDir = t.TempDir()
	t.Cleanup(func() { crashReportsDir = old })
	e := NewEcho()
	e.POST("/r", PostCrashReport)
	e.GET("/r", GetCrashReports)
	e.GET("/r/:id", GetCrashReport)

	body := `{"app_version":"1.4.12","device":"Pixel 8","reports":[{"time":"2026-10-11T10:00:00Z","error":"Bad state: boom","stack":"#0 main"}]}`
	rec := do(e, http.MethodPost, "/r", "application/json", body)
	if rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{}`, `not json`, `{"reports":[]}`, `{"reports":[{"error":"` + strings.Repeat("x", crashReportMaxBytes) + `"}]}`} {
		if rec := do(e, http.MethodPost, "/r", "application/json", bad); rec.Code == 200 {
			t.Errorf("accepted %.40s", bad)
		}
	}

	var list struct{ Data []crashReportItem }
	rec = do(e, http.MethodGet, "/r", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Fatalf("list: %s", rec.Body)
	}
	it := list.Data[0]
	if it.AppVersion != "1.4.12" || it.Device != "Pixel 8" || it.Count != 1 || it.FirstError != "Bad state: boom" || it.ReceivedAt.IsZero() {
		t.Errorf("item %+v", it)
	}
	rec = do(e, http.MethodGet, "/r/"+it.ID, "", "")
	if rec.Code != 200 || rec.Body.String() != body || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("download: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{"..%2F..%2Fetc%2Fpasswd", "1234", it.ID[:19] + "-00000000"} {
		if rec := do(e, http.MethodGet, "/r/"+id, "", ""); rec.Code != 404 {
			t.Errorf("%s: %d", id, rec.Code)
		}
	}

	// A non-admin signed-in user can send but not read.
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	r.Header.Set("Authorization", "Bearer x."+strings.TrimRight(b64(`{"username":"kid","role":"member"}`), "=")+".y")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("member list: %d", w.Code)
	}

	// Caps: never more than crashReportsMax files.
	for i := 0; i < crashReportsMax+5; i++ {
		do(e, http.MethodPost, "/r", "application/json", fmt.Sprintf(`{"reports":[{"error":"e%d"}]}`, i))
	}
	if n := len(crashFiles()); n != crashReportsMax {
		t.Errorf("%d reports kept", n)
	}
	files, _ := os.ReadDir(crashReportsDir)
	if len(files) != crashReportsMax {
		t.Errorf("%d files on disk", len(files))
	}
}
