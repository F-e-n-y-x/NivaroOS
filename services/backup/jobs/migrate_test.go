package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The Scheduled Tasks migration (spec §0, §4.2, §16.1 migrate_test.go).
// Backup is a separate module, so it goes through core's schedules API:
// it never rewrites schedules.json itself (no .pre-backup-migration
// copy); "the JSON rewrite" of §4.2 step 5 is marking each imported task
// disabled + migrated_to in core.

// loadFixtureTasks fills the fake core with testdata/schedules.json.
func loadFixtureTasks(t *testing.T, f *fakeSchedules) {
	t.Helper()
	raw, err := os.ReadFile("testdata/schedules.json")
	if err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]interface{}
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatal(err)
	}
	if f.keepMarker {
		// A core that knows the field sends it (empty) with every task.
		for _, tk := range tasks {
			if _, ok := tk[ScheduleMigratedField]; !ok {
				tk[ScheduleMigratedField] = ""
			}
		}
	}
	f.mu.Lock()
	f.tasks = tasks
	f.mu.Unlock()
}

// migrationEngine resolves the fixture's paths like the real engine:
// /DATA on the system disk, /mnt/tank on a data disk, and the rclone FUSE
// mount /mnt/gdrive_mydrive to its remote; the USB drive is absent.
func migrationEngine(h *harness) {
	h.eng.Lock()
	defer h.eng.Unlock()
	h.eng.LocationsV = []engine.Location{
		{Kind: EPCloud, RefID: "gdrive", Label: "Google Drive", Online: true},
		{Kind: EPCloud, RefID: "mydrive", Label: "My Drive", Online: true},
	}
	h.eng.ResolvePathFn = func(r engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
		ep := func(kind EndpointKind, ref, sub string) (engine.ResolvePathResult, error) {
			e := Endpoint{Kind: kind, RefID: ref, SubPath: strings.Trim(sub, "/"), Label: ref}
			return engine.ResolvePathResult{OK: true, Endpoint: &e}, nil
		}
		p := r.Path
		switch {
		case p == "/DATA" || strings.HasPrefix(p, "/DATA/"):
			return ep(EPVolume, "root-uuid", p)
		case strings.HasPrefix(p, "/mnt/tank/"):
			return ep(EPVolume, "tank-uuid", strings.TrimPrefix(p, "/mnt/tank/"))
		case strings.HasPrefix(p, "/mnt/gdrive_mydrive/"):
			return ep(EPCloud, "mydrive", strings.TrimPrefix(p, "/mnt/gdrive_mydrive/"))
		}
		return engine.ResolvePathResult{OK: false, Reason: engine.CodeEndpointUnknown}, nil
	}
}

func newMigrationHarness(t *testing.T, keepMarker bool) *harness {
	t.Helper()
	h := newHarness(t, false)
	h.sched.keepMarker = keepMarker
	h.svc.smb = &fakeSMB{conns: []SMBConnection{{ID: "1", Host: "nas", User: "u", Password: "p", Shares: []string{"share"}, MountPoint: "/mnt/nas"}}}
	loadFixtureTasks(t, h.sched)
	migrationEngine(h)
	return h
}

func reportItems(rep MigrationReport) map[string]MigrationItem {
	out := map[string]MigrationItem{}
	for _, it := range rep.Items {
		out[it.TaskID] = it
	}
	return out
}

func jobByTask(t *testing.T, h *harness, task string) Job {
	t.Helper()
	j, err := h.svc.store.JobByMigratedFrom(task)
	if err != nil {
		t.Fatalf("no job for %s: %v", task, err)
	}
	return j
}

const importedTasks = 19 // every backup/sync task with a source and a destination

// takenOverTasks are the imports Backup takes over in core: all but the
// two whose destination can't be resolved (task_absent_usb,
// task_gone_remote), which keep running in Scheduled Tasks.
const takenOverTasks = importedTasks - 2

func TestMigrationImportsTasks(t *testing.T) {
	h := newMigrationHarness(t, true)
	// The first pass runs at start, by itself.
	h.start()
	var rep MigrationReport
	h.eventually("migration at start", func() bool {
		rep, _ = h.svc.MigrationReport()
		return rep.State == MigrationDone
	})
	if rep.State != MigrationDone || rep.Imported != importedTasks || rep.RanAt == nil {
		t.Fatalf("report: state %s imported %d items %d", rep.State, rep.Imported, len(rep.Items))
	}
	items := reportItems(rep)
	jobs, _ := h.svc.store.ListJobs()
	if len(jobs) != importedTasks {
		t.Fatalf("%d jobs", len(jobs))
	}

	// Actions map onto job types.
	types := map[string]JobType{
		"task_copy": TypeCopy, "task_rclone_copy": TypeCopy, "task_sync": TypeMirror, "task_rclone_sync": TypeMirror,
		"task_rsync": TypeMirror, "task_rsync_backup": TypeMirror, "task_move": TypeCopy, "task_rclone_move": TypeCopy,
		"task_archive": TypeArchive, "task_tar": TypeArchive, "task_sync_mode": TypeMirror, "task_legacy_fields": TypeCopy,
	}
	for task, want := range types {
		j := jobByTask(t, h, task)
		if j.Type != want {
			t.Errorf("%s -> %s, want %s", task, j.Type, want)
		}
		if items[task].Result != MigratedImported || items[task].JobID != j.ID {
			t.Errorf("%s report item %+v", task, items[task])
		}
	}
	copyJob := jobByTask(t, h, "task_copy")
	if copyJob.Name != "Docs copy" || !copyJob.Enabled || copyJob.NeedsAttention != AttentionImported ||
		copyJob.Sources[0] != (Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Documents", Label: "root-uuid"}) ||
		copyJob.Dest.RefID != "tank-uuid" || copyJob.Dest.SubPath != "Backups/copy" || copyJob.Options.PreviewFirst ||
		len(copyJob.Triggers) != 1 || copyJob.Triggers[0].Cron != "0 3 * * *" || copyJob.Retry.Max != DefaultRetryMax {
		t.Errorf("copy job %+v", copyJob)
	}
	if m := jobByTask(t, h, "task_sync"); m.Retention.VersionsDays != DefaultVersionsDays || !m.Conditions.DestAvailable {
		t.Errorf("mirror retention %+v", m.Retention)
	}
	if a := jobByTask(t, h, "task_archive"); a.Retention.KeepLast != DefaultKeepLast {
		t.Errorf("archive retention %+v", a.Retention)
	}
	if n := jobByTask(t, h, "task_rclone_sync"); n.Name != "/DATA/Documents → /mnt/tank/Backups/rclone_sync" {
		t.Errorf("unnamed task: %q", n.Name)
	}
	if d := jobByTask(t, h, "task_rsync_backup"); d.Enabled {
		t.Error("a disabled task came in enabled")
	}
	notes := func(task string) string {
		var keys []string
		for _, n := range items[task].Notes {
			keys = append(keys, n.Key)
		}
		return strings.Join(keys, ",")
	}
	if !strings.Contains(notes("task_move"), "backup.migrate.note.move_to_copy") {
		t.Errorf("move note: %s", notes("task_move"))
	}

	// /DATA -> /DATA/Backup: the destination is excluded from the source.
	self := jobByTask(t, h, "task_self")
	if self.Type != TypeMirror || len(self.Filters.Exclude) != 1 || self.Filters.Exclude[0] != "/Backup/**" || !self.Enabled {
		t.Errorf("self-including job %+v", self.Filters)
	}
	if !strings.Contains(notes("task_self"), "backup.migrate.note.dest_excluded") {
		t.Errorf("self note: %s", notes("task_self"))
	}

	// Unresolvable ends: imported, disabled, needs attention.
	for _, task := range []string{"task_absent_usb", "task_gone_remote"} {
		j := jobByTask(t, h, task)
		if j.Enabled || j.NeedsAttention != AttentionMigratedUnresolved || items[task].Result != MigratedImportedUnresolved {
			t.Errorf("%s: enabled %v attention %q result %q", task, j.Enabled, j.NeedsAttention, items[task].Result)
		}
		if !strings.Contains(notes(task), "backup.migrate.note.unresolved_dest") {
			t.Errorf("%s notes %s", task, notes(task))
		}
	}
	if j := jobByTask(t, h, "task_absent_usb"); j.Dest.Label != "/media/usb-absent/Backups" {
		t.Errorf("the old path isn't shown: %+v", j.Dest)
	}

	// Clouds: the FUSE mount maps to its remote, remote:path directly.
	if j := jobByTask(t, h, "task_fuse"); j.Dest.Kind != EPCloud || j.Dest.RefID != "mydrive" || j.Dest.SubPath != "Photos" {
		t.Errorf("fuse dest %+v", j.Dest)
	}
	if j := jobByTask(t, h, "task_remote"); j.Dest != (Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "Backups/Docs", Label: "Google Drive"}) {
		t.Errorf("remote dest %+v", j.Dest)
	}
	// A saved network share.
	if j := jobByTask(t, h, "task_smb"); j.Sources[0].Kind != EPSMB || j.Sources[0].RefID != "1" || j.Sources[0].SubPath != "share/docs" || j.Sources[0].Label != `\\nas` {
		t.Errorf("smb source %+v", j.Sources[0])
	}

	// Extra args: typed flags kept, the rest reported.
	args := jobByTask(t, h, "task_args")
	if len(args.Filters.Exclude) != 1 || args.Filters.Exclude[0] != "*.tmp" || args.Filters.MaxSizeBytes != 100<<20 ||
		len(args.Filters.Include) != 1 || args.Filters.Include[0] != "docs/**" || !args.Options.CopyEmptyDirs {
		t.Errorf("args filters %+v options %+v", args.Filters, args.Options)
	}
	if d := items["task_args"].DroppedArgs; strings.Join(d, "|") != "--bwlimit 10M|--fast-list" {
		t.Errorf("dropped args %q", d)
	}

	// Tasks that aren't backups stay in Scheduled Tasks, untouched.
	for _, task := range []string{"task_command_backup", "task_vm", "task_container", "task_trim"} {
		if _, ok := items[task]; ok {
			t.Errorf("%s in the report", task)
		}
		tk := h.sched.task(task)
		if tk["enabled"] != true || tk[ScheduleMigratedField] != "" {
			t.Errorf("%s touched: %v", task, tk)
		}
	}
	// Imported tasks are disabled and marked in core.
	for task := range types {
		tk := h.sched.task(task)
		if tk["enabled"] != false || tk[ScheduleMigratedField] != ScheduleMigratedMarker {
			t.Errorf("%s not handed over: %v", task, tk)
		}
	}
	h.eventually("migrated notification", func() bool { return len(h.bus.notifications()) > 0 })
	if n := h.bus.named(EventNotify); len(n) != 1 || n[0][PropNotifyKey] != "backup.notify.migrated" || !strings.Contains(n[0][PropMessage], "19") {
		t.Errorf("notifications %v", n)
	}
	// The stored report is what GET /migration returns.
	stored, _ := h.svc.MigrationReport()
	if stored.Imported != importedTasks || len(stored.Items) != len(rep.Items) {
		t.Errorf("stored report %+v", stored)
	}
}

func TestMigrationSecondRunIsNoOp(t *testing.T) {
	for _, keep := range []bool{true, false} {
		h := newMigrationHarness(t, keep)
		h.start()
		h.eventually("first pass", func() bool {
			r, _ := h.svc.MigrationReport()
			return r.State == MigrationDone
		})
		h.eventually("migrated notification", func() bool { return len(h.bus.notifications()) == 1 })
		h.sched.mu.Lock()
		updates := h.sched.updates
		h.sched.mu.Unlock()
		jobs1, _ := h.svc.store.ListJobs()
		rep, err := h.svc.Migrate(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		jobs2, _ := h.svc.store.ListJobs()
		if len(jobs2) != len(jobs1) {
			t.Errorf("keepMarker=%v: %d jobs after the second pass, want %d", keep, len(jobs2), len(jobs1))
		}
		if h.sched.updates != updates {
			t.Errorf("keepMarker=%v: the second pass changed %d tasks", keep, h.sched.updates-updates)
		}
		// The report keeps the first pass's results.
		if rep.Imported != importedTasks || reportItems(rep)["task_copy"].Result != MigratedImported {
			t.Errorf("keepMarker=%v: second report %+v", keep, reportItems(rep)["task_copy"])
		}
		settle()
		if n := len(h.bus.notifications()); n != 1 {
			t.Errorf("keepMarker=%v: %d notifications", keep, n)
		}
	}
}

func TestMigrationCrashBetweenCommitAndMarking(t *testing.T) {
	h := newMigrationHarness(t, true)
	h.sched.updateErr = errors.New("core went away")
	rep, err := h.svc.Migrate(context.Background())
	if err == nil || rep.State != MigrationPending {
		t.Fatalf("first pass: %v %s", err, rep.State)
	}
	jobs1, _ := h.svc.store.ListJobs()
	if len(jobs1) != importedTasks {
		t.Fatalf("%d jobs committed", len(jobs1))
	}
	// The tasks still run in core meanwhile; the next pass only marks them.
	h.sched.updateErr = nil
	rep, err = h.svc.Migrate(context.Background())
	if err != nil || rep.State != MigrationDone {
		t.Fatalf("second pass: %v %+v", err, rep.State)
	}
	jobs2, _ := h.svc.store.ListJobs()
	if len(jobs2) != len(jobs1) {
		t.Fatalf("duplicates: %d jobs", len(jobs2))
	}
	if tk := h.sched.task("task_copy"); tk["enabled"] != false || tk[ScheduleMigratedField] != ScheduleMigratedMarker {
		t.Errorf("task not marked on the second pass: %v", tk)
	}
	// Release gives the disabled one back disabled, the rest enabled.
	n, err := h.svc.ReleaseScheduledTasks(context.Background())
	if err != nil || n != takenOverTasks {
		t.Fatalf("release: %d %v", n, err)
	}
	if h.sched.task("task_rsync_backup")["enabled"] != false || h.sched.task("task_copy")["enabled"] != true {
		t.Error("release didn't restore each task's own state")
	}
}

func TestMigrationWaitsForCoreAndEngine(t *testing.T) {
	h := newMigrationHarness(t, true)
	h.sched.listErr = errUnreachable
	rep, err := h.svc.Migrate(context.Background())
	if err == nil || rep.State != MigrationPending || !strings.Contains(rep.Detail, "Scheduled Tasks") {
		t.Errorf("core down: %v %+v", err, rep)
	}
	h.sched.listErr = nil
	h.eng.Lock()
	h.eng.ErrHealth = &engine.Error{Code: engine.CodeEngineUnavailable}
	h.eng.Unlock()
	rep, err = h.svc.Migrate(context.Background())
	if err == nil || rep.State != MigrationPending {
		t.Errorf("engine down: %v %+v", err, rep)
	}
	if jobs, _ := h.svc.store.ListJobs(); len(jobs) != 0 {
		t.Errorf("imported without the resolver: %d jobs", len(jobs))
	}
	h.eng.Lock()
	h.eng.ErrHealth = nil
	h.eng.Unlock()
	// The loop at start picks it up.
	h.start()
	h.eventually("migration done", func() bool {
		r, _ := h.svc.MigrationReport()
		return r.State == MigrationDone
	})
	if jobs, _ := h.svc.store.ListJobs(); len(jobs) != importedTasks {
		t.Errorf("%d jobs", len(jobs))
	}
}

func TestReleaseIsIdempotentBothWays(t *testing.T) {
	h := newMigrationHarness(t, true)
	h.svc.Migrate(context.Background())
	n, err := h.svc.ReleaseScheduledTasks(context.Background())
	if err != nil || n != takenOverTasks {
		t.Fatalf("release: %d %v", n, err)
	}
	for _, task := range []string{"task_copy", "task_self"} {
		if tk := h.sched.task(task); tk["enabled"] != true || tk[ScheduleMigratedField] != "" {
			t.Errorf("%s after release: %v", task, tk)
		}
	}
	if tk := h.sched.task("task_rsync_backup"); tk["enabled"] != false {
		t.Error("a task the user had off came back on")
	}
	if tk := h.sched.task("task_vm"); tk["enabled"] != true {
		t.Error("release touched a VM task")
	}
	// Again: nothing left to release.
	if n, err := h.svc.ReleaseScheduledTasks(context.Background()); err != nil || n != 0 {
		t.Errorf("second release: %d %v", n, err)
	}
	// Backup installed again: it takes the same tasks back, onto the same
	// jobs, remembering the tasks' own state again.
	jobs1, _ := h.svc.store.ListJobs()
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs2, _ := h.svc.store.ListJobs()
	if len(jobs2) != len(jobs1) || h.sched.task("task_copy")["enabled"] != false {
		t.Errorf("retake: %d jobs, task %v", len(jobs2), h.sched.task("task_copy"))
	}
	if n, _ := h.svc.ReleaseScheduledTasks(context.Background()); n != takenOverTasks || h.sched.task("task_rsync_backup")["enabled"] != false {
		t.Errorf("second cycle release: %d", n)
	}
}

func TestReleaseWithCoreThatDropsTheMarker(t *testing.T) {
	// Today's core doesn't store migrated_to: Backup's own records say
	// which tasks it holds.
	h := newMigrationHarness(t, false)
	h.svc.Migrate(context.Background())
	if tk := h.sched.task("task_copy"); tk["enabled"] != false {
		t.Fatal("task not disabled")
	}
	// The user deletes a migrated job in Backup & Sync: it is not imported
	// again, and uninstalling still hands the task back.
	del := jobByTask(t, h, "task_move")
	h.svc.store.DeleteJob(del.ID, "")
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.store.JobByMigratedFrom("task_move"); !isNoRecord(err) {
		t.Error("a deleted migrated job came back")
	}
	n, err := h.svc.ReleaseScheduledTasks(context.Background())
	if err != nil || n != takenOverTasks {
		t.Fatalf("release: %d %v", n, err)
	}
	if h.sched.task("task_move")["enabled"] != true || h.sched.task("task_copy")["enabled"] != true {
		t.Error("tasks not re-enabled")
	}
	if n, _ := h.svc.ReleaseScheduledTasks(context.Background()); n != 0 {
		t.Errorf("second release released %d", n)
	}
	// Someone switched a task back on in Scheduled Tasks while Backup held
	// it: the next pass switches it off again (never both running).
	h2 := newMigrationHarness(t, false)
	h2.svc.Migrate(context.Background())
	h2.sched.mu.Lock()
	h2.sched.tasks[0]["enabled"] = true
	h2.sched.mu.Unlock()
	h2.svc.Migrate(context.Background())
	if h2.sched.task("task_copy")["enabled"] != false {
		t.Error("a re-enabled task runs next to its job")
	}
}

func TestReleaseWithoutStoreRecord(t *testing.T) {
	// A lost store: every marked task is enabled, so uninstalling never
	// silently stops an old backup.
	h := newHarness(t, false)
	h.sched.keepMarker = true
	h.sched.tasks = []map[string]interface{}{
		{"id": "t1", "type": "backup", "enabled": false, ScheduleMigratedField: ScheduleMigratedMarker, "source_path": "/a", "dest_path": "/b"},
		{"id": "t2", "type": "backup", "enabled": false, ScheduleMigratedField: "", "source_path": "/a", "dest_path": "/b"},
	}
	n, err := h.svc.ReleaseScheduledTasks(context.Background())
	if err != nil || n != 1 || h.sched.task("t1")["enabled"] != true || h.sched.task("t2")["enabled"] != false {
		t.Errorf("release: %d %v %v %v", n, err, h.sched.task("t1"), h.sched.task("t2"))
	}
	h.sched.listErr = errUnreachable
	if _, err := h.svc.ReleaseScheduledTasks(context.Background()); err == nil {
		t.Error("release with core down reported success")
	}
}

func TestMapExtraArgs(t *testing.T) {
	f, o, dropped := mapExtraArgs(`--exclude="a b/**" --exclude c --include=*.jpg --max-size 1.5G -P --delete-after --transfers 8 --checksum --create-empty-src-dirs --exclude`)
	if strings.Join(f.Exclude, "|") != "a b/**|c" || strings.Join(f.Include, "|") != "*.jpg" || f.MaxSizeBytes != int64(1.5*float64(1<<30)) || !o.CopyEmptyDirs {
		t.Errorf("filters %+v options %+v", f, o)
	}
	if strings.Join(dropped, "|") != "--delete-after|--transfers 8|--checksum|--exclude" {
		t.Errorf("dropped %q", dropped)
	}
	if _, _, d := mapExtraArgs(""); len(d) != 0 {
		t.Error(d)
	}
	for in, want := range map[string]int64{"100": 100, "10k": 10 << 10, "2M": 2 << 20, "1G": 1 << 30, "1T": 1 << 40, "5B": 5} {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "x", "-1M"} {
		if _, err := parseSize(bad); err == nil {
			t.Errorf("parseSize(%q) accepted", bad)
		}
	}
	if got := splitArgs(`a "b c" 'd "e"' f`); strings.Join(got, "|") != `a|b c|d "e"|f` {
		t.Errorf("splitArgs %q", got)
	}
}

// An import that can't be resolved (the USB drive unplugged at the time)
// doesn't switch the user's task off in core: it keeps running there. A
// later pass resolves it once the drive is back, enables the job and only
// then takes the task over.
func TestUnresolvedImportLeavesTheTaskRunningUntilItResolves(t *testing.T) {
	h := newMigrationHarness(t, true)
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tk := h.sched.task("task_absent_usb"); tk["enabled"] != true || tk[ScheduleMigratedField] != "" {
		t.Fatalf("an unresolved import took its task over: %v", tk)
	}
	if j := jobByTask(t, h, "task_absent_usb"); j.Enabled || j.NeedsAttention != AttentionMigratedUnresolved {
		t.Fatalf("unresolved job %+v", j)
	}
	// Still absent: nothing changes.
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tk := h.sched.task("task_absent_usb"); tk["enabled"] != true {
		t.Fatalf("second pass: %v", tk)
	}
	// The drive is back.
	h.eng.Lock()
	prev := h.eng.ResolvePathFn
	h.eng.ResolvePathFn = func(r engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
		if strings.HasPrefix(r.Path, "/media/usb-absent/") {
			e := Endpoint{Kind: EPUSB, RefID: "usb-uuid", SubPath: strings.TrimPrefix(r.Path, "/media/usb-absent/"), Label: "Stick"}
			return engine.ResolvePathResult{OK: true, Endpoint: &e}, nil
		}
		return prev(r)
	}
	h.eng.Unlock()
	rep, err := h.svc.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j := jobByTask(t, h, "task_absent_usb")
	if !j.Enabled || j.NeedsAttention != AttentionImported || j.Dest.Kind != EPUSB || j.Dest.SubPath != "Backups" {
		t.Fatalf("resolved job %+v", j)
	}
	if tk := h.sched.task("task_absent_usb"); tk["enabled"] != false || tk[ScheduleMigratedField] != ScheduleMigratedMarker {
		t.Fatalf("resolved import didn't take its task over: %v", tk)
	}
	if r := reportItems(rep)["task_absent_usb"].Result; r != MigratedImported {
		t.Errorf("report says %q", r)
	}
	// Uninstalling gives it back enabled, as the user had it.
	if _, err := h.svc.ReleaseScheduledTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tk := h.sched.task("task_absent_usb"); tk["enabled"] != true {
		t.Errorf("released %v", tk)
	}
}

// A task an older version took over although its import was unresolved
// is handed back until the job can run.
func TestUnresolvedImportMarkedByAnOlderVersionIsHandedBack(t *testing.T) {
	h := newMigrationHarness(t, true)
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// What the older version did: marked it and remembered it enabled.
	tk := h.sched.task("task_absent_usb")
	h.sched.mu.Lock()
	tk["enabled"], tk[ScheduleMigratedField] = false, ScheduleMigratedMarker
	h.sched.mu.Unlock()
	if err := h.svc.store.SetMetaJSON(metaMigrationTask+"task_absent_usb", migratedTask{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tk := h.sched.task("task_absent_usb"); tk["enabled"] != true || tk[ScheduleMigratedField] != "" {
		t.Fatalf("not handed back: %v", tk)
	}
}

// An import whose destination another job already uses comes in
// unresolved and leaves its task in core.
func TestImportOverlappingAnotherJobsDestIsUnresolved(t *testing.T) {
	h := newMigrationHarness(t, true)
	own := sampleJob("Mine")
	own.Dest = Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Backups", Label: "tank"}
	h.createJob(own)
	if _, err := h.svc.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := jobByTask(t, h, "task_copy")
	if j.Enabled || j.NeedsAttention != AttentionMigratedUnresolved {
		t.Fatalf("overlapping import %+v (dest %+v)", j, j.Dest)
	}
	if tk := h.sched.task("task_copy"); tk[ScheduleMigratedField] != "" {
		t.Errorf("overlapping import took its task over: %v", tk)
	}
}
