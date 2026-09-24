package engine

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/fserrors"
)

// mountGuard answers "is it still safe to write here?" for a
// destination. For a local one its mount must still be in the table and
// its mount point must still show the same device. The second test
// needs no table refresh, so there is no window between an unmount and
// the next write in which rclone could fill the empty mount point on the
// root disk (spec §6.2 cancel on unmount). Every destination, local or
// not, has its identity marker checked again before the first delete.
type mountGuard struct {
	e *Engine
	j *job
	// local: the destination is a mount (volume, usb, merge); network
	// destinations only get the marker check before deletes.
	local      bool
	mountID    int
	mountPoint string
	dev        uint64
	// beforeDelete runs once, before the first delete or move into the
	// recycle folder: the identity marker is checked again (spec §6.2).
	beforeDelete func(ctx context.Context) error
	deleteOK     atomic.Int32 // 0 unchecked, 1 ok, 2 failed
	writes       atomic.Int64 // successful guarded writes (tests)
}

// testHookBeforeWrite, when set, runs before every guarded write check
// (tests use it to unmount "in the middle of a run").
var testHookBeforeWrite func()

// check fails with cancelled_unmounted (fatal, so rclone stops at once)
// and cancels the job when the destination is gone.
func (g *mountGuard) check() error {
	if h := testHookBeforeWrite; h != nil {
		h()
	}
	if !g.local {
		return nil
	}
	if g.e.mountPresent(g.mountID) {
		if dev, ok := mountDev(g.mountPoint); ok && dev == g.dev {
			return nil
		}
	}
	err := Errorf(CodeCancelledUnmounted, "%s is no longer mounted; nothing more is written", g.mountPoint)
	if g.j != nil {
		g.j.cancel(err)
	}
	return fserrors.FatalError(err)
}

func (g *mountGuard) checkDelete(ctx context.Context) error {
	if err := g.check(); err != nil {
		return err
	}
	switch g.deleteOK.Load() {
	case 1:
		return nil
	case 2:
		return fserrors.FatalError(Errorf(CodeDestMarkerMismatch, "the destination marker changed during the run"))
	}
	if g.beforeDelete != nil {
		if err := g.beforeDelete(ctx); err != nil {
			g.deleteOK.Store(2)
			if g.j != nil {
				g.j.cancel(err)
			}
			return fserrors.FatalError(err)
		}
	}
	g.deleteOK.Store(1)
	return nil
}

// guardFs wraps a destination filesystem so every write, delete and
// directory create is checked by its guard first. Only operations
// that are wrapped are advertised; anything else (multi-thread writers,
// chunked uploads) is hidden so nothing can write around the guard.
type guardFs struct {
	fs.Fs
	g        *mountGuard
	features *fs.Features
}

func newGuardFs(ctx context.Context, f fs.Fs, g *mountGuard) *guardFs {
	w := &guardFs{Fs: f, g: g}
	ft := *f.Features()
	// Drop every function the underlying Fs offers, then advertise the
	// ones this wrapper implements (Fill) and the underlying also has
	// (Mask).
	ft.Purge, ft.Copy, ft.Move, ft.DirMove, ft.MkdirMetadata = nil, nil, nil, nil, nil
	ft.ChangeNotify, ft.UnWrap, ft.WrapFs, ft.SetWrapper, ft.DirCacheFlush = nil, nil, nil, nil, nil
	ft.PublicLink, ft.PutUnchecked, ft.PutStream, ft.MergeDirs, ft.DirSetModTime = nil, nil, nil, nil, nil
	ft.CleanUp, ft.ListR, ft.ListP, ft.About, ft.OpenWriterAt, ft.OpenChunkWriter = nil, nil, nil, nil, nil, nil
	ft.UserInfo, ft.Disconnect, ft.Command, ft.Shutdown = nil, nil, nil, nil
	ft.Overlay = false
	w.features = ft.Fill(ctx, w).Mask(ctx, f)
	return w
}

func (w *guardFs) Features() *fs.Features { return w.features }

func (w *guardFs) wrap(o fs.Object) fs.Object {
	if o == nil {
		return nil
	}
	return &guardObject{Object: o, w: w}
}

func (w *guardFs) wrapEntries(entries fs.DirEntries) fs.DirEntries {
	for i, e := range entries {
		if o, ok := e.(fs.Object); ok {
			entries[i] = w.wrap(o)
		}
	}
	return entries
}

func unwrapObject(o fs.Object) fs.Object {
	if g, ok := o.(*guardObject); ok {
		return g.Object
	}
	return o
}

func (w *guardFs) List(ctx context.Context, dir string) (fs.DirEntries, error) {
	entries, err := w.Fs.List(ctx, dir)
	return w.wrapEntries(entries), err
}

func (w *guardFs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	o, err := w.Fs.NewObject(ctx, remote)
	if err != nil {
		return nil, err
	}
	return w.wrap(o), nil
}

func (w *guardFs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	if err := w.g.check(); err != nil {
		return nil, err
	}
	o, err := w.Fs.Put(ctx, in, src, options...)
	if err == nil {
		w.g.writes.Add(1)
	}
	return w.wrap(o), err
}

// PutStream uploads an object of unknown size (archives).
func (w *guardFs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	do := w.Fs.Features().PutStream
	if do == nil {
		return nil, fs.ErrorNotImplemented
	}
	if err := w.g.check(); err != nil {
		return nil, err
	}
	o, err := do(ctx, &guardReader{r: in, g: w.g}, src, options...)
	if err == nil {
		w.g.writes.Add(1)
	}
	return w.wrap(o), err
}

func (w *guardFs) Mkdir(ctx context.Context, dir string) error {
	if err := w.g.check(); err != nil {
		return err
	}
	return w.Fs.Mkdir(ctx, dir)
}

// MkdirMetadata creates a directory with owners and modes (-M).
func (w *guardFs) MkdirMetadata(ctx context.Context, dir string, metadata fs.Metadata) (fs.Directory, error) {
	do := w.Fs.Features().MkdirMetadata
	if do == nil {
		return nil, fs.ErrorNotImplemented
	}
	if err := w.g.check(); err != nil {
		return nil, err
	}
	return do(ctx, dir, metadata)
}

// DirSetModTime sets a directory's modtime.
func (w *guardFs) DirSetModTime(ctx context.Context, dir string, modTime time.Time) error {
	do := w.Fs.Features().DirSetModTime
	if do == nil {
		return fs.ErrorNotImplemented
	}
	if err := w.g.check(); err != nil {
		return err
	}
	return do(ctx, dir, modTime)
}

func (w *guardFs) Rmdir(ctx context.Context, dir string) error {
	if err := w.g.checkDelete(ctx); err != nil {
		return err
	}
	return w.Fs.Rmdir(ctx, dir)
}

// Purge removes a directory tree (retention).
func (w *guardFs) Purge(ctx context.Context, dir string) error {
	do := w.Fs.Features().Purge
	if do == nil {
		return fs.ErrorCantPurge
	}
	if err := w.g.checkDelete(ctx); err != nil {
		return err
	}
	return do(ctx, dir)
}

// Move renames within the destination - this is how files reach the
// recycle folder, so it counts as a delete for the guard.
func (w *guardFs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	do := w.Fs.Features().Move
	if do == nil {
		return nil, fs.ErrorCantMove
	}
	if err := w.g.checkDelete(ctx); err != nil {
		return nil, err
	}
	o, err := do(ctx, unwrapObject(src), remote)
	return w.wrap(o), err
}

// Copy copies server-side within the destination.
func (w *guardFs) Copy(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	do := w.Fs.Features().Copy
	if do == nil {
		return nil, fs.ErrorCantCopy
	}
	if err := w.g.check(); err != nil {
		return nil, err
	}
	o, err := do(ctx, unwrapObject(src), remote)
	return w.wrap(o), err
}

// DirMove renames a directory within the destination.
func (w *guardFs) DirMove(ctx context.Context, src fs.Fs, srcRemote, dstRemote string) error {
	do := w.Fs.Features().DirMove
	if do == nil {
		return fs.ErrorCantDirMove
	}
	if err := w.g.checkDelete(ctx); err != nil {
		return err
	}
	if g, ok := src.(*guardFs); ok {
		src = g.Fs
	}
	return do(ctx, src, srcRemote, dstRemote)
}

// About reports free space.
func (w *guardFs) About(ctx context.Context) (*fs.Usage, error) {
	do := w.Fs.Features().About
	if do == nil {
		return nil, fs.ErrorNotImplemented
	}
	return do(ctx)
}

// guardObject is an object of a guarded filesystem.
type guardObject struct {
	fs.Object
	w *guardFs
}

func (o *guardObject) Fs() fs.Info { return o.w }

// UnWrap returns the wrapped object.
func (o *guardObject) UnWrap() fs.Object { return o.Object }

func (o *guardObject) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	if err := o.w.g.check(); err != nil {
		return err
	}
	err := o.Object.Update(ctx, in, src, options...)
	if err == nil {
		o.w.g.writes.Add(1)
	}
	return err
}

func (o *guardObject) Remove(ctx context.Context) error {
	if err := o.w.g.checkDelete(ctx); err != nil {
		return err
	}
	return o.Object.Remove(ctx)
}

func (o *guardObject) SetModTime(ctx context.Context, t time.Time) error {
	if err := o.w.g.check(); err != nil {
		return err
	}
	return o.Object.SetModTime(ctx, t)
}

// Metadata passes the wrapped object's metadata through (-M).
func (o *guardObject) Metadata(ctx context.Context) (fs.Metadata, error) {
	if do, ok := o.Object.(fs.Metadataer); ok {
		return do.Metadata(ctx)
	}
	return nil, nil
}

// SetMetadata writes metadata to the wrapped object.
func (o *guardObject) SetMetadata(ctx context.Context, metadata fs.Metadata) error {
	do, ok := o.Object.(fs.SetMetadataer)
	if !ok {
		return fs.ErrorNotImplemented
	}
	if err := o.w.g.check(); err != nil {
		return err
	}
	return do.SetMetadata(ctx, metadata)
}

// guardReader re-checks the guard every 64 MiB of a long stream (an
// archive being written), so an unplugged drive stops it mid-file.
type guardReader struct {
	r    io.Reader
	g    *mountGuard
	seen int64
}

func (r *guardReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.seen += int64(n)
	if r.seen >= 64<<20 {
		r.seen = 0
		if gerr := r.g.check(); gerr != nil {
			return n, gerr
		}
	}
	return n, err
}

var (
	_ fs.Fs              = (*guardFs)(nil)
	_ fs.Object          = (*guardObject)(nil)
	_ fs.ObjectUnWrapper = (*guardObject)(nil)
	_ fs.Metadataer      = (*guardObject)(nil)
	_ fs.SetMetadataer   = (*guardObject)(nil)
)
