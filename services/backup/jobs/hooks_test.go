package jobs

import (
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Hooks (spec §8.2, §16.1 hooks_test.go): prior state respected, the
// one-at-a-time split, a VM shutdown timeout that never forces it off.

func TestHooksRespectPriorState(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Apps")
	j.Hooks = []Hook{
		{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich", "stopped-app"}},
		{Phase: HookPre, Action: HookShutdownVM, VM: "offvm"},
		// Explicit post hooks for the same things: they must not start
		// what was off before the run.
		{Phase: HookPost, Action: HookStartApps, Apps: []string{"stopped-app", "immich"}},
		{Phase: HookPost, Action: HookStartVM, VM: "offvm"},
	}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.startedJob(1)
	if h.apps.isRunning("immich") {
		t.Fatal("immich runs during the transfer")
	}
	r, _ := h.svc.store.GetRun(runID)
	hd := decodeHooksDone(r.HooksDone)
	if len(hd) != 2 || hd[0].PriorState["immich"] != priorRunning || hd[0].PriorState["stopped-app"] != priorStopped ||
		hd[1].PriorState[vmStateKey("offvm")] != priorShutOff || !hd[0].PreDone {
		t.Fatalf("HooksDone recorded before the transfer: %+v", hd)
	}
	h.finish(1, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	if !h.apps.isRunning("immich") || h.apps.isRunning("stopped-app") {
		t.Error("post hooks didn't restore the prior state")
	}
	for _, c := range h.apps.Calls() {
		if c == "start stopped-app" || c == "stop stopped-app" {
			t.Errorf("touched an app that was off: %v", h.apps.Calls())
		}
	}
	if calls := h.vms.Calls(); len(calls) != 0 {
		t.Errorf("touched a VM that was off: %v", calls)
	}
	// immich was started once, by the undo, not again by the post hook.
	n := 0
	for _, c := range h.apps.Calls() {
		if c == "start immich" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("immich started %d times: %v", n, h.apps.Calls())
	}
}

func TestExplicitPostHookStandsAlone(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Wake app")
	j.Hooks = []Hook{{Phase: HookPost, Action: HookStartApps, Apps: []string{"stopped-app"}}}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.finish(1, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	if !h.apps.isRunning("stopped-app") {
		t.Error("a post hook with no matching pre hook didn't run")
	}
}

func TestOneAtATimeSplitsTheArchive(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("App data")
	j.Type = TypeArchive
	j.Sources = []Endpoint{
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/immich", Preset: "appdata:immich"},
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/blinko", Preset: "appdata:blinko"},
	}
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich", "blinko"}, AppMode: AppModeOneAtATime}}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)

	_, req := h.startedJob(1)
	if len(req.Sources) != 1 || req.Sources[0].Preset != "appdata:immich" || req.Op != engine.OpArchive {
		t.Fatalf("first sub-transfer %+v", req.Sources)
	}
	if h.apps.isRunning("immich") || !h.apps.isRunning("blinko") {
		t.Fatal("wrong app stopped for the first sub-transfer")
	}
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 1, BytesTransferred: 10}, ArchiveName: "a1.tar.zst"})
	_, req = h.startedJob(2)
	if len(req.Sources) != 1 || req.Sources[0].Preset != "appdata:blinko" {
		t.Fatalf("second sub-transfer %+v", req.Sources)
	}
	if !h.apps.isRunning("immich") || h.apps.isRunning("blinko") {
		t.Fatal("immich not back before blinko stopped")
	}
	h.finish(2, engine.Result{Counts: engine.Counts{Added: 2, BytesTransferred: 20}, ArchiveName: "a2.tar.zst"})
	// Then the archives past keep_last are pruned, once for the run.
	_, req = h.startedJob(3)
	if req.Op != engine.OpPurgeArchives || req.Retention.KeepLast != DefaultKeepLast {
		t.Fatalf("prune %+v", req)
	}
	h.finish(3, engine.Result{})
	r := h.waitStatus(runID, StatusSuccess)
	want := []string{"stop immich", "start immich", "stop blinko", "start blinko"}
	if got := h.apps.Calls(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("app calls %v, want %v", got, want)
	}
	if r.FilesAdded != 3 || r.BytesTransferred != 30 {
		t.Errorf("counts not summed: added %d bytes %d", r.FilesAdded, r.BytesTransferred)
	}
}

func TestVMShutdownTimeoutNeverForcesOff(t *testing.T) {
	h := newHarness(t, true)
	h.vms.noACPI["win11"] = true
	j := sampleJob("VM")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookShutdownVM, VM: "win11"}}
	job := h.createJob(j)
	job = h.mutate(job, func(j *Job) { j.Hooks[0].TimeoutSec = 1 })
	runID := h.run(job, KindBackup)
	r := h.waitStatus(runID, StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrVMShutdownTimeout {
		t.Errorf("code %q", r.ErrorCode)
	}
	if calls := h.vms.Calls(); len(calls) != 1 || calls[0] != "shutdown win11" {
		t.Errorf("VM calls %v: only a graceful shutdown is allowed", calls)
	}
	if st, _ := h.vms.State(nil, "win11"); st != vmRunning {
		t.Errorf("VM %s after the timeout", st)
	}
	if len(h.eng.CallsTo("StartJob")) != 0 {
		t.Error("copied although the VM was still running")
	}
	if !h.notified("backup.notify.failed") {
		t.Error("no failure notification")
	}
}

func TestVMHookShutsDownAndStarts(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("VM")
	j.Sources[0].Preset = "vm:win11"
	j.Hooks = []Hook{{Phase: HookPre, Action: HookShutdownVM, VM: "win11"}}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.startedJob(1)
	if st, _ := h.vms.State(nil, "win11"); st != vmShutOff {
		t.Fatalf("VM %s during the copy", st)
	}
	if !h.svc.queue.IsBusy(LockVM, "win11") {
		t.Error("VM not locked while its backup runs")
	}
	h.finish(1, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	if st, _ := h.vms.State(nil, "win11"); st != vmRunning {
		t.Errorf("VM %s after the run", st)
	}
}

func TestHookFailurePolicies(t *testing.T) {
	t.Run("abort restarts what it stopped", func(t *testing.T) {
		h := newHarness(t, true)
		h.apps.stuck["blinko"] = true
		j := sampleJob("Apps")
		j.Hooks = []Hook{
			{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}},
			{Phase: HookPre, Action: HookStopApps, Apps: []string{"blinko"}},
		}
		job := h.createJob(j)
		job = h.mutate(job, func(j *Job) { j.Hooks[1].TimeoutSec = 1 })
		r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
		if ErrorCode(r.ErrorCode) != ErrAppStopFailed || len(h.eng.CallsTo("StartJob")) != 0 {
			t.Errorf("run %+v", r)
		}
		if !h.apps.isRunning("immich") {
			t.Error("immich left stopped after the abort")
		}
	})
	t.Run("continue makes it partial", func(t *testing.T) {
		h := newHarness(t, true)
		j := sampleJob("Apps")
		j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}, FailPolicy: FailContinue}}
		job := h.createJob(j)
		h.apps.listErr = nil
		// app-management can't find the app at run time.
		delete(h.apps.running, "immich")
		id := h.run(job, KindBackup)
		h.finish(1, okResult(1))
		r := h.waitStatus(id, StatusPartial)
		if ErrorCode(r.ErrorCode) != ErrAppStopFailed {
			t.Errorf("code %q", r.ErrorCode)
		}
		if !h.notified("backup.notify.failed") {
			t.Error("no warning for the failed hook")
		}
	})
	t.Run("post hook failure is partial", func(t *testing.T) {
		h := newHarness(t, true)
		j := sampleJob("Apps")
		j.Hooks = []Hook{{Phase: HookPost, Action: HookStartVM, VM: "offvm"}}
		job := h.createJob(j)
		h.vms.mu.Lock()
		delete(h.vms.states, "offvm") // removed while the job ran
		h.vms.mu.Unlock()
		id := h.run(job, KindBackup)
		h.finish(1, okResult(1))
		r := h.waitStatus(id, StatusPartial)
		if ErrorCode(r.ErrorCode) != ErrVMStartFailed {
			t.Errorf("code %q", r.ErrorCode)
		}
	})
}

func TestRestoreToOriginalWrapsHooks(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Apps")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	// Restore into the job's own source: immich must be stopped.
	id, _, _ := h.svc.queue.Enqueue(job, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual, RestoreSpec: restoreSpecJSON(job.Sources[0])})
	h.startedJob(1)
	if h.apps.isRunning("immich") {
		t.Fatal("restoring into a running app's folder")
	}
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 1}})
	r := h.waitStatus(id, StatusSuccess)
	if DecodeMessage(r.Summary).Key != "backup.run.summary.restored" || !h.apps.isRunning("immich") {
		t.Errorf("restore %+v", r)
	}
	// Elsewhere: no hooks.
	id2, _, _ := h.svc.queue.Enqueue(job, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual,
		RestoreSpec: restoreSpecJSON(Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Restored"})})
	h.startedJob(2)
	if !h.apps.isRunning("immich") {
		t.Error("a restore elsewhere stopped the app")
	}
	h.finish(2, engine.Result{})
	h.waitStatus(id2, StatusSuccess)
}
