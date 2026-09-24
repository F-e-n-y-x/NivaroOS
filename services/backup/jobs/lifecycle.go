package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The run lifecycle (spec §5): queued -> running (precheck, pre_hooks,
// transfer, verify, prune, post_hooks) -> a final state, or a resting
// waiting_user. Every transition is one store write plus one bus event.
// Post hooks always run once a pre hook did, whatever happened after.

// Default timings; tests shorten them through Config.Timings.
const (
	defaultEnginePoll       = time.Second
	defaultWaitRecheck      = 5 * time.Minute
	defaultEngineRetry      = time.Minute
	engineStopGrace         = time.Minute
	deferredRequeueAfterMid = 10 * time.Minute
)

// execOutcome tells the queue what to do with a run after a worker is
// done with it.
type execOutcome struct {
	requeue *RunRow // put back as queued (QueuedAt = not before)
	head    bool
}

// runExec is one worker executing one run.
type runExec struct {
	s   *Service
	a   *activeRun
	run RunRow
	job Job
	st  JobState
	log *RunLog

	creds map[string]engine.SMBCreds
	hooks []HookDone
	// hooksOnly: saveHooks writes only RunRow.HooksDone (the restart
	// loop, on a run no worker owns).
	hooksOnly bool
	// softCode is a failure that makes a finished run partial (a
	// "continue" pre hook, a post hook).
	softCode ErrorCode
	// guardPreview is the plan output of the transfer that tripped a
	// guard; the run's preview while it waits for the user.
	guardPreview string
	// sourceFiles: per source (sourceKey), the file count of each
	// one_at_a_time transfer, the next run's per-source baseline.
	sourceFiles map[string]int64

	liveMu    sync.Mutex
	live      LiveStats
	published LiveStats
}

// liveStats is GET /runs/:id's live block.
func (s *Service) liveStats(runID string) *LiveStats {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if st, ok := s.lives[runID]; ok {
		cp := st
		return &cp
	}
	return nil
}

func (s *Service) setLive(runID string, st *LiveStats) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if st == nil {
		delete(s.lives, runID)
		return
	}
	s.lives[runID] = *st
}

func (s *Service) enginePoll() time.Duration {
	if s.cfg.Timings.EnginePoll > 0 {
		return s.cfg.Timings.EnginePoll
	}
	return defaultEnginePoll
}

func (s *Service) waitRecheck() time.Duration {
	if s.cfg.Timings.WaitRecheck > 0 {
		return s.cfg.Timings.WaitRecheck
	}
	return defaultWaitRecheck
}

func (s *Service) engineRetry() time.Duration {
	if s.cfg.Timings.EngineRetry > 0 {
		return s.cfg.Timings.EngineRetry
	}
	return defaultEngineRetry
}

// purgeSpec is RunRow.RestoreSpec of a kind=prune purge run: the deleted
// job's destination, kept on the run because the job row is gone.
type purgeSpec struct {
	Job          Job    `json:"job"`
	DestFolderID string `json:"dest_folder_id"`
}

// restoreSpecStored is RunRow.RestoreSpec of a kind=restore run.
type restoreSpecStored = engine.RestoreSpec

// loadRunJob returns the job a run belongs to; a purge run carries its
// own snapshot.
func (s *Service) loadRunJob(r RunRow) (Job, error) {
	if RunKind(r.Kind) == KindPrune {
		var ps purgeSpec
		if err := json.Unmarshal([]byte(r.RestoreSpec), &ps); err != nil {
			return Job{}, fmt.Errorf("purge run %s: %w", r.ID, err)
		}
		ps.Job.DestFolderID = ps.DestFolderID
		return ps.Job, nil
	}
	return s.store.GetJob(r.JobID)
}

// finishOrphan ends a queued run whose job was deleted.
func (s *Service) finishOrphan(r RunRow) {
	if _, err := s.endWithoutRunning(r, StatusCancelled, ErrNotFound, Message{
		Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(ErrNotFound)},
	}); err != nil {
		log.Printf("backup: ending orphan run %s: %v", r.ID, err)
	}
}

// endWithoutRunning moves a queued or waiting run straight to a final
// state (cancel, decline, decision timeout, deleted job).
func (s *Service) endWithoutRunning(r RunRow, status RunStatus, code ErrorCode, summary Message) (RunRow, error) {
	now := s.now()
	r.Status, r.ErrorCode, r.Summary = string(status), string(code), EncodeMessage(summary)
	r.EndedAt = &now
	if err := s.store.SaveRun(r); err != nil {
		return r, err
	}
	if r.LogPath != "" {
		if lg, err := OpenRunLog(orLogPath(s, r)); err == nil {
			lg.Info("backup.log.finished", map[string]interface{}{"status_key": "backup.status." + r.Status})
			lg.Close()
			if p, err := compressLog(lg.Path()); err == nil {
				r.LogPath = p
				_ = s.store.SaveRun(r)
			}
		}
	}
	s.pub.publish(EventRunEnd, endProps(r, LiveStats{}))
	return r, nil
}

// execute runs one queued run to its next resting point.
func (s *Service) execute(ctx context.Context, a *activeRun) execOutcome {
	run, err := s.store.GetRun(a.runID)
	if err != nil {
		log.Printf("backup: run %s: %v", a.runID, err)
		return execOutcome{}
	}
	if RunStatus(run.Status) != StatusQueued {
		return execOutcome{}
	}
	job, err := s.loadRunJob(run)
	if err != nil {
		if isNoRecord(err) {
			s.finishOrphan(run)
		} else {
			log.Printf("backup: run %s: %v", run.ID, err)
		}
		return execOutcome{}
	}
	x := &runExec{s: s, a: a, run: run, job: job, hooks: decodeHooksDone(run.HooksDone)}
	if st, err := s.store.JobState(job.ID); err == nil {
		x.st = st
	}
	lg, err := OpenRunLog(orLogPath(s, run))
	if err != nil {
		log.Printf("backup: run %s: open log: %v", run.ID, err)
		_, _ = s.endWithoutRunning(run, StatusFailed, ErrIOError, failedSummary(ErrIOError))
		return execOutcome{}
	}
	x.log = lg
	defer func() {
		s.setLive(run.ID, nil)
		lg.Close()
	}()
	defer func() {
		// A bug in one run must not take the whole service (and every
		// other run) down; the run ends failed with internal.
		if p := recover(); p != nil {
			log.Printf("backup: run %s panicked: %v", x.run.ID, p)
			x.log.Raw(engine.LogError, fmt.Sprintf("internal error: %v", p))
			x.postHooks()
			x.finish(StatusFailed, ErrInternal, failedSummary(ErrInternal))
		}
	}()
	return x.do(ctx)
}

func (x *runExec) now() time.Time { return x.s.now() }

func (x *runExec) cancelCode() ErrorCode {
	if x.a != nil {
		if c := x.a.cancelReason(); c != "" {
			return c
		}
	}
	return ErrCancelledByUser
}

// setPhase records a phase change (one store write, one event, one log
// line).
func (x *runExec) setPhase(p RunPhase) {
	if RunPhase(x.run.Phase) == p {
		return
	}
	x.run.Phase = string(p)
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}
	x.log.Info("backup.log.phase", map[string]interface{}{"phase_key": "backup.phase." + string(p)})
	x.liveMu.Lock()
	st := x.live
	x.liveMu.Unlock()
	x.s.pub.publish(EventRunProgress, progressEventProps(x.run, st))
}

func (x *runExec) do(ctx context.Context) execOutcome {
	now := x.now()
	firstStart := x.run.StartedAt == nil
	if firstStart {
		x.run.StartedAt = &now
	}
	x.run.Status, x.run.Phase, x.run.EndedAt, x.run.ErrorCode = string(StatusRunning), string(PhasePrecheck), nil, ""
	x.run.LogPath = x.log.Path()
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
		return execOutcome{}
	}
	x.s.setLive(x.run.ID, &LiveStats{})
	x.s.pub.publish(EventRunBegin, runEventProps(x.run))
	x.log.Info("backup.log.phase", map[string]interface{}{"phase_key": "backup.phase." + string(PhasePrecheck)})

	if _, err := x.s.engine.Health(ctx); err != nil {
		return x.engineUnavailable(err)
	}
	if RunKind(x.run.Kind) != KindPrune {
		// Logs and plan lists go to the state folder on the system disk;
		// a run must not be what fills it (a purge only frees space).
		if free, err := freeBytes(x.s.store.DataDir()); err == nil && free < x.s.minStateFree() {
			x.log.Raw(engine.LogError, fmt.Sprintf("only %d MiB free for %s", free>>20, x.s.store.DataDir()))
			return x.finishFailure(ErrSystemDiskFull)
		}
	}
	creds, err := x.s.smbCredsFor(ctx, x.endpoints())
	if err != nil {
		x.log.Raw(engine.LogError, describeErr(err))
		return x.finishFailure(ErrEndpointUnknown)
	}
	x.creds = creds

	switch RunKind(x.run.Kind) {
	case KindPrune:
		return x.doPurge(ctx)
	case KindRestore:
		return x.doRestore(ctx)
	default:
		return x.doBackup(ctx, firstStart)
	}
}

// endpoints are every endpoint the run touches (for SMB credentials).
func (x *runExec) endpoints() []Endpoint {
	eps := append([]Endpoint{x.job.Dest}, x.job.Sources...)
	if RunKind(x.run.Kind) == KindRestore {
		if spec, err := decodeRestoreSpec(x.run.RestoreSpec); err == nil {
			eps = append(eps, spec.Target)
		}
	}
	return eps
}

// engineUnavailable requeues without spending an attempt: jobs wait for
// the engine and are never failed for it (spec §3.4).
func (x *runExec) engineUnavailable(err error) execOutcome {
	x.log.Raw(engine.LogWarn, "engine unavailable: "+describeErr(err))
	x.run.ErrorCode = string(ErrEngineUnavailable)
	return x.requeueAt(x.now().Add(x.s.engineRetry()), false)
}

// requeueAt puts the run back in the queue as queued.
func (x *runExec) requeueAt(at time.Time, head bool) execOutcome {
	x.run.Status, x.run.QueuedAt = string(StatusQueued), at
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
		return execOutcome{}
	}
	x.s.pub.publish(EventRunProgress, progressEventProps(x.run, LiveStats{}))
	r := x.run
	return execOutcome{requeue: &r, head: head}
}

// ---------------------------------------------------------------------
// Backup and preview runs

func (x *runExec) doBackup(ctx context.Context, firstStart bool) execOutcome {
	if code := x.checkConditions(ctx); code != "" {
		return x.unmet(code)
	}
	if x.needsPlan() {
		return x.plan(ctx)
	}
	if firstStart && RunTrigger(x.run.Trigger) == RunByVolumeMounted && x.job.Dest.Kind == EPUSB {
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.usb_running", Args: map[string]interface{}{"job": x.job.Name, "drive": x.job.Dest.Label}},
			Level:   NotifyLevelInfo, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "run",
			WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
		})
	}

	code, _ := x.preHooks(ctx)
	var res engine.Result
	if code == "" {
		res, code = x.transfer(ctx)
	}
	x.applyCounts(res)
	if ctx.Err() != nil && code == "" {
		code = x.cancelCode()
	}

	// Prune only after a clean success, and never while a guard is
	// waiting for review (spec §9).
	if code == "" && len(res.FileErrors) == 0 && res.Counts.Errored == 0 {
		x.prune(ctx)
	}
	if pc := x.postHooks(); pc != "" && x.softCode == "" {
		x.softCode = pc
	}

	switch {
	case code == "":
		return x.finishSuccess(res)
	case isGuardWait(code) && res.Guard != nil:
		return x.waitForUser(code, res.Guard, x.guardPreview)
	case code == ErrDestMarkerMismatch:
		x.markAttention(AttentionDestChanged)
		return x.finishFailure(code)
	}
	return x.finishFailure(code)
}

func isGuardWait(c ErrorCode) bool {
	return c == ErrDeleteGuard || c == ErrChangeGuard || c == ErrEmptySource
}

// needsPlan: a preview run, or a job that asks for a preview before its
// first real run and hasn't had one approved yet.
func (x *runExec) needsPlan() bool {
	if x.run.Decision != "" {
		return false
	}
	if RunKind(x.run.Kind) == KindPreview {
		return true
	}
	return x.job.Options.PreviewFirst && x.st.LastSuccessAt == nil
}

func typeOp(t JobType) engine.Op {
	switch t {
	case TypeMirror:
		return engine.OpSync
	case TypeArchive:
		return engine.OpArchive
	}
	return engine.OpCopy
}

// baseRequest is the engine request shared by every op of this run.
func (x *runExec) baseRequest(op engine.Op, sources []Endpoint) engine.JobRequest {
	req := engine.JobRequest{
		Op: op, RunID: x.run.ID, JobID: x.job.ID, Sources: sources, Dest: x.job.Dest,
		Filters: x.job.Filters, Options: x.job.Options, Guards: x.job.Guards, Retention: x.job.Retention,
		DestFolderID: x.job.DestFolderID, FirstRun: x.st.LastSuccessAt == nil,
	}
	if len(x.creds) > 0 {
		req.SMBCreds = x.creds
	}
	if x.st.LastSuccessAt != nil {
		req.Baseline = &engine.Baseline{SourceFiles: x.st.SourceFiles, DestFiles: x.st.DestFiles}
	}
	return req
}

// plan runs the dry run and rests in waiting_user with the preview.
func (x *runExec) plan(ctx context.Context) execOutcome {
	x.setPhase(PhaseTransfer)
	req := x.baseRequest(engine.OpPlan, x.job.Sources)
	req.PlanOp = typeOp(x.job.Type)
	req.PreviewFile = filepath.Join(x.s.store.DataDir(), PreviewsDir, safeName(x.run.ID)+".jsonl")
	res, code := x.runEngine(ctx, req, true)
	x.run.FilesAdded, x.run.FilesChanged, x.run.FilesDeleted = res.Counts.Added, res.Counts.Changed, res.Counts.Deleted
	x.run.BytesTotal = res.Counts.BytesAdd
	if code != "" && !(isGuardWait(code) && res.Guard != nil) {
		return x.finishFailure(code)
	}
	x.run.PreviewPath = req.PreviewFile
	var guard *engine.GuardInfo
	if res.Guard != nil {
		guard = res.Guard
	}
	return x.waitForUser(code, guard, req.PreviewFile)
}

// transfer moves the data: one engine job, or with a one_at_a_time
// hook, one per source wrapped in its app's stop and start.
func (x *runExec) transfer(ctx context.Context) (engine.Result, ErrorCode) {
	x.setPhase(PhaseTransfer)
	op := typeOp(x.job.Type)
	if x.run.Decision == DecideCopyOnce && op == engine.OpSync {
		op = engine.OpCopy
	}
	idx, h := oneAtATimeHook(x.job)
	if h == nil {
		req := x.baseRequest(op, x.job.Sources)
		x.withOverrides(&req)
		x.withGuardPreview(&req, 0)
		res, code := x.runEngine(ctx, req, true)
		x.keepGuardPreview(req, res, code)
		return res, code
	}
	var total engine.Result
	timeout := time.Duration(h.TimeoutSec) * time.Second
	for i, src := range x.job.Sources {
		if i > 0 {
			// Archive names carry the run time to the second; keep the
			// per-app archives of one run apart.
			sleepCtx(ctx, time.Until(x.now().Truncate(time.Second).Add(time.Second)))
		}
		app := strings.TrimPrefix(src.Preset, PresetAppData)
		x.setPhase(PhasePreHooks)
		if code := x.stopApps(ctx, idx, []string{app}, timeout, true); code != "" {
			return total, code
		}
		x.setPhase(PhaseTransfer)
		req := x.baseRequest(op, []Endpoint{src})
		req.Baseline = x.sourceBaseline(src)
		x.withOverrides(&req)
		x.withGuardPreview(&req, i)
		res, code := x.runEngine(ctx, req, true)
		x.keepGuardPreview(req, res, code)
		addResult(&total, res)
		if code == "" {
			if x.sourceFiles == nil {
				x.sourceFiles = map[string]int64{}
			}
			x.sourceFiles[sourceKey(src)] = res.Counts.SourceFiles
		}
		// Start this app again now, before the next one stops. One that
		// doesn't start stays recorded as running: the post phase and the
		// restart loop try it again.
		entry := x.hookEntry(idx)
		if entry.PriorState[app] == priorRunning {
			x.setPhase(PhasePostHooks)
			sctx, cancel := context.WithTimeout(context.Background(), timeout+time.Minute)
			sc := x.startApps(sctx, []string{app}, timeout)
			cancel()
			entry = x.hookEntry(idx)
			if sc != "" {
				if x.softCode == "" {
					x.softCode = sc
				}
			} else {
				entry.PriorState[app] = priorRestarted
			}
			if err := x.saveHooks(); err != nil {
				log.Printf("backup: run %s: %v", x.run.ID, err)
			}
		}
		if code != "" {
			return total, code
		}
	}
	entry := x.hookEntry(idx)
	apps, _ := toRestart(*entry)
	entry.PreDone, entry.PostDone = true, len(apps) == 0
	if err := x.saveHooks(); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}
	return total, ""
}

// sourceKey names a source in JobState.SourceFilesBy.
func sourceKey(ep Endpoint) string {
	return string(ep.Kind) + "|" + ep.RefID + "|" + ep.SubPath
}

// sourceBaseline is the sentinel baseline of a transfer of one source
// out of several: that source's own count from the last success. With
// none recorded (the first run since it was added, or state from an
// older version) there is no baseline - comparing one source with the
// whole job's count would trip the empty-source guard on every run.
func (x *runExec) sourceBaseline(src Endpoint) *engine.Baseline {
	if x.st.LastSuccessAt == nil {
		return nil
	}
	n, ok := x.st.SourceFilesBy[sourceKey(src)]
	if !ok {
		return nil
	}
	return &engine.Baseline{SourceFiles: n, DestFiles: x.st.DestFiles}
}

// withGuardPreview asks a copy or mirror transfer to keep its planning
// pass's list, so a run a guard stops can be reviewed like a preview.
func (x *runExec) withGuardPreview(req *engine.JobRequest, i int) {
	if req.Op != engine.OpSync && req.Op != engine.OpCopy {
		return
	}
	name := safeName(x.run.ID)
	if i > 0 {
		name += fmt.Sprintf("-%d", i)
	}
	req.PreviewFile = filepath.Join(x.s.store.DataDir(), PreviewsDir, name+".jsonl")
}

// keepGuardPreview keeps the list of the transfer that tripped a guard
// and removes it otherwise.
func (x *runExec) keepGuardPreview(req engine.JobRequest, res engine.Result, code ErrorCode) {
	if req.PreviewFile == "" {
		return
	}
	if isGuardWait(code) && res.Guard != nil {
		// Only a list the engine actually wrote; reviewing it and
		// continuing accepts what it shows (acceptedGuards), like a
		// preview.
		if _, err := os.Stat(req.PreviewFile); err == nil {
			x.guardPreview = req.PreviewFile
		}
		return
	}
	if err := os.Remove(req.PreviewFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("nivaroos-backup: run %s: removing its plan list: %v", x.run.ID, err)
	}
}

func addResult(dst *engine.Result, r engine.Result) {
	c := &dst.Counts
	c.Added += r.Counts.Added
	c.Changed += r.Counts.Changed
	c.Deleted += r.Counts.Deleted
	c.Skipped += r.Counts.Skipped
	c.Errored += r.Counts.Errored
	c.BytesTransferred += r.Counts.BytesTransferred
	c.BytesTotal += r.Counts.BytesTotal
	c.SourceFiles += r.Counts.SourceFiles
	c.DestFiles = r.Counts.DestFiles
	dst.FileErrors = append(dst.FileErrors, r.FileErrors...)
	dst.Checks = append(dst.Checks, r.Checks...)
	dst.MarkerWritten = dst.MarkerWritten || r.MarkerWritten
	if r.ArchiveName != "" {
		dst.ArchiveName = r.ArchiveName
	}
	if r.MountID != 0 {
		dst.MountID = r.MountID
	}
	if r.Guard != nil {
		dst.Guard = r.Guard
	}
	if r.ErrorCode != "" {
		dst.ErrorCode, dst.ErrorDetail = r.ErrorCode, r.ErrorDetail
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// overrides are the guards the user accepted for this run.
func (x *runExec) overrides() []string {
	var out []string
	if _, err := x.s.store.GetMetaJSON(runOverrideKey(x.run.ID), &out); err != nil {
		return nil
	}
	return out
}

// withOverrides puts the accepted guards on an engine request, bound to
// what the user reviewed when they accepted them.
func (x *runExec) withOverrides(req *engine.JobRequest) {
	req.GuardOverride = x.overrides()
	if len(req.GuardOverride) == 0 {
		return
	}
	var rev engine.Reviewed
	if ok, err := x.s.store.GetMetaJSON(runReviewedKey(x.run.ID), &rev); err == nil && ok {
		req.GuardReviewed = &rev
	}
}

func runOverrideKey(runID string) string { return "run." + runID + ".override" }

// runReviewedKey holds the engine.Reviewed counts of a run's decision.
func runReviewedKey(runID string) string { return "run." + runID + ".reviewed" }

// clearRunDecision forgets a run's accepted guards and reviewed counts.
func (s *Service) clearRunDecision(runID string) error {
	err := s.store.DeleteMeta(runOverrideKey(runID))
	if rerr := s.store.DeleteMeta(runReviewedKey(runID)); err == nil {
		err = rerr
	}
	return err
}

// runEngine starts one engine job and follows it to the end: live stats
// (progress events at most once a second, only when they change), the
// log stream, the phase the engine reports, cancel and max_duration.
func (x *runExec) runEngine(ctx context.Context, req engine.JobRequest, progress bool) (engine.Result, ErrorCode) {
	maxDur := time.Duration(x.job.Options.MaxDurationSec) * time.Second
	if maxDur <= 0 {
		maxDur = DefaultMaxDurationSec * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, maxDur)
	defer cancel()

	sctx, scancel := context.WithTimeout(ctx, 30*time.Second)
	eid, err := x.s.engine.StartJob(sctx, req)
	scancel()
	if err != nil {
		code := engine.CodeOf(err)
		if ctx.Err() != nil {
			code = x.cancelCode()
		}
		x.log.Raw(engine.LogError, "start: "+describeErr(err))
		return engine.Result{}, code
	}
	x.run.EngineJobID = int64(eid)
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}

	logCtx, logCancel := context.WithCancel(context.Background())
	defer logCancel()
	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		ch, err := x.s.engine.JobLog(logCtx, eid)
		if err != nil {
			return
		}
		for line := range ch {
			x.log.Write(line)
		}
	}()

	tick := time.NewTicker(x.s.enginePoll())
	defer tick.Stop()
	done := tctx.Done()
	var stopDeadline time.Time
	var stopReason ErrorCode
	misses := 0
	for {
		pctx, pcancel := context.WithTimeout(context.Background(), 10*time.Second)
		st, err := x.s.engine.JobStatus(pctx, eid)
		pcancel()
		if err != nil {
			misses++
			if engine.CodeOf(err) == engine.CodeNotFound || misses >= 10 {
				x.log.Raw(engine.LogError, "status: "+describeErr(err))
				return engine.Result{}, ErrEngineUnavailable
			}
		} else {
			misses = 0
			if progress {
				x.observe(st.Stats)
			}
			if st.State == engine.JobDone || st.State == engine.JobError {
				select {
				case <-logDone:
				case <-time.After(5 * time.Second):
				}
				res := engine.Result{}
				if st.Result != nil {
					res = *st.Result
				}
				code := res.ErrorCode
				if stopReason != "" && (code == "" || code == ErrCancelledByUser) {
					// The engine reports a stop as cancelled_by_user; say
					// why we stopped it.
					code = stopReason
				}
				if res.ErrorDetail != "" {
					x.log.Raw(engine.LogError, res.ErrorDetail)
				}
				return res, code
			}
		}
		if !stopDeadline.IsZero() && x.now().After(stopDeadline) {
			x.log.Raw(engine.LogError, "the engine did not stop the job within a minute")
			return engine.Result{}, stopReason
		}
		select {
		case <-done:
			done = nil
			stopReason = x.cancelCode()
			if ctx.Err() == nil && errors.Is(tctx.Err(), context.DeadlineExceeded) {
				stopReason = ErrMaxDuration
			}
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := x.s.engine.StopJob(stopCtx, eid); err != nil {
				x.log.Raw(engine.LogWarn, "stop: "+describeErr(err))
			}
			stopCancel()
			stopDeadline = x.now().Add(engineStopGrace)
		case <-tick.C:
		}
	}
}

// observe takes a stats sample: live block, phase, progress event.
func (x *runExec) observe(st engine.Stats) {
	live := LiveStats{
		Bytes: st.Bytes, TotalBytes: st.TotalBytes, Files: st.Files, TotalFiles: st.TotalFiles,
		SpeedBps: st.SpeedBps, ETASec: st.ETASec, Errors: st.Errors, CurrentFile: st.CurrentFile,
	}
	switch st.Step {
	case "verify":
		x.setPhase(PhaseVerify)
	case "prune":
		x.setPhase(PhasePrune)
	case "transfer", "plan", "restore":
		if RunPhase(x.run.Phase) != PhaseTransfer && RunPhase(x.run.Phase) != PhasePostHooks {
			x.setPhase(PhaseTransfer)
		}
	}
	x.liveMu.Lock()
	x.live = live
	changed := !liveEqual(live, x.published)
	if changed {
		x.published = live
	}
	x.liveMu.Unlock()
	x.s.setLive(x.run.ID, &live)
	if changed {
		x.s.pub.publish(EventRunProgress, progressEventProps(x.run, live))
	}
}

func liveEqual(a, b LiveStats) bool {
	etaA, etaB := int64(-1), int64(-1)
	if a.ETASec != nil {
		etaA = *a.ETASec
	}
	if b.ETASec != nil {
		etaB = *b.ETASec
	}
	a.ETASec, b.ETASec = nil, nil
	return a == b && etaA == etaB
}

func (x *runExec) applyCounts(res engine.Result) {
	c := res.Counts
	x.run.FilesAdded, x.run.FilesChanged, x.run.FilesDeleted = c.Added, c.Changed, c.Deleted
	x.run.FilesSkipped, x.run.FilesErrored = c.Skipped, c.Errored
	x.run.BytesTransferred, x.run.BytesTotal = c.BytesTransferred, c.BytesTotal
	if res.MarkerWritten {
		x.log.Info("backup.log.marker_written", nil)
	}
	if res.ArchiveName != "" {
		x.log.Info("backup.log.archive_done", map[string]interface{}{"name": res.ArchiveName, "bytes": c.BytesTransferred})
	}
	if len(res.FileErrors) > 0 {
		if err := saveFileErrors(x.log.Path(), res.FileErrors); err != nil {
			log.Printf("backup: run %s: file errors: %v", x.run.ID, err)
		}
	}
}

// prune drops recycle folders / archives past retention, unless pruning
// is suspended: another run of the job waits for review, or this run went
// ahead only because the user accepted a guard.
func (x *runExec) prune(ctx context.Context) {
	var op engine.Op
	switch {
	case x.job.Type == TypeMirror && x.job.Retention.VersionsDays > 0:
		op = engine.OpPurgeVersions
	case x.job.Type == TypeArchive && x.job.Retention.KeepLast > 0:
		op = engine.OpPurgeArchives
	default:
		return
	}
	if len(x.overrides()) > 0 {
		return
	}
	waiting, err := x.s.store.ListRuns(RunFilter{JobID: x.job.ID, Statuses: []RunStatus{StatusWaitingUser}, Limit: 1})
	if err != nil || len(waiting) > 0 {
		return
	}
	x.setPhase(PhasePrune)
	res, code := x.runEngine(ctx, x.baseRequest(op, nil), false)
	if code != "" {
		x.log.Raw(engine.LogWarn, "clean-up of old versions failed: "+string(code))
		return
	}
	x.log.Info("backup.log.pruned", map[string]interface{}{"name": fmt.Sprintf("%d", res.Counts.Deleted)})
}

// ---------------------------------------------------------------------
// Conditions (spec §7.4)

// checkConditions returns the unmet condition's code, "" when all hold.
func (x *runExec) checkConditions(ctx context.Context) ErrorCode {
	if w := x.job.Conditions.Window; w != nil && !inWindow(*w, x.now()) {
		x.log.Info("backup.log.check", map[string]interface{}{"check_key": "backup.err.window_closed.title", "status": string(engine.CheckFail)})
		return ErrWindowClosed
	}
	if x.job.Conditions.DestAvailable {
		res, err := x.s.engine.Resolve(ctx, engine.ResolveRequest{Endpoint: x.job.Dest, SMBCreds: x.credFor(x.job.Dest)})
		switch {
		case err != nil:
			code := engine.CodeOf(err)
			x.log.Raw(engine.LogError, "destination: "+describeErr(err))
			if code == ErrIOError || code == ErrEngineUnavailable {
				return ErrDestOffline
			}
			return code
		case !res.Online:
			x.log.Info("backup.log.check", map[string]interface{}{"check_key": "backup.check." + engine.CheckDestResolves, "status": string(engine.CheckFail)})
			return ErrDestOffline
		case res.Marker != nil && x.job.DestFolderID != "" && res.Marker.DestFolderID != x.job.DestFolderID:
			x.log.Info("backup.log.check", map[string]interface{}{"check_key": "backup.check." + engine.CheckDestMarker, "status": string(engine.CheckFail)})
			return ErrDestMarkerMismatch
		}
		if res.Root != "" {
			x.log.Info("backup.log.check", map[string]interface{}{"check_key": "backup.check." + engine.CheckDestResolves, "status": string(engine.CheckPass)})
		}
	}
	for _, src := range x.job.Sources {
		res, err := x.s.engine.Resolve(ctx, engine.ResolveRequest{Endpoint: src, SMBCreds: x.credFor(src)})
		if err != nil {
			code := engine.CodeOf(err)
			x.log.Raw(engine.LogError, "source: "+describeErr(err))
			if code == ErrIOError || code == ErrEngineUnavailable {
				return ErrSourceOffline
			}
			return code
		}
		if !res.Online {
			x.log.Info("backup.log.check", map[string]interface{}{"check_key": "backup.check." + engine.CheckSourceResolves, "status": string(engine.CheckFail)})
			return ErrSourceOffline
		}
	}
	return ""
}

func (x *runExec) credFor(ep Endpoint) *engine.SMBCreds {
	if ep.Kind != EPSMB {
		return nil
	}
	if c, ok := x.creds[ep.RefID]; ok {
		return &c
	}
	return nil
}

// inWindow reports whether t (server local time) is inside [start, end);
// end before start wraps midnight.
func inWindow(w TimeWindow, t time.Time) bool {
	parse := func(s string) int {
		var h, m int
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
			return -1
		}
		return h*60 + m
	}
	start, end := parse(w.Start), parse(w.End)
	if start < 0 || end < 0 || start == end {
		return true
	}
	lt := t.In(time.Local)
	now := lt.Hour()*60 + lt.Minute()
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

// unmet applies when_unmet: skip, fail, or wait (re-check every 5 min
// until WaitMaxMin after the first check, then skipped).
func (x *runExec) unmet(code ErrorCode) execOutcome {
	switch {
	case code == ErrDestMarkerMismatch:
		x.markAttention(AttentionDestChanged)
		return x.finishFailure(code)
	case code != ErrDestOffline && code != ErrSourceOffline && code != ErrWindowClosed:
		// Not a condition: the endpoint can never resolve (config error).
		return x.finishFailure(code)
	}
	switch x.job.Conditions.WhenUnmet {
	case UnmetFail:
		return x.finishFailure(code)
	case UnmetWait:
		waitMax := time.Duration(x.job.Conditions.WaitMaxMin) * time.Minute
		if waitMax <= 0 {
			waitMax = DefaultWaitMaxMin * time.Minute
		}
		next := x.now().Add(x.s.waitRecheck())
		if x.run.StartedAt != nil && next.Before(x.run.StartedAt.Add(waitMax)) {
			x.run.ErrorCode = string(code)
			x.run.Phase = string(PhasePrecheck)
			return x.requeueAt(next, false)
		}
	}
	return x.finishSkipped(code)
}

// ---------------------------------------------------------------------
// Restore and purge runs

func restoreToOriginal(job Job, spec engine.RestoreSpec) bool {
	return len(job.Sources) == 1 && sameEndpoint(spec.Target, job.Sources[0])
}

func (x *runExec) doRestore(ctx context.Context) execOutcome {
	spec, err := decodeRestoreSpec(x.run.RestoreSpec)
	if err != nil {
		x.log.Raw(engine.LogError, "restore spec: "+describeErr(err))
		return x.finishFailure(ErrInternal)
	}
	// Restoring into an app's or VM's own folder while it runs could
	// corrupt it: the job's stop/shutdown hooks wrap a restore to the
	// original place too.
	var code ErrorCode
	if restoreToOriginal(x.job, spec) {
		code, _ = x.preHooks(ctx)
	}
	var res engine.Result
	if code == "" {
		x.setPhase(PhaseTransfer)
		req := x.baseRequest(engine.OpRestore, nil)
		req.Restore = &spec
		res, code = x.runEngine(ctx, req, true)
	}
	x.applyCounts(res)
	if pc := x.postHooks(); pc != "" && x.softCode == "" {
		x.softCode = pc
	}
	if code != "" {
		return x.finishFailure(code)
	}
	summary := Message{Key: "backup.run.summary.restored", Args: map[string]interface{}{
		"files": res.Counts.Added + res.Counts.Changed, "bytes": res.Counts.BytesTransferred,
	}}
	status := StatusSuccess
	if len(res.FileErrors) > 0 || res.Counts.Errored > 0 || x.softCode != "" {
		status = StatusPartial
	}
	x.finish(status, x.softCode, summary)
	if status == StatusPartial {
		x.notifyPartial(res)
	}
	return execOutcome{}
}

func (x *runExec) doPurge(ctx context.Context) execOutcome {
	x.setPhase(PhaseTransfer)
	req := x.baseRequest(engine.OpPurgeDest, nil)
	req.FirstRun = false
	res, code := x.runEngine(ctx, req, true)
	x.applyCounts(res)
	if code != "" {
		return x.finishFailure(code)
	}
	x.finish(StatusSuccess, "", Message{Key: "backup.run.summary.pruned", Args: map[string]interface{}{"removed": res.Counts.Deleted}})
	return execOutcome{}
}

// ---------------------------------------------------------------------
// Endings

func failedSummary(code ErrorCode) Message {
	return Message{Key: "backup.run.summary.failed", Args: map[string]interface{}{"reason_key": errorReasonKey(code)}}
}

// finish writes a final state: the run row, the log's last line and
// compression, the run-end event.
func (x *runExec) finish(status RunStatus, code ErrorCode, summary Message) {
	now := x.now()
	x.run.Status, x.run.ErrorCode, x.run.Summary, x.run.EndedAt = string(status), string(code), EncodeMessage(summary), &now
	x.log.Info("backup.log.finished", map[string]interface{}{"status_key": "backup.status." + string(status)})
	x.log.Close()
	if p, err := compressLog(x.log.Path()); err == nil {
		x.run.LogPath = p
	} else {
		log.Printf("backup: run %s: compress log: %v", x.run.ID, err)
	}
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}
	if err := x.s.clearRunDecision(x.run.ID); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}
	x.liveMu.Lock()
	st := x.live
	x.liveMu.Unlock()
	x.s.pub.publish(EventRunEnd, endProps(x.run, st))
	if status == StatusSuccess || status == StatusPartial {
		// The run reached its drives: remember them as seen now.
		x.s.rememberDrives(x.job, &now)
	}
}

func (x *runExec) finishSuccess(res engine.Result) execOutcome {
	c := res.Counts
	status := StatusSuccess
	summary := Message{Key: "backup.run.summary.ok", Args: map[string]interface{}{
		"added": c.Added, "changed": c.Changed, "deleted": c.Deleted, "bytes": c.BytesTransferred,
	}}
	if c.Added == 0 && c.Changed == 0 && c.Deleted == 0 {
		summary = Message{Key: "backup.run.summary.ok_nothing"}
	}
	errored := c.Errored
	if int64(len(res.FileErrors)) > errored {
		errored = int64(len(res.FileErrors))
	}
	if errored > 0 || x.softCode != "" {
		status = StatusPartial
		summary = Message{Key: "backup.run.summary.partial", Args: map[string]interface{}{"added": c.Added, "changed": c.Changed, "errors": errored}}
	}
	x.finish(status, x.softCode, summary)
	endedAt := *x.run.EndedAt
	if err := x.s.store.UpdateJobState(x.job.ID, func(st *JobState) {
		st.LastSuccessAt = &endedAt
		st.SizeBytes = c.BytesTotal
		st.SourceFiles, st.DestFiles = c.SourceFiles, c.DestFiles
		st.SourceFilesBy = x.sourceFiles
		st.OfflineMisses, st.OfflineNotified, st.GuardTripped, st.StaleNotifiedAt = 0, false, false, nil
	}); err != nil {
		log.Printf("backup: job %s: %v", x.job.ID, err)
	}
	switch {
	case status == StatusPartial && errored > 0:
		x.notifyPartial(res)
	case status == StatusPartial:
		x.notifyFailure(x.softCode, NotifyLevelWarning)
	case x.job.Notify.OnSuccess:
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.success", Args: map[string]interface{}{"job": x.job.Name}},
			Level:   NotifyLevelSuccess, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "run",
			WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
		})
	}
	if RunTrigger(x.run.Trigger) == RunByVolumeMounted && x.job.Dest.Kind == EPUSB {
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.usb_done", Args: map[string]interface{}{"job": x.job.Name, "drive": x.job.Dest.Label}},
			Level:   NotifyLevelSuccess, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "run",
			WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
		})
	}
	return execOutcome{}
}

func (x *runExec) notifyPartial(res engine.Result) {
	errored := res.Counts.Errored
	if int64(len(res.FileErrors)) > errored {
		errored = int64(len(res.FileErrors))
	}
	x.s.notify(Notification{
		Message: Message{Key: "backup.notify.partial", Args: map[string]interface{}{"job": x.job.Name, "errors": errored}},
		Level:   NotifyLevelWarning, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "run",
		WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
	})
}

func (x *runExec) notifyFailure(code ErrorCode, level string) {
	x.s.notify(Notification{
		Message: Message{Key: "backup.notify.failed", Args: map[string]interface{}{"job": x.job.Name, "reason_key": errorReasonKey(code)}},
		Level:   level, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "run",
		WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
	})
}

// finishFailure ends a run that didn't finish its work: by the error's
// class it is retried, deferred to tomorrow, cancelled or failed.
func (x *runExec) finishFailure(code ErrorCode) execOutcome {
	if code == "" {
		code = ErrInternal
	}
	switch code {
	case ErrCancelledByUser, ErrCancelledUnmounted:
		x.finish(StatusCancelled, code, Message{Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(code)}})
		return execOutcome{}
	}
	if ClassOf(code) == ClassDeferred {
		retryAt := nextLocalMidnight(x.now()).Add(deferredRequeueAfterMid)
		x.finish(StatusSkipped, code, Message{Key: "backup.run.summary.deferred", Args: map[string]interface{}{
			"reason_key": errorReasonKey(code), "retry_at": retryAt.Format(time.RFC3339),
		}})
		// Once: a deferred retry that hits the quota again waits for the
		// next scheduled run instead.
		if RunTrigger(x.run.Trigger) != RunByRetry {
			x.enqueueRetry(retryAt)
		}
		return execOutcome{}
	}
	if RunKind(x.run.Kind) != KindRestore && RunKind(x.run.Kind) != KindPrune &&
		Retryable(code, x.job.Conditions.WhenUnmet) && x.run.Attempt <= x.job.Retry.Max {
		retryAt := x.now().Add(retryDelay(x.job.Retry, x.run.Attempt))
		x.log.Info("backup.log.retry", map[string]interface{}{
			"attempt": x.run.Attempt + 1, "max": x.job.Retry.Max + 1, "retry_at": retryAt.Format(time.RFC3339),
		})
		x.finish(StatusFailed, code, failedSummary(code))
		x.enqueueRetry(retryAt)
		return execOutcome{}
	}
	x.finish(StatusFailed, code, failedSummary(code))
	x.notifyFailure(code, NotifyLevelError)
	return execOutcome{}
}

// finishSkipped records an unmet condition that isn't a failure.
func (x *runExec) finishSkipped(code ErrorCode) execOutcome {
	x.finish(StatusSkipped, code, Message{Key: "backup.run.summary.skipped", Args: map[string]interface{}{"reason_key": errorReasonKey(code)}})
	if code != ErrDestOffline {
		return execOutcome{}
	}
	notifyNow := false
	if err := x.s.store.UpdateJobState(x.job.ID, func(st *JobState) {
		st.OfflineMisses++
		if st.OfflineMisses >= 2 && !st.OfflineNotified {
			st.OfflineNotified = true
			notifyNow = true
		}
	}); err != nil {
		log.Printf("backup: job %s: %v", x.job.ID, err)
	}
	if notifyNow {
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.offline", Args: map[string]interface{}{"job": x.job.Name, "dest": x.job.Dest.Label}},
			Level:   NotifyLevelWarning, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "app",
			WindowProps: map[string]interface{}{"section": "jobs", "jobId": x.job.ID},
		})
	}
	return execOutcome{}
}

// enqueueRetry queues the next attempt of this run.
func (x *runExec) enqueueRetry(at time.Time) {
	if _, _, err := x.s.queue.Enqueue(x.job, EnqueueOptions{
		Kind: RunKind(x.run.Kind), Trigger: RunByRetry, NotBefore: at, Attempt: x.run.Attempt + 1,
	}); err != nil {
		log.Printf("backup: job %s: queue retry: %v", x.job.ID, err)
	}
}

// retryDelay is the backoff before attempt+1; the last entry repeats.
func retryDelay(r Retry, attempt int) time.Duration {
	b := r.BackoffSec
	if len(b) == 0 {
		b = DefaultRetryBackoffSec
	}
	i := attempt - 1
	if i < 0 {
		i = 0
	}
	if i >= len(b) {
		i = len(b) - 1
	}
	return time.Duration(b[i]) * time.Second
}

func nextLocalMidnight(t time.Time) time.Time {
	lt := t.In(time.Local)
	return time.Date(lt.Year(), lt.Month(), lt.Day()+1, 0, 0, 0, 0, time.Local)
}

// waitForUser rests the run in waiting_user: a tripped guard, or a
// preview to approve. The worker slot and locks are released; post hooks
// already ran.
func (x *runExec) waitForUser(code ErrorCode, guard *engine.GuardInfo, previewPath string) execOutcome {
	now := x.now()
	x.run.Status, x.run.ErrorCode, x.run.EndedAt = string(StatusWaitingUser), string(code), &now
	if previewPath != "" {
		x.run.PreviewPath = previewPath
	}
	x.run.GuardInfo = ""
	summary := Message{Key: "backup.run.summary.preview", Args: map[string]interface{}{
		"add": x.run.FilesAdded, "update": x.run.FilesChanged, "delete": x.run.FilesDeleted,
	}}
	if guard != nil {
		raw, _ := json.Marshal(guard)
		x.run.GuardInfo = string(raw)
		pct := math.Round(guard.Pct)
		summary = Message{Key: "backup.run.summary.waiting", Args: map[string]interface{}{"guard_key": guardKey(guard.Guard), "pct": pct}}
		x.log.Warn("backup.log.guard_tripped", map[string]interface{}{"guard_key": guardKey(guard.Guard), "pct": pct, "limit": guard.Limit})
	}
	x.run.Summary = EncodeMessage(summary)
	if err := x.s.store.SaveRun(x.run); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
	}
	x.s.pub.publish(EventRunWaiting, runEventProps(x.run))
	if guard != nil {
		if err := x.s.store.UpdateJobState(x.job.ID, func(st *JobState) { st.GuardTripped = true }); err != nil {
			log.Printf("backup: job %s: %v", x.job.ID, err)
		}
	}
	// A guard always asks; a first-run preview asks when nobody is
	// looking at it already (it wasn't started by hand).
	switch {
	case guard != nil:
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.waiting", Args: map[string]interface{}{
				"job": x.job.Name, "guard_key": guardKey(guard.Guard), "pct": math.Round(guard.Pct),
			}},
			Level: NotifyLevelWarning, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "preview",
			WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
		})
	case RunTrigger(x.run.Trigger) != RunByManual:
		x.s.notify(Notification{
			Message: Message{Key: "backup.notify.preview_ready", Args: map[string]interface{}{"job": x.job.Name}},
			Level:   NotifyLevelInfo, JobID: x.job.ID, RunID: x.run.ID, WindowKind: "preview",
			WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.job.ID},
		})
	}
	return execOutcome{}
}

// markAttention sets a job's needs_attention (server bookkeeping: no
// revision bump) and tells the UI.
func (x *runExec) markAttention(att string) {
	j, err := x.s.store.MutateJob(x.job.ID, x.now(), false, func(j *Job) error {
		j.NeedsAttention = att
		return nil
	})
	if err != nil {
		log.Printf("backup: job %s: %v", x.job.ID, err)
		return
	}
	x.s.jobChanged(j, JobChangeUpdated)
}
