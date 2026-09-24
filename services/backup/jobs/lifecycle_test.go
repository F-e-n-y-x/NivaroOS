package jobs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The run lifecycle (spec §5, §16.1 lifecycle_test.go).

// mutate changes a stored job behind validation's back (values a test
// needs but the API wouldn't accept, like a one-second max_duration).
func (h *harness) mutate(j Job, fn func(j *Job)) Job {
	h.t.Helper()
	out, err := h.svc.store.MutateJob(j.ID, time.Now(), false, func(j *Job) error { fn(j); return nil })
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func eventStatuses(h *harness, name, runID string) []string {
	var out []string
	for _, p := range h.bus.named(name) {
		if p[PropRunID] == runID {
			out = append(out, p[PropStatus]+"/"+p[PropPhase])
		}
	}
	return out
}

func TestRunSuccess(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Docs")
	j.Notify.OnSuccess = true
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	id, req := h.startedJob(1)
	if req.Op != engine.OpCopy || req.RunID != runID || req.JobID != job.ID || !req.FirstRun || req.Baseline != nil || req.DestFolderID != job.DestFolderID {
		t.Fatalf("engine request: %+v", req)
	}
	eta := int64(9)
	h.eng.Progress(id, engine.Stats{Bytes: 50, TotalBytes: 100, Files: 1, TotalFiles: 2, ETASec: &eta, Step: "transfer"})
	h.eng.Log(id, engine.LogLine{Lvl: engine.LogInfo, Code: "raw", Raw: "copied a.txt"})
	h.eventually("live stats", func() bool {
		l := h.svc.liveStats(runID)
		return l != nil && l.Bytes == 50
	})
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 2, BytesTransferred: 100, BytesTotal: 1000, SourceFiles: 10, DestFiles: 10}, MarkerWritten: true})
	r := h.waitStatus(runID, StatusSuccess)
	if r.StartedAt == nil || r.EndedAt == nil || r.FilesAdded != 2 || r.BytesTotal != 1000 || r.Attempt != 1 {
		t.Errorf("run row: %+v", r)
	}
	if msg := DecodeMessage(r.Summary); msg == nil || msg.Key != "backup.run.summary.ok" {
		t.Errorf("summary %q", r.Summary)
	}
	if !strings.HasSuffix(r.LogPath, ".jsonl.gz") {
		t.Errorf("log not compressed: %q", r.LogPath)
	}
	lines, _, _, err := ReadLogPage(r.LogPath, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var text []string
	for _, l := range lines {
		text = append(text, l.Raw+l.MsgKey)
	}
	if !strings.Contains(strings.Join(text, "|"), "copied a.txt") || !strings.Contains(strings.Join(text, "|"), "backup.log.finished") {
		t.Errorf("log lines: %v", text)
	}
	st, _ := h.svc.store.JobState(job.ID)
	if st.LastSuccessAt == nil || st.SizeBytes != 1000 || st.SourceFiles != 10 {
		t.Errorf("job state: %+v", st)
	}
	if h.svc.liveStats(runID) != nil {
		t.Error("live stats kept after the run")
	}
	// One begin, progress, one end with the summary.
	h.busEvents(EventRunBegin, 1)
	if b := eventStatuses(h, EventRunBegin, runID); len(b) != 1 || b[0] != "running/precheck" {
		t.Errorf("run-begin: %v", b)
	}
	ends := h.busEvents(EventRunEnd, 1)
	if len(ends) != 1 || ends[0][PropStatus] != "success" || !strings.HasPrefix(ends[0][PropSummary], "backup.run.summary.ok|") {
		t.Errorf("run-end: %v", ends)
	}
	h.eventually("progress events", func() bool { return len(eventStatuses(h, EventRunProgress, runID)) >= 2 })
	if !h.notified("backup.notify.success") {
		t.Errorf("notifications %v", h.bus.notifications())
	}

	// The next run is incremental: baseline from this one.
	runID2 := h.run(job, KindBackup)
	_, req = h.startedJob(2)
	if req.FirstRun || req.Baseline == nil || req.Baseline.SourceFiles != 10 {
		t.Errorf("second request: first_run %v baseline %+v", req.FirstRun, req.Baseline)
	}
	h.finish(2, okResult(0))
	r = h.waitStatus(runID2, StatusSuccess)
	if DecodeMessage(r.Summary).Key != "backup.run.summary.ok_nothing" {
		t.Errorf("nothing-to-do summary %q", r.Summary)
	}
}

func TestRunPartial(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	runID := h.run(job, KindBackup)
	h.finish(1, engine.Result{
		Counts:     engine.Counts{Added: 5, Errored: 1},
		FileErrors: []engine.FileError{{Path: "locked.db", Code: ErrIOError, Detail: "permission denied"}},
	})
	r := h.waitStatus(runID, StatusPartial)
	if DecodeMessage(r.Summary).Key != "backup.run.summary.partial" {
		t.Errorf("summary %q", r.Summary)
	}
	if !h.notified("backup.notify.partial") {
		t.Errorf("notifications %v", h.bus.notifications())
	}
	d := h.svc.runDetail(r)
	if len(d.FileErrors) != 1 || d.FileErrors[0].Path != "locked.db" {
		t.Errorf("file errors in the API: %+v", d.FileErrors)
	}
}

func TestConfigErrorFailsWithoutRetry(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Docs")
	j.Retry = Retry{Max: 3}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.finish(1, engine.Result{ErrorCode: ErrNoSpace, ErrorDetail: "disk full"})
	r := h.waitStatus(runID, StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrNoSpace {
		t.Errorf("code %q", r.ErrorCode)
	}
	settle()
	if rows, _ := h.svc.store.ListRuns(RunFilter{JobID: job.ID}); len(rows) != 1 {
		t.Errorf("%d runs: a config error was retried", len(rows))
	}
	n := h.busEvents(EventNotify, 1)
	if len(n) != 1 || n[0][PropNotifyKey] != "backup.notify.failed" || n[0][PropLevel] != NotifyLevelError ||
		!strings.Contains(n[0][PropMessage], "Docs") || strings.Contains(n[0][PropMessage], "{") {
		t.Errorf("failure notification: %v", n)
	}
	var win map[string]interface{}
	if json.Unmarshal([]byte(n[0][PropWindow]), &win) != nil || win["kind"] != "run" {
		t.Errorf("notification window %q", n[0][PropWindow])
	}
}

func TestTransientRetryBackoff(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Docs")
	j.Retry = Retry{Max: 2, BackoffSec: []int{60, 600}}
	job := h.createJob(j)
	start := time.Now()
	h.setNow(func() time.Time { return start })
	first := h.run(job, KindBackup)
	h.finish(1, engine.Result{ErrorCode: ErrNetworkUnreachable})
	h.waitStatus(first, StatusFailed)

	retry := func(n int, wantDelay time.Duration) RunRow {
		t.Helper()
		var r RunRow
		h.eventually("retry queued", func() bool {
			rs := h.runsBy(job.ID, RunByRetry)
			if len(rs) < n {
				return false
			}
			r = rs[0]
			return true
		})
		if r.Attempt != n+1 || RunStatus(r.Status) != StatusQueued {
			t.Fatalf("retry %d: %+v", n, r)
		}
		if got := r.QueuedAt.Sub(h.svc.now()); got < wantDelay-time.Second || got > wantDelay+time.Second {
			t.Errorf("retry %d in %v, want %v", n, got, wantDelay)
		}
		return r
	}
	r2 := retry(1, 60*time.Second)
	settle()
	if len(h.eng.CallsTo("StartJob")) != 1 {
		t.Fatal("the retry didn't wait for its backoff")
	}
	h.setNow(func() time.Time { return start.Add(61 * time.Second) })
	h.svc.queue.wake()
	h.finish(2, engine.Result{ErrorCode: ErrIOError})
	h.waitStatus(r2.ID, StatusFailed)
	if containsStr(h.bus.notifications(), "backup.notify.failed") {
		t.Error("notified before the retries ran out")
	}
	now2 := start.Add(61 * time.Second)
	r3 := retry(2, 600*time.Second)
	if d := r3.QueuedAt.Sub(now2); d < 599*time.Second {
		t.Errorf("second backoff %v", d)
	}
	h.setNow(func() time.Time { return start.Add(time.Hour) })
	h.svc.queue.wake()
	h.finish(3, engine.Result{ErrorCode: ErrIOError})
	h.waitStatus(r3.ID, StatusFailed)
	settle()
	if n := len(h.runsBy(job.ID, RunByRetry)); n != 2 {
		t.Errorf("%d retries, want Max=2", n)
	}
	h.eventually("final failure notification", func() bool { return containsStr(h.bus.notifications(), "backup.notify.failed") })

	for attempt, want := range map[int]time.Duration{1: 60 * time.Second, 2: 600 * time.Second, 5: 600 * time.Second} {
		if got := retryDelay(Retry{BackoffSec: []int{60, 600}}, attempt); got != want {
			t.Errorf("retryDelay(%d) = %v", attempt, got)
		}
	}
	if got := retryDelay(Retry{}, 3); got != time.Hour {
		t.Errorf("default backoff attempt 3 = %v", got)
	}
}

func TestDeferredCloudQuota(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("To Drive")
	j.Dest = Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "Backups"}
	j.Retry = Retry{Max: 0}
	job := h.createJob(j)
	first := h.run(job, KindBackup)
	h.finish(1, engine.Result{ErrorCode: ErrCloudQuotaDaily})
	r := h.waitStatus(first, StatusSkipped)
	msg := DecodeMessage(r.Summary)
	if ErrorCode(r.ErrorCode) != ErrCloudQuotaDaily || msg.Key != "backup.run.summary.deferred" || msg.Args["retry_at"] == nil {
		t.Fatalf("deferred run: %+v", r)
	}
	var retry RunRow
	h.eventually("requeued after midnight", func() bool {
		rs := h.runsBy(job.ID, RunByRetry)
		if len(rs) == 1 {
			retry = rs[0]
		}
		return len(rs) == 1
	})
	lt := retry.QueuedAt.In(time.Local)
	if lt.Hour() != 0 || lt.Minute() != 10 || !retry.QueuedAt.After(time.Now()) {
		t.Errorf("retry at %v, want next 00:10", lt)
	}
	// Retry.Max = 0 doesn't matter: deferral isn't a retry of a failure.
	// The deferred retry hitting the quota again waits for the schedule.
	h.setNow(func() time.Time { return retry.QueuedAt.Add(time.Minute) })
	h.svc.queue.wake()
	h.finish(2, engine.Result{ErrorCode: ErrCloudQuotaDaily})
	h.waitStatus(retry.ID, StatusSkipped)
	settle()
	if n := len(h.runsBy(job.ID, RunByRetry)); n != 1 {
		t.Errorf("%d deferred retries, want 1", n)
	}
	if containsStr(h.bus.notifications(), "backup.notify.failed") {
		t.Error("a deferral notified as a failure")
	}
}

func TestEngineUnavailableWaits(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	h.eng.Lock()
	h.eng.ErrHealth = &engine.Error{Code: engine.CodeEngineUnavailable}
	h.eng.Unlock()
	runID := h.run(job, KindBackup)
	h.eventually("requeued", func() bool {
		r, _ := h.svc.store.GetRun(runID)
		return r.StartedAt != nil && RunStatus(r.Status) == StatusQueued && ErrorCode(r.ErrorCode) == ErrEngineUnavailable
	})
	h.eng.Lock()
	h.eng.ErrHealth = nil
	h.eng.Unlock()
	h.finish(1, okResult(1))
	r := h.waitStatus(runID, StatusSuccess)
	if r.Attempt != 1 {
		t.Errorf("waiting for the engine spent an attempt: %d", r.Attempt)
	}
}

func TestCancelRunningRunsPostHooks(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Docs")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	j.Retry = Retry{Max: 3}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.startedJob(1)
	if h.apps.isRunning("immich") {
		t.Fatal("immich still running during the transfer")
	}
	if _, err := h.svc.queue.Cancel(runID, ErrCancelledByUser); err != nil {
		t.Fatal(err)
	}
	r := h.waitStatus(runID, StatusCancelled)
	if ErrorCode(r.ErrorCode) != ErrCancelledByUser || len(h.eng.CallsTo("StopJob")) != 1 {
		t.Errorf("cancel: %+v", r)
	}
	if !h.apps.isRunning("immich") {
		t.Error("post hooks didn't restart immich after a cancel")
	}
	settle()
	if n := len(h.runsBy(job.ID, RunByRetry)); n != 0 {
		t.Error("a cancelled run was retried")
	}
}

func TestMaxDuration(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	job = h.mutate(job, func(j *Job) { j.Options.MaxDurationSec = 1 })
	runID := h.run(job, KindBackup)
	r := h.waitStatus(runID, StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrMaxDuration || len(h.eng.CallsTo("StopJob")) != 1 {
		t.Errorf("max_duration run: %+v", r)
	}
}

func TestGuardWaitsForUserThenProceeds(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Mirror")
	j.Type = TypeMirror
	j.Retention = Retention{VersionsDays: 30}
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	_, req := h.startedJob(1)
	if req.Op != engine.OpSync || len(req.GuardOverride) != 0 {
		t.Fatalf("first request %+v", req)
	}
	guard := &engine.GuardInfo{Guard: "delete", Pct: 37.4, Limit: 10, Count: 374, Total: 1000, Sample: []string{"a"}}
	h.finish(1, engine.Result{ErrorCode: ErrDeleteGuard, Guard: guard})
	r := h.waitStatus(runID, StatusWaitingUser)
	msg := DecodeMessage(r.Summary)
	if msg.Key != "backup.run.summary.waiting" || msg.Args["pct"].(float64) != 37 || r.GuardInfo == "" {
		t.Errorf("waiting run: %+v", r)
	}
	if !h.notified("backup.notify.waiting") || len(h.busEvents(EventRunWaiting, 1)) != 1 {
		t.Error("no waiting notification/event")
	}
	if st, _ := h.svc.store.JobState(job.ID); !st.GuardTripped {
		t.Error("guard not recorded")
	}
	// The worker slot is free: another job runs meanwhile.
	other := h.createJob(jobTo("other", "Other"))
	oid := h.run(other, KindBackup)
	h.finish(2, okResult(1))
	h.waitStatus(oid, StatusSuccess)
	// A trigger meanwhile coalesces onto the waiting run.
	if id, coalesced, _ := h.svc.queue.Enqueue(job, EnqueueOptions{Trigger: RunBySchedule}); !coalesced || id != runID {
		t.Error("trigger not coalesced onto the waiting run")
	}

	rec := doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: true}, reqOpts{})
	wantStatus(t, rec, 200)
	_, req = h.startedJob(3)
	if req.Op != engine.OpSync || len(req.GuardOverride) != 1 || req.GuardOverride[0] != "delete" {
		t.Fatalf("resumed request: %+v", req)
	}
	// The override covers what the user saw, not whatever the new plan
	// shows.
	if req.GuardReviewed == nil || req.GuardReviewed.Deleted != 374 {
		t.Fatalf("resumed request reviewed %+v, want the guard's 374 deletions", req.GuardReviewed)
	}
	h.finish(3, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	settle()
	// Pruning stays off for a run that went ahead on an override.
	for _, c := range h.eng.CallsTo("StartJob") {
		if c.Req.(engine.JobRequest).Op == engine.OpPurgeVersions {
			t.Error("pruned after an overridden guard")
		}
	}
	if _, ok, _ := h.svc.store.GetMeta(runOverrideKey(runID)); ok {
		t.Error("the override outlived its run")
	}
	if _, ok, _ := h.svc.store.GetMeta(runReviewedKey(runID)); ok {
		t.Error("the reviewed counts outlived their run")
	}
	// The next clean run prunes again.
	next := h.run(job, KindBackup)
	_, req = h.startedJob(4)
	if len(req.GuardOverride) != 0 {
		t.Error("an override carried over to the next run")
	}
	h.finish(4, okResult(1))
	_, req = h.startedJob(5)
	if req.Op != engine.OpPurgeVersions || req.Retention.VersionsDays != 30 {
		t.Fatalf("prune request %+v", req)
	}
	h.finish(5, engine.Result{Counts: engine.Counts{Deleted: 2}})
	h.waitStatus(next, StatusSuccess)
}

func TestGuardDeclineAndCopyOnce(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Mirror")
	j.Type = TypeMirror
	job := h.createJob(j)
	runID := h.run(job, KindBackup)
	h.finish(1, engine.Result{ErrorCode: ErrDeleteGuard, Guard: &engine.GuardInfo{Guard: "delete", Pct: 50, Limit: 10}})
	h.waitStatus(runID, StatusWaitingUser)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: false}, reqOpts{}), 200)
	r := h.waitStatus(runID, StatusCancelled)
	if r.Decision != "decline" || ErrorCode(r.ErrorCode) != ErrCancelledByUser {
		t.Errorf("declined: %+v", r)
	}

	// "Copy new files only this time" runs a copy instead of the mirror.
	run2 := h.run(job, KindBackup)
	h.finish(2, engine.Result{ErrorCode: ErrDeleteGuard, Guard: &engine.GuardInfo{Guard: "delete", Pct: 50, Limit: 10}})
	h.waitStatus(run2, StatusWaitingUser)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+run2+"/decide", DecideRequest{Proceed: true, Mode: DecideCopyOnce}, reqOpts{}), 200)
	_, req := h.startedJob(3)
	if req.Op != engine.OpCopy {
		t.Errorf("copy_once ran %s", req.Op)
	}
	h.finish(3, okResult(1))
	h.waitStatus(run2, StatusSuccess)
}

func TestDecisionTimeout(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	runID := h.run(job, KindBackup)
	h.finish(1, engine.Result{ErrorCode: ErrChangeGuard, Guard: &engine.GuardInfo{Guard: "change", Pct: 80, Limit: 30}})
	h.waitStatus(runID, StatusWaitingUser)
	h.svc.expireDecisions() // not yet
	if r, _ := h.svc.store.GetRun(runID); RunStatus(r.Status) != StatusWaitingUser {
		t.Fatal("expired early")
	}
	h.setNow(func() time.Time { return time.Now().Add(WaitingUserTimeout + time.Minute) })
	h.svc.expireDecisions()
	r := h.waitStatus(runID, StatusCancelled)
	if ErrorCode(r.ErrorCode) != ErrDecisionTimeout {
		t.Errorf("code %q", r.ErrorCode)
	}
	if !h.notified("backup.notify.decision_timeout") {
		t.Error("no timeout notification")
	}
}

func TestPreviewFirstRun(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Careful")
	j.Options.PreviewFirst = true
	job := h.createJob(j)
	runID, _, _ := h.svc.queue.Enqueue(job, EnqueueOptions{Kind: KindBackup, Trigger: RunBySchedule})
	_, req := h.startedJob(1)
	if req.Op != engine.OpPlan || req.PlanOp != engine.OpCopy || req.PreviewFile == "" {
		t.Fatalf("first run of a preview_first job: %+v", req)
	}
	// The engine writes the plan; the API pages it.
	plan := `{"op":"add","path":"a/new.txt","size":10}` + "\n" + `{"op":"delete","path":"old.txt","size":3}` + "\n"
	if err := os.WriteFile(req.PreviewFile, []byte(plan), 0o600); err != nil {
		t.Fatal(err)
	}
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 1, Deleted: 1, BytesAdd: 10}})
	r := h.waitStatus(runID, StatusWaitingUser)
	if r.PreviewPath != req.PreviewFile || DecodeMessage(r.Summary).Key != "backup.run.summary.preview" {
		t.Errorf("preview run %+v", r)
	}
	if !h.notified("backup.notify.preview_ready") {
		t.Error("a scheduled preview didn't ask for review")
	}
	rec := doRequest(h.svc, "GET", "/v1/backup/runs/"+runID+"/preview?op=delete", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var page PreviewPage
	envelope(t, rec, &page)
	if page.Counts.Add != 1 || page.Counts.Delete != 1 || page.Counts.BytesAdd != 10 || page.Total != 1 || page.Items[0].Path != "old.txt" || page.NextOffset != -1 {
		t.Errorf("preview page %+v", page)
	}
	rec = doRequest(h.svc, "GET", "/v1/backup/runs/"+runID+"/preview?q=NEW", nil, reqOpts{})
	page = PreviewPage{}
	envelope(t, rec, &page)
	if page.Total != 1 || page.Items[0].Path != "a/new.txt" {
		t.Errorf("search %+v", page)
	}
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/runs/"+runID+"/preview?op=nuke", nil, reqOpts{}), 400)

	// Approving runs the real copy with the preview's changes accepted.
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: true}, reqOpts{}), 200)
	_, req = h.startedJob(2)
	if req.Op != engine.OpCopy || len(req.GuardOverride) != 2 {
		t.Errorf("approved: %+v", req)
	}
	if req.GuardReviewed == nil || req.GuardReviewed.Deleted != 1 || req.GuardReviewed.Updated != 0 {
		t.Errorf("approved with reviewed %+v, want the preview's counts", req.GuardReviewed)
	}
	h.finish(2, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	// Later runs don't preview again.
	next := h.run(job, KindBackup)
	if _, req = h.startedJob(3); req.Op != engine.OpCopy {
		t.Errorf("second run previewed again: %s", req.Op)
	}
	h.finish(3, okResult(1))
	h.waitStatus(next, StatusSuccess)
}

func TestConditionsWhenUnmet(t *testing.T) {
	offline := func(h *harness) {
		h.eng.Lock()
		h.eng.ResolveFn = func(r engine.ResolveRequest) (engine.Resolved, error) {
			if r.Endpoint.RefID == "tank-uuid" {
				return engine.Resolved{Online: false}, nil
			}
			return engine.Resolved{Root: "/x", Online: true}, nil
		}
		h.eng.Unlock()
	}

	t.Run("skip", func(t *testing.T) {
		h := newHarness(t, true)
		offline(h)
		j := sampleJob("Stick")
		j.Conditions.WhenUnmet = UnmetSkip
		job := h.createJob(j)
		r1 := h.run(job, KindBackup)
		r := h.waitStatus(r1, StatusSkipped)
		if ErrorCode(r.ErrorCode) != ErrDestOffline || len(h.eng.CallsTo("StartJob")) != 0 {
			t.Errorf("skipped run %+v", r)
		}
		if containsStr(h.bus.notifications(), "backup.notify.offline") {
			t.Error("notified after one miss")
		}
		// The second miss in a row notifies, once.
		h.waitStatus(h.run(job, KindBackup), StatusSkipped)
		h.waitStatus(h.run(job, KindBackup), StatusSkipped)
		h.notified("backup.notify.offline")
		settle()
		n := 0
		for _, k := range h.bus.notifications() {
			if k == "backup.notify.offline" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d offline notifications, want 1", n)
		}
		d := h.svc.runDetail(r)
		if d.Status != StatusSkipped {
			t.Error(d.Status)
		}
	})

	t.Run("fail", func(t *testing.T) {
		h := newHarness(t, true)
		offline(h)
		j := sampleJob("Stick")
		j.Conditions.WhenUnmet = UnmetFail
		job := h.createJob(j)
		r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
		if ErrorCode(r.ErrorCode) != ErrDestOffline || !h.notified("backup.notify.failed") {
			t.Errorf("failed run %+v", r)
		}
	})

	t.Run("wait", func(t *testing.T) {
		h := newHarness(t, true)
		offline(h)
		j := sampleJob("Stick")
		j.Conditions.WhenUnmet = UnmetWait
		j.Conditions.WaitMaxMin = 60
		job := h.createJob(j)
		start := time.Now()
		h.setNow(func() time.Time { return start })
		runID := h.run(job, KindBackup)
		h.eventually("waiting re-check", func() bool {
			r, _ := h.svc.store.GetRun(runID)
			return RunStatus(r.Status) == StatusQueued && ErrorCode(r.ErrorCode) == ErrDestOffline
		})
		// The drive comes back: the next re-check runs it.
		h.eng.Lock()
		h.eng.ResolveFn = nil
		h.eng.Unlock()
		h.setNow(func() time.Time { return start.Add(6 * time.Minute) })
		h.svc.queue.wake()
		h.finish(1, okResult(1))
		h.waitStatus(runID, StatusSuccess)

		// It never comes back: skipped after wait_max_min.
		offline(h)
		h.setNow(func() time.Time { return start.Add(10 * time.Minute) })
		run2 := h.run(job, KindBackup)
		h.eventually("waiting", func() bool {
			r, _ := h.svc.store.GetRun(run2)
			return RunStatus(r.Status) == StatusQueued && r.StartedAt != nil
		})
		h.setNow(func() time.Time { return start.Add(2 * time.Hour) })
		h.svc.queue.wake()
		r := h.waitStatus(run2, StatusSkipped)
		if ErrorCode(r.ErrorCode) != ErrDestOffline {
			t.Errorf("code %q", r.ErrorCode)
		}
	})

	t.Run("window", func(t *testing.T) {
		h := newHarness(t, true)
		j := sampleJob("Night only")
		j.Conditions.Window = &TimeWindow{Start: "01:00", End: "05:00"}
		job := h.createJob(j)
		noon := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)
		h.setNow(func() time.Time { return noon })
		r := h.waitStatus(h.run(job, KindBackup), StatusSkipped)
		if ErrorCode(r.ErrorCode) != ErrWindowClosed {
			t.Errorf("code %q", r.ErrorCode)
		}
		h.setNow(func() time.Time { return noon.Add(15 * time.Hour) }) // 03:00
		id := h.run(job, KindBackup)
		h.finish(1, okResult(1))
		h.waitStatus(id, StatusSuccess)
	})
}

func TestInWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	cases := []struct {
		w    TimeWindow
		t    time.Time
		want bool
	}{
		{TimeWindow{"01:00", "05:00"}, at(3, 0), true},
		{TimeWindow{"01:00", "05:00"}, at(5, 0), false},
		{TimeWindow{"01:00", "05:00"}, at(0, 59), false},
		{TimeWindow{"22:00", "06:00"}, at(23, 30), true},
		{TimeWindow{"22:00", "06:00"}, at(5, 59), true},
		{TimeWindow{"22:00", "06:00"}, at(12, 0), false},
	}
	for _, c := range cases {
		if got := inWindow(c.w, c.t); got != c.want {
			t.Errorf("inWindow(%v, %v) = %v", c.w, c.t.Format("15:04"), got)
		}
	}
}

func TestDestMarkerMismatch(t *testing.T) {
	h := newHarness(t, true)
	h.eng.Lock()
	h.eng.ResolveFn = func(r engine.ResolveRequest) (engine.Resolved, error) {
		return engine.Resolved{Root: "/x", Online: true, Marker: &engine.Marker{V: 1, DestFolderID: "someone-else"}}, nil
	}
	h.eng.Unlock()
	job := h.createJob(sampleJob("Docs"))
	r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrDestMarkerMismatch || len(h.eng.CallsTo("StartJob")) != 0 {
		t.Errorf("run %+v", r)
	}
	got, _ := h.svc.store.GetJob(job.ID)
	if got.NeedsAttention != AttentionDestChanged || got.Revision != job.Revision {
		t.Errorf("job after mismatch: attention %q revision %d", got.NeedsAttention, got.Revision)
	}
	// "Reconnect to this folder" adopts the folder's marker.
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs/"+job.ID+"/reconnect-dest", nil, reqOpts{})
	wantStatus(t, rec, 200)
	got, _ = h.svc.store.GetJob(job.ID)
	if got.DestFolderID != "someone-else" || got.NeedsAttention != "" {
		t.Errorf("reconnected: %+v", got)
	}
}

func TestInterruptedRunReplaysPostHooks(t *testing.T) {
	h := newHarness(t, false)
	j := sampleJob("Apps")
	j.Hooks = []Hook{
		{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich", "stopped-app"}},
		{Phase: HookPre, Action: HookShutdownVM, VM: "win11"},
	}
	j.Retry = Retry{Max: 1}
	job := h.createJob(j)
	// The service died mid-transfer: immich and the VM were stopped by
	// the run, stopped-app was already off.
	h.apps.running["immich"] = false
	h.vms.states["win11"] = vmShutOff
	hooks, _ := json.Marshal([]HookDone{
		{HookIdx: 0, PreDone: true, PriorState: map[string]string{"immich": priorRunning, "stopped-app": priorStopped}},
		{HookIdx: 1, PreDone: true, PriorState: map[string]string{vmStateKey("win11"): priorRunning}},
	})
	started := time.Now().Add(-time.Hour)
	crashed := RunRow{ID: NewRunID(started), JobID: job.ID, Kind: string(KindBackup), Trigger: string(RunBySchedule),
		Status: string(StatusRunning), Phase: string(PhaseTransfer), Attempt: 1, QueuedAt: started, StartedAt: &started,
		HooksDone: string(hooks)}
	if err := h.svc.store.CreateRun(crashed); err != nil {
		t.Fatal(err)
	}
	// A crashed restore isn't repeated behind the user's back.
	restore := RunRow{ID: NewRunID(started), JobID: job.ID, Kind: string(KindRestore), Trigger: string(RunByManual),
		Status: string(StatusRunning), Attempt: 1, QueuedAt: started, StartedAt: &started, RestoreSpec: restoreSpecJSON(job.Sources[0])}
	h.svc.store.CreateRun(restore)

	h.start()
	r, _ := h.svc.store.GetRun(crashed.ID)
	if RunStatus(r.Status) != StatusInterrupted || ErrorCode(r.ErrorCode) != ErrInterrupted || r.EndedAt == nil {
		t.Fatalf("crashed run: %+v", r)
	}
	if !h.apps.isRunning("immich") || h.apps.isRunning("stopped-app") {
		t.Error("replay didn't restore the apps' prior state")
	}
	if st, _ := h.vms.State(nil, "win11"); st != vmRunning {
		t.Error("replay didn't start the VM")
	}
	for _, hd := range decodeHooksDone(r.HooksDone) {
		if !hd.PostDone {
			t.Errorf("hook %d not marked done after replay", hd.HookIdx)
		}
	}
	if rr, _ := h.svc.store.GetRun(restore.ID); RunStatus(rr.Status) != StatusInterrupted {
		t.Errorf("restore %s", rr.Status)
	}
	// Requeued once as a retry; the restore is not.
	retries := h.runsBy(job.ID, RunByRetry)
	if len(retries) != 1 || RunKind(retries[0].Kind) != KindBackup || retries[0].Attempt != 2 {
		t.Fatalf("requeued: %+v", retries)
	}
	h.finish(1, okResult(1))
	h.waitStatus(retries[0].ID, StatusSuccess)
	// Run-end events for the interrupted ones.
	h.busEvents(EventRunEnd, 2)
}

func TestPanicInRunIsContained(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	h.eng.Lock()
	h.eng.ResolveFn = func(engine.ResolveRequest) (engine.Resolved, error) { panic("boom") }
	h.eng.Unlock()
	r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrInternal {
		t.Errorf("code %q", r.ErrorCode)
	}
	// The service keeps working.
	h.eng.Lock()
	h.eng.ResolveFn = nil
	h.eng.Unlock()
	id := h.run(job, KindBackup)
	h.finish(1, okResult(1))
	h.waitStatus(id, StatusSuccess)
}

func TestEngineStartErrorFailsRun(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	h.eng.Lock()
	h.eng.ErrStartJob = &engine.Error{Code: engine.CodeCloudAuth, Detail: "token expired"}
	h.eng.Unlock()
	r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrCloudAuth {
		t.Errorf("code %q", r.ErrorCode)
	}
}

func TestPurgeOnDelete(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(sampleJob("Docs"))
	rec := doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+job.ID+"?purge_data=true", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var res DeleteJobResult
	envelope(t, rec, &res)
	if res.PurgeRunID == "" {
		t.Fatal("no purge run")
	}
	_, req := h.startedJob(1)
	if req.Op != engine.OpPurgeDest || req.DestFolderID != job.DestFolderID || req.JobID != job.ID || !sameEndpoint(req.Dest, job.Dest) {
		t.Errorf("purge request %+v", req)
	}
	h.finish(1, engine.Result{Counts: engine.Counts{Deleted: 12}})
	r := h.waitStatus(res.PurgeRunID, StatusSuccess)
	if d := h.svc.runDetail(r); d.JobName != "Docs" {
		t.Errorf("purge run of a deleted job lost its name: %+v", d.Run)
	}
}

// A real run a guard stops keeps its planning pass's list, so the user
// reviews it item by item (GET /runs/:id/preview) like a preview; a run
// that goes through leaves no list behind.
func TestGuardStoppedRunKeepsItsPlanList(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Mirror")
	j.Type = TypeMirror
	job := h.createJob(j)

	runID := h.run(job, KindBackup)
	_, req := h.startedJob(1)
	if req.PreviewFile == "" {
		t.Fatal("a mirror transfer doesn't ask for the plan list")
	}
	// What the engine's planning pass writes.
	item := `{"op":"delete","path":"a","size":1}` + "\n"
	if err := os.WriteFile(req.PreviewFile, []byte(item), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := &engine.GuardInfo{Guard: "delete", Pct: 50, Limit: 10, Count: 1, Total: 2, Sample: []string{"a"}}
	h.finish(1, engine.Result{ErrorCode: ErrDeleteGuard, Guard: guard})
	r := h.waitStatus(runID, StatusWaitingUser)
	if r.PreviewPath != req.PreviewFile {
		t.Fatalf("preview path %q, want %q", r.PreviewPath, req.PreviewFile)
	}
	rec := doRequest(h.svc, "GET", "/v1/backup/runs/"+runID+"/preview", nil, reqOpts{})
	wantStatus(t, rec, 200)

	// Declined; the next run succeeds and removes its own list.
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: false}, reqOpts{}), 200)
	next := h.run(job, KindBackup)
	_, req2 := h.startedJob(2)
	if err := os.WriteFile(req2.PreviewFile, []byte(item), 0o600); err != nil {
		t.Fatal(err)
	}
	h.finish(2, okResult(1))
	h.waitStatus(next, StatusSuccess)
	settle()
	if _, err := os.Stat(req2.PreviewFile); !os.IsNotExist(err) {
		t.Fatalf("a successful run left its plan list: %v", err)
	}
}
