package jobs

import (
	"encoding/json"
	"testing"
	"time"
)

// crashedAppRun seeds a run a crash left running with immich stopped by
// its pre hook.
func crashedAppRun(t *testing.T, h *harness, job Job, started time.Time) RunRow {
	t.Helper()
	hooks, _ := json.Marshal([]HookDone{{HookIdx: 0, PreDone: true, PriorState: map[string]string{"immich": priorRunning}}})
	r := RunRow{ID: NewRunID(started), JobID: job.ID, Kind: string(KindBackup), Trigger: string(RunBySchedule),
		Status: string(StatusRunning), Phase: string(PhaseTransfer), Attempt: 1, QueuedAt: started, StartedAt: &started,
		HooksDone: string(hooks)}
	if err := h.svc.store.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	return r
}

// A restart that failed at boot is retried once app-management answers.
func TestRestartLoopStartsTheAppOnceAppManagementAnswers(t *testing.T) {
	var apps *downApps
	h := newHarness(t, false, func(c *Config, h *harness) {
		apps = &downApps{fakeApps: h.apps}
		apps.down.Store(true)
		c.Apps = apps
		c.Timings.RestartRetry = 10 * time.Millisecond
	})
	j := sampleJob("Apps")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	h.apps.running["immich"] = false
	crashed := crashedAppRun(t, h, job, time.Now().Add(-time.Hour))
	h.start()
	time.Sleep(50 * time.Millisecond)
	if h.apps.isRunning("immich") {
		t.Fatal("immich started while app-management was down")
	}
	apps.down.Store(false)
	deadline := time.Now().Add(5 * time.Second)
	for !h.apps.isRunning("immich") {
		if time.Now().After(deadline) {
			t.Fatal("immich was never started again after app-management came back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		r, _ := h.svc.store.GetRun(crashed.ID)
		hd := decodeHooksDone(r.HooksDone)
		if len(hd) == 1 && hd[0].PostDone && hd[0].PriorState["immich"] == priorRestarted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hooks not recorded as done: %s", r.HooksDone)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The loop doesn't start an app another running backup holds, and gives
// up (with a notification) a day after the run ended.
func TestRestartLoopGivesUpAfterADay(t *testing.T) {
	h := newHarness(t, false)
	j := sampleJob("Apps")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	h.apps.running["immich"] = false
	h.apps.listErr = errAppMgmtDown
	crashed := crashedAppRun(t, h, job, time.Now().Add(-48*time.Hour))
	h.start()
	r, _ := h.svc.store.GetRun(crashed.ID)
	if hd := decodeHooksDone(r.HooksDone); len(hd) != 1 || hd[0].PostDone {
		t.Fatalf("replay with app-management down: %s", r.HooksDone)
	}
	// The run ended (interrupted) just now; pretend a day went by.
	h.setNow(func() time.Time { return time.Now().Add(restartGiveUp + time.Hour) })
	if h.svc.retryRestarts() {
		t.Fatal("still pending after giving up")
	}
	r, _ = h.svc.store.GetRun(crashed.ID)
	if hd := decodeHooksDone(r.HooksDone); !hd[0].PostDone {
		t.Fatalf("not given up: %s", r.HooksDone)
	}
	found := false
	for _, k := range h.bus.notifications() {
		if k == "backup.notify.restart_gave_up" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no give-up notification: %v", h.bus.notifications())
	}
}

// A run that stops an app that an earlier run left stopped keeps the
// earlier "running" record instead of overwriting it with "stopped".
func TestStopAppsKeepsAPendingRunningRecord(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Apps")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	x := &runExec{s: h.svc, run: RunRow{ID: "run_x", JobID: job.ID}, job: job, log: &RunLog{},
		hooks: []HookDone{{HookIdx: 0, PreDone: true, PriorState: map[string]string{"immich": priorRunning}}}, hooksOnly: true}
	h.apps.running["immich"] = false
	if code := x.stopApps(t.Context(), 0, []string{"immich"}, time.Second, true); code != "" {
		t.Fatal(code)
	}
	if got := x.hooks[0].PriorState["immich"]; got != priorRunning {
		t.Fatalf("prior state %q, want %q", got, priorRunning)
	}
}
