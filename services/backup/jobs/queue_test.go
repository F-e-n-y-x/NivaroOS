package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The queue (spec §8.1, §16.1 queue_test.go): coalescing, priority, the
// worker cap, destination prefix locks, restore versus backup, app locks
// and IsBusy with Scheduled Tasks.

// runningJobs returns the job ids of the engine jobs started so far.
func (h *harness) startedJobIDs() []string {
	var out []string
	for _, c := range h.eng.CallsTo("StartJob") {
		out = append(out, c.Req.(engine.JobRequest).JobID)
	}
	return out
}

// settle gives the dispatcher time to start whatever it would start.
func settle() { time.Sleep(60 * time.Millisecond) }

func jobTo(name, destSub string) Job {
	j := sampleJob(name)
	j.Sources[0].SubPath = "DATA/" + name
	j.Dest.SubPath = destSub
	return j
}

func TestCoalescing(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(sampleJob("Docs"))
	first := h.run(j, KindBackup)
	h.startedJob(1)
	// While it runs, every trigger lands on the same run.
	for _, by := range []RunTrigger{RunBySchedule, RunByVolumeMounted, RunByManual} {
		id, coalesced, err := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: by})
		if err != nil || !coalesced || id != first {
			t.Fatalf("%s: %s %v %v", by, id, coalesced, err)
		}
	}
	// A preview request is a run of the same job too.
	if id, coalesced, _ := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindPreview, Trigger: RunByManual}); !coalesced || id != first {
		t.Errorf("preview not coalesced: %s", id)
	}
	r, _ := h.svc.store.GetRun(first)
	if r.Coalesced != 4 {
		t.Errorf("coalesced = %d, want 4", r.Coalesced)
	}
	h.finish(1, okResult(1))
	r = h.waitStatus(first, StatusSuccess)
	// The worker's final save kept the count.
	if r.Coalesced != 4 {
		t.Errorf("coalesced after the run = %d", r.Coalesced)
	}
	// A restore is never coalesced onto a backup.
	restore, coalesced, _ := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual, RestoreSpec: restoreSpecJSON(j.Sources[0])})
	if coalesced || restore == first {
		t.Error("restore coalesced")
	}
	// And after the run ended, a trigger queues a new run.
	next, coalesced, _ := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule})
	if coalesced || next == first {
		t.Error("a trigger after the run ended was coalesced")
	}
}

func restoreSpecJSON(target Endpoint) string {
	raw, _ := json.Marshal(engine.RestoreSpec{VersionID: "current", Target: target, Conflict: engine.ConflictKeepBoth})
	return string(raw)
}

func TestPriorityAndConcurrencyCap(t *testing.T) {
	h := newHarness(t, false)
	// One worker, so the order is visible.
	s := DefaultAppSettings()
	s.MaxConcurrent = 1
	h.svc.store.SaveSettings(s)
	catch := h.createJob(jobTo("catch", "B/catch"))
	sched := h.createJob(jobTo("sched", "B/sched"))
	retry := h.createJob(jobTo("retry", "B/retry"))
	manual := h.createJob(jobTo("manual", "B/manual"))
	sched2 := h.createJob(jobTo("sched2", "B/sched2"))
	for _, x := range []struct {
		j  Job
		by RunTrigger
	}{{catch, RunByCatchUp}, {sched, RunBySchedule}, {retry, RunByRetry}, {sched2, RunBySchedule}, {manual, RunByManual}} {
		if _, _, err := h.svc.queue.Enqueue(x.j, EnqueueOptions{Kind: KindBackup, Trigger: x.by}); err != nil {
			t.Fatal(err)
		}
	}
	h.start()
	want := []string{manual.ID, retry.ID, sched.ID, sched2.ID, catch.ID}
	for i := range want {
		h.startedJob(i + 1)
		settle()
		if n := len(h.eng.CallsTo("StartJob")); n != i+1 {
			t.Fatalf("%d engine jobs with one worker, want %d", n, i+1)
		}
		h.finish(i+1, okResult(1))
	}
	got := h.startedJobIDs()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}

	// Raising the cap lets more run at once, the default is two.
	s.MaxConcurrent = 3
	h.svc.store.SaveSettings(s)
	for _, j := range []Job{catch, sched, retry, manual} {
		h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunByManual})
	}
	h.svc.queue.wake()
	h.startedJob(8)
	settle()
	if n := len(h.eng.CallsTo("StartJob")); n != 8 {
		t.Errorf("%d engine jobs with three workers, want 8", n)
	}
}

func TestDestinationPrefixLock(t *testing.T) {
	h := newHarness(t, true)
	parent := h.createJob(jobTo("parent", "Backups"))
	child := h.createJob(jobTo("child", "Backups/child"))
	sibling := h.createJob(jobTo("sibling", "Other"))
	h.run(parent, KindBackup)
	h.startedJob(1)
	childRun := h.run(child, KindBackup)
	sib := h.run(sibling, KindBackup)
	h.startedJob(2)
	settle()
	if got := h.startedJobIDs(); len(got) != 2 || got[1] != sibling.ID {
		t.Fatalf("started %v: the overlapping destination ran alongside", got)
	}
	if r, _ := h.svc.store.GetRun(childRun); RunStatus(r.Status) != StatusQueued {
		t.Errorf("child run %s, want queued", r.Status)
	}
	h.finish(1, okResult(1))
	h.startedJob(3)
	h.finish(2, okResult(1))
	h.waitStatus(sib, StatusSuccess)
	if got := h.startedJobIDs(); got[2] != child.ID {
		t.Errorf("after the parent ended: %v", got)
	}
}

func TestRestoreWaitsForBackupOfItsTarget(t *testing.T) {
	h := newHarness(t, true)
	docs := h.createJob(sampleJob("Docs"))
	h.run(docs, KindBackup)
	h.startedJob(1)
	// A restore of another job into the folder the backup is reading.
	other := h.createJob(jobTo("other", "Elsewhere"))
	target := Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Documents/restored"}
	rid, _, err := h.svc.queue.Enqueue(other, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual, RestoreSpec: restoreSpecJSON(target)})
	if err != nil {
		t.Fatal(err)
	}
	settle()
	if n := len(h.eng.CallsTo("StartJob")); n != 1 {
		t.Fatalf("restore into a folder being backed up started (%d jobs)", n)
	}
	h.finish(1, okResult(1))
	_, req := h.startedJob(2)
	if req.Op != engine.OpRestore {
		t.Errorf("second job %s", req.Op)
	}
	h.finish(2, engine.Result{Counts: engine.Counts{Added: 1}})
	h.waitStatus(rid, StatusSuccess)

	// And a restore of the same job reads its destination: a backup
	// writing there waits for it.
	restore, _, _ := h.svc.queue.Enqueue(docs, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual,
		RestoreSpec: restoreSpecJSON(Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Restores"})})
	h.startedJob(3)
	h.run(docs, KindBackup)
	settle()
	if n := len(h.eng.CallsTo("StartJob")); n != 3 {
		t.Fatalf("backup started while a restore read its destination")
	}
	h.finish(3, engine.Result{})
	h.waitStatus(restore, StatusSuccess)
	h.startedJob(4)
}

func TestAppLockSerialisesJobs(t *testing.T) {
	h := newHarness(t, true)
	hook := []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	a := jobTo("a", "A")
	a.Hooks = hook
	b := jobTo("b", "B")
	b.Hooks = hook
	ja, jb := h.createJob(a), h.createJob(b)
	h.run(ja, KindBackup)
	h.startedJob(1)
	rb := h.run(jb, KindBackup)
	settle()
	if n := len(h.eng.CallsTo("StartJob")); n != 1 {
		t.Fatal("two jobs stopping the same app ran together")
	}
	// Backup's side of the lock contract.
	if !h.svc.queue.IsBusy(LockApp, "immich") || h.svc.queue.IsBusy(LockApp, "blinko") || h.svc.queue.IsBusy(LockVM, "immich") {
		t.Error("IsBusy doesn't match the running hooks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := h.svc.queue.WaitFree(ctx, LockApp, "immich"); err == nil {
		t.Error("WaitFree returned while busy")
	}
	cancel()
	h.finish(1, okResult(1))
	h.startedJob(2)
	h.finish(2, okResult(1))
	h.waitStatus(rb, StatusSuccess)
	if h.svc.queue.IsBusy(LockApp, "immich") {
		t.Error("still busy after both ended")
	}
	if err := h.svc.queue.WaitFree(context.Background(), LockApp, "immich"); err != nil {
		t.Error(err)
	}
}

func TestHookWaitsForScheduledTask(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Docs")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	h.busy.set(LockApp, "immich", true)
	runID := h.run(job, KindBackup)
	h.waitStatus(runID, StatusRunning)
	time.Sleep(40 * time.Millisecond)
	if len(h.apps.Calls()) != 0 || len(h.eng.CallsTo("StartJob")) != 0 {
		t.Fatal("hook ran while a Scheduled Task held the app")
	}
	h.busy.set(LockApp, "immich", false)
	h.finish(1, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	if calls := h.apps.Calls(); len(calls) != 2 || calls[0] != "stop immich" || calls[1] != "start immich" {
		t.Errorf("app calls %v", calls)
	}
}

func TestScheduleBusyChecker(t *testing.T) {
	f := &fakeSchedules{tasks: []map[string]interface{}{
		{"id": "t1", "type": "vm", "target_id": "win11", "last_status": "running"},
		{"id": "t2", "type": "container", "target_name": "immich", "last_status": "success"},
		{"id": "t3", "type": "container", "target": "blinko", "last_status": "running"},
	}}
	b := &ScheduleBusy{Client: f, Poll: time.Millisecond}
	if !b.IsBusy(LockVM, "win11") || b.IsBusy(LockApp, "win11") || b.IsBusy(LockApp, "immich") || !b.IsBusy(LockApp, "blinko") {
		t.Error("ScheduleBusy answers wrong")
	}
	if b.IsBusy("disk", "sda") || b.IsBusy(LockVM, "") {
		t.Error("unknown kinds or empty targets are never busy")
	}
	// WaitFree re-reads core until the task finishes.
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.mu.Lock()
		f.tasks[0]["last_status"] = "success"
		f.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The snapshot is cached; WaitFree must refresh it.
	if err := b.WaitFree(ctx, LockVM, "win11"); err != nil {
		t.Fatalf("WaitFree: %v", err)
	}
	// Core down: nothing is busy.
	down := &ScheduleBusy{Client: &fakeSchedules{listErr: errUnreachable}}
	if down.IsBusy(LockVM, "win11") {
		t.Error("busy while core is down")
	}
}

func TestQueueSurvivesRestart(t *testing.T) {
	h := newHarness(t, false)
	a := h.createJob(jobTo("a", "A"))
	b := h.createJob(jobTo("b", "B"))
	ra, _, _ := h.svc.queue.Enqueue(a, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule})
	rb, _, _ := h.svc.queue.Enqueue(b, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule})
	// A fresh queue (a restart) rebuilds itself from the store; one
	// worker makes the order visible.
	s := DefaultAppSettings()
	s.MaxConcurrent = 1
	h.svc.store.SaveSettings(s)
	h.svc.queue = newQueue(h.svc)
	h.start()
	h.finish(1, okResult(1))
	h.finish(2, okResult(1))
	got := h.startedJobIDs()
	if got[0] != a.ID || got[1] != b.ID {
		t.Errorf("FIFO lost across restart: %v", got)
	}
	h.waitStatus(ra, StatusSuccess)
	h.waitStatus(rb, StatusSuccess)
}

func TestQueuedRunOfDeletedJobEnds(t *testing.T) {
	h := newHarness(t, false)
	j := h.createJob(sampleJob("Docs"))
	id, _, _ := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule})
	// The job row disappears under the queued run (a crash during delete).
	h.svc.store.db.Delete(&JobRow{}, "id = ?", j.ID)
	h.start()
	r := h.waitStatus(id, StatusCancelled)
	if ErrorCode(r.ErrorCode) != ErrNotFound {
		t.Errorf("orphan run code %q", r.ErrorCode)
	}
}

func TestCancelQueuedRun(t *testing.T) {
	h := newHarness(t, false)
	j := h.createJob(sampleJob("Docs"))
	id, _, _ := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule, NotBefore: time.Now().Add(time.Hour)})
	h.start()
	r, err := h.svc.queue.Cancel(id, ErrCancelledByUser)
	if err != nil || RunStatus(r.Status) != StatusCancelled || r.EndedAt == nil {
		t.Fatalf("cancel queued: %+v %v", r, err)
	}
	if _, err := h.svc.queue.Cancel(id, ErrCancelledByUser); err == nil {
		t.Error("cancel of an ended run succeeded")
	}
	settle()
	if n := len(h.eng.CallsTo("StartJob")); n != 0 {
		t.Error("cancelled run started anyway")
	}
	if ends := h.busEvents(EventRunEnd, 1); len(ends) != 1 || ends[0][PropStatus] != string(StatusCancelled) {
		t.Errorf("run-end events %v", ends)
	}
}

func TestLockConflicts(t *testing.T) {
	ep := func(sub string) Endpoint { return Endpoint{Kind: EPVolume, RefID: "u", SubPath: sub} }
	cases := []struct {
		a, b lockKey
		want bool
	}{
		{writeLock(ep("a")), writeLock(ep("a/b")), true},
		{writeLock(ep("a")), readLock(ep("a")), true},
		{readLock(ep("a")), readLock(ep("a")), false},
		{writeLock(ep("a")), writeLock(ep("ab")), false},
		{appLock("x"), appLock("x"), true},
		{appLock("x"), vmLock("x"), false},
		{appLock("x"), writeLock(ep("x")), false},
		{jobLock("bk_1"), jobLock("bk_2"), false},
	}
	for _, c := range cases {
		if got := c.a.conflicts(c.b); got != c.want {
			t.Errorf("%s vs %s = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := c.b.conflicts(c.a); got != c.want {
			t.Errorf("%s vs %s (reversed) = %v", c.b, c.a, got)
		}
	}
}
