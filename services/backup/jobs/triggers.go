package jobs

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Triggers (spec §7.2): schedule entries on the shared clock, volume
// mounts from the engine's mountwatch (matched by identity, after a
// settle delay, once per attach), and the catch-up pass after the clock
// gate opens. Manual runs go straight to the queue.

const (
	defaultVolumeSettle = 30 * time.Second
	defaultReconcile    = 60 * time.Second
	defaultCatchUpDelay = 3 * time.Minute
	// seenWriteEvery throttles "last seen" writes for mounted volumes.
	seenWriteEvery = 10 * time.Minute
)

type triggers struct {
	s *Service

	mu      sync.Mutex
	entries map[string][]cron.EntryID // job id -> clock entries
	known   map[int]engine.Volume     // mount id -> the mounted volume
	pending map[int]*time.Timer       // mount id -> settle timer
	seenAt  map[string]time.Time      // throttle key -> last write
	// seenBefore is every job's VolumeSeen as it was before this start:
	// catch-up compares it with the last success.
	seenBefore map[string]map[string]time.Time
}

func newTriggers(s *Service) *triggers {
	return &triggers{
		s: s, entries: map[string][]cron.EntryID{}, known: map[int]engine.Volume{},
		pending: map[int]*time.Timer{}, seenAt: map[string]time.Time{}, seenBefore: map[string]map[string]time.Time{},
	}
}

func (t *triggers) settle() time.Duration {
	if d := t.s.cfg.Timings.VolumeSettle; d > 0 {
		return d
	}
	return defaultVolumeSettle
}

func (t *triggers) reconcileEvery() time.Duration {
	if d := t.s.cfg.Timings.Reconcile; d > 0 {
		return d
	}
	return defaultReconcile
}

func (t *triggers) catchUpDelay() time.Duration {
	if d := t.s.cfg.Timings.CatchUpDelay; d > 0 {
		return d
	}
	return defaultCatchUpDelay
}

// start registers every enabled job's schedules and starts the volume
// watcher and the catch-up pass.
func (t *triggers) start(ctx context.Context) error {
	jobs, err := t.s.store.ListJobs()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if st, err := t.s.store.JobState(j.ID); err == nil && len(st.VolumeSeen) > 0 {
			cp := map[string]time.Time{}
			for k, v := range st.VolumeSeen {
				cp[k] = v
			}
			t.seenBefore[j.ID] = cp
		}
		t.syncJob(j)
	}
	t.s.goRun(func() { t.watchVolumes(ctx) })
	t.s.goRun(func() { t.catchUp(ctx) })
	return nil
}

// syncJob (re)registers a job's schedule triggers; a disabled job has
// none.
func (t *triggers) syncJob(j Job) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range t.entries[j.ID] {
		t.s.clock.Remove(id)
	}
	delete(t.entries, j.ID)
	if !j.Enabled {
		return
	}
	jobID := j.ID
	for _, tr := range j.Triggers {
		if tr.Kind != TriggerSchedule {
			continue
		}
		id, err := t.s.clock.Add(tr.Cron, func() { t.fire(jobID, RunBySchedule) })
		if err != nil {
			log.Printf("backup: job %s: schedule %q: %v", jobID, tr.Cron, err)
			continue
		}
		t.entries[jobID] = append(t.entries[jobID], id)
	}
}

func (t *triggers) removeJob(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.entries[id] {
		t.s.clock.Remove(e)
	}
	delete(t.entries, id)
}

// fire queues a triggered run of an enabled job.
func (t *triggers) fire(jobID string, by RunTrigger) {
	job, err := t.s.store.GetJob(jobID)
	if err != nil || !job.Enabled {
		return
	}
	if _, _, err := t.s.queue.Enqueue(job, EnqueueOptions{Kind: KindBackup, Trigger: by}); err != nil {
		log.Printf("backup: job %s: queue %s run: %v", jobID, by, err)
	}
}

// NextRun is the earliest next schedule activation of an enabled job.
func (t *triggers) NextRun(j Job, now time.Time) *time.Time {
	if !j.Enabled {
		return nil
	}
	var best time.Time
	for _, tr := range j.Triggers {
		if tr.Kind != TriggerSchedule {
			continue
		}
		n, err := t.s.clock.Next(tr.Cron, now)
		if err != nil || n.IsZero() {
			continue
		}
		if best.IsZero() || n.Before(best) {
			best = n
		}
	}
	if best.IsZero() {
		return nil
	}
	return &best
}

// ---------------------------------------------------------------------
// Volumes

func refKey(ep Endpoint) string { return string(ep.Kind) + "|" + ep.RefID }

// volumeMatches: the filesystem UUID, plus serial and size when the
// endpoint pins them (FAT/exFAT UUIDs are short and get cloned).
func volumeMatches(ep Endpoint, v engine.Volume) bool {
	if ep.Kind != EPVolume && ep.Kind != EPUSB {
		return false
	}
	if ep.RefID == "" || !strings.EqualFold(ep.RefID, v.UUID) {
		return false
	}
	if m := ep.Match; m != nil {
		if m.Serial != "" && m.Serial != v.Serial {
			return false
		}
		if m.SizeBytes != 0 && m.SizeBytes != v.Size {
			return false
		}
	}
	return true
}

// watchVolumes follows the engine's event stream, resubscribing with
// backoff when it ends, and reconciles against Volumes every minute so
// a dropped event can't lose an attach.
func (t *triggers) watchVolumes(ctx context.Context) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		ch, err := t.s.engine.Events(ctx)
		if err != nil {
			sleepCtx(ctx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		// Whatever is mounted when we (re)connect: at the very first
		// connect it's the boot state (catch-up's business, never an
		// attach); after a reconnect it's diffed like a reconcile.
		t.reconcile(ctx, first)
		first = false
		t.consume(ctx, ch)
	}
}

func (t *triggers) consume(ctx context.Context, ch <-chan engine.Event) {
	tick := time.NewTicker(t.reconcileEvery())
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Volume == nil {
				continue
			}
			switch ev.Type {
			case engine.EventVolumeMounted:
				t.mounted(ctx, *ev.Volume, ev.Initial)
			case engine.EventVolumeUnmounted:
				t.unmounted(ev.Volume.MountID)
			}
		case <-tick.C:
			t.reconcile(ctx, false)
		}
	}
}

// reconcile diffs the engine's mount table with what we know.
func (t *triggers) reconcile(ctx context.Context, initial bool) {
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	vols, err := t.s.engine.Volumes(vctx)
	cancel()
	if err != nil {
		return
	}
	present := map[int]bool{}
	for _, v := range vols {
		present[v.MountID] = true
		t.mounted(ctx, v, initial)
	}
	t.mu.Lock()
	var gone []int
	for id := range t.known {
		if !present[id] {
			gone = append(gone, id)
		}
	}
	t.mu.Unlock()
	for _, id := range gone {
		t.unmounted(id)
	}
}

// mounted records a mount; a new, non-initial one fires matching
// volume_mounted triggers after the settle delay.
func (t *triggers) mounted(ctx context.Context, v engine.Volume, initial bool) {
	t.mu.Lock()
	_, known := t.known[v.MountID]
	t.known[v.MountID] = v
	if !known && !initial {
		if old := t.pending[v.MountID]; old != nil {
			old.Stop()
		}
		mountID := v.MountID
		t.pending[mountID] = time.AfterFunc(t.settle(), func() { t.settled(ctx, mountID) })
	}
	t.mu.Unlock()
	t.markSeen(v)
}

func (t *triggers) unmounted(mountID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.known, mountID)
	if p := t.pending[mountID]; p != nil {
		p.Stop()
		delete(t.pending, mountID)
	}
}

// settled runs after the settle delay: if the mount is still there,
// every enabled job whose volume trigger matches it gets one run.
func (t *triggers) settled(ctx context.Context, mountID int) {
	if ctx.Err() != nil {
		return
	}
	t.mu.Lock()
	delete(t.pending, mountID)
	v, still := t.known[mountID]
	others := make([]engine.Volume, 0, len(t.known))
	for id, o := range t.known {
		if id != mountID {
			others = append(others, o)
		}
	}
	t.mu.Unlock()
	if !still {
		return
	}
	jobs, err := t.s.store.ListJobs()
	if err != nil {
		log.Printf("backup: volume trigger: %v", err)
		return
	}
	now := t.s.now()
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		for _, tr := range j.Triggers {
			if tr.Kind != TriggerVolumeMounted {
				continue
			}
			ref := triggerVolume(j, tr)
			if ref == nil || !volumeMatches(*ref, v) {
				continue
			}
			ambiguous := false
			for _, o := range others {
				if volumeMatches(*ref, o) {
					ambiguous = true
				}
			}
			if ambiguous {
				// Two drives answer to this identity (a cloned stick): a
				// run could write to the wrong one. The engine would fail
				// it with ambiguous_device; don't start one at all.
				log.Printf("backup: job %s: two mounted drives match %s; not starting a run", j.ID, ref.RefID)
				continue
			}
			if t.recordFire(j, tr, *ref, v.MountID, now) {
				if _, _, err := t.s.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunByVolumeMounted}); err != nil {
					log.Printf("backup: job %s: queue volume run: %v", j.ID, err)
				}
			}
		}
	}
}

// recordFire applies "once per attach" (by mount id) and min_gap_hours,
// and records the firing. It reports whether the trigger may fire.
func (t *triggers) recordFire(j Job, tr Trigger, ref Endpoint, mountID int, now time.Time) bool {
	ok := false
	key := refKey(ref)
	err := t.s.store.UpdateJobState(j.ID, func(st *JobState) {
		last, had := st.VolumeFires[key]
		if had && last.MountID == mountID {
			return
		}
		if had && tr.MinGapHours > 0 && now.Sub(last.At) < time.Duration(tr.MinGapHours)*time.Hour {
			return
		}
		if st.VolumeFires == nil {
			st.VolumeFires = map[string]VolumeFire{}
		}
		st.VolumeFires[key] = VolumeFire{MountID: mountID, At: now}
		ok = true
	})
	if err != nil {
		log.Printf("backup: job %s: %v", j.ID, err)
		return false
	}
	return ok
}

// markSeen remembers when the volumes jobs and remembered drives use were
// last seen mounted (throttled).
func (t *triggers) markSeen(v engine.Volume) {
	now := t.s.now()
	// throttle must be called with t.mu held.
	throttle := func(k string) bool {
		if last, ok := t.seenAt[k]; ok && now.Sub(last) < seenWriteEvery {
			return true
		}
		t.seenAt[k] = now
		return false
	}

	jobs, err := t.s.store.ListJobs()
	if err != nil {
		return
	}
	for _, j := range jobs {
		for _, tr := range j.Triggers {
			if tr.Kind != TriggerVolumeMounted {
				continue
			}
			ref := triggerVolume(j, tr)
			if ref == nil || !volumeMatches(*ref, v) {
				continue
			}
			key := refKey(*ref)
			t.mu.Lock()
			skip := throttle(j.ID + "|" + key)
			t.mu.Unlock()
			if skip {
				continue
			}
			_ = t.s.store.UpdateJobState(j.ID, func(st *JobState) {
				if st.VolumeSeen == nil {
					st.VolumeSeen = map[string]time.Time{}
				}
				st.VolumeSeen[key] = now
			})
		}
	}
	drives, err := t.s.store.RememberedDrives()
	if err != nil {
		return
	}
	for uuid, d := range drives {
		if !volumeMatches(d.Endpoint, v) {
			continue
		}
		t.mu.Lock()
		skip := throttle("usb|" + uuid)
		t.mu.Unlock()
		if !skip {
			_ = t.s.store.RememberDrive(d.Endpoint, &now)
		}
	}
}

// mountedMatch returns a mounted volume matching ref (ok=false when none,
// or when two do).
func (t *triggers) mountedMatch(ref Endpoint) (engine.Volume, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var found engine.Volume
	n := 0
	for _, v := range t.known {
		if volumeMatches(ref, v) {
			found = v
			n++
		}
	}
	return found, n == 1
}

// ---------------------------------------------------------------------
// Catch-up

// catchUp runs once, catchUpDelay after the clock gate opens: a schedule
// that should have fired since the job's last success (or creation), or
// a trigger volume that is present and was seen since the last success,
// queues one run per job (coalesced).
func (t *triggers) catchUp(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-t.s.clock.Armed():
	}
	sleepCtx(ctx, t.catchUpDelay())
	if ctx.Err() != nil {
		return
	}
	jobs, err := t.s.store.ListJobs()
	if err != nil {
		log.Printf("backup: catch-up: %v", err)
		return
	}
	now := t.s.now()
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		st, err := t.s.store.JobState(j.ID)
		if err != nil {
			continue
		}
		if t.catchUpDue(j, st, now) {
			if _, _, err := t.s.queue.Enqueue(j, EnqueueOptions{Kind: KindBackup, Trigger: RunByCatchUp}); err != nil {
				log.Printf("backup: job %s: queue catch-up: %v", j.ID, err)
			}
		}
	}
}

func (t *triggers) catchUpDue(j Job, st JobState, now time.Time) bool {
	since := j.CreatedAt
	if st.LastSuccessAt != nil {
		since = *st.LastSuccessAt
	}
	for _, tr := range j.Triggers {
		if !tr.CatchUp {
			continue
		}
		switch tr.Kind {
		case TriggerSchedule:
			next, err := t.s.clock.Next(tr.Cron, since)
			if err == nil && !next.IsZero() && next.Before(now) {
				return true
			}
		case TriggerVolumeMounted:
			ref := triggerVolume(j, tr)
			if ref == nil {
				continue
			}
			v, ok := t.mountedMatch(*ref)
			if !ok {
				continue
			}
			seen, wasSeen := t.seenBefore[j.ID][refKey(*ref)]
			if st.LastSuccessAt != nil && (!wasSeen || !st.LastSuccessAt.Before(seen)) {
				continue
			}
			// Counts as this attach: the real mount event later won't
			// fire again, and min_gap_hours applies.
			if t.recordFire(j, tr, *ref, v.MountID, now) {
				return true
			}
		}
	}
	return false
}
