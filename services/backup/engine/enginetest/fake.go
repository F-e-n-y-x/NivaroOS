// Package enginetest provides FakeEngine, an in-memory engine.API for the
// job side's tests: it records every call, answers from fields the test
// sets, and lets the test drive engine jobs and events by hand.
package enginetest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Call is one recorded API call: the method name and its request (nil for
// methods without one, the JobID for job methods).
type Call struct {
	Method string
	Req    interface{}
}

// FakeEngine implements engine.API. The zero value is not usable; use New.
//
// Set the exported fields (under Lock if other goroutines are running) to
// shape answers. A non-nil Err<Method> makes that method fail. Jobs start
// in state running and stay there until the test calls Finish.
type FakeEngine struct {
	mu sync.Mutex

	HealthV    engine.Health
	LocationsV []engine.Location
	VolumesV   []engine.Volume
	PrecheckV  engine.PrecheckResult
	VersionsV  []engine.Version
	DownloadV  *engine.Download
	// ResolveFn / ResolvePathFn / BrowseFn answer per request when set;
	// otherwise Resolve says online at /fake/<ref_id>, ResolvePath fails
	// with endpoint_unknown and Browse returns an empty listing.
	ResolveFn     func(engine.ResolveRequest) (engine.Resolved, error)
	ResolvePathFn func(engine.ResolvePathRequest) (engine.ResolvePathResult, error)
	BrowseFn      func(engine.BrowseRequest) (engine.BrowseResult, error)

	ErrHealth, ErrLocations, ErrVolumes, ErrResolve, ErrResolvePath, ErrBrowse error
	ErrPrecheck, ErrListVersions, ErrOpenDownload, ErrStartJob, ErrStopJob     error

	calls  []Call
	nextID engine.JobID
	jobs   map[engine.JobID]*fakeJob
	subs   []chan engine.Event
}

type fakeJob struct {
	req    engine.JobRequest
	status engine.JobStatus
	logs   []engine.LogLine
	done   chan struct{}
	// wake is closed and replaced whenever logs grow or the job ends.
	wake chan struct{}
}

// New returns a FakeEngine whose Health matches this build's contract.
func New() *FakeEngine {
	return &FakeEngine{
		HealthV: engine.Health{API: engine.APIVersion, Rclone: "v1.75.1", PID: os.Getpid(), StartedAt: time.Now()},
		jobs:    map[engine.JobID]*fakeJob{},
	}
}

var _ engine.API = (*FakeEngine)(nil)

// Lock / Unlock guard the exported fields while the engine is in use.
func (f *FakeEngine) Lock()   { f.mu.Lock() }
func (f *FakeEngine) Unlock() { f.mu.Unlock() }

func (f *FakeEngine) record(method string, req interface{}) {
	f.calls = append(f.calls, Call{Method: method, Req: req})
}

// Calls returns a copy of every call so far, in order.
func (f *FakeEngine) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallsTo returns the calls to one method.
func (f *FakeEngine) CallsTo(method string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *FakeEngine) Health(ctx context.Context) (engine.Health, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Health", nil)
	return f.HealthV, f.ErrHealth
}

func (f *FakeEngine) Locations(ctx context.Context) ([]engine.Location, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Locations", nil)
	return append([]engine.Location(nil), f.LocationsV...), f.ErrLocations
}

func (f *FakeEngine) Volumes(ctx context.Context) ([]engine.Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Volumes", nil)
	return append([]engine.Volume(nil), f.VolumesV...), f.ErrVolumes
}

func (f *FakeEngine) Resolve(ctx context.Context, req engine.ResolveRequest) (engine.Resolved, error) {
	f.mu.Lock()
	fn, err := f.ResolveFn, f.ErrResolve
	f.record("Resolve", req)
	f.mu.Unlock()
	if err != nil {
		return engine.Resolved{}, err
	}
	if fn != nil {
		return fn(req)
	}
	return engine.Resolved{Root: "/fake/" + req.Endpoint.RefID, Online: true, Quirks: []engine.Quirk{}}, nil
}

func (f *FakeEngine) ResolvePath(ctx context.Context, req engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
	f.mu.Lock()
	fn, err := f.ResolvePathFn, f.ErrResolvePath
	f.record("ResolvePath", req)
	f.mu.Unlock()
	if err != nil {
		return engine.ResolvePathResult{}, err
	}
	if fn != nil {
		return fn(req)
	}
	return engine.ResolvePathResult{OK: false, Reason: engine.CodeEndpointUnknown}, nil
}

func (f *FakeEngine) Browse(ctx context.Context, req engine.BrowseRequest) (engine.BrowseResult, error) {
	f.mu.Lock()
	fn, err := f.BrowseFn, f.ErrBrowse
	f.record("Browse", req)
	f.mu.Unlock()
	if err != nil {
		return engine.BrowseResult{}, err
	}
	if fn != nil {
		return fn(req)
	}
	return engine.BrowseResult{Path: req.Path, Entries: []engine.Entry{}}, nil
}

func (f *FakeEngine) Precheck(ctx context.Context, req engine.PrecheckRequest) (engine.PrecheckResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Precheck", req)
	return f.PrecheckV, f.ErrPrecheck
}

func (f *FakeEngine) ListVersions(ctx context.Context, req engine.VersionsRequest) ([]engine.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListVersions", req)
	return append([]engine.Version(nil), f.VersionsV...), f.ErrListVersions
}

func (f *FakeEngine) OpenDownload(ctx context.Context, req engine.DownloadRequest) (*engine.Download, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("OpenDownload", req)
	if f.ErrOpenDownload != nil {
		return nil, f.ErrOpenDownload
	}
	if f.DownloadV == nil {
		return nil, &engine.Error{Code: engine.CodeNotFound, Detail: "fake engine: no DownloadV set"}
	}
	return f.DownloadV, nil
}

// StartJob registers a running job and emits nothing; drive it with
// Progress, Log and Finish.
func (f *FakeEngine) StartJob(ctx context.Context, req engine.JobRequest) (engine.JobID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("StartJob", req)
	if f.ErrStartJob != nil {
		return 0, f.ErrStartJob
	}
	f.nextID++
	id := f.nextID
	now := time.Now()
	f.jobs[id] = &fakeJob{
		req:    req,
		status: engine.JobStatus{ID: id, RunID: req.RunID, Op: req.Op, State: engine.JobRunning, StartedAt: &now},
		done:   make(chan struct{}),
		wake:   make(chan struct{}),
	}
	return id, nil
}

// LastJob returns the id and request of the most recently started job
// (ok=false when none started).
func (f *FakeEngine) LastJob() (engine.JobID, engine.JobRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextID == 0 {
		return 0, engine.JobRequest{}, false
	}
	return f.nextID, f.jobs[f.nextID].req, true
}

func (f *FakeEngine) job(id engine.JobID) (*fakeJob, error) {
	j, ok := f.jobs[id]
	if !ok {
		return nil, &engine.Error{Code: engine.CodeNotFound, Detail: fmt.Sprintf("fake engine: no job %d", id)}
	}
	return j, nil
}

func (f *FakeEngine) JobStatus(ctx context.Context, id engine.JobID) (engine.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("JobStatus", id)
	j, err := f.job(id)
	if err != nil {
		return engine.JobStatus{}, err
	}
	return j.status, nil
}

// StopJob finishes a running job as cancelled_by_user, like the engine.
func (f *FakeEngine) StopJob(ctx context.Context, id engine.JobID) error {
	f.mu.Lock()
	f.record("StopJob", id)
	if f.ErrStopJob != nil {
		err := f.ErrStopJob
		f.mu.Unlock()
		return err
	}
	j, err := f.job(id)
	running := err == nil && (j.status.State == engine.JobRunning || j.status.State == engine.JobQueued)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if running {
		return f.Finish(id, engine.Result{ErrorCode: engine.CodeCancelledByUser, ErrorDetail: "stopped"})
	}
	return nil
}

// Progress replaces a running job's live stats.
func (f *FakeEngine) Progress(id engine.JobID, st engine.Stats) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(id)
	if err != nil {
		return err
	}
	j.status.Stats = st
	return nil
}

// Log appends a log line to a job.
func (f *FakeEngine) Log(id engine.JobID, line engine.LogLine) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(id)
	if err != nil {
		return err
	}
	if line.T.IsZero() {
		line.T = time.Now()
	}
	j.logs = append(j.logs, line)
	close(j.wake)
	j.wake = make(chan struct{})
	return nil
}

// Finish ends a job with res (state error when res.ErrorCode is set,
// done otherwise) and emits job.done to every Events subscriber.
func (f *FakeEngine) Finish(id engine.JobID, res engine.Result) error {
	f.mu.Lock()
	j, err := f.job(id)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	select {
	case <-j.done:
		f.mu.Unlock()
		return fmt.Errorf("fake engine: job %d already finished", id)
	default:
	}
	now := time.Now()
	j.status.State = engine.JobDone
	if res.ErrorCode != "" {
		j.status.State = engine.JobError
	}
	r := res
	j.status.Result = &r
	j.status.EndedAt = &now
	close(j.done)
	close(j.wake)
	j.wake = make(chan struct{})
	ev := engine.Event{Type: engine.EventJobDone, Time: now, JobID: id, RunID: j.status.RunID, State: j.status.State}
	f.mu.Unlock()
	f.Emit(ev)
	return nil
}

// JobLog streams a job's lines until it is finished and drained, or ctx
// ends.
func (f *FakeEngine) JobLog(ctx context.Context, id engine.JobID) (<-chan engine.LogLine, error) {
	f.mu.Lock()
	f.record("JobLog", id)
	j, err := f.job(id)
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make(chan engine.LogLine)
	go func() {
		defer close(out)
		sent := 0
		for {
			f.mu.Lock()
			pending := append([]engine.LogLine(nil), j.logs[sent:]...)
			wake := j.wake
			var finished bool
			select {
			case <-j.done:
				finished = true
			default:
			}
			f.mu.Unlock()
			for _, l := range pending {
				select {
				case out <- l:
					sent++
				case <-ctx.Done():
					return
				}
			}
			if finished && len(pending) == 0 {
				return
			}
			if len(pending) > 0 {
				continue
			}
			select {
			case <-wake:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Events subscribes to Emit and job.done events until ctx ends.
func (f *FakeEngine) Events(ctx context.Context) (<-chan engine.Event, error) {
	ch := make(chan engine.Event, 64)
	f.mu.Lock()
	f.record("Events", nil)
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		for i, s := range f.subs {
			if s == ch {
				f.subs = append(f.subs[:i], f.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}()
	return ch, nil
}

// Emit sends ev to every subscriber, dropping it for a full one (as the
// real engine does).
func (f *FakeEngine) Emit(ev engine.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	for _, s := range f.subs {
		select {
		case s <- ev:
		default:
		}
	}
}
