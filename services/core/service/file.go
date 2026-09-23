package service

import (
	"context"
	"io"

	"github.com/moby/sys/mountinfo"
)

// The copy/move/delete queue that used to live here (FileQueue, opStrArr,
// FileOperate, CheckFileStatus, ComputeOperateSizes) is replaced by the
// transfer engine - see service/transfer and service/transfers.go.

type reader struct {
	ctx context.Context
	r   io.Reader
}

// NewReader wraps an io.Reader to handle context cancellation.
//
// Context state is checked BEFORE every Read.
func NewReader(ctx context.Context, r io.Reader) io.Reader {
	if r, ok := r.(*reader); ok && ctx == r.ctx {
		return r
	}
	return &reader{ctx: ctx, r: r}
}

func (r *reader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}

type writer struct {
	ctx context.Context
	w   io.Writer
}

type copier struct {
	writer
}

func NewWriter(ctx context.Context, w io.Writer) io.Writer {
	if w, ok := w.(*copier); ok && ctx == w.ctx {
		return w
	}
	return &copier{writer{ctx: ctx, w: w}}
}

// Write implements io.Writer, but with context awareness.
func (w *writer) Write(p []byte) (n int, err error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
		return w.w.Write(p)
	}
}

// CompanionIOHandler moves data to and from companion devices (implemented
// in route/v1/companion.go). Every method must return an error if any part
// of the transfer failed.
type CompanionIOHandler interface {
	IsCompanionPath(path string) bool
	GetSize(ctx context.Context, path string) (int64, error)
	CopyFromCompanion(ctx context.Context, companionSrc, dst, style string, onProgress func(processed int64)) error
	CopyToCompanion(ctx context.Context, src, companionDst, style string, onProgress func(processed int64)) error
	DeleteCompanionPath(ctx context.Context, path string) error
}

var CompanionHandler CompanionIOHandler

func IsMounted(path string) bool {
	mounted, _ := mountinfo.Mounted(path)
	if mounted {
		return true
	}
	connections := MyService.Connections().GetConnectionsList()
	for _, v := range connections {
		if v.MountPoint == path {
			return true
		}
	}
	return false
}
