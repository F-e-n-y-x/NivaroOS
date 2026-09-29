// Package vfscachedir puts rclone's VFS cache for cloud mounts on disk.
//
// local-storage runs without a HOME, so rclone fell back to
// os.TempDir()/rclone - /tmp/rclone, which is RAM (tmpfs) on most
// installs. Every cached read and every upload still waiting sat in
// memory; one upload that could never succeed (a 5.9 GB file over
// TeraBox's 4 GB limit) held 5.6 GB of RAM for good.
//
// Setup points rclone at Dir (next to the rclone daemon's cache) and
// moves anything left in the old places over first, before any mount
// starts, so uploads still waiting are kept and carry on from disk.
package vfscachedir

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rclone/rclone/fs/config"
)

// Progress, when set, is called while a cache is copied between
// filesystems, so a long first move (gigabytes out of /tmp) can keep the
// service's start from timing out.
var Progress func()

// Dir is where cloud mounts cache: on disk, one place for every mount.
const Dir = "/var/cache/nivaroos/rclone"

// Legacy lists cache roots earlier versions used.
func Legacy() []string {
	dirs := []string{filepath.Join(os.TempDir(), "rclone"), "/tmp/rclone", "/root/.cache/rclone"}
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		dirs = append(dirs, filepath.Join(d, "rclone"))
	}
	return dirs
}

// Setup creates dir, moves the legacy caches into it and makes it
// rclone's cache directory. It returns what it moved, for the log.
func Setup(dir string, legacy []string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var moved []string
	seen := map[string]bool{}
	for _, old := range legacy {
		old = filepath.Clean(old)
		if seen[old] || old == filepath.Clean(dir) {
			continue
		}
		seen[old] = true
		m, err := migrate(old, dir)
		moved = append(moved, m...)
		if err != nil {
			return moved, err
		}
	}
	return moved, config.SetCacheDir(dir)
}

// migrate moves old/vfs/<remote> and old/vfsMeta/<remote> into dir.
// A remote already present in dir is left alone in both places (dir
// wins; nothing is overwritten).
func migrate(old, dir string) ([]string, error) {
	var moved []string
	for _, kind := range []string{"vfs", "vfsMeta"} {
		entries, err := os.ReadDir(filepath.Join(old, kind))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			src := filepath.Join(old, kind, e.Name())
			dst := filepath.Join(dir, kind, e.Name())
			if _, err := os.Lstat(dst); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return moved, err
			}
			if err := move(src, dst); err != nil {
				return moved, fmt.Errorf("moving %s: %w", src, err)
			}
			moved = append(moved, src)
		}
	}
	return moved, nil
}

// move renames, or copies then removes when src is on another
// filesystem (tmpfs to disk). A half-finished copy is removed so a retry
// starts clean, and src is only removed once the copy is complete.
func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	tmp := dst + ".moving"
	_ = os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o700)
		case info.Mode().IsRegular():
			if Progress != nil {
				Progress()
			}
			return copyFile(p, target, info)
		default:
			return nil // rclone keeps only files and dirs here
		}
	})
}

func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, progressReader{in}); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// progressReader calls Progress about every 256 MiB copied.
type progressReader struct{ r io.Reader }

var sinceProgress int64

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	sinceProgress += int64(n)
	if sinceProgress >= 256<<20 && Progress != nil {
		sinceProgress = 0
		Progress()
	}
	return n, err
}
