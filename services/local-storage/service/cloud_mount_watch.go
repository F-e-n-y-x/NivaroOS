package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mount"
	rconfig "github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/lib/atexit"
	"go.uber.org/zap"
)

// The cloud mount watcher keeps every configured cloud drive mounted:
//
//   - a mount that disappears without being asked (its FUSE connection
//     closed, unmounted outside NivaroOS, or no longer the mount at its
//     path) is remounted, with backoff, and the log says why;
//   - a drive still mounted by the rclone daemon is taken over once the
//     daemon has no uploads pending for it (see rclone_daemon.go);
//   - an account added to rclone.conf by someone else (core's legacy
//     /v1/recover OAuth callback) gets mounted here, never by the daemon.
//
// A drive that was unmounted on purpose (UnmountStorage: removing the
// account, reconnecting it, applying cache settings) is left alone until
// MountStorage is called for it again.

type mountTarget struct {
	Name       string // rclone remote
	MountPoint string
}

// mountEnv is what the watcher needs from the process (fakes in tests).
type mountEnv interface {
	// Targets: accounts that should be mounted now (config sections with
	// a mount point, minus the ones unmounted on purpose).
	Targets() []mountTarget
	// Ours: this process has a live mount entry for mp.
	Ours(mp string) bool
	// OursGone: our entry for mp is there, but the kernel no longer has
	// that mount at mp. "" when it's fine, else why.
	OursGone(mp string) string
	// InKernel: anything is mounted at mp.
	InKernel(mp string) bool
	// Drop forgets our dead mount at mp (and tidies its VFS).
	Drop(mp string)
	Mount(t mountTarget) error
	// LazyUnmount detaches a stale mount nobody serves (a previous
	// local-storage that died).
	LazyUnmount(mp string)
	// ExitReason: why our last mount at mp ended, if it did.
	ExitReason(mp string) string
}

type watchState struct {
	missing  int // ticks seen missing in a row
	failures int
	next     time.Time
	reason   string
}

type handoverState struct {
	next     time.Time
	loggedAt time.Time
	failures int
}

type mountWatcher struct {
	env    mountEnv
	daemon rcloneDaemon
	now    func() time.Time
	sleep  func(time.Duration)

	// settle: after the daemon's mount is gone, how long its own
	// clean-up (rclone unmounts the path again when its serve loop ends)
	// gets before we mount there.
	settle time.Duration
	// unmountWait: how long the daemon's unmount may take to show.
	unmountWait time.Duration

	mu       sync.Mutex
	state    map[string]*watchState
	handover map[string]*handoverState
}

func newMountWatcher(env mountEnv, d rcloneDaemon) *mountWatcher {
	return &mountWatcher{
		env: env, daemon: d, now: time.Now, sleep: time.Sleep,
		settle: 5 * time.Second, unmountWait: 60 * time.Second,
		state: map[string]*watchState{}, handover: map[string]*handoverState{},
	}
}

// remountBackoff: 5 s, 10 s, 20 s ... up to 10 min between tries.
func remountBackoff(failures int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < failures && d < 10*time.Minute; i++ {
		d *= 2
	}
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// daemonMounts: the daemon's mounts by mount point; known is false when
// the daemon couldn't be asked (then nothing at those paths is touched).
func (w *mountWatcher) daemonMounts() (map[string]daemonMount, bool) {
	out := map[string]daemonMount{}
	if w.daemon == nil {
		return out, true
	}
	ms, err := w.daemon.ListMounts()
	if errors.Is(err, errDaemonDown) {
		return out, true
	}
	if err != nil {
		logger.Error("cloud mounts: couldn't ask the rclone daemon for its mounts; leaving its paths alone this round", zap.Error(err))
		return out, false
	}
	for _, m := range ms {
		out[filepath.Clean(m.MountPoint)] = m
	}
	return out, true
}

// takeOver unmounts one daemon mount if it has nothing left to upload and
// waits until it is really gone. true: the path is free now.
func (w *mountWatcher) takeOver(m daemonMount) bool {
	mp := filepath.Clean(m.MountPoint)
	h := w.handover[mp]
	if h == nil {
		h = &handoverState{}
		w.handover[mp] = h
	}
	now := w.now()
	if now.Before(h.next) {
		return false
	}
	pending, err := w.daemon.PendingUploads(m.Fs)
	if err != nil {
		h.failures++
		h.next = now.Add(remountBackoff(h.failures))
		logger.Error("cloud mounts: couldn't check the rclone daemon's uploads for a drive it still mounts; not taking it over yet",
			zap.String("mountPoint", mp), zap.String("fs", m.Fs), zap.Error(err), zap.Duration("retryIn", h.next.Sub(now)))
		return false
	}
	if pending > 0 {
		h.next = now.Add(30 * time.Second)
		if now.Sub(h.loggedAt) >= 5*time.Minute {
			h.loggedAt = now
			logger.Info("cloud mounts: the rclone daemon still mounts this drive and has uploads pending; it keeps serving it until they finish",
				zap.String("mountPoint", mp), zap.String("fs", m.Fs), zap.Int("pendingUploads", pending))
		}
		return false
	}
	logger.Info("cloud mounts: taking over a drive the rclone daemon mounts (local-storage owns cloud mounts)",
		zap.String("mountPoint", mp), zap.String("fs", m.Fs))
	if err := w.daemon.Unmount(mp); err != nil {
		h.failures++
		h.next = now.Add(remountBackoff(h.failures))
		logger.Error("cloud mounts: the rclone daemon couldn't unmount its copy of the drive; retrying later",
			zap.String("mountPoint", mp), zap.Error(err), zap.Duration("retryIn", h.next.Sub(now)))
		return false
	}
	deadline := w.now().Add(w.unmountWait)
	for {
		ms, err := w.daemon.ListMounts()
		listed := false
		if err == nil {
			for _, x := range ms {
				if filepath.Clean(x.MountPoint) == mp {
					listed = true
				}
			}
		}
		if (err == nil || errors.Is(err, errDaemonDown)) && !listed && !w.env.InKernel(mp) {
			break
		}
		if !w.now().Before(deadline) {
			h.failures++
			h.next = w.now().Add(remountBackoff(h.failures))
			logger.Error("cloud mounts: the rclone daemon's mount didn't go away in time; not mounting over it",
				zap.String("mountPoint", mp), zap.Duration("waited", w.unmountWait))
			return false
		}
		w.sleep(500 * time.Millisecond)
	}
	// rclone's mount goroutine unmounts the path once more when its serve
	// loop ends; let that happen before a new mount is there to hit.
	w.sleep(w.settle)
	delete(w.handover, mp)
	return true
}

// Tick runs one pass. Safe to call from one goroutine at a time.
func (w *mountWatcher) Tick() {
	w.mu.Lock()
	defer w.mu.Unlock()

	daemonAt, known := w.daemonMounts()
	targets := w.env.Targets()
	want := map[string]mountTarget{}
	for _, t := range targets {
		want[t.MountPoint] = t
	}

	// Hand every daemon mount over (only the ones we mount get mounted
	// again here; others just stop being served twice).
	for mp, m := range daemonAt {
		if w.takeOver(m) {
			delete(daemonAt, mp)
		}
	}
	for mp := range w.handover {
		if _, ok := daemonAt[mp]; !ok {
			delete(w.handover, mp)
		}
	}
	for mp := range w.state {
		if _, ok := want[mp]; !ok {
			delete(w.state, mp)
		}
	}

	for _, t := range targets {
		mp := t.MountPoint
		s := w.state[mp]
		if s == nil {
			s = &watchState{}
			w.state[mp] = s
		}
		if w.env.Ours(mp) {
			why := w.env.OursGone(mp)
			if why == "" {
				*s = watchState{}
				continue
			}
			s.missing++
			if s.missing < 2 {
				continue
			}
			logger.Error("cloud mounts: a drive's mount disappeared without being asked; remounting",
				zap.String("remote", t.Name), zap.String("mountPoint", mp), zap.String("why", why))
			s.reason = why
			w.env.Drop(mp)
			if _, daemonHas := daemonAt[mp]; daemonHas || !known {
				continue // the daemon's: handed over on a later tick
			}
		} else {
			if _, daemonHas := daemonAt[mp]; daemonHas || !known {
				continue
			}
			s.missing++
			if s.missing < 2 {
				continue
			}
			if s.reason == "" {
				if r := w.env.ExitReason(mp); r != "" {
					s.reason = r
				} else {
					s.reason = "not mounted"
				}
				if s.failures == 0 {
					logger.Error("cloud mounts: a drive isn't mounted; mounting it",
						zap.String("remote", t.Name), zap.String("mountPoint", mp), zap.String("why", s.reason))
				}
			}
		}
		now := w.now()
		if now.Before(s.next) {
			continue
		}
		if w.env.InKernel(mp) {
			// Not ours and not the daemon's: a dead mount left behind.
			logger.Info("cloud mounts: detaching a stale mount nobody serves", zap.String("mountPoint", mp))
			w.env.LazyUnmount(mp)
		}
		if err := w.env.Mount(t); err != nil {
			s.failures++
			s.next = now.Add(remountBackoff(s.failures))
			logger.Error("cloud mounts: remount failed; will retry",
				zap.String("remote", t.Name), zap.String("mountPoint", mp), zap.String("why", s.reason),
				zap.Int("attempt", s.failures), zap.Duration("retryIn", s.next.Sub(now)), zap.Error(err))
			continue
		}
		logger.Info("cloud mounts: remounted", zap.String("remote", t.Name), zap.String("mountPoint", mp),
			zap.String("why", s.reason), zap.Int("failedTriesBefore", s.failures))
		*s = watchState{}
	}
}

// ---- the real process environment ----

var (
	// heldMounts: unmounted on purpose; the watcher leaves them alone.
	heldMounts = map[string]bool{}
	// mountDevs: the device each of our mounts got (mount.TopMountDevice).
	mountDevs = map[string]string{}
	// mountExit: why our last mount at a path ended.
	mountExit = map[string]string{}

	cloudWatchStopping atomic.Bool
	cloudDaemon        rcloneDaemon = newSocketDaemon(RcloneDaemonSocket)
	cloudWatcher       *mountWatcher
)

type processMountEnv struct{}

func cloudMountPointOK(mp string) bool {
	return mp != "" && filepath.Clean(mp) == mp && filepath.Dir(mp) == "/mnt"
}

func (processMountEnv) Targets() []mountTarget {
	if cloudWatchStopping.Load() {
		return nil
	}
	var out []mountTarget
	for _, name := range rconfig.LoadedData().GetSectionList() {
		mp, _ := rconfig.LoadedData().GetValue(name, "mount_point")
		if !cloudMountPointOK(mp) {
			continue
		}
		mountMu.Lock()
		held := heldMounts[mp]
		mountMu.Unlock()
		if !held {
			out = append(out, mountTarget{Name: name, MountPoint: mp})
		}
	}
	return out
}

func (processMountEnv) Ours(mp string) bool {
	mountMu.Lock()
	defer mountMu.Unlock()
	return MountLists[mp] != nil
}

func (processMountEnv) OursGone(mp string) string {
	mountMu.Lock()
	dev := mountDevs[mp]
	mountMu.Unlock()
	cur, ok := mount.TopMountDevice(mp)
	switch {
	case !ok:
		return "not a mount point any more (FUSE mount gone)"
	case dev != "" && cur != dev:
		return "another mount is on top of it at the same path"
	}
	return ""
}

func (processMountEnv) InKernel(mp string) bool {
	_, ok := mount.TopMountDevice(mp)
	return ok
}

func (processMountEnv) Drop(mp string) {
	mountMu.Lock()
	mnt := MountLists[mp]
	delete(MountLists, mp)
	delete(mountDevs, mp)
	mountMu.Unlock()
	if mnt != nil {
		// Only tidies up: its unmount won't touch another mount at mp
		// (pkg/mount StillOurs).
		go func() { _ = mnt.Unmount() }()
	}
}

func (processMountEnv) Mount(t mountTarget) error {
	return MyService.Storage().MountStorage(t.MountPoint, t.Name)
}

func (processMountEnv) LazyUnmount(mp string) { lazyUmountPath(mp) }

func (processMountEnv) ExitReason(mp string) string {
	mountMu.Lock()
	defer mountMu.Unlock()
	return mountExit[mp]
}

// StartCloudMountWatcher runs the watcher every 5 s until ctx ends or the
// process is exiting.
func StartCloudMountWatcher(ctx context.Context) {
	atexit.Register(func() { cloudWatchStopping.Store(true) })
	w := newMountWatcher(processMountEnv{}, cloudDaemon)
	cloudWatcher = w
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !cloudWatchStopping.Load() {
					w.Tick()
				}
			}
		}
	}()
}
