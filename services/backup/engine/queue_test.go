package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func jobState(t *testing.T, e *Engine, id JobID) JobState {
	t.Helper()
	st, err := e.JobStatus(context.Background(), id)
	must(t, err)
	return st.State
}

// TestEngineCapsConcurrentJobs: at most 2 jobs run, at most 1 of them
// touching a cloud remote; the others wait queued, and a cloud job
// waiting for its slot doesn't hold back a local one (spec §3.5).
func TestEngineCapsConcurrentJobs(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "eeeeeeee-aaaa-4bbb-8ccc-00000000000e", "ext4")
	dst := s.addVolume("dst", "ffffffff-aaaa-4bbb-8ccc-00000000000f", "ext4")
	cloudRoot := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloudRoot, 0o755))
	s.writeRcloneConf("[box]\ntype = alias\nremote = " + cloudRoot + "\n")
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})

	release := make(chan struct{})
	testHookBeforeWrite = func() { <-release }
	defer func() { testHookBeforeWrite = nil }()

	start := func(dest Endpoint, n int) JobID {
		req := baseReq(OpCopy, ep(src, "s"), dest)
		req.RunID = fmt.Sprintf("run_q%d", n)
		id, err := e.StartJob(context.Background(), req)
		must(t, err)
		return id
	}
	cloud1 := start(Endpoint{Kind: EPCloud, RefID: "box", SubPath: "one"}, 1)
	waitUntil(t, "the first cloud job to run", func() bool { return jobState(t, e, cloud1) == JobRunning })
	cloud2 := start(Endpoint{Kind: EPCloud, RefID: "box", SubPath: "two"}, 2)
	local1 := start(ep(dst, "l1"), 3)
	waitUntil(t, "the local job to run", func() bool { return jobState(t, e, local1) == JobRunning })
	local2 := start(ep(dst, "l2"), 4)
	if st := jobState(t, e, cloud2); st != JobQueued {
		t.Fatalf("second cloud job is %s, want queued (one cloud job at a time)", st)
	}
	if st := jobState(t, e, local2); st != JobQueued {
		t.Fatalf("third job is %s, want queued (two jobs at a time)", st)
	}
	// A queued job can be stopped before it starts.
	must(t, e.StopJob(context.Background(), local2))
	st := waitJob(t, e, local2)
	expectState(t, st, JobError, CodeCancelledByUser)
	if st.StartedAt != nil {
		t.Error("a job stopped in the queue has a start time")
	}

	close(release)
	for _, id := range []JobID{cloud1, cloud2, local1} {
		expectState(t, waitJob(t, e, id), JobDone, "")
	}
	if got := readTree(t, filepath.Join(cloudRoot, "two")); got["a"] != "1" {
		t.Fatalf("cloud copy = %v", got)
	}
}

func TestStartJobRejectsBadRequests(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	bad := []JobRequest{
		{Op: "explode", RunID: "r"},
		{Op: OpCopy},
		{Op: OpCopy, RunID: "r"},
		{Op: OpSync, RunID: "r", Sources: []Endpoint{ep(src, "a"), ep(src, "b")}, Dest: ep(dst, "")},
		{Op: OpArchive, RunID: "r", Sources: []Endpoint{ep(src, "")}, Dest: ep(dst, "")},
		{Op: OpPlan, RunID: "r", PlanOp: OpCheck, Sources: []Endpoint{ep(src, "")}},
		{Op: OpRestore, RunID: "r"},
		{Op: OpRestore, RunID: "r", Restore: &RestoreSpec{Conflict: "merge"}},
		{Op: OpPurgeArchives, RunID: "r"},
		{Op: OpPurgeDest, RunID: "r", Dest: ep(dst, "d")},
	}
	for i, req := range bad {
		if _, err := e.StartJob(context.Background(), req); CodeOf(err) != CodeInternal {
			t.Errorf("request %d (%s): err %v, want internal", i, req.Op, err)
		}
	}
	if _, err := e.JobStatus(context.Background(), 99999); CodeOf(err) != CodeNotFound {
		t.Errorf("unknown job: %v", err)
	}
}

func TestFinishedJobsAreDroppedAfterAnHour(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})
	st := runJob(t, e, baseReq(OpCopy, ep(src, "s"), ep(dst, "d")))
	e.jobs.sweep(st.EndedAt.Add(e.cfg.KeepFinished / 2))
	if _, err := e.JobStatus(context.Background(), st.ID); err != nil {
		t.Fatalf("dropped too early: %v", err)
	}
	e.jobs.sweep(st.EndedAt.Add(e.cfg.KeepFinished + 1))
	if _, err := e.JobStatus(context.Background(), st.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("still there after the keep time: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.SpoolDir, fmt.Sprintf("job-%d.ndjson", st.ID))); !os.IsNotExist(err) {
		t.Errorf("the log spool is still on disk: %v", err)
	}
}

func TestJobLogStreamsFromTheStart(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1", "b": "2"})
	st := runJob(t, e, baseReq(OpCopy, ep(src, "s"), ep(dst, "d")))
	lines := jobLog(t, e, st.ID)
	copied := 0
	for _, l := range lines {
		if l.T.IsZero() || l.Lvl == "" || l.Code == "" {
			t.Errorf("incomplete line %+v", l)
		}
		if l.Code == "copied" {
			copied++
		}
	}
	if copied != 2 {
		t.Fatalf("%d copied lines in %+v", copied, lines)
	}
	// A second reader gets the same lines again.
	if again := jobLog(t, e, st.ID); len(again) != len(lines) {
		t.Fatalf("second read: %d lines, first %d", len(again), len(lines))
	}
	if st.Stats.Files != 2 || st.Stats.Bytes != 2 {
		t.Errorf("final stats = %+v", st.Stats)
	}
}

func TestHealthAndClose(t *testing.T) {
	s := newTestSys(t)
	e := s.engine()
	h, err := e.Health(context.Background())
	must(t, err)
	if h.API != APIVersion || h.Rclone != "v1.75.1" || h.PID != os.Getpid() || h.StartedAt.IsZero() {
		t.Fatalf("health = %+v", h)
	}
	must(t, e.Close())
	if _, err := e.Health(context.Background()); CodeOf(err) != CodeEngineUnavailable {
		t.Fatalf("health after close: %v", err)
	}
	if _, err := e.StartJob(context.Background(), JobRequest{Op: OpPurgeVersions, RunID: "r"}); CodeOf(err) != CodeEngineUnavailable {
		t.Fatalf("start after close: %v", err)
	}
}
