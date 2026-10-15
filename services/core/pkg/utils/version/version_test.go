package version

import (
	"net/http"
	"net/http/httptest"
	"os"
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

func TestReleaseFile(t *testing.T) {
	old := ReleaseFile
	t.Cleanup(func() { ReleaseFile = old })
	ReleaseFile = t.TempDir() + "/release.json"
	if b := Info(); b.Release {
		t.Fatalf("no file: %+v", b)
	}
	os.WriteFile(ReleaseFile, []byte(`{"version":"1.5.0","channel":"beta","commit":"abcdef0123","built":"2026-10-20T00:00:00Z"}`), 0o644)
	if b := Info(); !b.Release || b.Version != "1.5.0" || b.Channel != "beta" || b.Short != "abcdef0" || b.Date != "2026-10-20T00:00:00Z" {
		t.Fatalf("release.json: %+v", b)
	}
}

func TestLatestRelease(t *testing.T) {
	rels := `[
		{"tag_name":"v1.4.16","body":"android"},
		{"tag_name":"mobile-v9.0.0"},
		{"tag_name":"server-v1.6.0-beta.1","prerelease":true,"body":"beta"},
		{"tag_name":"server-v1.5.1","body":"fixes"},
		{"tag_name":"server-v1.5.0"},
		{"tag_name":"server-v2.0.0","draft":true}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(rels)) }))
	t.Cleanup(srv.Close)
	old := GitHubAPI
	GitHubAPI = srv.URL
	t.Cleanup(func() { GitHubAPI = old })

	if s, found := LatestRelease("1.5.0", false); !found || s.State != StateBehind || s.LatestVersion != "1.5.1" || s.LatestMessage != "fixes" {
		t.Fatalf("stable: %+v", s)
	}
	if s, _ := LatestRelease("1.5.1", false); s.State != StateUpToDate {
		t.Fatalf("current: %+v", s)
	}
	if s, _ := LatestRelease("1.5.1", true); s.State != StateBehind || s.LatestVersion != "1.6.0-beta.1" {
		t.Fatalf("beta: %+v", s)
	}
	rels = `[{"tag_name":"v1.4.16"}]` // only Android releases: not found, the caller compares commits
	if _, found := LatestRelease("1.5.0", false); found {
		t.Fatal("android release taken for a server release")
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.5.1", "1.5.0", true}, {"1.10.0", "1.9.9", true}, {"1.5.0", "1.5.0", false}, {"1.6.0", "1.6.0-beta.1", true}, {"1.6.0-beta.1", "1.6.0", false}, {"1.6.0-beta.2", "1.6.0-beta.1", true}, {"1.4.0", "1.5.0", false}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
