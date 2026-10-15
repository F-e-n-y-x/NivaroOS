// Package version: which build of NivaroOS is running, and whether GitHub
// master has moved past it.
//
// The server is built from master (installer, nivaroos-deploy), so its
// identity is a commit, not a release number - the GitHub releases (v1.x)
// are the Android app. "Update available" means master has commits this
// build doesn't, asked of GitHub's compare API by core, cached; any failure
// is "couldn't check", never "update available".
package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// Set at build time (installer/install.sh):
//
//	-X github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/version.Commit=<full sha>
//	-X github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/version.Date=<commit date, RFC 3339>
//
// Without them a build from a git checkout still knows itself through Go's
// embedded VCS info (vcs.revision / vcs.time).
var (
	Commit  string
	Date    string
	Version string
)

type Build struct {
	Commit   string `json:"commit"` // full sha, "" when unknown
	Short    string `json:"short"`
	Date     string `json:"date"` // commit date, RFC 3339
	Version  string `json:"version"`
	Modified bool   `json:"modified,omitempty"` // built from a dirty tree
}

// Info is this binary's build identity.
func Info() Build {
	bi, ok := debug.ReadBuildInfo()
	return info(Commit, Date, Version, bi, ok)
}

func info(commit, date, ver string, bi *debug.BuildInfo, ok bool) Build {
	b := Build{Commit: commit, Date: date}
	if b.Commit == "" && ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Commit = s.Value
			case "vcs.time":
				b.Date = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
	}
	b.Short = b.Commit
	if len(b.Short) > 7 {
		b.Short = b.Short[:7]
	}
	b.Version = ver
	if b.Version == "" {
		b.Version = "dev"
		if b.Short != "" {
			b.Version = b.Short
			if t, err := time.Parse(time.RFC3339, b.Date); err == nil {
				b.Version = t.Format("2006.01.02") + "-" + b.Short
			}
		}
	}
	return b
}

const (
	StateUpToDate = "up_to_date"
	StateBehind   = "behind"
	StateUnknown  = "unknown"
)

type Status struct {
	State         string    `json:"state"`
	AheadBy       int       `json:"ahead_by"` // commits on master this build doesn't have
	LatestCommit  string    `json:"latest_commit,omitempty"`
	LatestMessage string    `json:"latest_message,omitempty"`
	LatestDate    string    `json:"latest_date,omitempty"`
	Reason        string    `json:"reason,omitempty"` // why it's unknown
	CheckedAt     time.Time `json:"checked_at"`
}

// GitHubAPI is swapped by tests.
var GitHubAPI = "https://api.github.com/repos/F-e-n-y-x/NivaroOS"

var client = &http.Client{Timeout: 10 * time.Second}

// Compare asks GitHub how far master is ahead of commit.
func Compare(commit string) Status {
	s := Status{State: StateUnknown, CheckedAt: time.Now()}
	if commit == "" {
		s.Reason = "this build doesn't know its commit"
		return s
	}
	req, _ := http.NewRequest(http.MethodGet, GitHubAPI+"/compare/"+commit+"...master", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		s.Reason = "GitHub is unreachable"
		return s
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		s.Reason = "this build's commit isn't on GitHub"
		return s
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		s.Reason = "GitHub rate limit, try again later"
		return s
	case res.StatusCode != http.StatusOK:
		s.Reason = fmt.Sprintf("GitHub answered %d", res.StatusCode)
		return s
	}
	var body struct {
		Status  string `json:"status"`
		AheadBy *int   `json:"ahead_by"`
		Commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message   string `json:"message"`
				Committer struct {
					Date string `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.AheadBy == nil || body.Status == "" {
		s.Reason = "unexpected answer from GitHub"
		return s
	}
	s.AheadBy = *body.AheadBy
	if s.AheadBy == 0 {
		s.State = StateUpToDate
		return s
	}
	s.State = StateBehind
	if n := len(body.Commits); n > 0 {
		last := body.Commits[n-1]
		s.LatestCommit, s.LatestMessage, s.LatestDate = last.SHA, last.Commit.Message, last.Commit.Committer.Date
	}
	return s
}

var (
	mu     sync.Mutex
	cached Status
	until  time.Time
)

// Check is Compare for this build, cached (GitHub allows 60 unauthenticated
// calls an hour): 30 minutes after an answer, 5 after a failure. refresh
// skips the cache, but not more than once a minute.
func Check(refresh bool) Status {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	if now.Before(until) && !(refresh && now.Sub(cached.CheckedAt) > time.Minute) {
		return cached
	}
	cached = Compare(Info().Commit)
	ttl := 30 * time.Minute
	if cached.State == StateUnknown {
		ttl = 5 * time.Minute
	}
	until = now.Add(ttl)
	return cached
}
