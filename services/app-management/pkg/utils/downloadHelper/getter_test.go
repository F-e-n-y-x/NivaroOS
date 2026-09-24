package downloadHelper

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		assert.NilError(t, err)
		_, err = f.Write([]byte(content))
		assert.NilError(t, err)
	}
	assert.NilError(t, w.Close())

	return buf.Bytes()
}

func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server
}

func TestDownload(t *testing.T) {
	server := serve(t, zipOf(t, map[string]string{
		"store-main/Apps/demo/docker-compose.yml": "name: demo\n",
		"store-main/category-list.json":           "[]",
	}))

	dst := filepath.Join(t.TempDir(), "out")
	assert.NilError(t, Download(server.URL+"/main.zip", dst))

	content, err := os.ReadFile(filepath.Join(dst, "store-main", "Apps", "demo", "docker-compose.yml"))
	assert.NilError(t, err)
	assert.Equal(t, string(content), "name: demo\n")
}

func TestDownloadRejectsZipSlip(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/etc/evil.txt"} {
		server := serve(t, zipOf(t, map[string]string{name: "x"}))

		root := t.TempDir()
		dst := filepath.Join(root, "out")
		err := Download(server.URL+"/main.zip", dst)
		assert.Assert(t, errors.Is(err, ErrIllegalPath), "%s: %v", name, err)

		_, statErr := os.Stat(filepath.Join(root, "evil.txt"))
		assert.Assert(t, os.IsNotExist(statErr), name)
	}
}

func TestDownloadLimits(t *testing.T) {
	oldFiles, oldSize := MaxFiles, MaxExtractedSize
	defer func() { MaxFiles, MaxExtractedSize = oldFiles, oldSize }()

	MaxFiles = 2
	server := serve(t, zipOf(t, map[string]string{"a": "1", "b": "2", "c": "3"}))
	err := Download(server.URL+"/main.zip", t.TempDir())
	assert.Assert(t, errors.Is(err, ErrTooManyFiles), err)

	MaxFiles = 100
	MaxExtractedSize = 10
	server = serve(t, zipOf(t, map[string]string{"big": "0123456789abcdef"}))
	err = Download(server.URL+"/main.zip", t.TempDir())
	assert.Assert(t, errors.Is(err, ErrTooLarge), err)
}

func TestDownloadOnlyHTTP(t *testing.T) {
	for _, src := range []string{"file:///etc/passwd", "git::https://example.com/x.git", "s3::https://bucket/x.zip", "/tmp/x.zip", "https://"} {
		assert.Assert(t, ValidateURL(src) != nil, src)
		assert.Assert(t, Download(src, t.TempDir()) != nil, src)
	}

	assert.NilError(t, ValidateURL("https://github.com/a/b/archive/refs/heads/main.zip"))

	// a redirect to a non-http scheme is refused
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer server.Close()
	assert.Assert(t, Download(server.URL+"/main.zip", t.TempDir()) != nil)
}

func TestDownloadHonoursContext(t *testing.T) {
	server := serve(t, zipOf(t, map[string]string{"a": "1"}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Assert(t, DownloadContext(ctx, server.URL+"/main.zip", t.TempDir()) != nil)
}
