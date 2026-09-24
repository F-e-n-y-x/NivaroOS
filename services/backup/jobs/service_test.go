package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Notifications, logs, retention, housekeeping, the publisher and the
// loopback clients' pure parts.

func TestNotifyTextInSync(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "assets", "lang", "en_US.json"))
	if err != nil {
		t.Skip("en_US.json not found (not a full checkout):", err)
	}
	var enUS map[string]string
	if err := json.Unmarshal(raw, &enUS); err != nil {
		t.Fatal(err)
	}
	if want := RenderNotifyText(enUS); !bytes.Equal(want, notifyEnJSON) {
		t.Fatal("jobs/notify_en.json is out of date: run `GOWORK=off go generate ./jobs` in services/backup")
	}
	// Every notification the service sends has its English text.
	for key := range NotifyKeys {
		if _, ok := notifyText[key]; !ok {
			t.Errorf("no English text for %s", key)
		}
	}
	for _, code := range AllErrorCodes() {
		if _, ok := notifyText[errorReasonKey(code)]; !ok {
			t.Errorf("no English title for error %s", code)
		}
	}
}

func TestRenderEnglish(t *testing.T) {
	got := RenderEnglish(Message{Key: "backup.notify.failed", Args: map[string]interface{}{"job": "Photos", "reason_key": errorReasonKey(ErrNoSpace)}})
	if !strings.Contains(got, "Photos") || !strings.Contains(got, notifyText[errorReasonKey(ErrNoSpace)]) || strings.Contains(got, "{") {
		t.Errorf("failed notification: %q", got)
	}
	if got := RenderEnglish(Message{Key: "backup.no.such.key"}); got != "backup.no.such.key" {
		t.Errorf("unknown key: %q", got)
	}
	cases := map[string]string{
		renderArg("bytes", float64(1500)):        "1.5 kB",
		renderArg("size_bytes", int64(999)):      "999 B",
		renderArg("bytes", int64(3_200_000_000)): "3.2 GB",
		renderArg("count", float64(3)):           "3",
		renderArg("pct", 37.5):                   "37.5",
		renderArg("guard_key", "backup.guard.x"): "backup.guard.x",
		renderArg("name", "a {b}"):               "a {b}",
		renderArg("retry_at", "not a time"):      "not a time",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("renderArg = %q, want %q", got, want)
		}
	}
	at := time.Date(2026, 9, 25, 0, 10, 0, 0, time.Local)
	if got := renderArg("retry_at", at.Format(time.RFC3339)); got != "2026-09-25 00:10" {
		t.Errorf("time arg %q", got)
	}
}

func TestLogPagingAcrossCompression(t *testing.T) {
	dir := t.TempDir()
	p := logPath(dir, "bk_1", "run_1")
	lg, err := OpenRunLog(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		lg.Info("backup.log.phase", map[string]interface{}{"phase_key": "backup.phase.transfer"})
	}
	lg.Raw(engine.LogError, strings.Repeat("x", maxLogLine*2))
	lines, next, eof, err := ReadLogPage(p, 0, 3)
	if err != nil || len(lines) != 3 || eof {
		t.Fatalf("page 1: %d %v %v", len(lines), eof, err)
	}
	lg.Close()
	gz, err := compressLog(p)
	if err != nil || !strings.HasSuffix(gz, ".gz") {
		t.Fatal(gz, err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Error("plain log left next to the .gz")
	}
	// A reader following the plain path keeps its offset.
	lines, next2, eof, err := ReadLogPage(p, next, 100)
	if err != nil || len(lines) != 3 || !eof || next2 <= next {
		t.Fatalf("page 2 after compression: %d lines, eof %v, next %d->%d, %v", len(lines), eof, next, next2, err)
	}
	if last := lines[2]; last.Lvl != engine.LogError || len(last.Raw) > maxLogLine+16 {
		t.Errorf("long line not capped: %d bytes", len(last.Raw))
	}
	lines, _, eof, _ = ReadLogPage(gz, next2, 10)
	if len(lines) != 0 || !eof {
		t.Error("reading past the end")
	}
	// Torn / foreign lines are shown raw.
	os.WriteFile(p, []byte("not json\n"+`{"lvl":"info","code":"raw","raw":"ok"}`+"\n"+`{"partial`), 0o600)
	lines, _, eof, _ = ReadLogPage(p, 0, 10)
	if len(lines) != 2 || lines[0].Raw != "not json" || lines[1].Raw != "ok" || !eof {
		t.Errorf("mixed log: %+v", lines)
	}
	if safeName("../../etc") == "../../etc" || safeName("") != "_" {
		t.Error("safeName lets a path through")
	}
}

func TestRetention(t *testing.T) {
	h := newHarness(t, false)
	job := h.createJob(sampleJob("Docs"))
	now := time.Now()
	mk := func(age time.Duration, st RunStatus) RunRow {
		ended := now.Add(-age)
		r := RunRow{ID: NewRunID(ended), JobID: job.ID, Kind: string(KindBackup), Status: string(st), QueuedAt: ended, EndedAt: &ended}
		lg, _ := OpenRunLog(logPath(h.svc.store.DataDir(), job.ID, r.ID))
		lg.Info("backup.log.finished", nil)
		lg.Close()
		r.LogPath, _ = compressLog(lg.Path())
		if st == StatusWaitingUser || st == StatusSuccess {
			r.PreviewPath = filepath.Join(h.svc.store.DataDir(), PreviewsDir, r.ID+".jsonl")
			os.WriteFile(r.PreviewPath, []byte("{}\n"), 0o600)
		}
		h.svc.store.CreateRun(r)
		return r
	}
	oldSuccess := mk(400*24*time.Hour, StatusSuccess) // the job's only success: kept
	oldFailed := mk(300*24*time.Hour, StatusFailed)   // past 180 days: gone
	oldWaiting := mk(300*24*time.Hour, StatusWaitingUser)
	recentFailed := mk(2*24*time.Hour, StatusFailed)
	weekOldSuccess := mk(8*24*time.Hour, StatusSkipped)
	h.svc.applyRetention()

	if _, err := h.svc.store.GetRun(oldFailed.ID); !isNoRecord(err) {
		t.Error("a run past log_retention_days survived")
	}
	if fileSize(oldFailed.LogPath) != 0 {
		t.Error("its log file survived")
	}
	for _, r := range []RunRow{oldSuccess, oldWaiting, recentFailed, weekOldSuccess} {
		if _, err := h.svc.store.GetRun(r.ID); err != nil {
			t.Errorf("run %s (%s) removed", r.ID, r.Status)
		}
	}
	// Plan output of a final run goes after a week; a waiting one keeps it.
	if got, _ := h.svc.store.GetRun(oldSuccess.ID); got.PreviewPath != "" {
		t.Error("an old preview was kept")
	}
	if got, _ := h.svc.store.GetRun(oldWaiting.ID); got.PreviewPath == "" {
		t.Error("the preview of a waiting run was removed")
	}

	// At most maxRunsPerJob runs per job.
	for i := 0; i < maxRunsPerJob+5; i++ {
		mk(time.Hour, StatusFailed)
	}
	h.svc.applyRetention()
	rows, _ := h.svc.store.ListRuns(RunFilter{JobID: job.ID})
	// The cap, plus the last success and the waiting run, which are never
	// removed.
	if len(rows) != maxRunsPerJob+2 {
		t.Errorf("%d runs kept, want %d", len(rows), maxRunsPerJob+2)
	}
	if _, err := h.svc.store.GetRun(oldSuccess.ID); err != nil {
		t.Error("the last success went with the per-job cap")
	}
}

func TestStaleNotification(t *testing.T) {
	h := newHarness(t, true)
	j := sampleJob("Nightly")
	j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *"}}
	job := h.createJob(j)
	h.svc.checkStale()
	if containsStr(h.bus.notifications(), "backup.notify.stale") {
		t.Fatal("a new job is stale")
	}
	later := time.Now().Add(time.Duration(job.Notify.StaleAfterHours+1) * time.Hour)
	h.setNow(func() time.Time { return later })
	h.svc.checkStale()
	h.svc.checkStale() // once a day at most
	h.eventually("stale notice", func() bool { return containsStr(h.bus.notifications(), "backup.notify.stale") })
	settle()
	n := 0
	for _, k := range h.bus.notifications() {
		if k == "backup.notify.stale" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d stale notices, want 1", n)
	}
	// Health says so too.
	item, _ := h.svc.jobItem(job, nil)
	if item.Health != HealthWarning {
		t.Errorf("health %q", item.Health)
	}
}

func TestJobHealth(t *testing.T) {
	now := time.Now()
	on, off := true, false
	j := sampleJob("x")
	j.Enabled, j.CreatedAt = true, now
	failed := &RunRow{Status: string(StatusFailed)}
	partial := &RunRow{Status: string(StatusPartial)}
	waiting := &RunRow{Status: string(StatusWaitingUser)}
	skippedOffline := &RunRow{Status: string(StatusSkipped), ErrorCode: string(ErrDestOffline)}
	disabled := j
	disabled.Enabled = false
	changed := j
	changed.NeedsAttention = AttentionDestChanged
	unresolved := j
	unresolved.NeedsAttention = AttentionMigratedUnresolved
	cases := []struct {
		name         string
		j            Job
		last, active *RunRow
		online       *bool
		want         string
	}{
		{"ok", j, nil, nil, &on, HealthOK},
		{"disabled wins", disabled, failed, nil, &off, HealthDisabled},
		{"failed", j, failed, nil, &on, HealthProblem},
		{"waiting", j, nil, waiting, &on, HealthProblem},
		{"dest changed", changed, nil, nil, &on, HealthProblem},
		{"offline drive", j, nil, nil, &off, HealthOffline},
		{"skipped offline", j, skippedOffline, nil, nil, HealthOffline},
		{"partial", j, partial, nil, &on, HealthWarning},
		{"unresolved import", unresolved, nil, nil, &on, HealthWarning},
	}
	for _, c := range cases {
		if got := jobHealth(c.j, JobState{}, c.last, c.active, c.online, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRunSteps(t *testing.T) {
	j := sampleJob("x")
	j.Type = TypeMirror
	j.Retention.VersionsDays = 30
	j.Options.Verify = true
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}}}
	keys := func(steps []RunStep) string {
		var out []string
		for _, s := range steps {
			out = append(out, s.Key[len("backup.run.step."):]+"="+s.State)
		}
		return strings.Join(out, " ")
	}
	running := RunRow{Kind: string(KindBackup), Status: string(StatusRunning), Phase: string(PhaseTransfer),
		HooksDone: `[{"hook_idx":0,"pre_done":true,"prior_state":{"immich":"running"}}]`}
	if got := keys(runSteps(j, running)); got != "precheck=done stop_apps=done transfer=active verify=pending prune=pending start_apps=pending" {
		t.Errorf("running: %s", got)
	}
	failed := running
	failed.Status = string(StatusFailed)
	failed.HooksDone = `[{"hook_idx":0,"pre_done":true,"post_done":true,"prior_state":{}}]`
	failed.Phase = string(PhaseTransfer)
	if got := keys(runSteps(j, failed)); got != "precheck=done stop_apps=done transfer=failed verify=skipped prune=skipped start_apps=done" {
		t.Errorf("failed: %s", got)
	}
	queued := RunRow{Kind: string(KindBackup), Status: string(StatusQueued)}
	for _, s := range runSteps(j, queued) {
		if s.State != StepPending {
			t.Errorf("queued step %s %s", s.Key, s.State)
		}
	}
}

// flakyBus fails registration a few times, then forgets its types once
// (a message-bus restart).
type flakyBus struct {
	fakeBus
	mu         sync.Mutex
	failReg    int
	forgetOnce bool
	regs       int
}

func (b *flakyBus) RegisterEventTypes(ctx context.Context, types []EventType) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.regs++
	if b.failReg > 0 {
		b.failReg--
		return errUnreachable
	}
	return b.fakeBus.RegisterEventTypes(ctx, types)
}

func (b *flakyBus) Publish(ctx context.Context, name string, props map[string]string) error {
	b.mu.Lock()
	forget := b.forgetOnce
	b.forgetOnce = false
	b.mu.Unlock()
	if forget {
		return &httpStatusError{Status: 404, Body: "event type not found"}
	}
	return b.fakeBus.Publish(ctx, name, props)
}

func TestPublisher(t *testing.T) {
	bus := &flakyBus{failReg: 1, forgetOnce: true}
	p := newPublisher(bus)
	// Events wait in the queue until the types are registered.
	p.publish(EventJobChanged, map[string]string{PropJobID: "bk_1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.run(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for len(bus.named(EventJobChanged)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("event never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	bus.mu.Lock()
	regs := bus.regs
	bus.mu.Unlock()
	if regs != 3 {
		t.Errorf("%d registrations, want 3 (fail, ok, again after the 404)", regs)
	}
	if len(bus.types) != len(EventTypes()) {
		t.Errorf("registered %d types", len(bus.types))
	}
	for _, et := range bus.types {
		if et.SourceID != EventSourceID || !strings.HasPrefix(et.Name, "nivaroos:backup:") || len(et.PropertyTypeList) == 0 {
			t.Errorf("event type %+v", et)
		}
	}

	// A full queue drops progress first, never an end event.
	p2 := newPublisher(&fakeBus{})
	for i := 0; i < publishQueue; i++ {
		p2.publish(EventRunProgress, nil)
	}
	p2.publish(EventRunProgress, map[string]string{"x": "dropped"})
	p2.publish(EventRunEnd, map[string]string{PropRunID: "kept"})
	found := false
	for len(p2.ch) > 0 {
		if ev := <-p2.ch; ev.name == EventRunEnd {
			found = true
		}
	}
	if !found {
		t.Error("run-end dropped from a full queue")
	}
	var nilPub *publisher
	nilPub.publish(EventRunEnd, nil) // no bus: a no-op, not a crash
}

func TestCoreDBPath(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "casaos.conf")
	if got := CoreDBPath(conf); got != DefaultCoreDB {
		t.Errorf("missing file: %q", got)
	}
	os.WriteFile(conf, []byte("[app]\nLogPath = /var/log\nDBPath     = /srv/nivaroos/\n[server]\nPort = 80\n"), 0o644)
	if got := CoreDBPath(conf); got != "/srv/nivaroos/db/casaOS.db" {
		t.Errorf("configured: %q", got)
	}
	os.WriteFile(conf, []byte("DBPath = relative\n"), 0o644)
	if got := CoreDBPath(conf); got != DefaultCoreDB {
		t.Errorf("relative DBPath: %q", got)
	}
	if DefaultCoreDB != "/var/lib/nivaroos/db/casaOS.db" {
		t.Errorf("DefaultCoreDB %q", DefaultCoreDB)
	}
}

func TestCoreDBSMB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "casaOS.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type oConnection struct {
		ID          uint `gorm:"primaryKey"`
		Username    string
		Password    string
		Host        string
		Port        string
		Status      string
		Directories string
		MountPoint  string
	}
	if err := db.Table("o_connections").AutoMigrate(&oConnection{}); err != nil {
		t.Fatal(err)
	}
	db.Table("o_connections").Create(&oConnection{Username: "u", Password: "secret", Host: "nas", Port: "445", Directories: "share, IPC$,photos,", MountPoint: "/mnt/nas"})
	sqlDB, _ := db.DB()
	sqlDB.Close()

	src := &CoreDBSMB{Path: dbPath}
	conns, err := src.Connections(context.Background())
	if err != nil || len(conns) != 1 {
		t.Fatalf("connections: %+v %v", conns, err)
	}
	c := conns[0]
	if c.ID != "1" || c.Password != "secret" || strings.Join(c.Shares, ",") != "share,photos" || c.Label() != `\\nas` {
		t.Errorf("connection %+v", c)
	}
	// No database at all: none, not an error.
	none, err := (&CoreDBSMB{Path: filepath.Join(t.TempDir(), "missing.db")}).Connections(context.Background())
	if err != nil || len(none) != 0 {
		t.Errorf("missing db: %v %v", none, err)
	}

	// Credentials go to the engine keyed by the endpoint's ref_id.
	h := newHarness(t, false)
	h.svc.smb = src
	creds, err := h.svc.smbCredsFor(context.Background(), []Endpoint{{Kind: EPSMB, RefID: "1", SubPath: "photos/2024"}, {Kind: EPVolume, RefID: "x"}})
	if err != nil || creds["1"] != (engine.SMBCreds{Host: "nas", Share: "photos", User: "u", Password: "secret"}) {
		t.Errorf("creds %+v %v", creds, err)
	}
	if _, err := h.svc.smbCredsFor(context.Background(), []Endpoint{{Kind: EPSMB, RefID: "9", SubPath: "x"}}); engine.CodeOf(err) != ErrEndpointUnknown {
		t.Errorf("deleted connection: %v", err)
	}
}

func TestHumanizeCron(t *testing.T) {
	cases := map[string]string{
		"* * * * *":    "backup.cron.every_minute",
		"*/15 * * * *": "backup.cron.every_n_minutes",
		"5 * * * *":    "backup.cron.hourly_at",
		"0 */6 * * *":  "backup.cron.every_n_hours",
		"30 3 * * *":   "backup.cron.daily_at",
		"0 3 * * 1-5":  "backup.cron.weekdays_at",
		"0 3 * * SUN":  "backup.cron.weekly_at",
		"0 3 15 * *":   "backup.cron.monthly_at",
		"0 3 * 1 *":    "backup.cron.custom",
		"@every 6h":    "backup.cron.custom",
		"@daily":       "backup.cron.daily_at",
		"0 3 1,15 * *": "backup.cron.custom",
		"0 3 * * */2":  "backup.cron.custom",
	}
	for spec, want := range cases {
		key, args := HumanizeCron(spec)
		if key != want {
			t.Errorf("%q -> %s, want %s", spec, key, want)
		}
		wantArgs := CronHumanKeys[key]
		if len(args) != len(wantArgs) {
			t.Errorf("%q args %v, want %v", spec, args, wantArgs)
		}
	}
	if _, args := HumanizeCron("0 3 * * 1-5"); args["days"] != "1,2,3,4,5" || args["time"] != "03:00" {
		t.Errorf("weekdays args %v", args)
	}
	if _, args := HumanizeCron("0 3 * * 5-7"); args["days"] != "0,5,6" {
		t.Errorf("Fri-Sun args %v", args)
	}
}
