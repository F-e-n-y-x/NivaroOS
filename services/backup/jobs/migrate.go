package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The Scheduled Tasks migration (spec §0 and §4.2). At every start the
// service imports core's backup and sync tasks as jobs, through core's
// schedules API over loopback, then disables each imported task and
// marks it migrated_to: "backup" (core's executor skips marked tasks).
// `nivaroos-backup release-scheduled-tasks` undoes the marking before an
// uninstall, restoring each task's enabled state as it was when Backup
// took it over. Both directions are idempotent:
//
//   - a task with a job (JobRow.MigratedFrom is UNIQUE) is never imported
//     twice; a crash between the database commit and marking the task
//     only redoes the marking;
//   - a marked task without a job was imported and then deleted by the
//     user in Backup & Sync: it stays as it is;
//   - release only touches tasks Backup took over (marked, or recorded
//     in its own store when core doesn't keep the marker).

// metaMigrationTask is the meta key prefix ("migration.task." + task id)
// of what Backup remembers about each task it took over.
const metaMigrationTask = "migration.task."

// migratedTask is that record.
type migratedTask struct {
	JobID string `json:"job_id"`
	// Enabled is the task's own enabled state when Backup took it over;
	// release restores it.
	Enabled  bool `json:"enabled"`
	Released bool `json:"released"`
}

func (s *Service) migrationRetry() time.Duration {
	if d := s.cfg.Timings.MigrationRetry; d > 0 {
		return d
	}
	return 30 * time.Second
}

// migrationLoop runs the migration once core answers, retrying with
// backoff (core or the engine may start after us). The report stays
// "pending" meanwhile; nothing is ever failed for being early.
func (s *Service) migrationLoop(ctx context.Context) {
	wait := s.migrationRetry()
	for ctx.Err() == nil {
		rep, err := s.Migrate(ctx)
		if err == nil && rep.State == MigrationDone {
			return
		}
		sleepCtx(ctx, wait)
		if wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// MigrationReport returns the stored report (pending when none yet).
func (s *Service) MigrationReport() (MigrationReport, error) {
	rep := MigrationReport{State: MigrationPending, Items: []MigrationItem{}}
	if _, err := s.store.GetMetaJSON(MetaMigrationV1, &rep); err != nil {
		return rep, err
	}
	if rep.Items == nil {
		rep.Items = []MigrationItem{}
	}
	return rep, nil
}

func (s *Service) saveReport(rep MigrationReport) {
	if err := s.store.SetMetaJSON(MetaMigrationV1, rep); err != nil {
		log.Printf("backup: migration report: %v", err)
	}
}

// Migrate runs one migration pass and stores its report.
func (s *Service) Migrate(ctx context.Context) (MigrationReport, error) {
	s.migrateMu.Lock()
	defer s.migrateMu.Unlock()
	prev, _ := s.MigrationReport()

	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	tasks, err := s.sched.ListTasks(lctx)
	cancel()
	if err != nil {
		rep := prev
		if rep.State != MigrationDone {
			rep.State, rep.ErrorCode, rep.Detail = MigrationPending, "", "Scheduled Tasks not reachable yet: "+describeErr(err)
			s.saveReport(rep)
		}
		return rep, err
	}
	hctx, hcancel := context.WithTimeout(ctx, 10*time.Second)
	_, herr := s.engine.Health(hctx)
	hcancel()
	if herr != nil {
		rep := prev
		if rep.State != MigrationDone {
			rep.State, rep.Detail = MigrationPending, "engine not ready yet: "+describeErr(herr)
			s.saveReport(rep)
		}
		return rep, herr
	}

	now := s.now()
	rep := MigrationReport{State: MigrationDone, RanAt: &now, Items: []MigrationItem{}}
	var newJobs []Job
	var newItems []int // index into rep.Items of each new job
	var toMark []ScheduleTask
	keepsMarker := coreKeepsMarker(tasks)
	stored, err := s.store.ListJobs()
	if err != nil {
		return prev, err
	}
	for _, t := range tasks {
		if !isBackupTask(t) {
			continue
		}
		if existing, err := s.store.JobByMigratedFrom(t.ID); err == nil {
			rep.Items = append(rep.Items, MigrationItem{
				TaskID: t.ID, TaskName: t.Name, Result: MigratedSkippedExists, JobID: existing.ID,
				Notes: []Message{}, DroppedArgs: []string{},
			})
			var rec migratedTask
			hadRec, err := s.store.GetMetaJSON(metaMigrationTask+t.ID, &rec)
			if err != nil {
				return prev, err
			}
			held := hadRec && !rec.Released
			if !hadRec {
				// A marked task with no record (a lost store) was the
				// user's to run: it is handed back enabled.
				rec.Enabled = true
			}
			if existing.NeedsAttention == AttentionMigratedUnresolved {
				// An import that couldn't be resolved (a drive unplugged
				// at the time) leaves the task running in core. Try again;
				// once it resolves the job takes over, as a fresh import
				// would have.
				enabled := t.Enabled
				if held || (!hadRec && t.MigratedTo == ScheduleMigratedMarker) {
					enabled = rec.Enabled // disabled by Backup, not by the user
				}
				ok, err := s.reresolveImport(ctx, existing, t, enabled, stored)
				if err != nil {
					return prev, err
				}
				if !ok {
					if held || t.MigratedTo == ScheduleMigratedMarker {
						// An earlier version took the task over anyway:
						// give it back until the job can run.
						if err := s.releaseTask(ctx, t, rec); err != nil {
							log.Printf("backup: migration: hand back unresolved task %s: %v", t.ID, err)
						}
					}
					continue
				}
				rep.Items[len(rep.Items)-1].Result = MigratedImported
				toMark = append(toMark, t)
				continue
			}
			// Redo the marking when it didn't happen (a crash after the
			// commit) or was undone (a release, or the task switched on
			// again). A core that drops the marker shows it missing
			// forever; Backup's own record stands in for it there.
			if t.Enabled || (t.MigratedTo != ScheduleMigratedMarker && (!held || keepsMarker)) {
				toMark = append(toMark, t)
			}
			continue
		} else if !isNoRecord(err) {
			return prev, err
		}
		var rec migratedTask
		hadRec, err := s.store.GetMetaJSON(metaMigrationTask+t.ID, &rec)
		if err != nil {
			return prev, err
		}
		if t.MigratedTo == ScheduleMigratedMarker || (hadRec && !rec.Released) {
			// Imported once, job deleted by the user since. (The record
			// covers a core that doesn't keep the marker.)
			rep.Items = append(rep.Items, MigrationItem{
				TaskID: t.ID, TaskName: t.Name, Result: MigratedSkippedExists, Notes: []Message{}, DroppedArgs: []string{},
			})
			continue
		}
		job, item := s.jobFromTask(ctx, t)
		if item.Result == MigratedImported && overlapsStored(job, "", append(stored, newJobs...)) {
			unresolvedOverlap(&job, &item)
		}
		rep.Items = append(rep.Items, item)
		newJobs = append(newJobs, job)
		newItems = append(newItems, len(rep.Items)-1)
		// Only a job that can run takes the task over; an unresolved one
		// leaves it running in core (a later pass retries).
		if item.Result == MigratedImported {
			toMark = append(toMark, t)
		}
	}

	// One transaction for every new job and its bookkeeping.
	if len(newJobs) > 0 {
		created := make([]Job, 0, len(newJobs))
		_, err := s.store.createJobs(newJobs, now, func(tx *gorm.DB) error {
			var rows []JobRow
			if err := tx.Where("migrated_from IS NOT NULL").Find(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				j, err := r.ToJob()
				if err != nil {
					return err
				}
				created = append(created, j)
			}
			return nil
		})
		if err != nil {
			rep = prev
			rep.State, rep.ErrorCode, rep.Detail = MigrationFailed, ErrStoreUnavailable, describeErr(err)
			s.saveReport(rep)
			return rep, err
		}
		byTask := map[string]Job{}
		for _, j := range created {
			if j.MigratedFrom != nil {
				byTask[*j.MigratedFrom] = j
			}
		}
		for i, idx := range newItems {
			if j, ok := byTask[*newJobs[i].MigratedFrom]; ok {
				rep.Items[idx].JobID = j.ID
				s.jobChanged(j, JobChangeCreated)
				s.rememberDrives(j, nil)
			}
		}
	}

	// Then disable and mark the tasks in core. A failure leaves the job
	// in place; the next pass only redoes this step.
	var markErr error
	for _, t := range toMark {
		if err := s.markTask(ctx, t); err != nil {
			markErr = err
			log.Printf("backup: migration: mark task %s: %v", t.ID, err)
		}
	}
	if markErr != nil {
		rep.State, rep.Detail = MigrationPending, "could not update Scheduled Tasks yet: "+describeErr(markErr)
	}
	// Keep the history of earlier passes' imports visible: an item that
	// this pass saw as skipped_exists keeps its original result.
	rep.Items = mergeMigrationItems(prev.Items, rep.Items)
	rep.Imported = 0
	for _, it := range rep.Items {
		if it.Result == MigratedImported || it.Result == MigratedImportedUnresolved {
			rep.Imported++
		}
	}
	s.saveReport(rep)
	if n := len(newJobs); n > 0 {
		s.notify(Notification{
			Message: Message{Key: "backup.notify.migrated", Args: map[string]interface{}{"count": n}},
			Level:   NotifyLevelInfo, WindowKind: "app", WindowProps: map[string]interface{}{"section": "settings"},
		})
	}
	return rep, markErr
}

// overlapsStored reports whether a job's destination overlaps another
// job's (checkDestOverlap).
func overlapsStored(j Job, selfID string, others []Job) bool {
	fe := fieldErrors{}
	checkDestOverlap(j, selfID, others, fe)
	return len(fe) > 0
}

// unresolvedOverlap turns an import whose destination another job
// already uses into an unresolved one (disabled, needs attention).
func unresolvedOverlap(j *Job, item *MigrationItem) {
	j.Enabled = false
	j.NeedsAttention = AttentionMigratedUnresolved
	item.Result = MigratedImportedUnresolved
	item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.dest_overlap"})
}

// reresolveImport maps an unresolved import's task again. When it now
// resolves (and doesn't overlap another job), the job gets the resolved
// endpoints, the task's enabled state and the "imported" badge; it
// reports whether it did.
func (s *Service) reresolveImport(ctx context.Context, existing Job, t ScheduleTask, enabled bool, stored []Job) (bool, error) {
	job, item := s.jobFromTask(ctx, t)
	if item.Result != MigratedImported || overlapsStored(job, existing.ID, stored) {
		return false, nil
	}
	j, err := s.store.MutateJob(existing.ID, s.now(), true, func(cur *Job) error {
		if cur.NeedsAttention != AttentionMigratedUnresolved {
			return nil // edited by the user meanwhile: theirs stands
		}
		cur.Sources, cur.Dest, cur.Filters = job.Sources, job.Dest, job.Filters
		cur.Enabled, cur.NeedsAttention = enabled, AttentionImported
		return nil
	})
	if err != nil {
		return false, err
	}
	s.jobChanged(j, JobChangeUpdated)
	s.rememberDrives(j, nil)
	return true, nil
}

// releaseTask gives one task back to Scheduled Tasks (its own enabled
// state, no marker).
func (s *Service) releaseTask(ctx context.Context, t ScheduleTask, rec migratedTask) error {
	uctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := s.sched.UpdateTask(uctx, t, rec.Enabled, ""); err != nil {
		return err
	}
	rec.Released = true
	return s.store.SetMetaJSON(metaMigrationTask+t.ID, rec)
}

// unresolvedImports reports whether any imported job still waits for its
// task to resolve (maintenance then runs another migration pass).
func (s *Service) unresolvedImports() bool {
	jobs, err := s.store.ListJobs()
	if err != nil {
		return false
	}
	for _, j := range jobs {
		if j.MigratedFrom != nil && j.NeedsAttention == AttentionMigratedUnresolved {
			return true
		}
	}
	return false
}

// coreKeepsMarker reports whether core stores migrated_to: a core that
// knows the field sends it with every task (empty when unset); an older
// one drops it on save and never sends it.
func coreKeepsMarker(tasks []ScheduleTask) bool {
	for _, t := range tasks {
		if _, ok := t.Raw[ScheduleMigratedField]; ok {
			return true
		}
	}
	return false
}

// mergeMigrationItems keeps the first recorded result of each task.
func mergeMigrationItems(prev, cur []MigrationItem) []MigrationItem {
	old := map[string]MigrationItem{}
	for _, it := range prev {
		old[it.TaskID] = it
	}
	out := make([]MigrationItem, 0, len(cur))
	for _, it := range cur {
		if o, ok := old[it.TaskID]; ok && it.Result == MigratedSkippedExists && o.Result != MigratedSkippedExists {
			if it.JobID == "" {
				o.JobID = ""
			}
			out = append(out, o)
			continue
		}
		out = append(out, it)
	}
	return out
}

// markTask disables a task and sets the marker, first remembering the
// task's own enabled state (unless Backup already holds it: then the
// task's current "disabled" is Backup's doing, not the user's).
func (s *Service) markTask(ctx context.Context, t ScheduleTask) error {
	key := metaMigrationTask + t.ID
	var rec migratedTask
	had, err := s.store.GetMetaJSON(key, &rec)
	if err != nil {
		return err
	}
	if !had || rec.Released {
		rec.Enabled = t.Enabled
	}
	rec.Released = false
	if j, err := s.store.JobByMigratedFrom(t.ID); err == nil {
		rec.JobID = j.ID
	}
	if err := s.store.SetMetaJSON(key, rec); err != nil {
		return err
	}
	uctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = s.sched.UpdateTask(uctx, t, false, ScheduleMigratedMarker)
	return err
}

// ReleaseScheduledTasks is `nivaroos-backup release-scheduled-tasks`:
// every task Backup took over gets its marker cleared and its enabled
// state back. It returns how many tasks it released; running it again
// releases nothing more.
func (s *Service) ReleaseScheduledTasks(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, fmt.Errorf("store unavailable: %w", s.storeErr)
	}
	s.migrateMu.Lock()
	defer s.migrateMu.Unlock()
	tasks, err := s.sched.ListTasks(ctx)
	if err != nil {
		return 0, fmt.Errorf("Scheduled Tasks: %w", err)
	}
	recs, err := s.migratedTasks()
	if err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, t := range tasks {
		rec, had := recs[t.ID]
		// A task is Backup's when core shows the marker, or - with a core
		// that doesn't keep unknown fields - when Backup's own record says
		// it took the task over and hasn't given it back.
		if t.MigratedTo != ScheduleMigratedMarker && (!had || rec.Released) {
			continue
		}
		key := metaMigrationTask + t.ID
		if !had {
			// No record (a lost store): enable it, so uninstalling Backup
			// never silently stops a user's old backup.
			rec.Enabled = true
		}
		if _, err := s.sched.UpdateTask(ctx, t, rec.Enabled, ""); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("task %s: %w", t.ID, err)
			}
			continue
		}
		rec.Released = true
		if err := s.store.SetMetaJSON(key, rec); err != nil && firstErr == nil {
			firstErr = err
		}
		n++
	}
	return n, firstErr
}

// isBackupTask selects what moves: type backup or sync with both a
// source and a destination (a "backup" task that only has a command is a
// command task and stays in Scheduled Tasks).
func isBackupTask(t ScheduleTask) bool {
	if t.Type != "backup" && t.Type != "sync" {
		return false
	}
	src, dst := taskPaths(t)
	return src != "" && dst != ""
}

// taskPaths is where core's executor takes source and destination from.
func taskPaths(t ScheduleTask) (string, string) {
	src, dst := strings.TrimSpace(t.SourcePath), strings.TrimSpace(t.DestPath)
	if src == "" {
		src = strings.TrimSpace(t.TargetID)
	}
	if dst == "" {
		dst = strings.TrimSpace(t.Target)
	}
	return src, dst
}

// taskAction is the executor's action: action, else sync_mode, else copy.
func taskAction(t ScheduleTask) string {
	a := t.Action
	if a == "" {
		a = t.SyncMode
	}
	if a == "" {
		a = "copy"
	}
	return a
}

// jobFromTask maps a task onto a job (spec §4.2 step 3). An endpoint
// that can't be resolved (drive absent, remote gone) still imports, as a
// disabled job that needs attention.
func (s *Service) jobFromTask(ctx context.Context, t ScheduleTask) (Job, MigrationItem) {
	settings, _ := s.store.Settings()
	item := MigrationItem{TaskID: t.ID, TaskName: t.Name, Result: MigratedImported, Notes: []Message{}, DroppedArgs: []string{}}
	srcPath, dstPath := taskPaths(t)

	jt := TypeCopy
	switch taskAction(t) {
	case "sync", "rclone_sync", "rsync", "rsync_backup":
		jt = TypeMirror
	case "move", "rclone_move":
		item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.move_to_copy"})
	case "archive", "tar_archive":
		jt = TypeArchive
	}

	src, srcOK := s.resolveTaskPath(ctx, srcPath)
	dst, dstOK := s.resolveTaskPath(ctx, dstPath)
	if !srcOK {
		item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.unresolved_source", Args: map[string]interface{}{"path": srcPath}})
	}
	if !dstOK {
		item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.unresolved_dest", Args: map[string]interface{}{"path": dstPath}})
	}

	filters, opts, dropped := mapExtraArgs(t.ExtraArgs)
	item.DroppedArgs = dropped
	if len(dropped) > 0 {
		item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.dropped_args", Args: map[string]interface{}{"args": strings.Join(dropped, " ")}})
	}

	name := strings.TrimSpace(t.Name)
	if name == "" {
		name = srcPath + " → " + dstPath
	}
	taskID := t.ID
	job := Job{
		Name: name, Type: jt, Enabled: t.Enabled, Sources: []Endpoint{src}, Dest: dst,
		Triggers:   []Trigger{},
		Conditions: Conditions{DestAvailable: true},
		Filters:    filters,
		Options: Options{
			PreviewFirst: false, // migrated jobs keep running; the guards protect them
			LowPriority:  true, CopyEmptyDirs: opts.CopyEmptyDirs,
		},
		Retry:  Retry{Max: DefaultRetryMax, BackoffSec: append([]int(nil), DefaultRetryBackoffSec...)},
		Notify: NotifyPrefs{OnFailure: true},
		Hooks:  []Hook{},
	}
	if jt == TypeMirror {
		job.Retention.VersionsDays = orDefault(settings.DefaultVersionsDays, DefaultVersionsDays)
	}
	if strings.TrimSpace(t.Cron) != "" {
		// Byte for byte: core used the same parser.
		job.Triggers = append(job.Triggers, Trigger{Kind: TriggerSchedule, Cron: t.Cron, CatchUp: settings.CatchUpDefault})
	}
	// A destination inside the source is excluded from it.
	if srcOK && dstOK {
		if rule, ok := DestExcludeRule(src, dst); ok {
			job.Filters.Exclude = append(job.Filters.Exclude, rule)
			item.Notes = append(item.Notes, Message{Key: "backup.migrate.note.dest_excluded", Args: map[string]interface{}{"path": dstPath}})
		}
	}

	norm, fe := NormalizeJob(job, ValidateEnv{Settings: settings})
	norm.MigratedFrom = &taskID
	if !srcOK || !dstOK || len(fe) > 0 {
		if len(fe) > 0 {
			log.Printf("backup: migration: task %s: %v", t.ID, sortedKeys(fe))
		}
		norm.Enabled = false
		norm.NeedsAttention = AttentionMigratedUnresolved
		item.Result = MigratedImportedUnresolved
	} else {
		norm.NeedsAttention = AttentionImported
	}
	return norm, item
}

// resolveTaskPath turns a schedules.json path into an endpoint:
// "remote:path" is a cloud remote, an absolute path goes through the
// engine's resolver (which maps /mnt/<type>_<name> FUSE mounts to their
// remote), and /mnt/<host>/<share>/... that the engine doesn't know is a
// saved network share. On failure the endpoint still carries the old
// path as its label, for the user to see.
func (s *Service) resolveTaskPath(ctx context.Context, p string) (Endpoint, bool) {
	unresolved := Endpoint{Kind: EPVolume, Label: p}
	if p == "" {
		return unresolved, false
	}
	if !strings.HasPrefix(p, "/") {
		remote, sub, ok := strings.Cut(p, ":")
		if !ok || remote == "" || strings.ContainsAny(remote, "/\\") {
			return unresolved, false
		}
		clean, okSub := CleanSubPath(strings.Trim(sub, "/"))
		if !okSub {
			return unresolved, false
		}
		ep := Endpoint{Kind: EPCloud, RefID: remote, SubPath: clean, Label: remote}
		locs, err := s.engine.Locations(ctx)
		if err != nil {
			return ep, false
		}
		for _, l := range locs {
			if l.Kind == EPCloud && l.RefID == remote {
				ep.Label = l.Label
				return ep, true
			}
		}
		return ep, false
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if res, err := s.engine.ResolvePath(rctx, engine.ResolvePathRequest{Path: path.Clean(p)}); err == nil && res.OK && res.Endpoint != nil {
		return *res.Endpoint, true
	}
	if ep, ok := s.smbEndpointForPath(ctx, path.Clean(p)); ok {
		return ep, true
	}
	return unresolved, false
}

// smbEndpointForPath maps /mnt/<host>/<share>/<sub> to a saved network
// share connection.
func (s *Service) smbEndpointForPath(ctx context.Context, p string) (Endpoint, bool) {
	if s.smb == nil {
		return Endpoint{}, false
	}
	conns, err := s.smb.Connections(ctx)
	if err != nil {
		return Endpoint{}, false
	}
	for _, c := range conns {
		if c.MountPoint == "" {
			continue
		}
		rest, ok := strings.CutPrefix(p, strings.TrimRight(c.MountPoint, "/")+"/")
		if !ok {
			continue
		}
		share, _, _ := strings.Cut(rest, "/")
		for _, sh := range c.Shares {
			if sh == share {
				sub, ok := CleanSubPath(rest)
				if !ok {
					return Endpoint{}, false
				}
				return Endpoint{Kind: EPSMB, RefID: c.ID, SubPath: sub, Label: c.Label()}, true
			}
		}
	}
	return Endpoint{}, false
}

// migratedOptions are the extra_args that map onto options.
type migratedOptions struct {
	CopyEmptyDirs bool
}

// quietFlags only changed rclone's or rsync's console output; dropping
// them changes nothing, so they aren't reported.
var quietFlags = map[string]bool{
	"-v": true, "-vv": true, "-vvv": true, "--verbose": true, "-P": true, "--progress": true,
	"--stats-one-line": true, "-q": true, "--quiet": true, "-h": true, "--human-readable": true,
}

// mapExtraArgs maps the typed-equivalent flags of a task's extra_args
// (--exclude, --include, --max-size, --create-empty-src-dirs) and returns
// every other one as dropped. Free-form flags are never kept (spec §3.6).
func mapExtraArgs(extra string) (Filters, migratedOptions, []string) {
	f := Filters{ExcludePresets: []string{}, Exclude: []string{}}
	var o migratedOptions
	dropped := []string{}
	fields := splitArgs(extra)
	for i := 0; i < len(fields); i++ {
		a := fields[i]
		name, val, hasVal := strings.Cut(a, "=")
		takesValue := func() (string, bool) {
			if hasVal {
				return val, true
			}
			if i+1 < len(fields) {
				i++
				return fields[i], true
			}
			return "", false
		}
		switch name {
		case "--exclude":
			if v, ok := takesValue(); ok && validFilterRule(v) {
				f.Exclude = append(f.Exclude, v)
			} else {
				dropped = append(dropped, strings.TrimSpace(a+" "+v))
			}
		case "--include":
			if v, ok := takesValue(); ok && validFilterRule(v) {
				f.Include = append(f.Include, v)
			} else {
				dropped = append(dropped, strings.TrimSpace(a+" "+v))
			}
		case "--max-size":
			v, ok := takesValue()
			n, err := parseSize(v)
			if ok && err == nil && n > 0 {
				f.MaxSizeBytes = n
			} else {
				dropped = append(dropped, strings.TrimSpace(a+" "+v))
			}
		case "--create-empty-src-dirs":
			o.CopyEmptyDirs = true
		default:
			if quietFlags[a] {
				continue
			}
			// Keep a flag and its value together in the report.
			if !hasVal && strings.HasPrefix(a, "--") && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				a += " " + fields[i+1]
				i++
			}
			dropped = append(dropped, a)
		}
	}
	return f, o, dropped
}

func validFilterRule(r string) bool {
	fe := fieldErrors{}
	f := Filters{Exclude: []string{r}}
	normalizeFilters(&f, fe)
	return len(fe) == 0 && len(f.Exclude) == 1
}

// splitArgs splits like a shell would for plain words and '...' / "..."
// quoting (core split extra_args on spaces; quotes let a pattern hold
// one).
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inWord := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

// parseSize parses rclone's size suffixes (1024-based: 100k, 10M, 1G).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty size")
	}
	mult := int64(1)
	switch strings.ToUpper(s[len(s)-1:]) {
	case "B":
		s = s[:len(s)-1]
	case "K":
		mult, s = 1<<10, s[:len(s)-1]
	case "M":
		mult, s = 1<<20, s[:len(s)-1]
	case "G":
		mult, s = 1<<30, s[:len(s)-1]
	case "T":
		mult, s = 1<<40, s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return int64(f * float64(mult)), nil
}

// migratedTasks lists the task records (for tests and diagnostics).
func (s *Service) migratedTasks() (map[string]migratedTask, error) {
	rows, err := s.store.MetaWithPrefix(metaMigrationTask)
	if err != nil {
		return nil, err
	}
	out := map[string]migratedTask{}
	for _, r := range rows {
		var rec migratedTask
		if err := json.Unmarshal([]byte(r.Value), &rec); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(r.Key, metaMigrationTask)] = rec
	}
	return out, nil
}
