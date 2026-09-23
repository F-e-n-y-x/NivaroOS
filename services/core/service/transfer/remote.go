package transfer

import (
	"context"
	"errors"
)

// Remote handles paths the local filesystem can't (companion devices). The
// engine treats each selected remote item as one unit: it can't plan
// per-file, but it still records a real outcome for it.
type Remote interface {
	Handles(path string) bool
	Size(ctx context.Context, path string) (int64, error)
	// Transfer copies src into the folder destDir (either side may be
	// remote). progress receives byte deltas. It must return an error if
	// anything inside src failed to arrive.
	Transfer(ctx context.Context, src, destDir string, conflict Conflict, progress func(int64)) error
	Delete(ctx context.Context, path string) error
}

var errNoRemote = errors.New("this location isn't available")

func (m *Manager) isRemote(path string) bool {
	return m.opts.Remote != nil && m.opts.Remote.Handles(path)
}

func (m *Manager) remoteSize(ctx context.Context, path string) (int64, error) {
	if m.opts.Remote == nil {
		return 0, errNoRemote
	}
	return m.opts.Remote.Size(ctx, path)
}

func (m *Manager) remoteDelete(ctx context.Context, path string) error {
	if m.opts.Remote == nil {
		return errNoRemote
	}
	return m.opts.Remote.Delete(ctx, path)
}

func (m *Manager) remoteTransfer(ctx context.Context, j *job, t topItem) error {
	if m.opts.Remote == nil {
		return errNoRemote
	}
	err := m.opts.Remote.Transfer(ctx, t.src, t.dst, j.Conflict, func(n int64) {
		m.update(j, func(j *job) { j.BytesDone += n })
	})
	if err != nil {
		return err
	}
	// A move removes the source only after the transfer reported full
	// success.
	if j.Kind == KindMove {
		if m.isRemote(t.src) {
			return m.opts.Remote.Delete(ctx, t.src)
		}
		return removeLocalTree(t.src)
	}
	return nil
}
