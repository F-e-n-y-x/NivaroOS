package v1

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// `\s` in the old pattern matched newlines, so one "source" could write
// several lines (any APT option, or a [trusted=yes] repo) into sources.
func TestSourceLineIsExactlyOneLine(t *testing.T) {
	good := []string{
		"deb http://deb.debian.org/debian trixie main contrib",
		"deb-src [arch=amd64 signed-by=/usr/share/keyrings/x.gpg] https://example.org/apt stable main",
	}
	bad := []string{
		"deb http://a b main\nAPT::Foo bar",
		"deb http://a b main\r\ndeb [trusted=yes] http://evil x main",
		"deb [trusted=yes\n] http://a b main",
		"rm -rf /",
	}
	for _, l := range good {
		if !validSourceLine.MatchString(l) {
			t.Errorf("rejected %q", l)
		}
	}
	for _, l := range bad {
		if validSourceLine.MatchString(l) {
			t.Errorf("accepted %q", l)
		}
	}
}

// The list returns full paths; deleting one of them was always refused.
func TestSourceFileAcceptsTheListedPathsOnly(t *testing.T) {
	ok := map[string]string{
		"/etc/apt/sources.list.d/docker.list": "/etc/apt/sources.list.d/docker.list",
		"docker.list":                         "/etc/apt/sources.list.d/docker.list",
		"/etc/apt/sources.list":               "/etc/apt/sources.list",
		"":                                    "/etc/apt/sources.list",
	}
	for in, want := range ok {
		got, err := resolveSourceFile(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"/etc/passwd", "../../etc/passwd", "/etc/apt/sources.list.d/../../passwd", "/etc/apt/sources.list.d/sub/x.list"} {
		if got, err := resolveSourceFile(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

// Search kept the first 100 hits in apt-cache's order and sorted after:
// "python3" returned 100 of 5,403 results without python3 itself.
func TestSearchRanksExactAndPrefixMatchesFirst(t *testing.T) {
	var hits []aptPackageInfo
	for i := 0; i < 300; i++ {
		hits = append(hits, aptPackageInfo{Name: fmt.Sprintf("lib-python3-thing%03d", i)})
	}
	hits = append(hits, aptPackageInfo{Name: "python3-pip"}, aptPackageInfo{Name: "python3"})
	got := rankSearch(hits, "python3", 100)
	if len(got) != 100 || got[0].Name != "python3" || got[1].Name != "python3-pip" {
		t.Fatalf("got %d results, first %q %q", len(got), got[0].Name, got[1].Name)
	}
}

// deb822 ".sources" files (Debian 13's default) weren't listed at all.
func TestDeb822SourcesAreListed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "google-chrome.sources")
	os.WriteFile(p, []byte("Types: deb\nURIs: https://dl.google.com/linux/chrome/deb/\nSuites: stable\nComponents: main\nSigned-By: /x.gpg\n\nTypes: deb deb-src\nURIs: http://a http://b\nSuites: trixie\nComponents: main contrib\nEnabled: no\n"), 0o644)
	got := parseDeb822Sources(p)
	if len(got) != 1 || got[0].URI != "https://dl.google.com/linux/chrome/deb/" || got[0].Suite != "stable" || !got[0].ReadOnly {
		t.Fatalf("got %+v", got)
	}
}

// Uninstall named one package, but apt-get remove can take dependents with
// it (removing containerd removes docker-ce). The preview parses apt's
// simulation.
func TestRemovalPreviewListsEveryPackageThatGoes(t *testing.T) {
	sim := "NOTE: This is only a simulation!\nReading package lists...\nThe following packages will be REMOVED:\n  containerd.io docker-ce\nRemv docker-ce [5:29.8.1-1~debian.13~trixie]\nRemv containerd.io [2.3.5-1~debian.13~trixie]\n"
	got := parseRemovalSimulation(sim)
	if len(got) != 2 || got[0] != "docker-ce" || got[1] != "containerd.io" {
		t.Fatalf("got %v", got)
	}
}
