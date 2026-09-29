package cloudcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// Usage is one account's footprint in a cache directory.
type Usage struct {
	// UsedBytes is the disk space the cached data really takes (cache files
	// are sparse: only the parts read are stored).
	UsedBytes int64 `json:"used_bytes"`
	Files     int   `json:"files"`
	// PendingFiles/PendingBytes: written here, not uploaded yet.
	PendingFiles int   `json:"pending_files"`
	PendingBytes int64 `json:"pending_bytes"`
}

type itemMeta struct {
	Size  int64 `json:"Size"`
	Dirty bool  `json:"Dirty"`
}

// readMeta parses one vfsMeta file. ok=false means it couldn't be read or
// parsed - callers must then treat the item as possibly pending.
func readMeta(path string) (itemMeta, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return itemMeta{}, false
	}
	var m itemMeta
	if json.Unmarshal(raw, &m) != nil {
		return itemMeta{}, false
	}
	return m, true
}

// DataRoot / MetaRoot are where rclone keeps remote's cache under dir.
func DataRoot(dir, remote string) string { return filepath.Join(dir, "vfs", remote) }
func MetaRoot(dir, remote string) string { return filepath.Join(dir, "vfsMeta", remote) }

func diskBytes(fi fs.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}

// walkFiles calls fn for every regular file under root with its path
// relative to root. A missing root is not an error.
func walkFiles(root string, fn func(rel, full string, d fs.DirEntry) error) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		return fn(rel, p, d)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Remotes lists the remotes that have anything in the cache dir.
func Remotes(dir string) []string {
	seen := map[string]bool{}
	for _, sub := range []string{"vfs", "vfsMeta"} {
		ents, _ := os.ReadDir(filepath.Join(dir, sub))
		for _, e := range ents {
			if e.IsDir() {
				seen[e.Name()] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Scan measures remote's cache in dir.
func Scan(dir, remote string) (Usage, error) {
	var u Usage
	err := walkFiles(DataRoot(dir, remote), func(_, _ string, d fs.DirEntry) error {
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		u.UsedBytes += diskBytes(fi)
		u.Files++
		return nil
	})
	if err != nil {
		return u, err
	}
	err = walkFiles(MetaRoot(dir, remote), func(rel, full string, _ fs.DirEntry) error {
		m, ok := readMeta(full)
		if !ok {
			// Unreadable metadata: count it as pending, the safe side.
			u.PendingFiles++
			if fi, err := os.Stat(filepath.Join(DataRoot(dir, remote), rel)); err == nil {
				u.PendingBytes += fi.Size()
			}
			return nil
		}
		if m.Dirty {
			u.PendingFiles++
			u.PendingBytes += m.Size
		}
		return nil
	})
	return u, err
}

// ClearResult reports what Clear did.
type ClearResult struct {
	RemovedFiles int   `json:"removed_files"`
	FreedBytes   int64 `json:"freed_bytes"`
	// KeptPending are files kept because they haven't uploaded yet (or
	// their state couldn't be read).
	KeptPending int `json:"kept_pending"`
}

// Clear removes remote's cached data in dir that is safe to drop: items
// whose metadata says they're uploaded (not dirty), and data files rclone
// has no metadata for (it drops those itself on start). Items waiting to
// upload, or with unreadable metadata, are always kept. The remote must not
// be mounted from this cache dir while this runs.
func Clear(dir, remote string) (ClearResult, error) {
	var res ClearResult
	data, meta := DataRoot(dir, remote), MetaRoot(dir, remote)
	keep := map[string]bool{}
	err := walkFiles(meta, func(rel, full string, _ fs.DirEntry) error {
		m, ok := readMeta(full)
		if !ok || m.Dirty {
			keep[rel] = true
			res.KeptPending++
			return nil
		}
		if n, removed := removeFile(filepath.Join(data, rel)); removed {
			res.FreedBytes += n
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		res.RemovedFiles++
		return nil
	})
	if err != nil {
		return res, err
	}
	// Data files with no metadata left (orphans).
	err = walkFiles(data, func(rel, full string, _ fs.DirEntry) error {
		if keep[rel] {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(meta, rel)); err == nil {
			return nil // metadata appeared meanwhile: leave it
		}
		if n, removed := removeFile(full); removed {
			res.FreedBytes += n
			res.RemovedFiles++
		}
		return nil
	})
	pruneEmptyDirs(data)
	pruneEmptyDirs(meta)
	return res, err
}

func removeFile(p string) (int64, bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return 0, false
	}
	n := diskBytes(fi)
	if os.Remove(p) != nil {
		return 0, false
	}
	return n, true
}

// pruneEmptyDirs removes empty directories under root, and root itself when
// it ends up empty.
func pruneEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	// Deepest first.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d) // fails (and is kept) when not empty
	}
}

// MigrateResult reports what Migrate did with one old cache directory.
type MigrateResult struct {
	From         string   `json:"from"`
	MovedFiles   int      `json:"moved_files"`
	MovedBytes   int64    `json:"moved_bytes"`
	RemovedFiles int      `json:"removed_files"`
	FreedBytes   int64    `json:"freed_bytes"`
	Left         []string `json:"left,omitempty"` // pending items that couldn't move (kept in From)
	RemovedDir   bool     `json:"removed_dir"`
}

// MigrateOptions tune Migrate; tests fake the free-space check.
type MigrateOptions struct {
	// Free returns bytes available at path; nil skips the check.
	Free func(path string) (uint64, error)
}

// Migrate moves one old cache directory into newDir: files still waiting
// to upload are moved (so the mount on newDir picks them up and uploads
// them), already-uploaded cached data is dropped, and oldDir is removed if
// that leaves it empty. Nothing may be mounted on oldDir while this runs.
// A pending file that can't be moved (no space, a clash with a pending file
// of the same name in newDir, unreadable metadata) stays where it is and is
// listed in Left - never deleted.
func Migrate(oldDir, newDir string, opt MigrateOptions) (MigrateResult, error) {
	res := MigrateResult{From: oldDir}
	oldDir, newDir = filepath.Clean(oldDir), filepath.Clean(newDir)
	if oldDir == newDir {
		return res, nil
	}
	if _, err := os.Stat(oldDir); errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	for _, remote := range Remotes(oldDir) {
		if err := migrateRemote(oldDir, newDir, remote, opt, &res); err != nil {
			return res, err
		}
	}
	pruneEmptyDirs(filepath.Join(oldDir, "vfs"))
	pruneEmptyDirs(filepath.Join(oldDir, "vfsMeta"))
	if ents, err := os.ReadDir(oldDir); err == nil && len(ents) == 0 {
		res.RemovedDir = os.Remove(oldDir) == nil
	}
	return res, nil
}

func migrateRemote(oldDir, newDir, remote string, opt MigrateOptions, res *MigrateResult) error {
	oData, oMeta := DataRoot(oldDir, remote), MetaRoot(oldDir, remote)
	nData, nMeta := DataRoot(newDir, remote), MetaRoot(newDir, remote)
	pending := map[string]bool{}
	err := walkFiles(oMeta, func(rel, full string, _ fs.DirEntry) error {
		m, ok := readMeta(full)
		if !ok {
			pending[rel] = true
			res.Left = append(res.Left, filepath.Join(remote, rel)+" (unreadable metadata)")
			return nil
		}
		if !m.Dirty {
			if n, removed := removeFile(filepath.Join(oData, rel)); removed {
				res.FreedBytes += n
			}
			if os.Remove(full) == nil {
				res.RemovedFiles++
			}
			return nil
		}
		pending[rel] = true
		if tm, exists := readMetaIfExists(filepath.Join(nMeta, rel)); exists && (tm == nil || tm.Dirty) {
			res.Left = append(res.Left, filepath.Join(remote, rel)+" (a pending upload with the same name is already in the new cache)")
			return nil
		}
		srcData := filepath.Join(oData, rel)
		size := int64(0)
		if fi, err := os.Stat(srcData); err == nil {
			size = fi.Size()
		}
		if opt.Free != nil && size > 0 && !sameFS(srcData, newDir) {
			free, ferr := opt.Free(newDir)
			if ferr != nil || int64(free) < size+(64<<20) {
				res.Left = append(res.Left, filepath.Join(remote, rel)+" (not enough free space in the new cache)")
				return nil
			}
		}
		if size > 0 || fileExists(srcData) {
			if err := moveFile(srcData, filepath.Join(nData, rel)); err != nil {
				res.Left = append(res.Left, fmt.Sprintf("%s (%v)", filepath.Join(remote, rel), err))
				return nil
			}
		}
		if err := moveFile(full, filepath.Join(nMeta, rel)); err != nil {
			// Data moved but metadata didn't: put the data back so the
			// item stays whole in the old cache.
			_ = moveFile(filepath.Join(nData, rel), srcData)
			res.Left = append(res.Left, fmt.Sprintf("%s (%v)", filepath.Join(remote, rel), err))
			return nil
		}
		res.MovedFiles++
		res.MovedBytes += size
		return nil
	})
	if err != nil {
		return err
	}
	// Orphan data files (no metadata): rclone would drop them anyway.
	_ = walkFiles(oData, func(rel, full string, _ fs.DirEntry) error {
		if pending[rel] {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(oMeta, rel)); err == nil {
			return nil
		}
		if n, removed := removeFile(full); removed {
			res.FreedBytes += n
			res.RemovedFiles++
		}
		return nil
	})
	return nil
}

// readMetaIfExists: exists=false when there's no file; meta=nil when there
// is one but it can't be parsed.
func readMetaIfExists(p string) (meta *itemMeta, exists bool) {
	if _, err := os.Lstat(p); err != nil {
		return nil, false
	}
	m, ok := readMeta(p)
	if !ok {
		return nil, true
	}
	return &m, true
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func sameFS(a, b string) bool {
	sa, sb := statDev(a), statDev(b)
	return sa != 0 && sa == sb
}

// statDev returns the device of path or of its nearest existing parent.
func statDev(p string) uint64 {
	for {
		var st syscall.Stat_t
		if syscall.Stat(p, &st) == nil {
			return uint64(st.Dev)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0
		}
		p = parent
	}
}

// moveFile renames src to dst, copying across filesystems (copy to a
// temporary name, fsync, rename, then remove src), keeping mode and times.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var le *os.LinkError
	if !errors.As(err, &le) || !errors.Is(le.Err, syscall.EXDEV) {
		return err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), ".nivaroos-migrating-"+filepath.Base(dst))
	if err := copyFile(src, tmp, fi); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string, fi fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode().Perm())
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if err == nil && n != fi.Size() {
		err = fmt.Errorf("short copy: %d of %d bytes", n, fi.Size())
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Chtimes(dst, fi.ModTime(), fi.ModTime())
}
