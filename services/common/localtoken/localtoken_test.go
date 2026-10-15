package localtoken

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func withTempPath(t *testing.T) string {
	t.Helper()
	old := Path
	Path = filepath.Join(t.TempDir(), "run", "local-automation.token")
	t.Cleanup(func() { Path = old })
	return Path
}

func TestCreatedOnceOwnerOnly(t *testing.T) {
	path := withTempPath(t)
	var wg sync.WaitGroup
	got := make([]string, 8)
	for i := range got {
		wg.Add(1)
		go func(i int) { defer wg.Done(); got[i] = Get() }(i)
	}
	wg.Wait()
	for _, g := range got {
		if len(g) != 64 || g != got[0] {
			t.Fatalf("concurrent LocalToken calls disagree or are malformed: %q", got)
		}
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v; want 0600", fi.Mode(), err)
	}

	// A token anybody else could read is no token.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if Read() != "" {
		t.Error("a group/world-readable token file was accepted")
	}
}

func TestAddLoopbackOnly(t *testing.T) {
	withTempPath(t)
	for url, want := range map[string]bool{
		"http://127.0.0.1:1/x":   true,
		"http://localhost:1/x":   true,
		"http://[::1]:1/x":       true,
		"http://192.168.1.2:1/x": false,
		"https://example.com/x":  false,
	} {
		r := httptest.NewRequest(http.MethodGet, url, nil)
		Add(r)
		if got := r.Header.Get(Header) != ""; got != want {
			t.Errorf("%s: token sent = %v, want %v", url, got, want)
		}
	}
}
