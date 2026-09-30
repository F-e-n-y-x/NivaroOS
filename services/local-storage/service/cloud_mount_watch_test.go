package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeKernel is the mount table plus our live entries.
type fakeEnv struct {
	targets  []mountTarget
	ours     map[string]bool
	kernel   map[string]string // mp -> who: "ours", "daemon", "stale"
	gone     map[string]string // OursGone reasons
	mountErr error
	mounts   []string
	lazy     []string
	dropped  []string
	exit     map[string]string
}

func newFakeEnv(t ...mountTarget) *fakeEnv {
	return &fakeEnv{targets: t, ours: map[string]bool{}, kernel: map[string]string{}, gone: map[string]string{}, exit: map[string]string{}}
}

func (e *fakeEnv) Targets() []mountTarget      { return e.targets }
func (e *fakeEnv) Ours(mp string) bool         { return e.ours[mp] }
func (e *fakeEnv) OursGone(mp string) string   { return e.gone[mp] }
func (e *fakeEnv) InKernel(mp string) bool     { _, ok := e.kernel[mp]; return ok }
func (e *fakeEnv) ExitReason(mp string) string { return e.exit[mp] }
func (e *fakeEnv) LazyUnmount(mp string)       { e.lazy = append(e.lazy, mp); delete(e.kernel, mp) }
func (e *fakeEnv) Drop(mp string) {
	e.dropped = append(e.dropped, mp)
	delete(e.ours, mp)
	delete(e.gone, mp)
}
func (e *fakeEnv) Mount(t mountTarget) error {
	e.mounts = append(e.mounts, t.MountPoint)
	if e.mountErr != nil {
		return e.mountErr
	}
	if who := e.kernel[t.MountPoint]; who != "" {
		return errors.New("directory already mounted by " + who)
	}
	e.ours[t.MountPoint] = true
	e.kernel[t.MountPoint] = "ours"
	return nil
}

type fakeDaemon struct {
	env      *fakeEnv
	mounts   []daemonMount
	pending  map[string]int
	down     bool
	listErr  error
	unmounts []string
	// lingerTicks: ListMounts calls during which an unmounted mount still
	// shows in the kernel (rclone closing it).
	linger int
}

func (d *fakeDaemon) ListMounts() ([]daemonMount, error) {
	if d.down {
		return nil, errDaemonDown
	}
	if d.listErr != nil {
		return nil, d.listErr
	}
	if d.linger > 0 {
		d.linger--
		if d.linger == 0 {
			for mp, who := range d.env.kernel {
				if who == "daemon-closing" {
					delete(d.env.kernel, mp)
				}
			}
		}
	}
	return append([]daemonMount(nil), d.mounts...), nil
}
func (d *fakeDaemon) PendingUploads(fs string) (int, error) { return d.pending[fs], nil }
func (d *fakeDaemon) Unmount(mp string) error {
	d.unmounts = append(d.unmounts, mp)
	var keep []daemonMount
	for _, m := range d.mounts {
		if m.MountPoint != mp {
			keep = append(keep, m)
		}
	}
	d.mounts = keep
	if d.linger > 0 {
		d.env.kernel[mp] = "daemon-closing"
	} else {
		delete(d.env.kernel, mp)
	}
	return nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) sleep(d time.Duration)   { c.t = c.t.Add(d) }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func testWatcher(env *fakeEnv, d rcloneDaemon) (*mountWatcher, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_790_000_000, 0)}
	w := newMountWatcher(env, d)
	w.now, w.sleep = c.now, c.sleep
	return w, c
}

var gdrive = mountTarget{Name: "google_drive_drive_1", MountPoint: "/mnt/google_drive_drive_1"}

func TestWatcherLeavesHealthyMountAlone(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.ours[gdrive.MountPoint] = true
	env.kernel[gdrive.MountPoint] = "ours"
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})
	for i := 0; i < 5; i++ {
		w.Tick()
		c.advance(5 * time.Second)
	}
	if len(env.mounts)+len(env.dropped)+len(env.lazy) != 0 {
		t.Fatalf("touched a healthy mount: mounts=%v dropped=%v lazy=%v", env.mounts, env.dropped, env.lazy)
	}
}

func TestWatcherRemountsVanishedMount(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.ours[gdrive.MountPoint] = true
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})
	env.gone[gdrive.MountPoint] = "not a mount point any more (FUSE mount gone)"

	w.Tick() // first sighting: confirm on the next tick
	if len(env.mounts) != 0 {
		t.Fatal("remounted on the first sighting")
	}
	c.advance(5 * time.Second)
	w.Tick()
	if len(env.dropped) != 1 || len(env.mounts) != 1 || !env.ours[gdrive.MountPoint] {
		t.Fatalf("want drop+remount, got dropped=%v mounts=%v", env.dropped, env.mounts)
	}
	if st := w.state[gdrive.MountPoint]; st.failures != 0 || st.reason != "" {
		t.Fatalf("state not reset after a good remount: %+v", st)
	}
}

func TestWatcherBacksOffFailingRemount(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.mountErr = errors.New("token expired")
	env.exit[gdrive.MountPoint] = "the mount ended by itself"
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})

	// Tick every 5 s for 2 minutes: tries at +5s, then 5, 10, 20, 40 s apart.
	for i := 0; i < 25; i++ {
		w.Tick()
		c.advance(5 * time.Second)
	}
	if n := len(env.mounts); n < 4 || n > 6 {
		t.Fatalf("want ~5 tries with backoff over 2 min, got %d", n)
	}
	st := w.state[gdrive.MountPoint]
	if st.reason != "the mount ended by itself" {
		t.Fatalf("reason not kept for the log: %q", st.reason)
	}
	if remountBackoff(1) != 5*time.Second || remountBackoff(3) != 20*time.Second || remountBackoff(50) != 10*time.Minute {
		t.Fatal("backoff schedule changed")
	}
	env.mountErr = nil
	c.advance(10 * time.Minute)
	w.Tick()
	if !env.ours[gdrive.MountPoint] || w.state[gdrive.MountPoint].failures != 0 {
		t.Fatal("didn't recover once mounting works again")
	}
}

func TestWatcherSkipsHeldMounts(t *testing.T) {
	env := newFakeEnv() // held mounts aren't targets
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})
	for i := 0; i < 3; i++ {
		w.Tick()
		c.advance(5 * time.Second)
	}
	if len(env.mounts) != 0 {
		t.Fatal("mounted a drive that was unmounted on purpose")
	}
}

// The 2026-09-30 incident: the daemon still mounted the drive. The watcher
// must not mount over it, and must wait for the daemon's mount to be gone
// (and settle) before mounting its own.
func TestWatcherTakesOverDaemonMountSafely(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.kernel[gdrive.MountPoint] = "daemon"
	d := &fakeDaemon{env: env, mounts: []daemonMount{{Fs: gdrive.Name + ":", MountPoint: gdrive.MountPoint}},
		pending: map[string]int{gdrive.Name + ":": 3}}
	w, c := testWatcher(env, d)

	for i := 0; i < 4; i++ {
		w.Tick()
		c.advance(5 * time.Second)
	}
	if len(d.unmounts) != 0 || len(env.mounts) != 0 || len(env.lazy) != 0 {
		t.Fatalf("touched the daemon's mount while it had uploads pending: unmounts=%v mounts=%v lazy=%v", d.unmounts, env.mounts, env.lazy)
	}

	// Uploads done; the daemon's mount takes a few polls to close.
	d.pending[gdrive.Name+":"] = 0
	d.linger = 3
	c.advance(30 * time.Second)
	start := c.t
	w.Tick()
	if len(d.unmounts) != 1 {
		t.Fatalf("want one daemon unmount, got %v", d.unmounts)
	}
	if env.kernel[gdrive.MountPoint] == "daemon-closing" {
		t.Fatal("returned while the daemon's mount was still closing")
	}
	if c.t.Sub(start) < w.settle {
		t.Fatalf("didn't let the daemon settle: waited %s", c.t.Sub(start))
	}
	if len(env.lazy) != 0 {
		t.Fatal("lazy-detached the daemon's mount")
	}
	// Taken over; our mount follows after the usual confirmation tick.
	c.advance(5 * time.Second)
	w.Tick()
	if !env.ours[gdrive.MountPoint] || len(env.mounts) != 1 {
		t.Fatalf("not mounted by local-storage after the handover: mounts=%v", env.mounts)
	}
}

func TestWatcherDaemonUnknownLeavesPathAlone(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.kernel[gdrive.MountPoint] = "daemon"
	d := &fakeDaemon{env: env, listErr: errors.New("timeout")}
	w, c := testWatcher(env, d)
	for i := 0; i < 4; i++ {
		w.Tick()
		c.advance(5 * time.Second)
	}
	if len(env.lazy)+len(env.mounts) != 0 {
		t.Fatal("touched a path while the daemon couldn't be asked")
	}
}

func TestWatcherDetachesStaleMountWhenDaemonDown(t *testing.T) {
	env := newFakeEnv(gdrive)
	env.kernel[gdrive.MountPoint] = "stale" // left by a crashed process
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})
	w.Tick()
	c.advance(5 * time.Second)
	w.Tick()
	if len(env.lazy) != 1 || !env.ours[gdrive.MountPoint] {
		t.Fatalf("want stale mount detached then remounted: lazy=%v mounts=%v", env.lazy, env.mounts)
	}
}

func TestWatcherHandsOverUntargetedDaemonMounts(t *testing.T) {
	env := newFakeEnv() // e.g. an account removed from rclone.conf
	env.kernel["/mnt/old"] = "daemon"
	d := &fakeDaemon{env: env, mounts: []daemonMount{{Fs: "old:", MountPoint: "/mnt/old"}}, pending: map[string]int{}}
	w, _ := testWatcher(env, d)
	w.Tick()
	if len(d.unmounts) != 1 || len(env.mounts) != 0 {
		t.Fatalf("want the daemon's extra mount unmounted and nothing mounted: %v %v", d.unmounts, env.mounts)
	}
}

func TestCloudMountPointOK(t *testing.T) {
	for mp, want := range map[string]bool{"/mnt/a": true, "/mnt/a/b": false, "/mnt/../etc": false, "": false, "/media/a": false} {
		if got := cloudMountPointOK(mp); got != want {
			t.Errorf("%q: %v", mp, got)
		}
	}
	if !strings.HasPrefix(RcloneDaemonSocket, "/var/run/rclone/") {
		t.Fatal("socket moved")
	}
}
