package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// compareVersions orders X.Y.Z[-pre] versions (semver precedence: a
// pre-release sorts before its release). Unparseable versions sort first.
func compareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return boolCmp(oka, okb)
	}
	for i := 0; i < 3; i++ {
		if pa.n[i] != pb.n[i] {
			return intCmp(pa.n[i], pb.n[i])
		}
	}
	if pa.pre == "" || pb.pre == "" {
		return boolCmp(pa.pre == "", pb.pre == "")
	}
	x, y := strings.Split(pa.pre, "."), strings.Split(pb.pre, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		nx, ex := strconv.Atoi(x[i])
		ny, ey := strconv.Atoi(y[i])
		if ex == nil && ey == nil {
			return intCmp(nx, ny)
		}
		if (ex == nil) != (ey == nil) {
			return boolCmp(ex != nil, ey != nil) // numeric identifiers sort first
		}
		return strings.Compare(x[i], y[i])
	}
	return intCmp(len(x), len(y))
}

type version struct {
	n   [3]int
	pre string
}

func parseVersion(s string) (version, bool) {
	var v version
	s = strings.TrimPrefix(s, "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.n[i] = n
	}
	v.pre = pre
	return v, true
}

func intCmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func boolCmp(a, b bool) int { // false < true
	switch {
	case a == b:
		return 0
	case b:
		return -1
	}
	return 1
}

// GitHub's release list (only the fields used).
type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Body       string    `json:"body"`
	Published  string    `json:"published_at"`
	Assets     []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Available is a server release this box could install.
type Available struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Channel     string `json:"channel"`
	Notes       string `json:"notes_md,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Size        int64  `json:"size"`
	TarName     string `json:"tar_name"`
	TarURL      string `json:"tar_url"`
	SumsURL     string `json:"sums_url"`
	SigURL      string `json:"sig_url"`
}

// pickRelease is the newest server-vX.Y.Z release newer than current for
// this channel and arch (beta boxes also take stable releases), skipping
// drafts and bad versions. current "" (a from-source install) takes any.
func pickRelease(list []ghRelease, channel, arch, current string, bad []string) *Available {
	var best *Available
	for _, r := range list {
		ver, ok := strings.CutPrefix(r.Tag, "server-v")
		if !ok || r.Draft || (r.Prerelease && channel != "beta") {
			continue
		}
		if _, ok := parseVersion(ver); !ok || slices.Contains(bad, ver) {
			continue
		}
		if current != "" && compareVersions(ver, current) <= 0 {
			continue
		}
		if best != nil && compareVersions(ver, best.Version) <= 0 {
			continue
		}
		a := &Available{Version: ver, Tag: r.Tag, Channel: "stable", Notes: r.Body, PublishedAt: r.Published, TarName: TarName(ver, arch)}
		if r.Prerelease {
			a.Channel = "beta"
		}
		for _, as := range r.Assets {
			switch as.Name {
			case a.TarName:
				a.TarURL, a.Size = as.URL, as.Size
			case "SHA256SUMS":
				a.SumsURL = as.URL
			case "SHA256SUMS.minisig":
				a.SigURL = as.URL
			}
		}
		if a.TarURL != "" && a.SumsURL != "" && a.SigURL != "" {
			best = a
		}
	}
	return best
}

// ReleasesAPI is swapped by tests.
var ReleasesAPI = "https://api.github.com/repos/F-e-n-y-x/NivaroOS/releases?per_page=30"

func fetchReleases(ctx context.Context, c *http.Client) ([]ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", res.Status)
	}
	var list []ghRelease
	return list, json.NewDecoder(res.Body).Decode(&list)
}
