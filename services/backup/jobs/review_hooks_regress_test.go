package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// downApps is app-management while it isn't answering (stopped before
// Backup at shutdown, or not started yet at boot).
type downApps struct {
	*fakeApps
	down atomic.Bool
}

var errAppMgmtDown = errors.New("dial tcp 127.0.0.1:0: connect: connection refused")

func (a *downApps) List(ctx context.Context) (map[string]bool, error) {
	if a.down.Load() {
		return nil, errAppMgmtDown
	}
	return a.fakeApps.List(ctx)
}

func (a *downApps) Start(ctx context.Context, name string) error {
	if a.down.Load() {
		return errAppMgmtDown
	}
	return a.fakeApps.Start(ctx, name)
}

// A restart that failed because app-management wasn't reachable must not
// be recorded as done: otherwise no later replay ever restarts the app,
// and an app the backup stopped stays stopped for good.
func TestReplayWithAppManagementDownKeepsHookPending(t *testing.T) {
	var apps *downApps
	h := newHarness(t, false, func(c *Config, h *harness) {
		apps = &downApps{fakeApps: h.apps}
		apps.down.Store(true)
		c.Apps = apps
	})
	j := sampleJob("Apps")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	job := h.createJob(j)
	h.apps.running["immich"] = false
	hooks, _ := json.Marshal([]HookDone{{HookIdx: 0, PreDone: true, PriorState: map[string]string{"immich": priorRunning}}})
	started := time.Now().Add(-time.Hour)
	crashed := RunRow{ID: NewRunID(started), JobID: job.ID, Kind: string(KindBackup), Trigger: string(RunBySchedule),
		Status: string(StatusRunning), Phase: string(PhaseTransfer), Attempt: 1, QueuedAt: started, StartedAt: &started,
		HooksDone: string(hooks)}
	if err := h.svc.store.CreateRun(crashed); err != nil {
		t.Fatal(err)
	}
	h.start()
	r, _ := h.svc.store.GetRun(crashed.ID)
	if h.apps.isRunning("immich") {
		t.Fatal("test setup: immich started although app-management was down")
	}
	for _, hd := range decodeHooksDone(r.HooksDone) {
		if hd.PostDone {
			t.Errorf("hook %d marked post_done although immich was never restarted: nothing will retry it", hd.HookIdx)
		}
	}
}

// A one_at_a_time archive sends one engine job per source, but each gets
// the whole job's baseline (the sum over every source). The engine's
// empty-source sentinel then compares one app's file count with all
// apps' together and trips (waiting_user) on every run after the first
// once any source holds less than half the total.
func TestOneAtATimeBaselineIsPerSource(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("App data")
	j.Type = TypeArchive
	j.Sources = []Endpoint{
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/immich", Preset: "appdata:immich"},
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/blinko", Preset: "appdata:blinko"},
	}
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich", "blinko"}, AppMode: AppModeOneAtATime}}
	job := h.createJob(j)
	first := h.run(job, KindBackup)
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 100, SourceFiles: 100}, ArchiveName: "a1.tar.zst"})
	h.finish(2, engine.Result{Counts: engine.Counts{Added: 10000, SourceFiles: 10000}, ArchiveName: "a2.tar.zst"})
	h.finish(3, engine.Result{}) // prune
	h.waitStatus(first, StatusSuccess)

	h.run(job, KindBackup)
	_, req := h.startedJob(4)
	if req.Baseline == nil {
		t.Fatal("second run has no baseline")
	}
	if req.Baseline.SourceFiles != 100 {
		t.Errorf("immich's sub-transfer got baseline source_files=%d (the whole job's), not its own 100: the engine sees a %.0f%% drop and trips empty_source",
			req.Baseline.SourceFiles, 100*float64(req.Baseline.SourceFiles-100)/float64(req.Baseline.SourceFiles))
	}
}
