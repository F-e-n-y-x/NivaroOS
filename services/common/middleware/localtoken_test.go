package middleware

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

func withTokenPaths(t *testing.T) (tokenPath, strictPath string) {
	t.Helper()
	dir := t.TempDir()
	oldT, oldS := localtoken.Path, LocalStrictPath
	localtoken.Path, LocalStrictPath = filepath.Join(dir, "run", "local-automation.token"), filepath.Join(dir, "strict")
	t.Cleanup(func() { localtoken.Path, LocalStrictPath = oldT, oldS })
	return localtoken.Path, LocalStrictPath
}

func TestLocalTokenAuth(t *testing.T) {
	_, strict := withTokenPaths(t)
	tok := localtoken.Get()
	if tok == "" {
		t.Fatal("no token")
	}
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    bool
	}{
		{"token direct", "127.0.0.1:5555", map[string]string{LocalTokenHeader: tok}, true},
		{"token via gateway", "127.0.0.1:5555", map[string]string{LocalTokenHeader: tok, "X-Forwarded-For": "127.0.0.1"}, true},
		{"wrong token", "127.0.0.1:5555", map[string]string{LocalTokenHeader: "x" + tok[1:], "X-Forwarded-For": "127.0.0.1"}, false},
		{"token from a browser", "127.0.0.1:5555", map[string]string{LocalTokenHeader: tok, "Origin": "http://x"}, false},
		{"token from the LAN", "192.168.1.20:5555", map[string]string{LocalTokenHeader: tok}, false},
		// The H1 hole: legacy loopback only while not strict.
		{"no token, plain loopback (legacy)", "127.0.0.1:5555", nil, true},
	}
	for _, c := range cases {
		if got := IsLocalAutomation(req(c.remote, c.headers)); got != c.want {
			t.Errorf("%s: IsLocalAutomation = %v, want %v", c.name, got, c.want)
		}
	}

	if err := os.WriteFile(strict, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, h := range []map[string]string{nil, {"X-Forwarded-For": "127.0.0.1", LocalAutomationHeader: "1"}} {
		if IsLocalAutomation(req("127.0.0.1:5555", h)) {
			t.Errorf("strict: loopback without token %v still trusted", h)
		}
	}
	if !IsLocalAutomation(req("127.0.0.1:5555", map[string]string{LocalTokenHeader: tok})) {
		t.Error("strict: token no longer trusted")
	}
}
