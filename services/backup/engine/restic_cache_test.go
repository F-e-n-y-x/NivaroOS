package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// restic's cache is trimmed to its cap, least recently used repository
// first, but only once no restic repository is open.
func TestResticCacheTrimmedWhenIdle(t *testing.T) {
	t.Setenv("NIVAROOS_RESTIC", "/bin/true") // openRepo runs nothing here
	dir := t.TempDir()
	old := resticCacheMax
	resticCacheMax = 150
	t.Cleanup(func() { resticCacheMax = old })
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		must(t, os.MkdirAll(filepath.Join(p, "index"), 0o700))
		must(t, os.WriteFile(filepath.Join(p, "index", "ab"), make([]byte, 100), 0o600))
		ts := time.Now().Add(-age)
		must(t, os.Chtimes(p, ts, ts))
		return p
	}
	stale, recent := mk("aaaa", 2*time.Hour), mk("bbbb", time.Minute)
	must(t, os.WriteFile(filepath.Join(dir, "CACHEDIR.TAG"), []byte("x"), 0o600))
	e := &Engine{cfg: Config{ResticCacheDir: dir, Logf: t.Logf}}

	r1, err := e.openRepo(context.Background(), nil, nil, nil)
	must(t, err)
	r2, err := e.openRepo(context.Background(), nil, nil, nil)
	must(t, err)
	r1.close()
	r1.close() // twice: counted once
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("trimmed while a repository was open: %v", err)
	}
	r2.close()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("least recently used cache kept: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent cache removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CACHEDIR.TAG")); err != nil {
		t.Errorf("CACHEDIR.TAG removed: %v", err)
	}
}
