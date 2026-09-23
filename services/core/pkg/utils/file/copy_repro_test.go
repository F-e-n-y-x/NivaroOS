package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Repro: one file in a folder can't be written at the destination (a
// directory already sits at its path). CopyDirCtx must report that.
func TestCopyDirCtxReportsPerFileFailures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "album")
	dst := t.TempDir()
	_ = os.MkdirAll(src, 0o755)
	for _, n := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		_ = os.WriteFile(filepath.Join(src, n), []byte("data-"+n), 0o644)
	}
	// Block b.jpg at the destination.
	_ = os.MkdirAll(filepath.Join(dst, "album", "b.jpg", "x"), 0o755)

	err := CopyDirCtx(context.Background(), src, dst, "overwrite")
	if err == nil {
		t.Fatalf("CopyDirCtx returned success although b.jpg could not be copied")
	}
}
