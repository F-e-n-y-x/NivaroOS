package httper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheVFSOpt(t *testing.T) {
	def := `{"CacheMode": 3, "CacheMaxSize": 21474836480, "CacheMaxAge": 3600000000000, "WriteBack": 5000000000}`
	if got := CacheVFSOpt(filepath.Join(t.TempDir(), "missing.conf")); got != def {
		t.Fatalf("defaults: %s", got)
	}
	p := filepath.Join(t.TempDir(), "ls.conf")
	os.WriteFile(p, []byte("[cloud_cache]\nMode = writes\nMaxSize = 1073741824\nMaxAge = 600\nDir = /DATA/c\n"), 0o644)
	want := `{"CacheMode": 2, "CacheMaxSize": 1073741824, "CacheMaxAge": 600000000000, "WriteBack": 5000000000}`
	if got := CacheVFSOpt(p); got != want {
		t.Fatalf("got %s", got)
	}
}
