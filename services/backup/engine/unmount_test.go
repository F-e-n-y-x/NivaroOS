package engine

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitUntil polls cond for up to 10 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestUnmountMidRunStopsEveryWrite unplugs the destination while files
// are being copied: the run ends cancelled_unmounted and no write is let
// through after the mount left the table (spec §6.2, §16.2).
func TestUnmountMidRunStopsEveryWrite(t *testing.T) {
	s, e, src, dst := twoVolumes(t)
	files := map[string]string{}
	for i := 0; i < 80; i++ {
		files[filepathJoin("f", i)] = strings.Repeat("y", 512)
	}
	writeTree(t, filepath.Join(src.MountPoint, "big"), files)

	// The hook runs at the start of every guard check. Check 6 unplugs
	// the drive and waits until the engine's table shows it, so checks
	// 1-5 are the only ones that may pass.
	// Checks running at the same time (several transfers) wait for the
	// unplug to finish too.
	const unplugAt = 6
	var checks atomic.Int64
	var once sync.Once
	gone := make(chan struct{})
	testHookBeforeWrite = func() {
		if checks.Add(1) < unplugAt {
			return
		}
		once.Do(func() {
			s.unmount(dst)
			s.notify()
			waitUntil(t, "the watcher to see the unmount", func() bool { return !e.mountPresent(dst.ID) })
			close(gone)
		})
		<-gone
	}
	defer func() { testHookBeforeWrite = nil }()

	req := baseReq(OpCopy, ep(src, "big"), ep(dst, "b"))
	req.RunID = "run_unplug"
	id, err := e.StartJob(context.Background(), req)
	must(t, err)
	st := waitJob(t, e, id)
	expectState(t, st, JobError, CodeCancelledUnmounted)
	// Everything in the folder (the marker included) came through one of
	// the checks before the unplug.
	if written := len(readTree(t, filepath.Join(dst.MountPoint, "b"))); written > unplugAt-1 {
		t.Fatalf("%d files in the destination, at most %d passed the guard before the unmount", written, unplugAt-1)
	}
}

// TestGuardRefusesAfterUnmount checks the guard itself: once the mount ID
// is gone every write, delete and mkdir fails, and the job is cancelled.
func TestGuardRefusesAfterUnmount(t *testing.T) {
	s, e, _, dst := twoVolumes(t)
	tgt, err := e.resolve(context.Background(), ep(dst, "g"), nil)
	must(t, err)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	j := &job{ctx: ctx, cancel: cancel}
	g := e.newGuard(j, tgt)
	f, err := tgt.fsAt(context.Background(), "", fsOpts{})
	must(t, err)
	gf := newGuardFs(context.Background(), f, g)
	must(t, gf.Mkdir(context.Background(), "ok"))

	s.unmount(dst)
	if _, err := e.fresh(); err != nil {
		t.Fatal(err)
	}
	if err := gf.Mkdir(context.Background(), "late"); CodeOf(err) != CodeCancelledUnmounted {
		t.Fatalf("mkdir after unmount: %v", err)
	}
	if err := gf.Rmdir(context.Background(), "ok"); CodeOf(err) != CodeCancelledUnmounted {
		t.Fatalf("rmdir after unmount: %v", err)
	}
	if CodeOf(context.Cause(ctx)) != CodeCancelledUnmounted {
		t.Fatalf("job cause = %v", context.Cause(ctx))
	}
	if g.writes.Load() != 0 {
		t.Fatalf("%d guarded writes counted", g.writes.Load())
	}
}

// TestUnmountOfSourceCancels: the mount watcher cancels a running job
// whose source drive left, even while nothing is being written.
func TestUnmountOfSourceCancels(t *testing.T) {
	s, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1", "b": "2"})
	release := make(chan struct{})
	var once sync.Once
	testHookBeforeWrite = func() {
		once.Do(func() {
			s.unmount(src)
			s.notify()
			<-release
		})
	}
	defer func() { testHookBeforeWrite = nil }()
	id, err := e.StartJob(context.Background(), withRun(baseReq(OpCopy, ep(src, "s"), ep(dst, "d")), "run_src_gone"))
	must(t, err)
	j, err := e.jobs.get(id)
	must(t, err)
	waitUntil(t, "the job to be cancelled", func() bool { return j.ctx.Err() != nil })
	close(release)
	st := waitJob(t, e, id)
	expectState(t, st, JobError, CodeCancelledUnmounted)
	if !strings.Contains(st.Result.ErrorDetail, "source") {
		t.Errorf("detail %q doesn't name the source", st.Result.ErrorDetail)
	}
}

// TestGuardChecksMarkerBeforeFirstDelete: writes pass, but the first
// delete (or move into the recycle folder) re-checks the marker, and a
// failed check stops every later delete and cancels the job.
func TestGuardChecksMarkerBeforeFirstDelete(t *testing.T) {
	_, e, _, dst := twoVolumes(t)
	tgt, err := e.resolve(context.Background(), ep(dst, "g"), nil)
	must(t, err)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls := 0
	g := e.newGuard(&job{ctx: ctx, cancel: cancel}, tgt)
	g.beforeDelete = func(context.Context) error {
		calls++
		return Errorf(CodeDestMarkerMismatch, "marker changed")
	}
	f, err := tgt.fsAt(context.Background(), "", fsOpts{})
	must(t, err)
	gf := newGuardFs(context.Background(), f, g)
	must(t, gf.Mkdir(context.Background(), "x"))
	must(t, gf.Mkdir(context.Background(), "y"))
	for _, dir := range []string{"x", "y"} {
		if err := gf.Rmdir(context.Background(), dir); CodeOf(err) != CodeDestMarkerMismatch {
			t.Fatalf("rmdir %s: %v", dir, err)
		}
	}
	if calls != 1 {
		t.Errorf("marker read %d times, want once", calls)
	}
	if CodeOf(context.Cause(ctx)) != CodeDestMarkerMismatch {
		t.Errorf("job cause %v", context.Cause(ctx))
	}
}
