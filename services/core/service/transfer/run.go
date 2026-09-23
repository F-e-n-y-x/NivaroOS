package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// entry is one thing to create at the destination (or delete).
type entry struct {
	src   string
	dst   string
	isDir bool
	link  bool
	size  int64
	mode  fs.FileMode
	mtime time.Time
	// top is the top-level source this entry came from (for move cleanup).
	top string
}

// topItem is one selected source and where it lands.
type topItem struct {
	src    string
	dst    string // "" for delete
	isDir  bool
	remote bool
	skip   bool // conflict policy says leave it alone
}

func (m *Manager) run(ctx context.Context, j *job) {
	var err error
	switch j.Kind {
	case KindDelete:
		err = m.runDelete(ctx, j)
	default:
		err = m.runTransfer(ctx, j)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		select {
		case <-m.closed:
			m.finishLocked(j, StateInterrupted, "")
		default:
			m.finishLocked(j, StateCancelled, "")
		}
	case err != nil:
		m.finishLocked(j, StateFailed, humanError(err))
	case j.FilesFailed > 0:
		m.finishLocked(j, StateDoneWithErrors, "")
	default:
		m.finishLocked(j, StateDone, "")
	}
}

// ---- copy / move ----

func (m *Manager) runTransfer(ctx context.Context, j *job) error {
	dest := filepath.Clean(j.Dest)
	if !m.isRemote(dest) {
		fi, err := os.Stat(dest)
		if err != nil {
			return fmt.Errorf("destination folder: %w", err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("destination %s is not a folder", dest)
		}
	}
	m.addAffected(j, dest)

	// Resolve each selected item's final name (conflict policy applies to
	// the item as a whole, the way desktop file managers do it).
	var tops []topItem
	for _, src := range j.Sources {
		src = filepath.Clean(src)
		t := topItem{src: src}
		if m.isRemote(src) || m.isRemote(dest) {
			t.remote = true
			t.dst = dest
			tops = append(tops, t)
			continue
		}
		fi, err := os.Lstat(src)
		if err != nil {
			if j.Conflict == ConflictResume && os.IsNotExist(err) {
				continue // a retried move already moved this one
			}
			m.addFailure(j, src, err)
			continue
		}
		t.isDir = fi.IsDir()
		if t.isDir && (dest == src || strings.HasPrefix(dest+"/", src+"/")) {
			m.addFailure(j, src, errors.New("can't copy or move a folder into itself"))
			continue
		}
		name := filepath.Base(src)
		t.dst = filepath.Join(dest, name)
		if j.Kind == KindMove && t.dst == src {
			// Moving into the folder it's already in: nothing to do.
			t.skip = true
			tops = append(tops, t)
			continue
		}
		if _, err := os.Lstat(t.dst); err == nil {
			switch {
			case j.Conflict == ConflictResume:
				// Finish into the existing item.
			case filepath.Dir(src) == dest || j.Conflict == ConflictRename:
				// Pasting into the same folder always keeps both.
				t.dst = uniqueName(dest, name)
			case j.Conflict == ConflictSkip:
				t.skip = true
			}
		}
		m.addAffected(j, filepath.Dir(src))
		tops = append(tops, t)
	}

	// Scan: build the full plan and totals before copying anything, so the
	// progress and "N of M files" are real.
	var plan []entry
	for _, t := range tops {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if t.skip {
			continue
		}
		if t.remote {
			size, err := m.remoteSize(ctx, t.src)
			if err != nil {
				m.addFailure(j, t.src, err)
				continue
			}
			m.update(j, func(j *job) { j.FilesTotal++; j.BytesTotal += size })
			continue
		}
		plan = append(plan, m.scan(ctx, j, t)...)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.setState(j, StateRunning)

	// Fast path for moves on the same filesystem: one rename per selected
	// item, atomic, no data copied.
	movedTops := map[string]bool{}
	if j.Kind == KindMove {
		for _, t := range tops {
			if t.skip || t.remote || !sameDevice(t.src, filepath.Dir(t.dst)) {
				continue
			}
			if _, err := os.Lstat(t.dst); err == nil {
				continue // merge/overwrite needs the per-file path
			}
			if err := os.Rename(t.src, t.dst); err == nil {
				movedTops[t.src] = true
				var files int
				var bytes int64
				for _, e := range plan {
					if e.top == t.src && !e.isDir {
						files++
						bytes += e.size
					}
				}
				m.update(j, func(j *job) { j.FilesDone += files; j.BytesDone += bytes })
			}
		}
	}

	verified := map[string]bool{} // source files safe to remove (move)
	var unflushed []string        // small files copied without their own fsync
	swept := map[string]bool{}
	for _, e := range plan {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if movedTops[e.top] {
			continue
		}
		if d := filepath.Dir(e.dst); j.Conflict == ConflictResume && !swept[d] {
			swept[d] = true
			m.sweepTemps(d)
		}
		if e.isDir {
			if err := os.MkdirAll(e.dst, dirMode(e.mode)); err != nil {
				m.addFailure(j, e.src, err)
			}
			continue
		}
		m.update(j, func(j *job) { j.Current = e.src })
		if skipExisting(j.Conflict, e) {
			m.update(j, func(j *job) { j.FilesSkipped++; j.BytesDone += e.size })
			if j.Kind == KindMove {
				// Identical copy already there: the source can go.
				if j.Conflict == ConflictResume {
					verified[e.src] = true
				}
			}
			continue
		}
		var err error
		if j.Kind == KindMove && sameDevice(e.src, filepath.Dir(e.dst)) {
			err = renameReplacing(e.src, e.dst)
			if err == nil {
				m.update(j, func(j *job) { j.FilesDone++; j.BytesDone += e.size })
				continue
			}
		}
		var copied int64
		syncNow := e.size >= syncEachFrom
		err = copyEntry(ctx, e, syncNow, func(n int64) {
			copied += n
			m.update(j, func(j *job) { j.BytesDone += n })
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Count the failed file as processed so progress still ends at
			// 100% - the failure itself is what gets reported.
			rest := e.size - copied
			m.update(j, func(j *job) { j.BytesDone += rest })
			m.addFailure(j, e.src, err)
			continue
		}
		verified[e.src] = true
		if !syncNow {
			unflushed = append(unflushed, e.src)
		}
		m.update(j, func(j *job) { j.FilesDone++ })
	}

	// One flush for all the small files, before anything counts as done -
	// and before a move deletes a single source file.
	if len(unflushed) > 0 {
		m.update(j, func(j *job) { j.Current = dest })
		if err := m.flushFS(dest); err != nil {
			for _, src := range unflushed {
				delete(verified, src)
			}
			m.addFailure(j, dest, fmt.Errorf("flushing to disk: %w", err))
		}
	}

	for _, t := range tops {
		if !t.remote || t.skip {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		m.update(j, func(j *job) { j.Current = t.src })
		err := m.remoteTransfer(ctx, j, t)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.addFailure(j, t.src, err)
			continue
		}
		m.update(j, func(j *job) { j.FilesDone++ })
	}

	if j.Kind == KindMove {
		m.removeMovedSources(j, plan, verified, movedTops)
	}

	if m.opts.AfterWrite != nil && !m.isRemote(dest) {
		// The job only shows as "syncing" once the hook reports activity
		// (a copy to a plain local folder never flashes that state).
		if err := m.opts.AfterWrite(ctx, dest, func(cur string) {
			m.update(j, func(j *job) { j.Current = cur; j.State = StateSyncing })
			m.notify(true)
		}); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.addFailure(j, dest, err)
		}
	}
	return nil
}

// scan walks one selected item into plan entries. Unreadable parts become
// recorded failures - never silently dropped, never counted as zero bytes.
// Totals are reported in batches so a huge tree shows its count growing.
func (m *Manager) scan(ctx context.Context, j *job, t topItem) []entry {
	var out []entry
	var files, reportedFiles int
	var bytes, reportedBytes int64
	flush := func(cur string) {
		df, db := files-reportedFiles, bytes-reportedBytes
		reportedFiles, reportedBytes = files, bytes
		m.update(j, func(j *job) { j.FilesTotal += df; j.BytesTotal += db; j.Current = cur })
	}
	walkErr := filepath.WalkDir(t.src, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			m.addFailure(j, p, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			m.addFailure(j, p, err)
			return nil
		}
		rel, _ := filepath.Rel(t.src, p)
		e := entry{src: p, dst: filepath.Join(t.dst, rel), mode: info.Mode(), mtime: info.ModTime(), top: t.src}
		switch {
		case d.IsDir():
			e.isDir = true
		case info.Mode()&fs.ModeSymlink != 0:
			e.link = true
			files++
		case info.Mode().IsRegular():
			e.size = info.Size()
			files++
			bytes += e.size
		default:
			m.addFailure(j, p, errors.New("special file (device, socket or pipe) can't be copied"))
			return nil
		}
		out = append(out, e)
		if len(out)%500 == 0 {
			flush(p)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		m.addFailure(j, t.src, walkErr)
	}
	flush("")
	return out
}

// removeMovedSources deletes the source side of a cross-device move - only
// files that verifiably arrived, then directories only if now empty. A file
// that failed to copy is still exactly where it was.
func (m *Manager) removeMovedSources(j *job, plan []entry, verified, movedTops map[string]bool) {
	var dirs []string
	for _, e := range plan {
		if movedTops[e.top] {
			continue
		}
		if e.isDir {
			dirs = append(dirs, e.src)
			continue
		}
		if verified[e.src] {
			if err := os.Remove(e.src); err != nil && !os.IsNotExist(err) {
				m.addFailure(j, e.src, fmt.Errorf("copied, but the original couldn't be removed: %w", err))
			}
		}
	}
	// Deepest first.
	sort.Slice(dirs, func(a, b int) bool { return len(dirs[a]) > len(dirs[b]) })
	for _, d := range dirs {
		_ = os.Remove(d) // fails (correctly) if something is left inside
	}
}

// ---- delete ----

func (m *Manager) runDelete(ctx context.Context, j *job) error {
	var plan []entry
	for _, src := range j.Sources {
		src = filepath.Clean(src)
		if src == "/" || src == "" {
			m.addFailure(j, src, errors.New("refusing to delete the root folder"))
			continue
		}
		m.addAffected(j, filepath.Dir(src))
		if m.isRemote(src) {
			plan = append(plan, entry{src: src, top: "remote"})
			m.update(j, func(j *job) { j.FilesTotal++ })
			continue
		}
		if _, err := os.Lstat(src); err != nil {
			m.addFailure(j, src, err)
			continue
		}
		plan = append(plan, m.scan(ctx, j, topItem{src: src, dst: src})...)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.setState(j, StateRunning)
	var dirs []string
	for _, e := range plan {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.isDir {
			dirs = append(dirs, e.src)
			continue
		}
		m.update(j, func(j *job) { j.Current = e.src })
		var err error
		if e.top == "remote" {
			err = m.remoteDelete(ctx, e.src)
		} else {
			err = os.Remove(e.src)
		}
		if err != nil && !os.IsNotExist(err) {
			m.addFailure(j, e.src, err)
			continue
		}
		m.update(j, func(j *job) { j.FilesDone++; j.BytesDone += e.size })
	}
	sort.Slice(dirs, func(a, b int) bool { return len(dirs[a]) > len(dirs[b]) })
	for _, d := range dirs {
		if err := os.Remove(d); err != nil && !os.IsNotExist(err) {
			// Only report a folder when nothing inside it already failed -
			// otherwise the inner failure is the real story.
			if !hasFailureUnder(j, d) {
				m.addFailure(j, d, err)
			}
		}
	}
	return nil
}

func hasFailureUnder(j *job, dir string) bool {
	for _, f := range j.Failures {
		if strings.HasPrefix(f.Path, dir+"/") {
			return true
		}
	}
	return false
}

// ---- file primitives ----

const copyBuf = 1 << 20

// Files at least this big are fsynced one by one; smaller ones are flushed
// together at the end of the job (see flushFS). A per-file fsync costs a
// disk seek or more, which dominated copies of many small files.
const syncEachFrom = 8 << 20

// sweepTemps removes temp files in dir that an earlier run of the service
// left behind when it died mid-copy. Ones created since this process
// started may belong to a live job and are left alone.
func (m *Manager) sweepTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		n := de.Name()
		i := strings.LastIndex(n, ".nvtmp-")
		if !strings.HasPrefix(n, ".") || i < 0 || !isHexID(n[i+len(".nvtmp-"):]) {
			continue
		}
		if info, err := de.Info(); err == nil && info.ModTime().Before(m.started) {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}

func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// syncFS flushes the whole filesystem that dir is on.
func syncFS(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Syncfs(int(f.Fd()))
}

// copyEntry copies one file or symlink to e.dst through a temporary name,
// optionally fsyncs, verifies the size and only then renames it into place.
func copyEntry(ctx context.Context, e entry, syncNow bool, progress func(int64)) error {
	dir := filepath.Dir(e.dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(e.dst); err == nil && fi.IsDir() {
		return fmt.Errorf("a folder with this name already exists at the destination")
	}
	tmp := filepath.Join(dir, "."+filepath.Base(e.dst)+".nvtmp-"+newID())

	if e.link {
		target, err := os.Readlink(e.src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, tmp); err != nil {
			// Filesystems without symlinks (exFAT, some FUSE/CIFS mounts):
			// fall back to copying what the link points at.
			info, statErr := os.Stat(e.src)
			if statErr != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("symlink can't be created here and its target isn't a regular file: %w", err)
			}
			e.link = false
			e.size = info.Size()
			return copyEntry(ctx, e, syncNow, progress)
		}
		if err := os.Rename(tmp, e.dst); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}

	in, err := os.Open(e.src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			_ = os.Remove(tmp)
		}
	}()

	buf := make([]byte, copyBuf)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			progress(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if syncNow {
		if err := out.Sync(); err != nil {
			return fmt.Errorf("flushing to disk: %w", err)
		}
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("finishing write: %w", err)
	}
	if st, err := os.Stat(tmp); err != nil {
		return err
	} else if st.Size() != written {
		return fmt.Errorf("verification failed: wrote %d bytes but %d arrived", written, st.Size())
	}
	if st, err := in.Stat(); err == nil && st.Size() != written {
		return fmt.Errorf("the source file changed size while it was being copied")
	}
	_ = os.Chmod(tmp, e.mode.Perm())
	_ = os.Chtimes(tmp, time.Now(), e.mtime)
	if err := os.Rename(tmp, e.dst); err != nil {
		return err
	}
	ok = true
	return nil
}

// skipExisting applies the per-file part of the conflict policy.
func skipExisting(c Conflict, e entry) bool {
	fi, err := os.Lstat(e.dst)
	if err != nil {
		return false
	}
	switch c {
	case ConflictSkip:
		return true
	case ConflictResume:
		if e.link {
			return fi.Mode()&fs.ModeSymlink != 0
		}
		d := fi.ModTime().Sub(e.mtime)
		return fi.Mode().IsRegular() && fi.Size() == e.size && d < 2*time.Second && d > -2*time.Second
	}
	return false
}

// renameReplacing is a same-filesystem move of one file onto dst.
func renameReplacing(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(dst); err == nil && fi.IsDir() {
		return fmt.Errorf("a folder with this name already exists at the destination")
	}
	return os.Rename(src, dst)
}

func sameDevice(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Lstat(a, &sa) != nil || syscall.Stat(b, &sb) != nil {
		return false
	}
	return sa.Dev == sb.Dev
}

func dirMode(m fs.FileMode) fs.FileMode {
	p := m.Perm()
	if p == 0 {
		return 0o755
	}
	return p | 0o700
}

// uniqueName returns "name (2).ext", "name (3).ext", ... not present in dir.
func uniqueName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if strings.HasSuffix(strings.ToLower(stem), ".tar") {
		ext = stem[len(stem)-4:] + ext
		stem = stem[:len(stem)-4]
	}
	for i := 2; ; i++ {
		c := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// humanError turns syscall errors into something a person can act on.
func humanError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syscall.ENOSPC):
		return "not enough free space at the destination"
	case errors.Is(err, syscall.EDQUOT):
		return "storage quota exceeded"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		return "no longer exists"
	case errors.Is(err, syscall.ENAMETOOLONG):
		return "name is too long for the destination drive"
	case errors.Is(err, syscall.EROFS):
		return "the destination drive is read-only"
	case errors.Is(err, syscall.EIO):
		return "input/output error (the drive or connection may have a problem)"
	case errors.Is(err, syscall.EINVAL):
		return "the name contains characters the destination drive doesn't allow"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
