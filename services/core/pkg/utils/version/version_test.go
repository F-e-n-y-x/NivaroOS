package version

import (
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"testing"
)

func TestInfo(t *testing.T) {
	vcs := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc1234def5678"},
		{Key: "vcs.time", Value: "2026-10-16T09:30:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	if b := info("", "", "", vcs, true); b.Commit != "abc1234def5678" || b.Short != "abc1234" || b.Version != "2026.10.16-abc1234" || !b.Modified {
		t.Fatalf("vcs fallback: %+v", b)
	}
	if b := info("0123456789", "2026-01-02T00:00:00Z", "", vcs, true); b.Commit != "0123456789" || b.Date != "2026-01-02T00:00:00Z" || b.Version != "2026.01.02-0123456" {
		t.Fatalf("ldflags win: %+v", b)
	}
	if b := info("", "", "", nil, false); b.Commit != "" || b.Version != "dev" {
		t.Fatalf("no info: %+v", b)
	}
}

func serve(t *testing.T, code int, body string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compare/abc...master" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(code)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := GitHubAPI
	GitHubAPI = srv.URL
	t.Cleanup(func() { GitHubAPI = old })
}

func TestCompare(t *testing.T) {
	serve(t, 200, `{"status":"ahead","ahead_by":3,"commits":[{"sha":"s1","commit":{"message":"one"}},{"sha":"s3","commit":{"message":"three","committer":{"date":"2026-10-16T10:00:00Z"}}}]}`)
	s := Compare("abc")
	if s.State != StateBehind || s.AheadBy != 3 || s.LatestCommit != "s3" || s.LatestMessage != "three" || s.LatestDate == "" {
		t.Fatalf("behind: %+v", s)
	}

	serve(t, 200, `{"status":"identical","ahead_by":0,"commits":[]}`)
	if s := Compare("abc"); s.State != StateUpToDate {
		t.Fatalf("identical: %+v", s)
	}
	// Built from a commit master doesn't have yet.
	serve(t, 200, `{"status":"behind","ahead_by":0,"behind_by":2}`)
	if s := Compare("abc"); s.State != StateUpToDate {
		t.Fatalf("local ahead: %+v", s)
	}
}

// Every failure is "unknown" - never a false "update available".
func TestCompareFailures(t *testing.T) {
	for name, c := range map[string]struct {
		code int
		body string
	}{
		"rate limited":  {403, `{"message":"API rate limit exceeded"}`},
		"429":           {429, ``},
		"not on github": {404, `{"message":"Not Found"}`},
		"server error":  {502, ``},
		"garbage":       {200, `<html>`},
		"no ahead_by":   {200, `{"message":"ok"}`},
	} {
		serve(t, c.code, c.body)
		if s := Compare("abc"); s.State != StateUnknown || s.Reason == "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	GitHubAPI = "http://127.0.0.1:1" // nothing listens
	if s := Compare("abc"); s.State != StateUnknown {
		t.Errorf("unreachable: %+v", s)
	}
	if s := Compare(""); s.State != StateUnknown {
		t.Errorf("no commit: %+v", s)
	}
}
