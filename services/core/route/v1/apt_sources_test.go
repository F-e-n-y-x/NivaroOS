package v1

import "testing"

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
