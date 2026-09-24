package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Hooks (spec §8.2): stop and start apps through app-management, shut
// down and start VMs through vm-sidecar. Every app's or VM's prior state
// is written to RunRow.HooksDone *before* it is touched, so post hooks -
// and after a crash, the replay at start - only restart what this run
// stopped, and never start something the user had turned off. A VM is
// never forced off: a guest that ignores the ACPI shutdown fails the run
// with vm_shutdown_timeout, with nothing copied and the VM still running.

// Prior states recorded in HookDone.PriorState.
const (
	priorRunning = "running"
	priorStopped = "stopped"
	priorShutOff = "shut off"
	// priorRestarted replaces priorRunning once the undo started the app
	// or VM again, so a retry never starts it twice and a failed one
	// stays pending (the key stays: withoutRecorded still sees it).
	priorRestarted = "restarted"
)

// busyWaitMax is how long a hook waits for a Scheduled Task holding its
// app or VM before failing with busy (spec §8.1).
const busyWaitMax = 30 * time.Minute

// hookPoll is how often a hook re-checks an app's or VM's state.
func (s *Service) hookPoll() time.Duration {
	if s.cfg.Timings.HookPoll > 0 {
		return s.cfg.Timings.HookPoll
	}
	return 2 * time.Second
}

func vmStateKey(vm string) string { return "vm:" + vm }

func decodeHooksDone(s string) []HookDone {
	var out []HookDone
	if strings.TrimSpace(s) != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

// hookEntry returns (creating) the HookDone of a hook index.
func (x *runExec) hookEntry(idx int) *HookDone {
	for i := range x.hooks {
		if x.hooks[i].HookIdx == idx {
			return &x.hooks[i]
		}
	}
	x.hooks = append(x.hooks, HookDone{HookIdx: idx, PriorState: map[string]string{}})
	return &x.hooks[len(x.hooks)-1]
}

// saveHooks persists HookDone; it must succeed before an app or VM is
// touched, or a crash could leave it stopped with no record.
func (x *runExec) saveHooks() error {
	raw, err := json.Marshal(x.hooks)
	if err != nil {
		return err
	}
	x.run.HooksDone = string(raw)
	if x.hooksOnly {
		// The restart loop works on runs that are over (or waiting for a
		// decision): only its own column, never a stale copy of the row.
		return x.s.store.SaveRunHooks(x.run.ID, x.run.HooksDone)
	}
	return x.s.store.SaveRun(x.run)
}

// waitNotBusy waits for Scheduled Tasks to release an app or VM.
func (x *runExec) waitNotBusy(ctx context.Context, kind, target string) ErrorCode {
	if x.s.schedBusy == nil || !x.s.schedBusy.IsBusy(kind, target) {
		return ""
	}
	wctx, cancel := context.WithTimeout(ctx, busyWaitMax)
	defer cancel()
	if err := x.s.schedBusy.WaitFree(wctx, kind, target); err != nil {
		if ctx.Err() != nil {
			return x.cancelCode()
		}
		x.log.Raw(engine.LogError, fmt.Sprintf("%s %s is still used by a Scheduled Task after %s", kind, target, busyWaitMax))
		return ErrBusy
	}
	return ""
}

// preHooks runs the pre hooks in order. It returns the error code that
// aborts the run ("" to go on) and whether a "continue" hook failed.
func (x *runExec) preHooks(ctx context.Context) (ErrorCode, bool) {
	softFail := false
	started := false
	for idx, h := range x.job.Hooks {
		if h.Phase != HookPre || (h.Action == HookStopApps && h.AppMode == AppModeOneAtATime) {
			continue
		}
		if !started {
			x.setPhase(PhasePreHooks)
			started = true
		}
		code := x.runHook(ctx, idx, h, true)
		if code == "" {
			continue
		}
		if ctx.Err() != nil {
			return x.cancelCode(), softFail
		}
		if h.FailPolicy == FailContinue && code != ErrBusy {
			softFail = true
			x.softCode = code
			continue
		}
		return code, softFail
	}
	return "", softFail
}

// runHook performs one hook action. record says whether stopped things
// are recorded for the post-phase undo (pre hooks) or not (post hooks).
func (x *runExec) runHook(ctx context.Context, idx int, h Hook, record bool) ErrorCode {
	timeout := time.Duration(h.TimeoutSec) * time.Second
	switch h.Action {
	case HookStopApps:
		return x.stopApps(ctx, idx, h.Apps, timeout, record)
	case HookStartApps:
		return x.startApps(ctx, h.Apps, timeout)
	case HookShutdownVM:
		return x.shutdownVM(ctx, idx, h.VM, timeout, record)
	case HookStartVM:
		return x.startVM(ctx, h.VM, timeout)
	}
	return ErrInternal
}

func (x *runExec) stopApps(ctx context.Context, idx int, apps []string, timeout time.Duration, record bool) ErrorCode {
	if x.s.apps == nil {
		return ErrAppStopFailed
	}
	for _, a := range apps {
		if code := x.waitNotBusy(ctx, LockApp, a); code != "" {
			return code
		}
	}
	states, err := x.s.apps.List(ctx)
	if err != nil {
		x.log.Raw(engine.LogError, "app-management: "+describeErr(err))
		return ErrAppStopFailed
	}
	var toStop []string
	var entry *HookDone
	if record {
		entry = x.hookEntry(idx)
	}
	for _, a := range apps {
		running, known := states[a]
		if !known {
			x.log.Raw(engine.LogError, fmt.Sprintf("app %s is not installed", a))
			return ErrAppStopFailed
		}
		if !running {
			// An app this run already stopped and hasn't started again
			// (a decision re-ran the run after a failed restart) keeps its
			// "running": recording "stopped" now would leave it off.
			if record && entry.PriorState[a] != priorRunning {
				entry.PriorState[a] = priorStopped
			}
			x.log.Info("backup.log.app_was_off", map[string]interface{}{"app": a})
			continue
		}
		if record {
			entry.PriorState[a] = priorRunning
		}
		toStop = append(toStop, a)
	}
	if record {
		if err := x.saveHooks(); err != nil {
			x.log.Raw(engine.LogError, "store: "+describeErr(err))
			return ErrInternal
		}
	}
	for _, a := range toStop {
		if err := x.s.apps.Stop(ctx, a); err != nil {
			x.log.Raw(engine.LogError, fmt.Sprintf("stop %s: %v", a, err))
			return x.ctxOr(ctx, ErrAppStopFailed)
		}
	}
	for _, a := range toStop {
		if !x.waitApp(ctx, a, false, timeout) {
			x.log.Raw(engine.LogError, fmt.Sprintf("%s did not stop within %s", a, timeout))
			return x.ctxOr(ctx, ErrAppStopFailed)
		}
		x.log.Info("backup.log.app_stopped", map[string]interface{}{"app": a})
	}
	if record {
		entry = x.hookEntry(idx)
		entry.PreDone = true
		if err := x.saveHooks(); err != nil {
			return ErrInternal
		}
	}
	return ""
}

// waitApp polls until the app runs (want) or stopped (!want).
func (x *runExec) waitApp(ctx context.Context, app string, want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		states, err := x.s.apps.List(ctx)
		if err == nil {
			if running, ok := states[app]; ok && running == want {
				return true
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(x.s.hookPoll()):
		}
	}
}

func (x *runExec) startApps(ctx context.Context, apps []string, timeout time.Duration) ErrorCode {
	if x.s.apps == nil {
		return ErrAppStartFailed
	}
	var failed bool
	for _, a := range apps {
		if err := x.s.apps.Start(ctx, a); err != nil {
			x.log.Raw(engine.LogError, fmt.Sprintf("start %s: %v", a, err))
			failed = true
			continue
		}
		if !x.waitApp(ctx, a, true, timeout) {
			x.log.Raw(engine.LogError, fmt.Sprintf("%s did not start within %s", a, timeout))
			failed = true
			continue
		}
		x.log.Info("backup.log.app_started", map[string]interface{}{"app": a})
	}
	if failed {
		return ErrAppStartFailed
	}
	return ""
}

func (x *runExec) shutdownVM(ctx context.Context, idx int, vm string, timeout time.Duration, record bool) ErrorCode {
	if x.s.vms == nil {
		return ErrVMShutdownTimeout
	}
	if code := x.waitNotBusy(ctx, LockVM, vm); code != "" {
		return code
	}
	state, err := x.s.vms.State(ctx, vm)
	if err != nil {
		x.log.Raw(engine.LogError, "vm-sidecar: "+describeErr(err))
		return x.ctxOr(ctx, ErrVMShutdownTimeout)
	}
	if state == vmShutOff {
		if record {
			entry := x.hookEntry(idx)
			if entry.PriorState[vmStateKey(vm)] != priorRunning {
				entry.PriorState[vmStateKey(vm)] = priorShutOff
			}
			entry.PreDone = true
			if err := x.saveHooks(); err != nil {
				return ErrInternal
			}
		}
		x.log.Info("backup.log.vm_was_off", map[string]interface{}{"vm": vm})
		return ""
	}
	if record {
		x.hookEntry(idx).PriorState[vmStateKey(vm)] = priorRunning
		if err := x.saveHooks(); err != nil {
			return ErrInternal
		}
	}
	if err := x.s.vms.Shutdown(ctx, vm); err != nil {
		x.log.Raw(engine.LogError, fmt.Sprintf("shut down %s: %v", vm, err))
		return x.ctxOr(ctx, ErrVMShutdownTimeout)
	}
	if !x.waitVM(ctx, vm, vmShutOff, timeout) {
		x.log.Raw(engine.LogError, fmt.Sprintf("%s did not shut down within %s (no ACPI or guest agent?); it was not forced off", vm, timeout))
		return x.ctxOr(ctx, ErrVMShutdownTimeout)
	}
	x.log.Info("backup.log.vm_shutdown", map[string]interface{}{"vm": vm})
	if record {
		x.hookEntry(idx).PreDone = true
		if err := x.saveHooks(); err != nil {
			return ErrInternal
		}
	}
	return ""
}

func (x *runExec) waitVM(ctx context.Context, vm, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if st, err := x.s.vms.State(ctx, vm); err == nil && st == want {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(x.s.hookPoll()):
		}
	}
}

func (x *runExec) startVM(ctx context.Context, vm string, timeout time.Duration) ErrorCode {
	if x.s.vms == nil {
		return ErrVMStartFailed
	}
	if st, err := x.s.vms.State(ctx, vm); err == nil && st == vmRunning {
		return ""
	}
	if err := x.s.vms.Start(ctx, vm); err != nil {
		x.log.Raw(engine.LogError, fmt.Sprintf("start %s: %v", vm, err))
		return ErrVMStartFailed
	}
	if !x.waitVM(ctx, vm, vmRunning, timeout) {
		x.log.Raw(engine.LogError, fmt.Sprintf("%s did not start within %s", vm, timeout))
		return ErrVMStartFailed
	}
	x.log.Info("backup.log.vm_started", map[string]interface{}{"vm": vm})
	return ""
}

// ctxOr returns the cancel reason when ctx ended, else code.
func (x *runExec) ctxOr(ctx context.Context, code ErrorCode) ErrorCode {
	if ctx.Err() != nil {
		return x.cancelCode()
	}
	return code
}

// undoPreHooks restarts, newest first, every app and VM this run stopped
// that was running before. It runs with its own context: post hooks run
// even after a cancel (spec §5). It returns the first failure.
//
// Each app or VM is recorded as restarted as soon as it runs again, and
// a hook is only done once all of its targets are: a restart that failed
// (app-management or vm-sidecar not answering at boot or shutdown, a
// broken app) stays pending and the restart loop (restarts.go) tries it
// again, so nothing a backup stopped stays stopped for good.
func (x *runExec) undoPreHooks() ErrorCode {
	var first ErrorCode
	pending := false
	for i := len(x.hooks) - 1; i >= 0; i-- {
		hd := &x.hooks[i]
		if hd.PostDone {
			continue
		}
		timeout := time.Duration(DefaultHookTimeoutSec) * time.Second
		if hd.HookIdx < len(x.job.Hooks) && x.job.Hooks[hd.HookIdx].TimeoutSec > 0 {
			timeout = time.Duration(x.job.Hooks[hd.HookIdx].TimeoutSec) * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Minute)
		apps, vms := toRestart(*hd)
		var code ErrorCode
		if len(apps) > 0 {
			code = x.restartApps(ctx, hd, apps, timeout)
		}
		for _, vm := range vms {
			if x.heldByOtherRun(LockVM, vm) {
				if code == "" {
					code = ErrVMStartFailed
				}
				continue
			}
			if c := x.startVM(ctx, vm, timeout); c != "" {
				if code == "" {
					code = c
				}
				continue
			}
			hd.PriorState[vmStateKey(vm)] = priorRestarted
		}
		cancel()
		leftApps, leftVMs := toRestart(*hd)
		hd.PostDone = len(leftApps) == 0 && len(leftVMs) == 0
		if !hd.PostDone {
			pending = true
		}
		if err := x.saveHooks(); err != nil && code == "" {
			code = ErrInternal
		}
		if code != "" && first == "" {
			first = code
		}
	}
	if pending && !x.hooksOnly {
		x.s.kickRestarts() // the loop itself keeps its own backoff
	}
	return first
}

// toRestart lists a hook's apps and VMs that were running before the run
// and haven't been started again yet.
func toRestart(hd HookDone) (apps, vms []string) {
	for k, st := range hd.PriorState {
		if st != priorRunning {
			continue
		}
		if vm, ok := strings.CutPrefix(k, "vm:"); ok {
			vms = append(vms, vm)
		} else {
			apps = append(apps, k)
		}
	}
	sort.Strings(apps)
	sort.Strings(vms)
	return apps, vms
}

// restartApps starts apps again one by one, recording each that runs.
// When app-management doesn't answer nothing is tried (each start would
// only fail after its timeout); an app someone else already started
// counts as restarted.
func (x *runExec) restartApps(ctx context.Context, hd *HookDone, apps []string, timeout time.Duration) ErrorCode {
	if x.s.apps == nil {
		return ErrAppStartFailed
	}
	states, err := x.s.apps.List(ctx)
	if err != nil {
		x.log.Raw(engine.LogError, "app-management: "+describeErr(err))
		return ErrAppStartFailed
	}
	var code ErrorCode
	for _, a := range apps {
		if running, known := states[a]; known && running {
			hd.PriorState[a] = priorRestarted
			continue
		}
		if x.heldByOtherRun(LockApp, a) {
			// Another backup stopped it on purpose; it is started again
			// once that run is over.
			code = ErrAppStartFailed
			continue
		}
		if c := x.startApps(ctx, []string{a}, timeout); c != "" {
			code = c
			continue
		}
		hd.PriorState[a] = priorRestarted
	}
	return code
}

// heldByOtherRun reports whether a running run other than this one holds
// the hook lock of an app or VM (it may have stopped it on purpose).
func (x *runExec) heldByOtherRun(kind, target string) bool {
	if x.s.queue == nil {
		return false
	}
	holder, busy := x.s.queue.holder(kind, target)
	return busy && holder != x.run.ID
}

// postHooks undoes the pre hooks, then runs the job's explicit post
// hooks. Failures make the run partial (spec §5), never failed.
func (x *runExec) postHooks() ErrorCode {
	hasPost := false
	for _, h := range x.job.Hooks {
		if h.Phase == HookPost {
			hasPost = true
		}
	}
	pending := false
	for _, hd := range x.hooks {
		if !hd.PostDone {
			pending = true
		}
	}
	if !hasPost && !pending {
		return ""
	}
	x.setPhase(PhasePostHooks)
	first := x.undoPreHooks()
	for idx, h := range x.job.Hooks {
		if h.Phase != HookPost {
			continue
		}
		h = x.withoutRecorded(h)
		if (h.Action == HookStartApps && len(h.Apps) == 0) || (h.Action == HookStartVM && h.VM == "") {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.TimeoutSec)*time.Second+time.Minute)
		code := x.runHook(ctx, idx, h, false)
		cancel()
		if code != "" && first == "" {
			first = code
		}
	}
	return first
}

// withoutRecorded drops from an explicit post start hook every app or VM
// a pre hook of this run recorded: undoPreHooks already restarted the
// ones that were running, and one that was off before the run stays off
// (spec §8.2). Other actions are returned unchanged.
func (x *runExec) withoutRecorded(h Hook) Hook {
	recorded := func(key string) bool {
		for _, hd := range x.hooks {
			if _, ok := hd.PriorState[key]; ok {
				return true
			}
		}
		return false
	}
	switch h.Action {
	case HookStartApps:
		apps := make([]string, 0, len(h.Apps))
		for _, a := range h.Apps {
			if !recorded(a) {
				apps = append(apps, a)
			}
		}
		h.Apps = apps
	case HookStartVM:
		if recorded(vmStateKey(h.VM)) {
			h.VM = ""
		}
	}
	return h
}

// oneAtATimeHook returns the pre stop_apps hook in one_at_a_time mode.
func oneAtATimeHook(j Job) (int, *Hook) {
	for i := range j.Hooks {
		h := &j.Hooks[i]
		if h.Phase == HookPre && h.Action == HookStopApps && h.AppMode == AppModeOneAtATime {
			return i, h
		}
	}
	return -1, nil
}

// replayHooks is the start-up replay (spec §8.2) for a run a crash left
// running: restart what it stopped before it is marked interrupted.
func (s *Service) replayHooks(run *RunRow, job Job) ErrorCode {
	hooks := decodeHooksDone(run.HooksDone)
	pending := false
	for _, hd := range hooks {
		if !hd.PostDone {
			pending = true
		}
	}
	if !pending {
		return ""
	}
	lg, err := OpenRunLog(orLogPath(s, *run))
	if err != nil {
		return ErrInternal
	}
	defer lg.Close()
	x := &runExec{s: s, run: *run, job: job, log: lg, hooks: hooks}
	code := x.undoPreHooks()
	lg.Info("backup.log.replayed", nil)
	*run = x.run
	return code
}

func orLogPath(s *Service, r RunRow) string {
	if r.LogPath != "" && !strings.HasSuffix(r.LogPath, ".gz") {
		return r.LogPath
	}
	return logPath(s.store.DataDir(), r.JobID, r.ID)
}
