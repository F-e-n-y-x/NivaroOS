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

// Cache-Control per file kind, and build-time .br/.gz served as-is.
func TestStaticCachingAndPrecompressed(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "js"), 0o755)
	os.MkdirAll(filepath.Join(dir, "css"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ui</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.87b43194.js"), []byte("console.log(1)"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.87b43194.js.br"), []byte("BR"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.87b43194.js.gz"), []byte("GZ"), 0o644)
	os.WriteFile(filepath.Join(dir, "js", "i18n.57a6e0b7.js"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "css", "custom.css"), []byte("body{}"), 0o644)

	state := service.NewState()
	state.SetWWWPath(dir)
	h := NewStaticRoute(state).GetRoute()
	get := func(path, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			r.Header.Set("Accept-Encoding", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	const immutable = "public, max-age=31536000, immutable"
	for path, want := range map[string]string{
		"/":                    "no-cache, no-store, must-revalidate",
		"/index.html":          "no-cache, no-store, must-revalidate",
		"/app.87b43194.js":     immutable,
		"/js/i18n.57a6e0b7.js": immutable,
		"/css/custom.css":      "no-cache",
		"/js/gone.12345678.js": "",
	} {
		if got := get(path, "").Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", path, got, want)
		}
	}

	for accept, want := range map[string]string{"gzip, deflate, br": "BR", "gzip": "GZ"} {
		w := get("/app.87b43194.js", accept)
		if w.Body.String() != want || w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" ||
			w.Header().Get("Cache-Control") != immutable {
			t.Errorf("%q: body %q enc %q type %q cc %q", accept, w.Body.String(), w.Header().Get("Content-Encoding"), w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
		}
	}
	if w := get("/app.87b43194.js", ""); w.Body.String() != "console.log(1)" || w.Header().Get("Content-Encoding") != "" {
		t.Errorf("identity: %q %q", w.Body.String(), w.Header().Get("Content-Encoding"))
	}
	// No .gz on disk: still gzipped on the fly.
	if w := get("/css/custom.css", "gzip"); w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("fallback gzip: %v", w.Header())
	}
}

// L6: no anti-framing header - any site could frame the UI.
func TestStaticRouteRefusesFraming(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ui</html>"), 0o644)
	state := service.NewState()
	state.SetWWWPath(dir)
	w := httptest.NewRecorder()
	NewStaticRoute(state).GetRoute().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Header().Get("X-Frame-Options") != "SAMEORIGIN" || w.Header().Get("Content-Security-Policy") != "frame-ancestors 'self'" {
		t.Fatalf("headers %v", w.Header())
	}
}
