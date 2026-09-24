package engine

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// mountWatch keeps the engine's mount snapshot current (spec §7.3). It
// waits on POLLPRI of an open /proc/self/mountinfo, which the kernel
// raises on every change of the mount table, and re-reads the table on
// a backstop timer too. Every change is diffed by mount ID into
// volume.mounted / volume.unmounted events, and published so jobs whose
// mounts vanished are cancelled.
//
// It needs no help from usb-mount.sh, fstab tooling or local-storage:
// whatever mounts or unmounts, the table changes.
type mountWatch struct {
	e    *Engine
	last *snapshot // what the last diff saw

	waiter changeWaiter
	quit   chan struct{}
	done   chan struct{}
	once   sync.Once
}

// changeWaiter blocks until the mount table may have changed or the
// timeout passes. The real one polls the mountinfo fd; tests drive one by
// hand.
// wait must return soon after quit is closed.
type changeWaiter interface {
	wait(timeout time.Duration, quit <-chan struct{}) (changed bool, err error)
	close() error
}

// newChangeWaiter is how a watcher gets its waiter; tests replace it.
var newChangeWaiter = func(path string) (changeWaiter, error) { return openPollWaiter(path) }

func newMountWatch(e *Engine, initial *snapshot) *mountWatch {
	return &mountWatch{e: e, last: initial, quit: make(chan struct{}), done: make(chan struct{})}
}

func (w *mountWatch) start() error {
	waiter, err := newChangeWaiter(w.e.cfg.MountInfoPath)
	if err != nil {
		// Still usable: the backstop timer keeps the table current.
		w.e.cfg.Logf("engine: mount watcher falls back to a %s poll: %v", w.e.cfg.WatchBackstop, err)
		waiter = sleepWaiter{}
	}
	w.waiter = waiter
	go w.loop()
	return nil
}

func (w *mountWatch) stop() {
	w.once.Do(func() {
		close(w.quit)
		<-w.done
		if err := w.waiter.close(); err != nil {
			w.e.cfg.Logf("engine: closing mount watcher: %v", err)
		}
	})
}

func (w *mountWatch) loop() {
	defer close(w.done)
	for {
		select {
		case <-w.quit:
			return
		default:
		}
		if _, err := w.waiter.wait(w.e.cfg.WatchBackstop, w.quit); err != nil {
			w.e.cfg.Logf("engine: waiting for mount changes: %v", err)
			select {
			case <-w.quit:
				return
			case <-time.After(time.Second):
			}
		}
		select {
		case <-w.quit:
			return
		default:
		}
		w.refresh()
	}
}

// refresh re-reads the table, publishes it and emits the differences
// since the last refresh.
func (w *mountWatch) refresh() {
	s, err := w.e.takeSnapshot()
	if err != nil {
		w.e.cfg.Logf("engine: %v", err)
		return
	}
	w.e.publish(s)
	for _, ev := range diffVolumes(w.last, s, w.e.now()) {
		w.e.subs.emit(ev)
	}
	w.last = s
}

// diffVolumes turns two snapshots into mount events: a mount ID that
// appeared is mounted, one that disappeared is unmounted.
func diffVolumes(old, cur *snapshot, now time.Time) []Event {
	var out []Event
	prev := map[int]Volume{}
	for _, v := range old.vols {
		prev[v.vol.MountID] = v.vol
	}
	next := map[int]bool{}
	for _, v := range cur.vols {
		next[v.vol.MountID] = true
		if _, ok := prev[v.vol.MountID]; !ok {
			vol := v.vol
			out = append(out, Event{Type: EventVolumeMounted, Time: now, Volume: &vol})
		}
	}
	for _, v := range old.vols {
		if !next[v.vol.MountID] {
			vol := v.vol
			out = append(out, Event{Type: EventVolumeUnmounted, Time: now, Volume: &vol})
		}
	}
	return out
}

// pollWaiter polls an open file for POLLPRI|POLLERR: the kernel sets
// them on /proc/self/mountinfo whenever the mount namespace changes, and
// clears them as poll reports them.
type pollWaiter struct {
	f *os.File
}

func openPollWaiter(path string) (*pollWaiter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &pollWaiter{f: f}, nil
}

// pollFD waits for POLLPRI or POLLERR on fd, up to timeout.
func pollFD(fd int, timeout time.Duration) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLPRI | unix.POLLERR}}
	for {
		n, err := unix.Poll(fds, int(timeout/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err
		}
		return n > 0 && fds[0].Revents&(unix.POLLPRI|unix.POLLERR) != 0, nil
	}
}

func (p *pollWaiter) wait(timeout time.Duration, quit <-chan struct{}) (bool, error) {
	// Poll in short slices so a stop is noticed quickly.
	const slice = time.Second
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return false, nil
		}
		if left > slice {
			left = slice
		}
		changed, err := pollFD(int(p.f.Fd()), left)
		if err != nil || changed {
			return changed, err
		}
		select {
		case <-quit:
			return false, nil
		default:
		}
	}
}

func (p *pollWaiter) close() error { return p.f.Close() }

// sleepWaiter is the fallback when mountinfo can't be polled.
type sleepWaiter struct{}

func (sleepWaiter) wait(timeout time.Duration, quit <-chan struct{}) (bool, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-quit:
	case <-t.C:
	}
	return false, nil
}

func (sleepWaiter) close() error { return nil }
