package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Triggers (spec §7, §16.1 triggers_test.go): schedule entries, catch-up
// coalescing, the clock gate, DST, and volume_mounted matching.

// runsBy returns a job's runs queued by trigger.
func (h *harness) runsBy(jobID string, by RunTrigger) []RunRow {
	rows, err := h.svc.store.ListRuns(RunFilter{JobID: jobID})
	if err != nil {
		h.t.Fatal(err)
	}
	var out []RunRow
	for _, r := range rows {
		if RunTrigger(r.Trigger) == by {
			out = append(out, r)
		}
	}
	return out
}

func TestScheduleTriggersFollowTheJob(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Nightly")
	j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *"}, {Kind: TriggerSchedule, Cron: "30 12 * * 0"}}
	job := h.createJob(j)
	if h.clock.count() != 2 {
		t.Fatalf("%d clock entries, want 2", h.clock.count())
	}
	if n := h.clock.fire("0 3 * * *"); n != 1 {
		t.Fatalf("fired %d", n)
	}
	h.eventually("a scheduled run", func() bool { return len(h.runsBy(job.ID, RunBySchedule)) == 1 })

	// next_run is the earliest of the schedules.
	next := h.svc.trig.NextRun(job, time.Date(2026, 9, 27, 12, 0, 0, 0, time.Local)) // a Sunday
	if next == nil || next.Hour() != 12 || next.Minute() != 30 {
		t.Errorf("next run %v", next)
	}

	// Disabling removes the entries, enabling brings them back.
	off, _ := h.svc.store.MutateJob(job.ID, time.Now(), true, func(j *Job) error { j.Enabled = false; return nil })
	h.svc.jobChanged(off, JobChangeToggled)
	if h.clock.count() != 0 || h.svc.trig.NextRun(off, time.Now()) != nil {
		t.Error("a disabled job keeps its schedule")
	}
	// A late tick of a disabled job queues nothing.
	h.svc.trig.fire(job.ID, RunBySchedule)
	on, _ := h.svc.store.MutateJob(job.ID, time.Now(), true, func(j *Job) error { j.Enabled = true; return nil })
	h.svc.jobChanged(on, JobChangeToggled)
	if h.clock.count() != 2 {
		t.Errorf("%d entries after enabling", h.clock.count())
	}
	h.svc.jobChanged(on, JobChangeDeleted)
	if h.clock.count() != 0 {
		t.Error("a deleted job keeps its schedule")
	}
	if n := len(h.runsBy(job.ID, RunBySchedule)); n != 1 {
		t.Errorf("%d scheduled runs, want 1", n)
	}
}

func TestCatchUpCoalesces(t *testing.T) {
	h := newHarness(t, false)
	// Created three days ago, never succeeded: two missed schedules.
	past := time.Now().Add(-72 * time.Hour)
	h.setNow(func() time.Time { return past })
	j := sampleJob("Missed")
	j.Triggers = []Trigger{
		{Kind: TriggerSchedule, Cron: "0 3 * * *", CatchUp: true},
		{Kind: TriggerSchedule, Cron: "0 4 * * *", CatchUp: true},
	}
	missed := h.createJob(j)
	// One that ran since its last activation: nothing to catch up.
	j2 := sampleJob("Fine")
	j2.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *", CatchUp: true}}
	fine := h.createJob(j2)
	h.setNow(nil)
	now := time.Now()
	h.svc.store.UpdateJobState(fine.ID, func(st *JobState) { st.LastSuccessAt = &now })
	// One that opted out.
	j3 := sampleJob("Opted out")
	j3.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *", CatchUp: false}}
	h.setNow(func() time.Time { return past })
	optOut := h.createJob(j3)
	h.setNow(nil)

	h.start()
	h.eventually("catch-up run", func() bool { return len(h.runsBy(missed.ID, RunByCatchUp)) == 1 })
	settle()
	if n := len(h.runsBy(missed.ID, RunByCatchUp)); n != 1 {
		t.Errorf("%d catch-up runs for two missed schedules, want 1", n)
	}
	if n := len(h.runsBy(fine.ID, RunByCatchUp)) + len(h.runsBy(optOut.ID, RunByCatchUp)); n != 0 {
		t.Errorf("%d catch-up runs for jobs with nothing to catch up", n)
	}
}

func TestCatchUpWaitsForTheClockGate(t *testing.T) {
	h := newHarness(t, false)
	h.clock = newFakeClock(false)
	h.svc.clock = h.clock
	h.svc.trig = newTriggers(h.svc)
	h.setNow(func() time.Time { return time.Now().Add(-72 * time.Hour) })
	j := sampleJob("Missed")
	j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *", CatchUp: true}}
	job := h.createJob(j)
	h.setNow(nil)
	h.start()
	settle()
	if n := len(h.runsBy(job.ID, RunByCatchUp)); n != 0 {
		t.Fatal("catch-up ran before the clock gate opened")
	}
	h.clock.arm()
	h.eventually("catch-up after the gate", func() bool { return len(h.runsBy(job.ID, RunByCatchUp)) == 1 })
}

func TestRealClockGate(t *testing.T) {
	// Unsynced: the gate opens on the timeout and says so once.
	c := NewRealClock(time.Local)
	c.syncedFn = func() bool { return false }
	c.poll, c.gateTimeout = time.Millisecond, 30*time.Millisecond
	var warned atomic.Int32
	c.onUnsynced = func() { warned.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	if isArmed(c) {
		t.Fatal("armed before the timeout")
	}
	select {
	case <-c.Armed():
	case <-time.After(2 * time.Second):
		t.Fatal("gate never opened")
	}
	if c.Synced() || warned.Load() != 1 {
		t.Errorf("synced %v, warned %d", c.Synced(), warned.Load())
	}
	c.Stop()

	// Synced: at once, no warning.
	c2 := NewRealClock(time.Local)
	c2.syncedFn = func() bool { return true }
	c2.onUnsynced = func() { t.Error("warned although synced") }
	c2.Start(ctx)
	select {
	case <-c2.Armed():
	case <-time.After(2 * time.Second):
		t.Fatal("gate never opened")
	}
	if !c2.Synced() {
		t.Error("not synced")
	}
	c2.Stop()

	// Nothing fires while the gate is closed.
	c3 := NewRealClock(time.Local)
	c3.syncedFn = func() bool { return false }
	c3.poll, c3.gateTimeout = 5*time.Millisecond, time.Hour
	var fired atomic.Int32
	if _, err := c3.Add("@every 1s", func() { fired.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if _, err := c3.Add("not cron", func() {}); err == nil {
		t.Error("an invalid spec was accepted")
	}
	c3.Start(ctx)
	time.Sleep(1200 * time.Millisecond)
	if fired.Load() != 0 {
		t.Error("an entry fired before the gate opened")
	}
	c3.Stop() // a closed gate stops cleanly too
}

func TestDSTBehaviour(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	// The server zone, spelled out (changing time.Local would race the
	// other tests' goroutines).
	const nightly = "CRON_TZ=Europe/Berlin 30 2 * * *"
	c := NewRealClock(berlin)

	// Spring forward: 2026-03-29 02:00 -> 03:00, so 02:30 doesn't exist
	// that night and the run is skipped (not moved).
	from := time.Date(2026, 3, 28, 12, 0, 0, 0, berlin)
	next, err := c.Next(nightly, from)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 3, 30, 2, 30, 0, 0, berlin); !next.Equal(want) {
		t.Errorf("spring forward: next %v, want %v (skipped)", next, want)
	}

	// Fall back: 2026-10-25 03:00 -> 02:00, 02:30 happens twice; it runs
	// once.
	from = time.Date(2026, 10, 24, 12, 0, 0, 0, berlin)
	first, _ := c.Next(nightly, from)
	second, _ := c.Next(nightly, first)
	// ... also when cron asks a moment after the run started.
	if again, _ := c.Next(nightly, first.Add(3*time.Second)); again.Day() != 26 {
		t.Errorf("fall back, asked late: %v", again)
	}
	if first.Day() != 25 || first.Hour() != 2 || first.Minute() != 30 {
		t.Errorf("fall back first run %v", first)
	}
	if second.Day() != 26 {
		t.Errorf("fall back: second run %v, want the next day (once only)", second)
	}
	// Constant delays count real time: an @every 1h keeps firing hourly
	// through the repeated hour.
	at := time.Date(2026, 10, 25, 1, 30, 0, 0, berlin) // CEST
	for i := 0; i < 3; i++ {
		n, _ := c.Next("@every 1h", at.In(berlin))
		if d := n.Sub(at); d != time.Hour {
			t.Fatalf("@every 1h from %v: %v later", at, d)
		}
		at = n
	}
	// The preview shows the same real times.
	p := PreviewCron(nightly, time.Date(2026, 3, 28, 12, 0, 0, 0, berlin))
	if len(p.Next) < 2 || p.Next[0].In(berlin).Day() != 30 || p.Next[1].In(berlin).Day() != 31 {
		t.Errorf("preview over DST: %+v", p)
	}
}

// ---------------------------------------------------------------------
// volume_mounted

var stick = engine.Volume{MountID: 101, UUID: "3A4F-1C22", Serial: "S1", Size: 128 << 30, FSType: "exfat", MountPoint: "/media/usb1", Tran: "usb"}

// usbJob backs up to the stick when it is plugged in. Tests of mount
// events turn catch-up off: its pass at start would otherwise race them
// for the same attach.
func usbJob(name string, minGap int, catchUp bool) Job {
	j := sampleJob(name)
	j.Dest = Endpoint{Kind: EPUSB, RefID: stick.UUID, Match: &DevMatch{Serial: "S1", SizeBytes: stick.Size}, SubPath: "Backups", Label: "Sandisk"}
	j.Triggers = []Trigger{{Kind: TriggerVolumeMounted, MinGapHours: minGap, CatchUp: catchUp}}
	return j
}

// watching waits until the trigger watcher has subscribed and done its
// first reconcile.
func (h *harness) watching() {
	h.t.Helper()
	h.eventually("volume watcher", func() bool {
		return len(h.eng.CallsTo("Events")) > 0 && len(h.eng.CallsTo("Volumes")) > 0
	})
}

func mountEv(v engine.Volume, initial bool) engine.Event {
	return engine.Event{Type: engine.EventVolumeMounted, Volume: &v, Initial: initial}
}

func unmountEv(v engine.Volume) engine.Event {
	return engine.Event{Type: engine.EventVolumeUnmounted, Volume: &v}
}

func TestVolumeMountedFiresOncePerAttach(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(usbJob("Stick", 0, false))
	h.watching()
	h.eng.Emit(mountEv(stick, false))
	h.eventually("volume run", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 1 })
	// The same mount again (a duplicate event, a reconcile) is the same attach.
	h.eng.Emit(mountEv(stick, false))
	settle()
	h.finish(1, okResult(1))
	h.eventually("run ends", func() bool {
		rs := h.runsBy(job.ID, RunByVolumeMounted)
		return len(rs) == 1 && RunStatus(rs[0].Status) == StatusSuccess
	})
	h.eng.Emit(mountEv(stick, false))
	settle()
	if n := len(h.runsBy(job.ID, RunByVolumeMounted)); n != 1 {
		t.Fatalf("%d runs for one attach", n)
	}
	// Unplug and plug in again: a new mount id, a new run.
	h.eng.Emit(unmountEv(stick))
	again := stick
	again.MountID = 102
	h.eng.Emit(mountEv(again, false))
	h.eventually("second attach", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 2 })
	// USB notifications: running, then done.
	h.finish(2, okResult(1))
	h.eventually("usb_done", func() bool { return containsStr(h.bus.notifications(), "backup.notify.usb_done") })
	if !h.notified("backup.notify.usb_running") {
		t.Error("no usb_running notification")
	}
}

func TestVolumeMountedMatchesByIdentity(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(usbJob("Stick", 0, false))
	h.watching()
	for i, v := range []engine.Volume{
		{MountID: 201, UUID: "3a4f-1c22", Serial: "OTHER", Size: stick.Size}, // same UUID, another drive
		{MountID: 202, UUID: stick.UUID, Serial: "S1", Size: 64 << 30},       // right serial, wrong size
		{MountID: 203, UUID: "FFFF-0000", Serial: "S1", Size: stick.Size},    // another filesystem
	} {
		h.eng.Emit(mountEv(v, false))
		settle()
		if n := len(h.runsBy(job.ID, RunByVolumeMounted)); n != 0 {
			t.Fatalf("case %d fired a run", i)
		}
	}
	// UUIDs compare case-insensitively (FAT UUIDs are shown both ways).
	v := stick
	v.UUID = "3a4f-1c22"
	v.MountID = 204
	h.eng.Emit(mountEv(v, false))
	h.eventually("match", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 1 })
}

func TestVolumeMountedIgnoresAmbiguousClone(t *testing.T) {
	h := newHarness(t, true)
	j := usbJob("Stick", 0, false)
	j.Dest.Match = nil // only the (cloneable) UUID pins it
	job := h.createJob(j)
	h.watching()
	clone := stick
	clone.MountID, clone.Serial = 300, "CLONE"
	h.eng.Emit(mountEv(clone, false))
	h.eng.Emit(mountEv(stick, false))
	settle()
	settle()
	if n := len(h.runsBy(job.ID, RunByVolumeMounted)); n != 0 {
		t.Fatalf("%d runs with two drives answering to one UUID", n)
	}
}

func TestVolumeMountedSkipsMountsPresentAtStart(t *testing.T) {
	h := newHarness(t, false)
	h.eng.Lock()
	h.eng.VolumesV = []engine.Volume{stick}
	h.eng.Unlock()
	// Has succeeded after the drive was last seen: no catch-up either.
	job := h.createJob(usbJob("Stick", 0, true))
	now := time.Now()
	h.svc.store.UpdateJobState(job.ID, func(st *JobState) {
		st.LastSuccessAt = &now
		st.VolumeSeen = map[string]time.Time{refKey(job.Dest): now.Add(-time.Hour)}
	})
	h.start()
	h.watching()
	// An initial event from the engine is the boot state too.
	h.eng.Emit(mountEv(stick, true))
	settle()
	settle()
	if rows, _ := h.svc.store.ListRuns(RunFilter{JobID: job.ID}); len(rows) != 0 {
		t.Fatalf("a mount present at start queued %d runs", len(rows))
	}
}

func TestVolumeCatchUpAfterRestart(t *testing.T) {
	h := newHarness(t, false)
	h.eng.Lock()
	h.eng.VolumesV = []engine.Volume{stick}
	h.eng.Unlock()
	job := h.createJob(usbJob("Stick", 0, true))
	// The drive was attached last night, but the box went down before the
	// run succeeded.
	yesterday := time.Now().Add(-24 * time.Hour)
	h.svc.store.UpdateJobState(job.ID, func(st *JobState) {
		st.LastSuccessAt = &yesterday
		st.VolumeSeen = map[string]time.Time{refKey(job.Dest): time.Now().Add(-time.Hour)}
	})
	h.start()
	h.eventually("volume catch-up", func() bool { return len(h.runsBy(job.ID, RunByCatchUp)) == 1 })
	// The catch-up counted as this attach: a late mount event doesn't add a run.
	h.eng.Emit(mountEv(stick, false))
	settle()
	rows, _ := h.svc.store.ListRuns(RunFilter{JobID: job.ID})
	if len(rows) != 1 {
		t.Errorf("%d runs, want the one catch-up", len(rows))
	}
}

func TestVolumeMinGapHours(t *testing.T) {
	h := newHarness(t, true)
	job := h.createJob(usbJob("Stick", 24, false))
	h.watching()
	h.eng.Emit(mountEv(stick, false))
	h.eventually("first run", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 1 })
	h.finish(1, okResult(1))
	h.eventually("first run ends", func() bool {
		rs := h.runsBy(job.ID, RunByVolumeMounted)
		return RunStatus(rs[0].Status).Final()
	})
	// Re-plugged an hour later: within the gap.
	h.setNow(func() time.Time { return time.Now().Add(time.Hour) })
	h.eng.Emit(unmountEv(stick))
	v2 := stick
	v2.MountID = 110
	h.eng.Emit(mountEv(v2, false))
	settle()
	if n := len(h.runsBy(job.ID, RunByVolumeMounted)); n != 1 {
		t.Fatalf("%d runs within min_gap_hours", n)
	}
	// A day later: fires again.
	h.setNow(func() time.Time { return time.Now().Add(25 * time.Hour) })
	h.eng.Emit(unmountEv(v2))
	v3 := stick
	v3.MountID = 111
	h.eng.Emit(mountEv(v3, false))
	h.eventually("after the gap", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 2 })
}

func TestVolumeUnmountedBeforeSettleDoesNotFire(t *testing.T) {
	h := newHarness(t, true, withTimings(Timings{
		EnginePoll: 5 * time.Millisecond, VolumeSettle: 100 * time.Millisecond, Reconcile: time.Hour,
		CatchUpDelay: time.Millisecond, MigrationRetry: time.Hour, Maintenance: time.Hour, HookPoll: time.Millisecond,
	}))
	job := h.createJob(usbJob("Stick", 0, false))
	h.watching()
	h.eng.Emit(mountEv(stick, false))
	time.Sleep(10 * time.Millisecond)
	h.eng.Emit(unmountEv(stick))
	time.Sleep(200 * time.Millisecond)
	if n := len(h.runsBy(job.ID, RunByVolumeMounted)); n != 0 {
		t.Fatal("a drive pulled during the settle delay fired a run")
	}
}

func TestVolumeReconcileCatchesDroppedEvents(t *testing.T) {
	h := newHarness(t, true, withTimings(Timings{
		EnginePoll: 5 * time.Millisecond, VolumeSettle: 5 * time.Millisecond, Reconcile: 20 * time.Millisecond,
		CatchUpDelay: time.Millisecond, MigrationRetry: time.Hour, Maintenance: time.Hour, HookPoll: time.Millisecond,
	}))
	job := h.createJob(usbJob("Stick", 0, false))
	h.watching()
	// The mount event was lost; the backstop sees the drive in the table.
	h.eng.Lock()
	h.eng.VolumesV = []engine.Volume{stick}
	h.eng.Unlock()
	h.eventually("reconciled attach", func() bool { return len(h.runsBy(job.ID, RunByVolumeMounted)) == 1 })
}
