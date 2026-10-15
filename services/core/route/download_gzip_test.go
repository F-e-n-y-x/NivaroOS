package route

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Downloads must keep their Content-Length and raw Range bytes (resume).
func TestDownloadsAreNotGzipped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(p, []byte(strings.Repeat("compressible ", 10000)), 0o644)
	h := InitV1Router()
	for _, rng := range []string{"", "bytes=100-"} {
		req := httptest.NewRequest("GET", "/v1/file?path="+p, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Accept-Encoding", "gzip")
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code/100 != 2 || rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Content-Length") == "" {
			t.Errorf("range %q: status %d, encoding %q, length %q", rng, rec.Code, rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Length"))
		}
	}
}
