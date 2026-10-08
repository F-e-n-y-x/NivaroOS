package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
)

// The web UI as the gateway serves it: index at /, no-cache on HTML,
// asset names with '+', 404 for missing files and dot-segment escapes.
func TestStaticRoute(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ui</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "img", "application-epub+zip.1.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("x"), 0o644)
	defer os.Remove(filepath.Join(filepath.Dir(dir), "secret.txt"))

	state := service.NewState()
	state.SetWWWPath(dir)
	h := NewStaticRoute(state).GetRoute()

	for path, want := range map[string]int{
		"/":                               200,
		"/index.html":                     200,
		"/img/application-epub+zip.1.svg": 200,
		"/nope.js":                        404,
		"/img/../../secret.txt":           404,
		"/img/%2e%2e/%2e%2e/secret.txt":   404,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("%s: %d want %d", path, w.Code, want)
		}
		if want == 200 && (path == "/" || path == "/index.html") && w.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
			t.Errorf("%s: Cache-Control %q", path, w.Header().Get("Cache-Control"))
		}
	}
}
