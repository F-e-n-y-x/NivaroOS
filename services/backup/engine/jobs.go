package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/rc"
)

// job is one engine job from StartJob to its removal an hour after it
// ended.
type job struct {
	id    JobID
	req   JobRequest
	cloud bool // touches a cloud remote: at most MaxCloud at a time
	group string
	log   *logSpool

	ctx    context.Context
	cancel context.CancelCauseFunc

	mu       sync.Mutex
	state    JobState
	step     string
	started  *time.Time
	ended    *time.Time
	result   *Result
	final    Stats // stats frozen at the end
	progress progress
	// watched are the mounts this job reads or writes; if one leaves the
	// table the job is cancelled with cancelled_unmounted.
	watched map[int]string
}

// progress is what an operation reports itself when rclone's accounting
// doesn't see the work (archive, extract) or doesn't know the totals.
type progress struct {
	totalBytes, totalFiles int64
	bytes, files           int64 // own counters (archive, extract)
	own                    bool  // bytes/files above are authoritative
	current                string
	ownStart               time.Time
}

func (j *job) setStep(step string) {
	j.mu.Lock()
	j.step = step
	j.mu.Unlock()
}

func (j *job) setTotals(files, bytes int64) {
	j.mu.Lock()
	j.progress.totalFiles, j.progress.totalBytes = files, bytes
	j.mu.Unlock()
}

// addOwn counts bytes and files an operation moved itself.
func (j *job) addOwn(bytes, files int64, current string) {
	j.mu.Lock()
	if !j.progress.own {
		j.progress.own = true
		j.progress.ownStart = time.Now()
	}
	j.progress.bytes += bytes
	j.progress.files += files
	if current != "" {
		j.progress.current = current
	}
	j.mu.Unlock()
}

// watch registers a mount this job depends on.
func (j *job) watch(id int, what string) {
	j.mu.Lock()
	if j.watched == nil {
		j.watched = map[int]string{}
	}
	j.watched[id] = what
	j.mu.Unlock()
}

// logLine appends one line to the job's log.
func (j *job) logLine(lvl, code, key string, args map[string]interface{}) {
	j.appendLine(LogLine{T: time.Now(), Lvl: lvl, Code: code, MsgKey: key, Args: args})
}

func (j *job) logRaw(lvl, raw string) {
	j.appendLine(LogLine{T: time.Now(), Lvl: lvl, Code: "raw", Raw: raw})
}

// appendLine logs l with every share password of the job removed from
// its texts: a backend that echoes its connection settings in an error
// must not put them in a run log (spec §14).
func (j *job) appendLine(l LogLine) {
	l.Raw = j.redact(l.Raw)
	for k, v := range l.Args {
		if s, ok := v.(string); ok {
			l.Args[k] = j.redact(s)
		}
	}
	j.log.append(l)
}

// redact removes the job's SMB passwords from s.
func (j *job) redact(s string) string {
	for _, c := range j.req.SMBCreds {
		c := c
		s = redact(s, &c)
	}
	return s
}

// jobManager queues and runs jobs (spec §3.5: at most MaxRunning jobs,
// at most MaxCloud of them touching a cloud remote).
type jobManager struct {
	e *Engine

	mu      sync.Mutex
	nextID  JobID
	jobs    map[JobID]*job
	queue   []*job
	running int
	cloud   int
	closed  bool
	wg      sync.WaitGroup
}

func newJobManager(e *Engine) *jobManager {
	return &jobManager{e: e, jobs: map[JobID]*job{}}
}

// validOps are the ops StartJob accepts.
var validOps = map[Op]bool{OpCopy: true, OpSync: true, OpArchive: true, OpPlan: true, OpCheck: true,
	OpPurgeVersions: true, OpPurgeArchives: true, OpRestore: true, OpPurgeDest: true}

// validateRequest rejects requests no run could satisfy (a caller bug).
func validateRequest(req JobRequest) error {
	if !validOps[req.Op] {
		return Errorf(CodeInternal, "unknown op %q", req.Op)
	}
	if req.RunID == "" {
		return Errorf(CodeInternal, "run_id is required")
	}
	switch req.Op {
	case OpCopy, OpSync, OpCheck:
		if len(req.Sources) != 1 {
			return Errorf(CodeInternal, "%s takes exactly one source, got %d", req.Op, len(req.Sources))
		}
	case OpArchive:
		if len(req.Sources) < 1 || len(req.Sources) > maxArchiveSources {
			return Errorf(CodeInternal, "archive takes 1 to %d sources, got %d", maxArchiveSources, len(req.Sources))
		}
		if req.JobID == "" {
			return Errorf(CodeInternal, "archive needs job_id (it names the archive)")
		}
	case OpPlan:
		switch req.PlanOp {
		case OpCopy, OpSync:
			if len(req.Sources) != 1 {
				return Errorf(CodeInternal, "plan of %s takes exactly one source, got %d", req.PlanOp, len(req.Sources))
			}
		case OpArchive:
			if len(req.Sources) < 1 || len(req.Sources) > maxArchiveSources {
				return Errorf(CodeInternal, "plan of archive takes 1 to %d sources, got %d", maxArchiveSources, len(req.Sources))
			}
		default:
			return Errorf(CodeInternal, "plan_op must be copy, sync or archive, got %q", req.PlanOp)
		}
	case OpPurgeArchives:
		if req.JobID == "" {
			return Errorf(CodeInternal, "purge_archives needs job_id")
		}
	case OpPurgeDest:
		if req.DestFolderID == "" {
			return Errorf(CodeInternal, "purge_dest needs dest_folder_id: nothing is deleted without the marker check")
		}
	case OpRestore:
		if req.Restore == nil {
			return Errorf(CodeInternal, "restore needs a restore spec")
		}
		switch req.Restore.Conflict {
		case ConflictKeepBoth, ConflictOverwrite, ConflictSkip:
		default:
			return Errorf(CodeInternal, "unknown conflict policy %q", req.Restore.Conflict)
		}
	}
	return nil
}

// touchesCloud reports whether a request uses a cloud remote.
func touchesCloud(req JobRequest) bool {
	if req.Dest.Kind == EPCloud {
		return true
	}
	for _, s := range req.Sources {
		if s.Kind == EPCloud {
			return true
		}
	}
	return req.Restore != nil && req.Restore.Target.Kind == EPCloud
}

// StartJob queues a job and returns at once.
func (e *Engine) StartJob(ctx context.Context, req JobRequest) (JobID, error) {
	if err := validateRequest(req); err != nil {
		return 0, err
	}
	return e.jobs.start(req)
}

func (m *jobManager) start(req JobRequest) (JobID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, Errorf(CodeEngineUnavailable, "engine is shutting down")
	}
	m.nextID++
	id := m.nextID
	spool, err := newLogSpool(m.e.cfg.SpoolDir, id, m.e.cfg.Logf)
	if err != nil {
		m.nextID--
		return 0, err
	}
	ctx, cancel := context.WithCancelCause(m.e.ctx)
	j := &job{
		id: id, req: req, cloud: touchesCloud(req), group: "bk_" + req.RunID, log: spool,
		ctx: ctx, cancel: cancel, state: JobQueued,
	}
	m.jobs[id] = j
	m.queue = append(m.queue, j)
	m.dispatchLocked()
	return id, nil
}

// dispatchLocked starts queued jobs while there are free slots; a cloud
// job waiting for the cloud slot doesn't hold back local jobs behind it.
func (m *jobManager) dispatchLocked() {
	for i := 0; i < len(m.queue) && m.running < m.e.cfg.MaxRunning; {
		j := m.queue[i]
		if j.cloud && m.cloud >= m.e.cfg.MaxCloud {
			i++
			continue
		}
		m.queue = append(m.queue[:i], m.queue[i+1:]...)
		m.running++
		if j.cloud {
			m.cloud++
		}
		now := time.Now()
		j.mu.Lock()
		j.state, j.started = JobRunning, &now
		j.mu.Unlock()
		m.wg.Add(1)
		go m.run(j)
	}
}

func (m *jobManager) run(j *job) {
	defer m.wg.Done()
	res, err := m.execute(j)
	m.finish(j, res, err)
	m.mu.Lock()
	m.running--
	if j.cloud {
		m.cloud--
	}
	m.dispatchLocked()
	m.mu.Unlock()
}

// execute runs the op under recover (a panic in our code must not take
// every other job down with the process), at low priority when asked.
func (m *jobManager) execute(j *job) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			m.e.cfg.Logf("engine: job %d (%s) panicked: %v\n%s", j.id, j.req.Op, r, debug.Stack())
			err = Errorf(CodeInternal, "the engine hit a bug while running %s: %v", j.req.Op, r)
		}
	}()
	ctx := accounting.WithStatsGroup(j.ctx, j.group)
	if d := j.req.Options.MaxDurationSec; d > 0 && j.req.Op != OpPlan {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, time.Duration(d)*time.Second,
			Errorf(CodeMaxDuration, "the run took longer than its limit of %s", time.Duration(d)*time.Second))
		defer cancel()
	}
	if j.req.Options.LowPriority {
		return runLowPriority(func() (Result, error) { return m.e.runOp(ctx, j) })
	}
	return m.e.runOp(ctx, j)
}

// finish records the result, freezes the stats and emits job.done.
func (m *jobManager) finish(j *job, res Result, err error) {
	if err != nil {
		ee := engineErr(err, "")
		if ctxErr := j.ctx.Err(); ctxErr != nil {
			// A cancel wins over whatever error the cancel caused.
			if cause := context.Cause(j.ctx); cause != nil {
				ee = engineErr(cause, "")
			}
		}
		res.ErrorCode, res.ErrorDetail = ee.Code, ee.Detail
		if ee.Guard != nil {
			res.Guard = ee.Guard
		}
	}
	res.ErrorDetail = j.redact(res.ErrorDetail)
	for i := range res.FileErrors {
		res.FileErrors[i].Detail = j.redact(res.FileErrors[i].Detail)
	}
	if res.Checks == nil {
		res.Checks = []Check{}
	}
	if res.FileErrors == nil {
		res.FileErrors = []FileError{}
	}
	st := m.e.liveStats(j)
	now := time.Now()
	j.mu.Lock()
	j.final = st
	j.ended = &now
	j.result = &res
	j.state = JobDone
	if res.ErrorCode != "" {
		j.state = JobError
	}
	state := j.state
	j.mu.Unlock()
	deleteStatsGroup(j.group)
	j.log.finish()
	m.e.subs.emit(Event{Type: EventJobDone, Time: now, JobID: j.id, RunID: j.req.RunID, State: state})
}

// deleteStatsGroup frees rclone's accounting for a finished job.
func deleteStatsGroup(group string) {
	if call := rc.Calls.Get("core/stats-delete"); call != nil {
		_, _ = call.Fn(context.Background(), rc.Params{"group": group})
	}
}

func (m *jobManager) get(id JobID) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, Errorf(CodeNotFound, "no engine job %d", id)
	}
	return j, nil
}

// JobStatus returns live stats while running and the result when done.
func (e *Engine) JobStatus(ctx context.Context, id JobID) (JobStatus, error) {
	j, err := e.jobs.get(id)
	if err != nil {
		return JobStatus{}, err
	}
	st := e.liveStats(j)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == JobDone || j.state == JobError {
		st = j.final
	}
	out := JobStatus{ID: j.id, RunID: j.req.RunID, Op: j.req.Op, State: j.state, Stats: st, StartedAt: j.started, EndedAt: j.ended}
	if j.result != nil {
		r := *j.result
		out.Result = &r
	}
	return out, nil
}

// StopJob cancels a queued or running job.
func (e *Engine) StopJob(ctx context.Context, id JobID) error {
	j, err := e.jobs.get(id)
	if err != nil {
		return err
	}
	e.jobs.mu.Lock()
	for i, q := range e.jobs.queue {
		if q == j {
			e.jobs.queue = append(e.jobs.queue[:i], e.jobs.queue[i+1:]...)
			e.jobs.mu.Unlock()
			j.cancel(Errorf(CodeCancelledByUser, "stopped before it started"))
			e.jobs.finish(j, Result{}, Errorf(CodeCancelledByUser, "stopped before it started"))
			return nil
		}
	}
	e.jobs.mu.Unlock()
	j.cancel(Errorf(CodeCancelledByUser, "stopped by the user"))
	return nil
}

// JobLog streams a job's log from its first line.
func (e *Engine) JobLog(ctx context.Context, id JobID) (<-chan LogLine, error) {
	j, err := e.jobs.get(id)
	if err != nil {
		return nil, err
	}
	return j.log.stream(ctx), nil
}

// checkMounts cancels running jobs whose watched mounts left the table
// (spec §6.2 cancel on unmount).
func (m *jobManager) checkMounts(s *snapshot) {
	m.mu.Lock()
	var list []*job
	for _, j := range m.jobs {
		list = append(list, j)
	}
	m.mu.Unlock()
	for _, j := range list {
		j.mu.Lock()
		active := j.state == JobRunning
		var gone []string
		for id, what := range j.watched {
			if _, ok := s.table.byMountID(id); !ok {
				gone = append(gone, fmt.Sprintf("%s (mount %d)", what, id))
			}
		}
		j.mu.Unlock()
		if active && len(gone) > 0 {
			j.cancel(Errorf(CodeCancelledUnmounted, "%s was unmounted during the run", gone[0]))
		}
	}
}

// janitor drops finished jobs after KeepFinished.
func (m *jobManager) janitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.sweep(time.Now())
		}
	}
}

func (m *jobManager) sweep(now time.Time) {
	m.mu.Lock()
	var drop []*job
	for id, j := range m.jobs {
		j.mu.Lock()
		old := j.ended != nil && now.Sub(*j.ended) > m.e.cfg.KeepFinished
		j.mu.Unlock()
		if old {
			delete(m.jobs, id)
			drop = append(drop, j)
		}
	}
	m.mu.Unlock()
	for _, j := range drop {
		j.log.remove()
	}
}

// stopAll refuses new jobs and cancels every queued or running one.
func (m *jobManager) stopAll() {
	m.mu.Lock()
	m.closed = true
	queued := m.queue
	m.queue = nil
	var running []*job
	for _, j := range m.jobs {
		running = append(running, j)
	}
	m.mu.Unlock()
	for _, j := range queued {
		j.cancel(Errorf(CodeEngineUnavailable, "the engine is shutting down"))
		m.finish(j, Result{}, Errorf(CodeEngineUnavailable, "the engine is shutting down"))
	}
	for _, j := range running {
		j.cancel(Errorf(CodeEngineUnavailable, "the engine is shutting down"))
	}
}

func (m *jobManager) wait() { m.wg.Wait() }

// runningJobs lists the jobs in state running (for log attribution).
func (m *jobManager) runningJobs() []*job {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*job
	for _, j := range m.jobs {
		j.mu.Lock()
		if j.state == JobRunning {
			out = append(out, j)
		}
		j.mu.Unlock()
	}
	return out
}
