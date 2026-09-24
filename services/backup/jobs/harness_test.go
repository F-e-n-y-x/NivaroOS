package jobs

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine/enginetest"
)

// Test doubles for everything the job side talks to, and a harness that
// runs a real Service on a temp data dir with short timings.

// TestMain keeps the service's log lines (audit, retries, migration
// notes) out of a plain `go test`; -v shows them.
func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Verbose() {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}

// fakeClock is a Clock whose entries fire only when the test says so.
type fakeClock struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]fakeEntry
	armed   chan struct{}
	armOnce sync.Once
	synced  bool
}

type fakeEntry struct {
	spec string
	fn   func()
}

func newFakeClock(armed bool) *fakeClock {
	c := &fakeClock{entries: map[cron.EntryID]fakeEntry{}, armed: make(chan struct{})}
	if armed {
		c.arm()
	}
	return c
}

func (c *fakeClock) arm() { c.armOnce.Do(func() { close(c.armed) }) }

func (c *fakeClock) Add(spec string, fn func()) (cron.EntryID, error) {
	if _, err := CronParser.Parse(spec); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	c.entries[c.next] = fakeEntry{spec, fn}
	return c.next, nil
}

func (c *fakeClock) Remove(id cron.EntryID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, id)
}

func (c *fakeClock) Next(spec string, from time.Time) (time.Time, error) {
	s, err := ParseSchedule(spec)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(from), nil
}

func (c *fakeClock) Armed() <-chan struct{} { return c.armed }
func (c *fakeClock) Synced() bool           { c.mu.Lock(); defer c.mu.Unlock(); return c.synced }

// fire runs every entry with spec (as the cron scheduler would).
func (c *fakeClock) fire(spec string) int {
	c.mu.Lock()
	var fns []func()
	for _, e := range c.entries {
		if e.spec == spec {
			fns = append(fns, e.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return len(fns)
}

func (c *fakeClock) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// fakeApps is an AppController with apps that stop and start at once
// (or never, with stuck).
type fakeApps struct {
	mu      sync.Mutex
	running map[string]bool
	stuck   map[string]bool // Stop is accepted but the app keeps running
	calls   []string
	listErr error
}

func newFakeApps(apps map[string]bool) *fakeApps {
	return &fakeApps{running: apps, stuck: map[string]bool{}}
}

func (a *fakeApps) List(ctx context.Context) (map[string]bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.listErr != nil {
		return nil, a.listErr
	}
	out := map[string]bool{}
	for k, v := range a.running {
		out[k] = v
	}
	return out, nil
}

func (a *fakeApps) Stop(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "stop "+name)
	if _, ok := a.running[name]; !ok {
		return errors.New("no such app")
	}
	if !a.stuck[name] {
		a.running[name] = false
	}
	return nil
}

func (a *fakeApps) Start(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "start "+name)
	if _, ok := a.running[name]; !ok {
		return errors.New("no such app")
	}
	a.running[name] = true
	return nil
}

func (a *fakeApps) Calls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

func (a *fakeApps) isRunning(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running[name]
}

// fakeVMs is a VMController; a VM in noACPI ignores shutdown.
type fakeVMs struct {
	mu     sync.Mutex
	states map[string]string
	noACPI map[string]bool
	calls  []string
}

func newFakeVMs(states map[string]string) *fakeVMs {
	return &fakeVMs{states: states, noACPI: map[string]bool{}}
}

func (v *fakeVMs) List(ctx context.Context) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := map[string]string{}
	for k, s := range v.states {
		out[k] = s
	}
	return out, nil
}

func (v *fakeVMs) State(ctx context.Context, name string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.states[name]
	if !ok {
		return "", errors.New("no such VM")
	}
	return s, nil
}

func (v *fakeVMs) Shutdown(ctx context.Context, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, "shutdown "+name)
	if !v.noACPI[name] {
		v.states[name] = vmShutOff
	}
	return nil
}

func (v *fakeVMs) Start(ctx context.Context, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, "start "+name)
	if _, ok := v.states[name]; !ok {
		return errors.New("no such VM")
	}
	v.states[name] = vmRunning
	return nil
}

func (v *fakeVMs) Calls() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.calls...)
}

// fakeSchedules is core's Scheduled Tasks, in memory. It keeps
// migrated_to like the updated core does (keepMarker=false simulates an
// older core that drops unknown fields).
type fakeSchedules struct {
	mu         sync.Mutex
	tasks      []map[string]interface{}
	keepMarker bool
	listErr    error
	updateErr  error
	updates    int
}

func (f *fakeSchedules) ListTasks(ctx context.Context) ([]ScheduleTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]ScheduleTask, 0, len(f.tasks))
	for _, t := range f.tasks {
		cp := map[string]interface{}{}
		for k, v := range t {
			cp[k] = v
		}
		out = append(out, scheduleTaskFromRaw(cp))
	}
	return out, nil
}

func (f *fakeSchedules) UpdateTask(ctx context.Context, task ScheduleTask, enabled bool, migratedTo string) (ScheduleTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return ScheduleTask{}, f.updateErr
	}
	for _, t := range f.tasks {
		if t["id"] == task.ID {
			t["enabled"] = enabled
			if f.keepMarker {
				t[ScheduleMigratedField] = migratedTo
			}
			f.updates++
			return scheduleTaskFromRaw(t), nil
		}
	}
	return ScheduleTask{}, &httpStatusError{Status: 400, Body: "task not found"}
}

func (f *fakeSchedules) task(id string) map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t["id"] == id {
			return t
		}
	}
	return nil
}

// fakeBus records published events.
type fakeBus struct {
	mu     sync.Mutex
	types  []EventType
	events []busEvent
}

func (b *fakeBus) RegisterEventTypes(ctx context.Context, types []EventType) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.types = types
	return nil
}

func (b *fakeBus) Publish(ctx context.Context, name string, props map[string]string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, busEvent{name, props})
	return nil
}

func (b *fakeBus) named(name string) []map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]string
	for _, e := range b.events {
		if e.name == name {
			out = append(out, e.props)
		}
	}
	return out
}

// notifications returns the keys of every notification so far.
func (b *fakeBus) notifications() []string {
	var out []string
	for _, p := range b.named(EventNotify) {
		out = append(out, p[PropNotifyKey])
	}
	return out
}

// fakeSMB is a fixed list of saved network shares.
type fakeSMB struct{ conns []SMBConnection }

func (f *fakeSMB) Connections(ctx context.Context) ([]SMBConnection, error) {
	return append([]SMBConnection(nil), f.conns...), nil
}

// fakeBusy is a Scheduled Tasks BusyChecker the test drives.
type fakeBusy struct {
	mu   sync.Mutex
	busy map[string]bool
}

func (f *fakeBusy) IsBusy(kind, target string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy[kind+":"+target]
}

func (f *fakeBusy) set(kind, target string, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy == nil {
		f.busy = map[string]bool{}
	}
	f.busy[kind+":"+target] = v
}

func (f *fakeBusy) WaitFree(ctx context.Context, kind, target string) error {
	for f.IsBusy(kind, target) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

// ---------------------------------------------------------------------

type harness struct {
	t      *testing.T
	svc    *Service
	eng    *enginetest.FakeEngine
	clock  *fakeClock
	apps   *fakeApps
	vms    *fakeVMs
	sched  *fakeSchedules
	bus    *fakeBus
	busy   *fakeBusy
	cancel context.CancelFunc
	nowMu  sync.Mutex
	nowFn  func() time.Time
}

type harnessOpt func(*Config, *harness)

func withNow(fn func() time.Time) harnessOpt {
	return func(c *Config, h *harness) { h.nowFn = fn }
}

func withTimings(t Timings) harnessOpt {
	return func(c *Config, h *harness) { c.Timings = t }
}

// newHarness builds a service on a temp dir. start=false leaves Start to
// the test (for recovery tests that seed the store first).
func newHarness(t *testing.T, start bool, opts ...harnessOpt) *harness {
	t.Helper()
	h := &harness{
		t: t, eng: enginetest.New(), clock: newFakeClock(true),
		apps:  newFakeApps(map[string]bool{"immich": true, "blinko": true, "stopped-app": false}),
		vms:   newFakeVMs(map[string]string{"win11": vmRunning, "offvm": vmShutOff}),
		sched: &fakeSchedules{keepMarker: true}, bus: &fakeBus{}, busy: &fakeBusy{},
	}
	cfg := Config{
		DataDir: t.TempDir(), Version: "test", Engine: h.eng, Clock: h.clock, Apps: h.apps, VMs: h.vms,
		Schedules: h.sched, ScheduleBusy: h.busy, Bus: h.bus, SMB: &fakeSMB{},
		Timings: Timings{
			EnginePoll: 5 * time.Millisecond, WaitRecheck: 20 * time.Millisecond, EngineRetry: 20 * time.Millisecond,
			HookPoll: 2 * time.Millisecond, VolumeSettle: 10 * time.Millisecond, Reconcile: time.Hour,
			CatchUpDelay: time.Millisecond, MigrationRetry: time.Hour, Maintenance: time.Hour,
		},
	}
	for _, o := range opts {
		o(&cfg, h)
	}
	cfg.Now = func() time.Time {
		h.nowMu.Lock()
		fn := h.nowFn
		h.nowMu.Unlock()
		if fn != nil {
			return fn()
		}
		return time.Now()
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	if start {
		h.start()
	}
	t.Cleanup(h.stop)
	return h
}

func (h *harness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	if err := h.svc.Start(ctx); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) stop() {
	if h.cancel != nil {
		h.cancel()
		h.svc.Wait()
		h.cancel = nil
	} else if h.svc.store != nil {
		h.svc.store.Close()
	}
}

func (h *harness) setNow(fn func() time.Time) {
	h.nowMu.Lock()
	h.nowFn = fn
	h.nowMu.Unlock()
}

// sampleJob is a valid local mirror job.
func sampleJob(name string) Job {
	return Job{
		Name: name, Type: TypeCopy, Enabled: true,
		Sources:    []Endpoint{{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Documents", Label: "System disk"}},
		Dest:       Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "NivaroOS Backups/" + name, Label: "tank"},
		Triggers:   []Trigger{},
		Conditions: Conditions{DestAvailable: true},
		Retry:      Retry{Max: 0},
		Notify:     NotifyPrefs{OnFailure: true},
	}
}

// createJob validates and stores a job.
func (h *harness) createJob(j Job) Job {
	h.t.Helper()
	norm, fe := NormalizeJob(j, h.svc.validateEnv(context.Background()))
	if len(fe) > 0 {
		h.t.Fatalf("invalid job: %v", fe)
	}
	out, err := h.svc.store.CreateJob(norm, h.svc.now())
	if err != nil {
		h.t.Fatal(err)
	}
	h.svc.jobChanged(out, JobChangeCreated)
	return out
}

// run queues a manual run of a job.
func (h *harness) run(j Job, kind RunKind) string {
	h.t.Helper()
	id, _, err := h.svc.queue.Enqueue(j, EnqueueOptions{Kind: kind, Trigger: RunByManual})
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

const waitFor = 5 * time.Second

// eventually polls cond until it holds or the test times out.
func (h *harness) eventually(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(waitFor)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitStatus waits for a run to reach one of statuses and returns it.
func (h *harness) waitStatus(runID string, statuses ...RunStatus) RunRow {
	h.t.Helper()
	var r RunRow
	h.eventually(fmt.Sprintf("run %s in %v", runID, statuses), func() bool {
		var err error
		r, err = h.svc.store.GetRun(runID)
		if err != nil {
			return false
		}
		for _, s := range statuses {
			if RunStatus(r.Status) == s {
				return true
			}
		}
		return false
	})
	// A resting state is saved before the worker's last steps (job state,
	// notifications, releasing its locks): wait for it to let go.
	if st := RunStatus(r.Status); st.Final() || st == StatusWaitingUser {
		h.eventually("worker of "+runID+" done", func() bool { return h.svc.queue.active(runID) == nil })
		r, _ = h.svc.store.GetRun(runID)
	}
	return r
}

// notified waits (briefly) for a notification with key; the bus delivers
// asynchronously.
func (h *harness) notified(key string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if containsStr(h.bus.notifications(), key) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// busEvents waits until at least n events called name arrived.
func (h *harness) busEvents(name string, n int) []map[string]string {
	h.t.Helper()
	h.eventually(fmt.Sprintf("%d %s events", n, name), func() bool { return len(h.bus.named(name)) >= n })
	return h.bus.named(name)
}

// startedJob waits for the nth StartJob call (1-based) and returns it.
func (h *harness) startedJob(n int) (engine.JobID, engine.JobRequest) {
	h.t.Helper()
	var calls []enginetest.Call
	h.eventually(fmt.Sprintf("engine job %d", n), func() bool {
		calls = h.eng.CallsTo("StartJob")
		return len(calls) >= n
	})
	req := calls[n-1].Req.(engine.JobRequest)
	// Engine job ids are assigned in call order.
	return engine.JobID(n), req
}

// finish ends the nth engine job with res.
func (h *harness) finish(n int, res engine.Result) engine.JobRequest {
	h.t.Helper()
	id, req := h.startedJob(n)
	if err := h.eng.Finish(id, res); err != nil {
		h.t.Fatal(err)
	}
	return req
}

func okResult(added int64) engine.Result {
	return engine.Result{Counts: engine.Counts{Added: added, BytesTransferred: added * 100, BytesTotal: added * 100, SourceFiles: 10, DestFiles: 10}}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasPrefixIn(list []string, p string) bool {
	for _, x := range list {
		if strings.HasPrefix(x, p) {
			return true
		}
	}
	return false
}
