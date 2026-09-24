package engine

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/walk"
	"golang.org/x/sys/unix"
)

// Reason keys for skipped files (backup.log.skipped reason_key). They are
// UI texts in en_US.json; TestEngineKeysInEnUS keeps them there.
const (
	reasonExists      = "backup.reason.exists"
	reasonIdentical   = "backup.reason.identical"
	reasonNotFound    = "backup.reason.not_found"
	reasonUnsafePath  = "backup.reason.unsafe_path"
	reasonSpecialFile = "backup.reason.special_file"
)

// ReasonKeys lists every reason key the engine logs.
var ReasonKeys = []string{reasonExists, reasonIdentical, reasonNotFound, reasonUnsafePath, reasonSpecialFile}

// keepBothName is "name (restored 2026-09-24).ext", then "... 2)" and so
// on while exists says the name is taken.
func keepBothName(rel string, day time.Time, exists func(string) bool) string {
	dir, base := path.Split(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // ".bashrc"
		stem, ext = base, ""
	}
	date := day.Format("2006-01-02")
	for n := 1; ; n++ {
		label := "restored " + date
		if n > 1 {
			label = fmt.Sprintf("restored %s %d", date, n)
		}
		cand := dir + stem + " (" + label + ")" + ext
		if !exists(cand) {
			return cand
		}
	}
}

// restoreCounter tallies a restore, safe for the copy workers.
type restoreCounter struct {
	mu                               sync.Mutex
	added, changed, skipped, errored int64
	bytes                            int64
	fileErrors                       []FileError
}

func (c *restoreCounter) fail(j *job, rel string, err error) {
	code := classifyError(err)
	c.mu.Lock()
	c.errored++
	if len(c.fileErrors) < maxFileErrors {
		c.fileErrors = append(c.fileErrors, FileError{Path: rel, Code: code, Detail: err.Error()})
	}
	c.mu.Unlock()
	j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
		Args: map[string]interface{}{"path": rel, "reason_key": "backup.err." + string(code) + ".title"}, Raw: err.Error()})
}

func (c *restoreCounter) skip(j *job, rel, reason string) {
	c.mu.Lock()
	c.skipped++
	c.mu.Unlock()
	j.logLine(LogInfo, "skipped", "backup.log.skipped", map[string]interface{}{"path": rel, "reason_key": reason})
}

// selected reports whether rel is one of paths or below one (all when
// paths is empty).
func selected(paths []string, rel string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if p == "" || rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// runRestore copies paths of a version (current, recycle folder or
// archive) of a job's destination to a target folder.
func (e *Engine) runRestore(ctx context.Context, j *job) (Result, error) {
	r := j.req
	spec := *r.Restore
	var res Result
	j.phase(stepResolve, "backup.phase.precheck")
	dir, archive, err := versionRoot(spec.VersionID)
	if err != nil {
		return res, err
	}
	paths := make([]string, 0, len(spec.Paths))
	for _, p := range spec.Paths {
		c, err := cleanSubPath(p)
		if err != nil {
			return res, err
		}
		paths = append(paths, c)
	}
	dest, err := e.resolve(ctx, r.Dest, credsFor(r.SMBCreds, r.Dest))
	if err != nil {
		return res, err
	}
	if !dest.online {
		return res, Errorf(CodeDestOffline, "the backup at %s is not available", dest.display)
	}
	if r.DestFolderID != "" {
		if _, err := e.checkMarker(ctx, dest, r.DestFolderID, false); err != nil {
			return res, err
		}
	}
	tgt, err := e.resolve(ctx, spec.Target, credsFor(r.SMBCreds, spec.Target))
	if err != nil {
		return res, err
	}
	if !tgt.online {
		return res, Errorf(CodeSourceOffline, "the restore folder %s is not available", tgt.display)
	}
	if dest.local {
		j.watch(dest.mv.m.ID, "backup ("+dest.mountPoint+")")
	}
	if tgt.local {
		j.watch(tgt.mv.m.ID, "restore folder ("+tgt.mountPoint+")")
		res.MountID = tgt.mv.m.ID
	}
	if dest.local && tgt.local && sameSpace(dest, tgt) && (pathWithin(tgt.path, dest.path) || pathWithin(dest.path, tgt.path)) && tgt.path != "" {
		if pathWithin(tgt.path, dest.path) {
			return res, Errorf(CodeDestInsideSource, "restoring into the backup folder itself (%s) is not allowed", tgt.path)
		}
	}
	meta := dest.local && tgt.local
	rctx, ci := rcloneContext(ctx)
	ci.Metadata = meta
	if tgt.q.ModifyWindow > 0 {
		ci.ModifyWindow = fs.Duration(tgt.q.ModifyWindow)
	}
	if r.Options.LowPriority {
		ci.Transfers = 2
	}
	tf, guard, err := e.openDest(rctx, j, tgt, "", fsOpts{links: meta}, "")
	if err != nil {
		return res, err
	}
	if tgt.local && !spec.DryRun {
		if err := guard.check(); err != nil {
			return res, err
		}
		if err := os.MkdirAll(tgt.path, 0o755); err != nil {
			return res, Errorf(classifyError(err), "creating %s: %w", tgt.path, err)
		}
	}
	j.phase(stepRestore, "backup.phase.transfer")
	c := &restoreCounter{}
	if archive != "" {
		src, err := dest.fsAt(rctx, "", fsOpts{})
		if err != nil {
			return res, engineErr(err, "opening the backup")
		}
		err = e.restoreArchive(rctx, j, src, archive, paths, tgt, tf, guard, spec, c)
		fillRestoreResult(&res, c)
		return res, err
	}
	src, err := dest.fsAt(rctx, dir, fsOpts{links: meta})
	if err != nil {
		return res, engineErr(err, "opening the backup")
	}
	err = e.restoreFiles(rctx, j, src, dir == "", paths, tgt, tf, spec, c)
	fillRestoreResult(&res, c)
	return res, err
}

func fillRestoreResult(res *Result, c *restoreCounter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res.Counts.Added, res.Counts.Changed, res.Counts.Skipped, res.Counts.Errored = c.added, c.changed, c.skipped, c.errored
	res.Counts.BytesTransferred = c.bytes
	res.FileErrors = append([]FileError(nil), c.fileErrors...)
}

// restoreFiles restores from a folder tree (current copy or a recycle
// folder) through rclone, with a few copies in parallel.
func (e *Engine) restoreFiles(ctx context.Context, j *job, src fs.Fs, isCurrent bool, paths []string, tgt *target, tf fs.Fs, spec RestoreSpec, c *restoreCounter) error {
	if isCurrent {
		// The current copy of a mirror also holds its recycle folder and
		// marker; they are never restored as data.
		fctx, _, err := withFilter(ctx, filterSpec{protectDest: true})
		if err != nil {
			return err
		}
		ctx = fctx
	}
	var objs []fs.Object
	var total int64
	collect := func(o fs.Object) {
		objs = append(objs, o)
		total += max(o.Size(), 0)
	}
	roots := paths
	if len(roots) == 0 {
		roots = []string{""}
	}
	for _, p := range roots {
		if p != "" {
			if o, err := src.NewObject(ctx, p); err == nil {
				collect(o)
				continue
			} else if !errIsNotFound(err) && !errors.Is(err, fs.ErrorIsDir) && !errors.Is(err, fs.ErrorNotAFile) {
				return engineErr(err, "reading the backup")
			}
		}
		err := walk.Walk(ctx, src, p, false, -1, func(_ string, entries fs.DirEntries, err error) error {
			if err != nil {
				return err
			}
			for _, en := range entries {
				if o, ok := en.(fs.Object); ok {
					collect(o)
				}
			}
			return nil
		})
		if errors.Is(err, fs.ErrorDirNotFound) {
			c.skip(j, p, reasonNotFound)
			continue
		}
		if err != nil {
			return engineErr(err, "reading the backup")
		}
	}
	j.setTotals(int64(len(objs)), total)
	if free := e.freeSpace(ctx, tgt); free != nil && !spec.DryRun && *free < total {
		return Errorf(CodeNoSpace, "restoring needs %d bytes, the folder has %d free", total, *free)
	}
	workers := fs.GetConfig(ctx).Transfers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan fs.Object)
	var wg sync.WaitGroup
	var fatal error
	var fatalMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for o := range jobs {
				if err := e.restoreOne(ctx, j, o, tf, spec, c); err != nil {
					fatalMu.Lock()
					if fatal == nil {
						fatal = err
					}
					fatalMu.Unlock()
				}
			}
		}()
	}
	for _, o := range objs {
		if ctx.Err() != nil {
			break
		}
		fatalMu.Lock()
		stop := fatal != nil
		fatalMu.Unlock()
		if stop {
			break
		}
		jobs <- o
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return fatal
}

// restoreOne copies one object according to the conflict policy. Only
// run-ending problems (unmounted, no space) are returned; a file that
// fails is counted.
func (e *Engine) restoreOne(ctx context.Context, j *job, o fs.Object, tf fs.Fs, spec RestoreSpec, c *restoreCounter) error {
	rel := o.Remote()
	existing, err := tf.NewObject(ctx, rel)
	if err != nil && !errIsNotFound(err) {
		if errors.Is(err, fs.ErrorIsDir) || errors.Is(err, fs.ErrorNotAFile) {
			c.skip(j, rel, reasonExists)
			return nil
		}
		c.fail(j, rel, err)
		return nil
	}
	name := rel
	if existing != nil {
		if operations.Equal(ctx, o, existing) {
			c.skip(j, rel, reasonIdentical)
			return nil
		}
		switch spec.Conflict {
		case ConflictSkip:
			c.skip(j, rel, reasonExists)
			return nil
		case ConflictKeepBoth:
			name = keepBothName(rel, e.now(), func(cand string) bool {
				_, err := tf.NewObject(ctx, cand)
				return err == nil || errors.Is(err, fs.ErrorIsDir)
			})
			existing = nil
		}
	}
	if spec.DryRun {
		c.mu.Lock()
		if existing != nil {
			c.changed++
		} else {
			c.added++
		}
		c.bytes += max(o.Size(), 0)
		c.mu.Unlock()
		return nil
	}
	if _, err := operations.Copy(ctx, tf, existing, name, o); err != nil {
		if code := classifyError(err); code == CodeCancelledUnmounted || code == CodeNoSpace || ctx.Err() != nil {
			return engineErr(err, "restoring "+rel)
		}
		c.fail(j, rel, err)
		return nil
	}
	c.mu.Lock()
	if existing != nil {
		c.changed++
	} else {
		c.added++
	}
	c.bytes += max(o.Size(), 0)
	c.mu.Unlock()
	j.logLine(LogInfo, "copied", "backup.log.copied", map[string]interface{}{"path": name})
	return nil
}

// restoreArchive extracts the selected members of an archive.
func (e *Engine) restoreArchive(ctx context.Context, j *job, src fs.Fs, archive string, paths []string, tgt *target, tf fs.Fs, guard *mountGuard, spec RestoreSpec, c *restoreCounter) error {
	if idx, ok, err := readIndex(ctx, src, archive); err == nil && ok {
		var files, bytes int64
		for _, en := range idx.entries {
			if !en.D && selected(paths, en.P) {
				files++
				bytes += en.S
			}
		}
		j.setTotals(files, bytes)
		if free := e.freeSpace(ctx, tgt); free != nil && !spec.DryRun && *free < bytes {
			return Errorf(CodeNoSpace, "restoring needs %d bytes, the folder has %d free", bytes, *free)
		}
	}
	tr, closer, err := openArchive(ctx, src, archive)
	if err != nil {
		return err
	}
	defer closer()
	x := &extractor{e: e, j: j, tgt: tgt, tf: tf, guard: guard, spec: spec, c: c, links: map[string]string{}, safeDirs: map[string]bool{}}
	for {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Errorf(CodeIOError, "reading the archive: %w", err)
		}
		rel, err := cleanSubPath(strings.TrimSuffix(h.Name, "/"))
		if err != nil || rel == "" {
			if rel != "" || err != nil {
				c.skip(j, h.Name, reasonUnsafePath)
			}
			continue
		}
		if !selected(paths, rel) {
			continue
		}
		if err := x.entry(ctx, h, rel, tr); err != nil {
			return err
		}
	}
	return x.finishDirs()
}

// extractor writes archive members to a restore target.
type extractor struct {
	e        *Engine
	j        *job
	tgt      *target
	tf       fs.Fs
	guard    *mountGuard
	spec     RestoreSpec
	c        *restoreCounter
	links    map[string]string // member name -> restored path, for hard links
	safeDirs map[string]bool   // parent folders already checked for symlinks
	dirs     []dirTimes
}

type dirTimes struct {
	path string
	h    *tar.Header
}

func (x *extractor) entry(ctx context.Context, h *tar.Header, rel string, r io.Reader) error {
	if !x.tgt.local {
		return x.remoteEntry(ctx, h, rel, r)
	}
	if err := x.guard.check(); err != nil {
		return err
	}
	if err := x.safeParent(rel); err != nil {
		x.c.skip(x.j, rel, reasonUnsafePath)
		return nil
	}
	dst := filepath.Join(x.tgt.path, filepath.FromSlash(rel))
	if h.Typeflag == tar.TypeDir {
		if x.spec.DryRun {
			return nil
		}
		if err := os.MkdirAll(dst, 0o700); err != nil {
			x.c.fail(x.j, rel, err)
			return nil
		}
		x.dirs = append(x.dirs, dirTimes{path: dst, h: h})
		return nil
	}
	target := dst
	if fi, err := os.Lstat(dst); err == nil {
		if fi.IsDir() {
			x.c.skip(x.j, rel, reasonExists)
			return nil
		}
		switch x.spec.Conflict {
		case ConflictSkip:
			x.c.skip(x.j, rel, reasonExists)
			return nil
		case ConflictKeepBoth:
			if h.Typeflag == tar.TypeReg && fi.Mode().IsRegular() && fi.Size() == h.Size && fi.ModTime().Equal(h.ModTime) {
				x.c.skip(x.j, rel, reasonIdentical)
				return nil
			}
			name := keepBothName(rel, x.e.now(), func(cand string) bool {
				_, err := os.Lstat(filepath.Join(x.tgt.path, filepath.FromSlash(cand)))
				return err == nil
			})
			target = filepath.Join(x.tgt.path, filepath.FromSlash(name))
		}
	}
	if x.spec.DryRun {
		x.c.mu.Lock()
		if target == dst && fileExists(dst) {
			x.c.changed++
		} else {
			x.c.added++
		}
		x.c.bytes += h.Size
		x.c.mu.Unlock()
		return nil
	}
	existed := fileExists(target)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		x.c.fail(x.j, rel, err)
		return nil
	}
	if existed {
		// Replace atomically: extract beside it, then rename over it.
		tmp := target + ".nivaro-restore.tmp"
		if err := x.write(h, tmp, r, rel); err != nil {
			_ = os.Remove(tmp)
			return x.fileErr(rel, err)
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return x.fileErr(rel, err)
		}
	} else if err := x.write(h, target, r, rel); err != nil {
		_ = os.Remove(target)
		return x.fileErr(rel, err)
	}
	x.links[h.Name] = target
	x.c.mu.Lock()
	if existed {
		x.c.changed++
	} else {
		x.c.added++
	}
	x.c.bytes += h.Size
	x.c.mu.Unlock()
	x.j.addOwn(h.Size, 1, rel)
	x.j.logLine(LogInfo, "copied", "backup.log.copied", map[string]interface{}{"path": rel})
	return nil
}

// fileErr counts a failed file; only run-ending errors are returned.
func (x *extractor) fileErr(rel string, err error) error {
	if code := classifyError(err); code == CodeNoSpace || code == CodeCancelledUnmounted {
		return err
	}
	if errors.Is(err, errSpecialFile) {
		x.c.skip(x.j, rel, reasonSpecialFile)
		return nil
	}
	x.c.fail(x.j, rel, err)
	return nil
}

var errSpecialFile = errors.New("special file")

// write creates one member at p with its mode, owner and times.
func (x *extractor) write(h *tar.Header, p string, r io.Reader, rel string) error {
	mode := os.FileMode(h.Mode).Perm()
	switch h.Typeflag {
	case tar.TypeReg:
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err := writeSparse(f, r, h.Size, x.guard); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	case tar.TypeSymlink:
		if err := os.Symlink(h.Linkname, p); err != nil {
			return err
		}
	case tar.TypeLink:
		first, ok := x.links[h.Linkname]
		if !ok {
			return fmt.Errorf("%w: hard link to %s, which was not restored", errSpecialFile, h.Linkname)
		}
		return os.Link(first, p)
	case tar.TypeFifo:
		if err := unix.Mkfifo(p, uint32(mode)); err != nil {
			return err
		}
	case tar.TypeChar, tar.TypeBlock:
		kind := uint32(unix.S_IFCHR)
		if h.Typeflag == tar.TypeBlock {
			kind = unix.S_IFBLK
		}
		if err := unix.Mknod(p, kind|uint32(mode), int(unix.Mkdev(uint32(h.Devmajor), uint32(h.Devminor)))); err != nil {
			return fmt.Errorf("%w: %v", errSpecialFile, err)
		}
	default:
		return fmt.Errorf("%w: tar type %c", errSpecialFile, h.Typeflag)
	}
	applyOwnerModeTimes(p, h)
	return nil
}

// applyOwnerModeTimes restores what the filesystem can keep; FAT and
// exFAT keep no owners or modes, which is not an error.
func applyOwnerModeTimes(p string, h *tar.Header) {
	if os.Geteuid() == 0 {
		_ = os.Lchown(p, h.Uid, h.Gid)
	}
	if h.Typeflag != tar.TypeSymlink {
		_ = os.Chmod(p, os.FileMode(h.Mode).Perm()|modeSpecial(h.Mode))
	}
	ts := []unix.Timespec{unix.NsecToTimespec(h.AccessTime.UnixNano()), unix.NsecToTimespec(h.ModTime.UnixNano())}
	if h.AccessTime.IsZero() {
		ts[0] = ts[1]
	}
	_ = unix.UtimesNanoAt(unix.AT_FDCWD, p, ts, unix.AT_SYMLINK_NOFOLLOW)
}

func modeSpecial(m int64) os.FileMode {
	var out os.FileMode
	if m&0o4000 != 0 {
		out |= os.ModeSetuid
	}
	if m&0o2000 != 0 {
		out |= os.ModeSetgid
	}
	if m&0o1000 != 0 {
		out |= os.ModeSticky
	}
	return out
}

// finishDirs sets folder modes and times last (writing into a folder
// changes its modtime), deepest first.
func (x *extractor) finishDirs() error {
	for i := len(x.dirs) - 1; i >= 0; i-- {
		applyOwnerModeTimes(x.dirs[i].path, x.dirs[i].h)
	}
	return nil
}

// safeParent refuses to write through a symbolic link: every folder
// between the target root and rel must be a real folder (or not exist
// yet), so an archive can't plant a link and then write through it.
func (x *extractor) safeParent(rel string) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	cur := ""
	for _, part := range strings.Split(dir, "/") {
		cur = joinSub(cur, part)
		if x.safeDirs[cur] {
			continue
		}
		fi, err := os.Lstat(filepath.Join(x.tgt.path, filepath.FromSlash(cur)))
		if errors.Is(err, os.ErrNotExist) {
			return nil // nothing further down exists either
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a folder", cur)
		}
		x.safeDirs[cur] = true
	}
	return nil
}

// remoteEntry restores a member to a network target (files only).
func (x *extractor) remoteEntry(ctx context.Context, h *tar.Header, rel string, r io.Reader) error {
	if h.Typeflag != tar.TypeReg {
		if h.Typeflag != tar.TypeDir {
			x.c.skip(x.j, rel, reasonSpecialFile)
		}
		return nil
	}
	name := rel
	if o, err := x.tf.NewObject(ctx, rel); err == nil {
		switch x.spec.Conflict {
		case ConflictSkip:
			x.c.skip(x.j, rel, reasonExists)
			return nil
		case ConflictKeepBoth:
			if o.Size() == h.Size {
				x.c.skip(x.j, rel, reasonIdentical)
				return nil
			}
			name = keepBothName(rel, x.e.now(), func(cand string) bool {
				_, err := x.tf.NewObject(ctx, cand)
				return err == nil
			})
		}
	}
	if x.spec.DryRun {
		x.c.mu.Lock()
		x.c.added++
		x.c.bytes += h.Size
		x.c.mu.Unlock()
		return nil
	}
	if _, err := operations.RcatSize(ctx, x.tf, name, io.NopCloser(io.LimitReader(r, h.Size)), h.Size, h.ModTime, nil); err != nil {
		return x.fileErr(rel, err)
	}
	x.c.mu.Lock()
	x.c.added++
	x.c.bytes += h.Size
	x.c.mu.Unlock()
	x.j.addOwn(h.Size, 1, rel)
	x.j.logLine(LogInfo, "copied", "backup.log.copied", map[string]interface{}{"path": name})
	return nil
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// sparseBlock is the granularity of hole detection when extracting.
const sparseBlock = 64 << 10

// writeSparse writes size bytes of r to f, seeking over all-zero blocks
// instead of writing them, so a sparse file (a VM disk image) comes back
// sparse and not fully allocated. The guard is re-checked every 64 MiB.
func writeSparse(f *os.File, r io.Reader, size int64, g *mountGuard) error {
	buf := make([]byte, sparseBlock)
	var off, sinceCheck int64
	for off < size {
		n := int64(len(buf))
		if size-off < n {
			n = size - off
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		if isZero(buf[:n]) {
			off += n
		} else {
			if _, err := f.WriteAt(buf[:n], off); err != nil {
				return err
			}
			off += n
		}
		sinceCheck += n
		if g != nil && sinceCheck >= 64<<20 {
			sinceCheck = 0
			if err := g.check(); err != nil {
				return err
			}
		}
	}
	// A trailing hole still has to count towards the size.
	return f.Truncate(size)
}

func isZero(b []byte) bool {
	for len(b) >= 8 {
		if b[0]|b[1]|b[2]|b[3]|b[4]|b[5]|b[6]|b[7] != 0 {
			return false
		}
		b = b[8:]
	}
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
