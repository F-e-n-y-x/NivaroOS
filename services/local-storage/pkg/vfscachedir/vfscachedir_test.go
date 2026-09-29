package vfscachedir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rclone/rclone/fs/config"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A pending upload in the old /tmp cache moves to disk with its metadata,
// and rclone is pointed at the new place.
func TestSetupMovesLegacyCacheAndSetsDir(t *testing.T) {
	old, dir := t.TempDir(), filepath.Join(t.TempDir(), "rclone")
	write(t, filepath.Join(old, "vfs", "terabox_x", "Linux.iso"), "iso bytes")
	write(t, filepath.Join(old, "vfsMeta", "terabox_x", "Linux.iso"), `{"Dirty":true}`)
	moved, err := Setup(dir, []string{old})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved %v", moved)
	}
	if read(t, filepath.Join(dir, "vfs", "terabox_x", "Linux.iso")) != "iso bytes" ||
		read(t, filepath.Join(dir, "vfsMeta", "terabox_x", "Linux.iso")) != `{"Dirty":true}` {
		t.Fatal("contents not carried over")
	}
	if _, err := os.Stat(filepath.Join(old, "vfs", "terabox_x")); !os.IsNotExist(err) {
		t.Fatal("old copy still there")
	}
	if config.GetCacheDir() != dir {
		t.Fatalf("cache dir = %s", config.GetCacheDir())
	}
}

// A remote already cached in the new place is never overwritten.
func TestSetupKeepsWhatIsAlreadyThere(t *testing.T) {
	old, dir := t.TempDir(), filepath.Join(t.TempDir(), "rclone")
	write(t, filepath.Join(old, "vfs", "drive", "a"), "old")
	write(t, filepath.Join(dir, "vfs", "drive", "a"), "new")
	if _, err := Setup(dir, []string{old, old, dir}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "vfs", "drive", "a")) != "new" || read(t, filepath.Join(old, "vfs", "drive", "a")) != "old" {
		t.Fatal("overwrote or dropped a cache")
	}
}

// The cross-filesystem path: copy, then remove the source.
func TestMoveByCopy(t *testing.T) {
	src, dst := filepath.Join(t.TempDir(), "s"), filepath.Join(t.TempDir(), "d")
	write(t, filepath.Join(src, "sub", "f"), "x")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "sub", "f")) != "x" {
		t.Fatal("copy failed")
	}
}
