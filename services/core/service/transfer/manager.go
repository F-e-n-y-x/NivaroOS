// Package transfer is the Files app's copy / move / delete engine.
//
// It replaces the old FileQueue/opStrArr machinery (service/file.go +
// service/notify.go), whose failures are documented in
// docs/specs/2026-09-23-files-reliability.md: per-file errors were dropped
// so jobs always "finished", cross-drive moves deleted sources that never
// copied, and two racing completion detectors could drop queued jobs.
//
// Rules this package keeps:
//   - Every file's outcome is recorded; a job with any failure ends as
//     done_with_errors and lists what failed.
//   - A file is written to a temporary name, fsynced, size-verified, and only
//     then renamed into place - a failure never leaves a truncated file
//     under the real name.
//   - A move never deletes a source file that didn't verifiably arrive.
//   - Only the Manager mutates job state; everyone else gets snapshots.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

type Kind string

const (
	KindCopy   Kind = "copy"
	KindMove   Kind = "move"
	KindDelete Kind = "delete"
)

// Conflict says what to do when the destination name already exists.
type Conflict string

const (
	ConflictOverwrite Conflict = "overwrite"
	ConflictSkip      Conflict = "skip"
	ConflictRename    Conflict = "rename" // keep both: "name (2).ext"
	// ConflictResume is what Retry uses: finish an earlier job in place -
	// files already at the destination with the same size and time are
	// skipped, anything else is (re)written. Never creates "name (2)".
	ConflictResume Conflict = "resume"
)

type State string

const (
	StateQueued         State = "queued"
	StateScanning       State = "scanning"
	StateRunning        State = "running"
	StateSyncing        State = "syncing" // waiting for a cloud mount to finish uploading
	StateDone           State = "done"
	StateDoneWithErrors State = "done_with_errors"
	StateFailed         State = "failed"
	StateCancelled      State = "cancelled"
	StateInterrupted    State = "interrupted" // service stopped mid-job
)

func (s State) Terminal() bool {
	switch s {
	case StateDone, StateDoneWithErrors, StateFailed, StateCancelled, StateInterrupted:
		return true
	}
	return false
}

// Spec is what a client asks for.
type Spec struct {
	Kind     Kind     `json:"kind"`
	Sources  []string `json:"sources"`
	Dest     string   `json:"dest"`
	Conflict Conflict `json:"conflict"`
}

type Failure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// maxFailures caps the failure list kept per job (the count is always exact).
const maxFailures = 200

// Job is a point-in-time snapshot. Safe to hold and marshal.
type Job struct {
	ID       string   `json:"id"`
	Kind     Kind     `json:"kind"`
	Sources  []string `json:"sources"`
	Dest     string   `json:"dest"`
	Conflict Conflict `json:"conflict"`
	State    State    `json:"state"`
	Error    string   `json:"error,omitempty"`

	FilesTotal   int    `json:"files_total"`
	FilesDone    int    `json:"files_done"`
	FilesFailed  int    `json:"files_failed"`
	FilesSkipped int    `json:"files_skipped"`
	BytesTotal   int64  `json:"bytes_total"`
	BytesDone    int64  `json:"bytes_done"`
	Speed        int64  `json:"speed"`
	Current      string `json:"current,omitempty"`

	Failures []Failure `json:"failures"`

	// Directories whose listing this job changed - clients refresh exactly
	// these instead of guessing with timers.
	AffectedDirs []string `json:"affected_dirs"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Options struct {
	// Jobs that may run at the same time. Default 2.
	MaxConcurrent int
	// Where job history is kept across restarts ("" = memory only).
	StatePath string
	// Called (from a single goroutine, never concurrently) with the full job
	// list whenever something changed, at most every PublishInterval for
	// progress-only changes and immediately for state changes.
	OnChange        func([]Job)
	PublishInterval time.Duration
	// Keep this many finished jobs in history. Default 50.
	HistoryLimit int
	// Optional hook for paths the local filesystem can't handle (companion
	// devices). See Remote.
	Remote Remote
	// Optional hook run after a job wrote into Dest, e.g. to wait for an
	// rclone mount to finish uploading. Returning an error fails the job.
	AfterWrite func(ctx context.Context, dest string, progress func(string)) error
}

type job struct {
	Job
	cancel    context.CancelFunc
	lastBytes int64
	lastTick  time.Time
}

type Manager struct {
	opts Options

	mu      sync.Mutex
	jobs    map[string]*job
	order   []string // creation order
	queue   []string // queued ids, FIFO
	running int

	changed chan bool // true = publish now (state change)
	closed  chan struct{}
	wg      sync.WaitGroup
}

func NewManager(opts Options) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	if opts.PublishInterval <= 0 {
		opts.PublishInterval = 500 * time.Millisecond
	}
	if opts.HistoryLimit <= 0 {
		opts.HistoryLimit = 50
	}
	m := &Manager{
		opts:    opts,
		jobs:    map[string]*job{},
		changed: make(chan bool, 1),
		closed:  make(chan struct{}),
	}
	m.load()
	m.wg.Add(1)
	go m.publisher()
	return m
}

// Close cancels running jobs (they end as interrupted) and stops the
// publisher. Used by tests and on shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	m.mu.Unlock()
	select {
	case <-m.closed:
	default:
		close(m.closed)
	}
	m.wg.Wait()
	m.save()
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	ErrNoSources = errors.New("nothing to transfer")
	ErrNoDest    = errors.New("destination folder is required")
	ErrBadKind   = errors.New("unknown operation")
	ErrNotFound  = errors.New("job not found")
)

func (m *Manager) Submit(spec Spec) (Job, error) {
	switch spec.Kind {
	case KindCopy, KindMove:
		if spec.Dest == "" {
			return Job{}, ErrNoDest
		}
	case KindDelete:
	default:
		return Job{}, ErrBadKind
	}
	if len(spec.Sources) == 0 {
		return Job{}, ErrNoSources
	}
	if spec.Conflict == "" {
		spec.Conflict = ConflictOverwrite
	}
	j := &job{Job: Job{
		ID:        newID(),
		Kind:      spec.Kind,
		Sources:   append([]string(nil), spec.Sources...),
		Dest:      spec.Dest,
		Conflict:  spec.Conflict,
		State:     StateQueued,
		CreatedAt: time.Now(),
		Failures:  []Failure{},
	}}
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	m.queue = append(m.queue, j.ID)
	snap := j.snapshot()
	m.pruneLocked()
	m.mu.Unlock()
	m.notify(true)
	m.schedule()
	return snap, nil
}

func (j *job) snapshot() Job {
	s := j.Job
	s.Sources = append([]string(nil), j.Sources...)
	s.Failures = append([]Failure{}, j.Failures...)
	s.AffectedDirs = append([]string{}, j.AffectedDirs...)
	return s
}

func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return j.snapshot(), true
}

// List returns every known job, oldest first.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		if j, ok := m.jobs[id]; ok {
			out = append(out, j.snapshot())
		}
	}
	return out
}

// Cancel stops a queued or running job. Files already copied stay; a move's
// sources are only ever removed per verified file, so nothing is lost.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if j.State == StateQueued {
		m.removeFromQueueLocked(id)
		m.finishLocked(j, StateCancelled, "")
	} else if j.cancel != nil {
		j.cancel()
	}
	m.mu.Unlock()
	m.notify(true)
	return nil
}

// Retry re-runs a finished job in resume mode, so only what didn't make it
// is transferred again (for a move, what's left at the source is exactly
// what failed).
func (m *Manager) Retry(id string) (Job, error) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, ErrNotFound
	}
	if !j.State.Terminal() {
		m.mu.Unlock()
		return Job{}, errors.New("job is still running")
	}
	spec := Spec{Kind: j.Kind, Sources: append([]string(nil), j.Sources...), Dest: j.Dest, Conflict: ConflictResume}
	m.mu.Unlock()
	if spec.Kind == KindDelete {
		spec.Conflict = ""
	}
	nj, err := m.Submit(spec)
	if err != nil {
		return nj, err
	}
	// The retry supersedes the original: every client should now see one
	// job (the retry's outcome), not the old failure next to it.
	m.mu.Lock()
	delete(m.jobs, id)
	m.removeFromOrderLocked(id)
	m.mu.Unlock()
	m.notify(true)
	return nj, nil
}

// Dismiss drops a finished job from the history.
func (m *Manager) Dismiss(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if !j.State.Terminal() {
		return errors.New("job is still running")
	}
	delete(m.jobs, id)
	m.removeFromOrderLocked(id)
	go m.notify(true)
	return nil
}

func (m *Manager) removeFromQueueLocked(id string) {
	for i, q := range m.queue {
		if q == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *Manager) removeFromOrderLocked(id string) {
	for i, q := range m.order {
		if q == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}

// pruneLocked keeps the history bounded (oldest finished jobs go first).
func (m *Manager) pruneLocked() {
	finished := 0
	for _, id := range m.order {
		if j := m.jobs[id]; j != nil && j.State.Terminal() {
			finished++
		}
	}
	for i := 0; finished > m.opts.HistoryLimit && i < len(m.order); {
		id := m.order[i]
		if j := m.jobs[id]; j != nil && j.State.Terminal() {
			delete(m.jobs, id)
			m.order = append(m.order[:i], m.order[i+1:]...)
			finished--
			continue
		}
		i++
	}
}

// schedule starts queued jobs while there's capacity.
func (m *Manager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.running < m.opts.MaxConcurrent && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		j, ok := m.jobs[id]
		if !ok || j.State != StateQueued {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		j.cancel = cancel
		now := time.Now()
		j.StartedAt = &now
		j.lastTick = now
		j.State = StateScanning
		m.running++
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.run(ctx, j)
			m.mu.Lock()
			m.running--
			j.cancel = nil
			m.pruneLocked()
			m.mu.Unlock()
			m.notify(true)
			m.save()
			m.schedule()
		}()
	}
}

// finishLocked moves a job to a terminal state.
func (m *Manager) finishLocked(j *job, s State, errMsg string) {
	now := time.Now()
	j.State = s
	j.Error = errMsg
	j.FinishedAt = &now
	j.Speed = 0
	j.Current = ""
	sort.Strings(j.AffectedDirs)
}

// ---- progress bookkeeping (called by the runner) ----

func (m *Manager) update(j *job, fn func(*job)) {
	m.mu.Lock()
	fn(j)
	m.mu.Unlock()
	m.notify(false)
}

func (m *Manager) setState(j *job, s State) {
	m.mu.Lock()
	j.State = s
	m.mu.Unlock()
	m.notify(true)
}

func (m *Manager) addFailure(j *job, path string, err error) {
	m.update(j, func(j *job) {
		j.FilesFailed++
		if len(j.Failures) < maxFailures {
			j.Failures = append(j.Failures, Failure{Path: path, Error: humanError(err)})
		}
	})
}

func (m *Manager) addAffected(j *job, dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range j.AffectedDirs {
		if d == dir {
			return
		}
	}
	j.AffectedDirs = append(j.AffectedDirs, dir)
}

// ---- publishing ----

func (m *Manager) notify(now bool) {
	select {
	case m.changed <- now:
	default:
		if now {
			// A pending progress-only signal might be sitting in the
			// channel; upgrade it so a state change never waits a tick.
			select {
			case <-m.changed:
			default:
			}
			select {
			case m.changed <- true:
			default:
			}
		}
	}
}

func (m *Manager) publisher() {
	defer m.wg.Done()
	var last time.Time
	pending := false
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	publish := func() {
		pending = false
		last = time.Now()
		m.tickSpeeds()
		if m.opts.OnChange != nil {
			m.opts.OnChange(m.List())
		}
	}
	for {
		select {
		case <-m.closed:
			return
		case now := <-m.changed:
			if now || time.Since(last) >= m.opts.PublishInterval {
				publish()
			} else if !pending {
				pending = true
				timer.Reset(m.opts.PublishInterval - time.Since(last))
			}
		case <-timer.C:
			if pending {
				publish()
			}
		}
	}
}

// tickSpeeds refreshes the bytes/sec estimate of running jobs.
func (m *Manager) tickSpeeds() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, j := range m.jobs {
		if j.State != StateRunning {
			continue
		}
		dt := now.Sub(j.lastTick).Seconds()
		if dt < 0.2 {
			continue
		}
		inst := float64(j.BytesDone-j.lastBytes) / dt
		if inst < 0 {
			inst = 0
		}
		if j.Speed == 0 {
			j.Speed = int64(inst)
		} else {
			j.Speed = int64(float64(j.Speed)*0.5 + inst*0.5)
		}
		j.lastBytes = j.BytesDone
		j.lastTick = now
	}
}
