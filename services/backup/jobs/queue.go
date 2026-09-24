package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Queue runs queued runs (spec §8.1): a worker pool of
// AppSettings.MaxConcurrent, priority manual > retry > triggered >
// catch-up (FIFO within one), one active run per job (later triggers are
// coalesced onto it), and locks so two runs never write overlapping
// folders, a restore waits for a backup reading its target, and jobs
// whose hooks touch the same app or VM run one after the other.
//
// Queued runs are RunRows in status queued; the queue's in-memory list is
// rebuilt from them at start, so nothing queued is lost on a restart.
type Queue struct {
	s *Service

	mu      sync.Mutex
	items   []*queueItem
	running map[string]*activeRun // run id ->
	seq     uint64
	kick    chan struct{}
}

type queueItem struct {
	runID     string
	jobID     string
	kind      RunKind
	prio      int
	head      bool // resumed after a decision: before everything else
	seq       uint64
	notBefore time.Time
}

// activeRun is a run a worker is executing.
type activeRun struct {
	runID  string
	jobID  string
	locks  []lockKey
	cancel context.CancelFunc
	// cancelCode is why cancel was called (cancelled_by_user, ...).
	mu         sync.Mutex
	cancelCode ErrorCode
}

func (a *activeRun) setCancel(code ErrorCode) {
	a.mu.Lock()
	if a.cancelCode == "" {
		a.cancelCode = code
	}
	a.mu.Unlock()
	a.cancel()
}

func (a *activeRun) cancelReason() ErrorCode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelCode
}

// Priorities, highest first.
const (
	prioCatchUp   = 1
	prioTriggered = 2
	prioRetry     = 3
	prioManual    = 4
)

func priorityOf(t RunTrigger) int {
	switch t {
	case RunByManual:
		return prioManual
	case RunByRetry:
		return prioRetry
	case RunByCatchUp:
		return prioCatchUp
	}
	return prioTriggered
}

func newQueue(s *Service) *Queue {
	return &Queue{s: s, running: map[string]*activeRun{}, kick: make(chan struct{}, 1)}
}

func (q *Queue) wake() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------
// Locks

// lockKey is one thing a run holds while it executes. Write and read
// locks are on endpoint folders; exclusive locks are on names (a job, an
// app, a VM).
type lockKey struct {
	mode string // "w" | "r" | "x"
	ep   Endpoint
	name string
}

func writeLock(ep Endpoint) lockKey { return lockKey{mode: "w", ep: ep} }
func readLock(ep Endpoint) lockKey  { return lockKey{mode: "r", ep: ep} }
func exclusive(name string) lockKey { return lockKey{mode: "x", name: name} }
func appLock(app string) lockKey    { return exclusive(LockApp + ":" + app) }
func vmLock(vm string) lockKey      { return exclusive(LockVM + ":" + vm) }
func jobLock(jobID string) lockKey  { return exclusive("job:" + jobID) }
func (k lockKey) String() string {
	if k.mode == "x" {
		return "x:" + k.name
	}
	return k.mode + ":" + string(k.ep.Kind) + "|" + k.ep.RefID + "|" + k.ep.SubPath
}

// conflicts: exclusive locks on the same name; a write lock against any
// write or read lock on an overlapping folder (a prefix counts). Two
// readers never conflict.
func (k lockKey) conflicts(o lockKey) bool {
	if k.mode == "x" || o.mode == "x" {
		return k.mode == o.mode && k.name == o.name
	}
	if k.mode == "r" && o.mode == "r" {
		return false
	}
	return endpointsOverlap(k.ep, o.ep)
}

// locksFor is what a run holds: its job (one data run per job), the
// folders it writes and reads, and every app and VM its hooks touch.
// A restore holds its target for writing and the job's destination for
// reading, so it waits for a backup writing that destination; because a
// backup holds its sources for reading, a restore into a folder a running
// backup reads waits too.
func locksFor(job Job, run RunRow) []lockKey {
	var locks []lockKey
	switch RunKind(run.Kind) {
	case KindRestore:
		locks = append(locks, readLock(job.Dest))
		if spec, err := decodeRestoreSpec(run.RestoreSpec); err == nil {
			locks = append(locks, writeLock(spec.Target))
			if !restoreToOriginal(job, spec) {
				return locks // no hooks around a restore elsewhere
			}
		}
	case KindPrune:
		locks = append(locks, writeLock(job.Dest))
	default:
		locks = append(locks, jobLock(job.ID), writeLock(job.Dest))
		for _, s := range job.Sources {
			locks = append(locks, readLock(s))
		}
	}
	for _, h := range job.Hooks {
		for _, a := range h.Apps {
			locks = append(locks, appLock(a))
		}
		if h.VM != "" {
			locks = append(locks, vmLock(h.VM))
		}
	}
	return locks
}

func (q *Queue) lockFree(want []lockKey) bool {
	for _, a := range q.running {
		for _, held := range a.locks {
			for _, w := range want {
				if held.conflicts(w) {
					return false
				}
			}
		}
	}
	return true
}

// IsBusy is Backup's side of the lock contract (locks.go): an app or VM
// is busy while a running backup's hooks may touch it.
func (q *Queue) IsBusy(kind, target string) bool {
	_, busy := q.holder(kind, target)
	return busy
}

func (q *Queue) holder(kind, target string) (string, bool) {
	want := exclusive(kind + ":" + target)
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, a := range q.running {
		for _, held := range a.locks {
			if held.conflicts(want) {
				return a.runID, true
			}
		}
	}
	return "", false
}

// WaitFree blocks until kind/target is free or ctx ends.
func (q *Queue) WaitFree(ctx context.Context, kind, target string) error {
	for q.IsBusy(kind, target) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil
}

var _ BusyChecker = (*Queue)(nil)

// ---------------------------------------------------------------------
// Enqueueing

// EnqueueOptions tunes Enqueue.
type EnqueueOptions struct {
	Kind        RunKind
	Trigger     RunTrigger
	NotBefore   time.Time // zero = now
	Attempt     int       // 0 = 1
	RestoreSpec string    // kind=restore
	Summary     string
}

// Enqueue queues a run of a job and returns its id. A backup or preview
// run is coalesced onto the job's active one (queued, running or
// waiting_user), whose coalesced count grows; coalesced reports that.
func (q *Queue) Enqueue(job Job, o EnqueueOptions) (runID string, coalesced bool, err error) {
	if o.Kind == "" {
		o.Kind = KindBackup
	}
	now := q.s.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	if o.Kind == KindBackup || o.Kind == KindPreview {
		active, err := q.s.store.ListRuns(RunFilter{
			JobID: job.ID, Kinds: []RunKind{KindBackup, KindPreview},
			Statuses: []RunStatus{StatusQueued, StatusRunning, StatusWaitingUser}, Limit: 1,
		})
		if err != nil {
			return "", false, err
		}
		if len(active) == 1 {
			r := active[0]
			if err := q.s.store.IncCoalesced(r.ID); err != nil {
				return "", false, err
			}
			// A manual request lifts a waiting scheduled run to the front.
			for _, it := range q.items {
				if it.runID == r.ID && priorityOf(o.Trigger) > it.prio {
					it.prio = priorityOf(o.Trigger)
					if o.Trigger == RunByManual {
						it.notBefore = time.Time{}
					}
				}
			}
			q.wake()
			return r.ID, true, nil
		}
	}
	attempt := o.Attempt
	if attempt <= 0 {
		attempt = 1
	}
	queuedAt := now
	if o.NotBefore.After(now) {
		queuedAt = o.NotBefore
	}
	r := RunRow{
		ID: NewRunID(now), JobID: job.ID, Kind: string(o.Kind), Trigger: string(o.Trigger),
		Status: string(StatusQueued), Attempt: attempt, QueuedAt: queuedAt, RestoreSpec: o.RestoreSpec,
		Summary: o.Summary,
	}
	if err := q.s.store.CreateRun(r); err != nil {
		return "", false, err
	}
	q.addLocked(r, false)
	q.s.pub.publish(EventRunProgress, progressEventProps(r, LiveStats{}))
	return r.ID, false, nil
}

// addLocked puts a stored queued run into the in-memory queue.
// QueuedAt doubles as "not before" for retries and waiting re-checks.
func (q *Queue) addLocked(r RunRow, head bool) {
	for _, it := range q.items {
		if it.runID == r.ID {
			return
		}
	}
	q.seq++
	q.items = append(q.items, &queueItem{
		runID: r.ID, jobID: r.JobID, kind: RunKind(r.Kind), prio: priorityOf(RunTrigger(r.Trigger)),
		head: head, seq: q.seq, notBefore: r.QueuedAt,
	})
	q.wake()
}

// requeue puts a run back (a waiting condition re-check, engine
// unavailable, a decision to proceed).
func (q *Queue) requeue(r RunRow, head bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.addLocked(r, head)
}

// remove drops a queued run from the in-memory queue; false when it
// wasn't queued (already running or unknown).
func (q *Queue) remove(runID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range q.items {
		if it.runID == runID {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return true
		}
	}
	return false
}

// active returns the executing run, if runID is one.
func (q *Queue) active(runID string) *activeRun {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running[runID]
}

// activeForJob reports whether a run of the job is executing now.
func (q *Queue) activeForJob(jobID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, a := range q.running {
		if a.jobID == jobID {
			return true
		}
	}
	return false
}

// load rebuilds the in-memory queue from the store (at start).
func (q *Queue) load() error {
	rows, err := q.s.store.ListRuns(RunFilter{Statuses: []RunStatus{StatusQueued}})
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	// Oldest first, so FIFO order survives the restart.
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, r := range rows {
		q.addLocked(r, false)
	}
	return nil
}

// ---------------------------------------------------------------------
// Dispatch

// run is the dispatcher: it starts every eligible run whose locks are
// free while worker slots remain, and sleeps until something changes or
// the next delayed run becomes eligible.
func (q *Queue) run(ctx context.Context) {
	for {
		next := q.dispatch(ctx)
		var timer <-chan time.Time
		if !next.IsZero() {
			d := time.Until(next)
			if d < 50*time.Millisecond {
				d = 50 * time.Millisecond
			}
			timer = time.After(d)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.kick:
		case <-timer:
		}
	}
}

// dispatch starts what it can and returns when the earliest delayed run
// becomes eligible (zero when none).
func (q *Queue) dispatch(ctx context.Context) time.Time {
	settings, err := q.s.store.Settings()
	if err != nil {
		settings = DefaultAppSettings()
	}
	slots := settings.MaxConcurrent
	if slots < 1 {
		slots = DefaultMaxConcurrent
	}
	now := q.s.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	sort.SliceStable(q.items, func(i, j int) bool {
		a, b := q.items[i], q.items[j]
		if a.head != b.head {
			return a.head
		}
		if a.prio != b.prio {
			return a.prio > b.prio
		}
		return a.seq < b.seq
	})
	var next time.Time
	kept := q.items[:0]
	for _, it := range q.items {
		if it.notBefore.After(now) {
			if next.IsZero() || it.notBefore.Before(next) {
				next = it.notBefore
			}
			kept = append(kept, it)
			continue
		}
		if len(q.running) >= slots || ctx.Err() != nil {
			kept = append(kept, it)
			continue
		}
		run, err := q.s.store.GetRun(it.runID)
		if err != nil || RunStatus(run.Status) != StatusQueued {
			// Cancelled or removed meanwhile: drop it.
			continue
		}
		job, err := q.s.loadRunJob(run)
		if err != nil {
			if isNoRecord(err) {
				q.s.finishOrphan(run)
				continue
			}
			log.Printf("backup: queue: %s: %v", run.ID, err)
			kept = append(kept, it)
			continue
		}
		locks := locksFor(job, run)
		if !q.lockFree(locks) {
			kept = append(kept, it)
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		a := &activeRun{runID: run.ID, jobID: run.JobID, locks: locks, cancel: cancel}
		q.running[run.ID] = a
		q.s.wg.Add(1)
		go func(it *queueItem) {
			defer q.s.wg.Done()
			defer cancel()
			res := q.s.execute(rctx, a)
			q.mu.Lock()
			delete(q.running, a.runID)
			q.mu.Unlock()
			if res.requeue != nil {
				q.requeue(*res.requeue, res.head)
			}
			q.wake()
		}(it)
	}
	q.items = kept
	return next
}

// Cancel stops a run by id: a queued or waiting run ends at once; a
// running one is told to stop (its post hooks still run).
func (q *Queue) Cancel(runID string, code ErrorCode) (RunRow, error) {
	if a := q.active(runID); a != nil {
		a.setCancel(code)
		return q.s.store.GetRun(runID)
	}
	q.mu.Lock()
	r, err := q.s.store.GetRun(runID)
	if err != nil {
		q.mu.Unlock()
		return RunRow{}, err
	}
	switch RunStatus(r.Status) {
	case StatusQueued, StatusWaitingUser:
	default:
		q.mu.Unlock()
		if RunStatus(r.Status) == StatusRunning {
			// Running in the store but no worker: it was just handed back
			// (e.g. a waiting re-check); let the next pass see it again.
			return r, fmt.Errorf("%w: run %s is between steps, try again", errInvalidState, runID)
		}
		return r, fmt.Errorf("%w: run %s already ended (%s)", errInvalidState, runID, r.Status)
	}
	for i, it := range q.items {
		if it.runID == runID {
			q.items = append(q.items[:i], q.items[i+1:]...)
			break
		}
	}
	q.mu.Unlock()
	return q.s.endWithoutRunning(r, StatusCancelled, code, Message{Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(code)}})
}

// errInvalidState marks a request that doesn't fit the run's state.
var errInvalidState = errors.New("invalid state")

// ---------------------------------------------------------------------
// Restore / purge specs stored on the run

func decodeRestoreSpec(s string) (restoreSpecStored, error) {
	var spec restoreSpecStored
	if strings.TrimSpace(s) == "" {
		return spec, errors.New("no restore spec")
	}
	err := json.Unmarshal([]byte(s), &spec)
	return spec, err
}
