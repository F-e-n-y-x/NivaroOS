package httper

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nivaroos-local-storage is the only owner of cloud mounts. Core asking
// the rclone daemon to mount (or unmount) remotes as well put two mounts on
// one /mnt/<remote> path, and the older one closing unmounted the newer
// (2026-09-30 05:08). Keep those rc calls out of core.
func TestCoreNeverMountsThroughTheDaemon(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "build") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, call := range []string{`"/mount/mount"`, `"mount/mount"`, `"/mount/unmount"`, `"mount/unmount"`, `"/mount/unmountall"`} {
			if strings.Contains(string(raw), call) {
				t.Errorf("%s calls the rclone daemon's %s: mount through nivaroos-local-storage instead", p, call)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
