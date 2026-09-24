package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine/enginetest"
)

// The job store (spec §4): idempotent open, folder modes, revisions,
// run ids, meta, remembered drives, atomic files.

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenStoreIsIdempotentAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backup")
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := st.CreateJob(sampleJob("keep me"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	// An older install (or the installer with another umask) left a
	// folder too open: the next open tightens it.
	if err := os.Chmod(filepath.Join(dir, LogsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, d := range []string{"", LogsDir, PreviewsDir, StagingDir, SecretsDir} {
		fi, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s mode %v, want 0700", d, fi.Mode().Perm())
		}
	}
	fi, err := os.Stat(filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("database mode %v, want 0600", fi.Mode().Perm())
	}
	got, err := st.GetJob(j.ID)
	if err != nil || got.Name != "keep me" {
		t.Fatalf("job lost across reopen: %+v %v", got, err)
	}
	v, ok, err := st.GetMeta(MetaSchemaVersion)
	if err != nil || !ok || v != "1" {
		t.Errorf("schema_version = %q %v %v", v, ok, err)
	}
}

func TestOpenStoreRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(MetaSchemaVersion, "99"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := OpenStore(dir); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("opening a newer schema: %v, want a refusal", err)
	}
	if _, err := OpenStore(""); err == nil {
		t.Error("empty data dir accepted")
	}
}

func TestServiceComesUpWithoutStore(t *testing.T) {
	// A data dir that can't be created: the service still answers /health
	// and reports store_unavailable everywhere else.
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{DataDir: filepath.Join(file, "sub"), Engine: enginetest.New(), Bus: &fakeBus{}})
	if err == nil || svc == nil {
		t.Fatalf("New = %v, %v; want a service and an error", svc, err)
	}
	if err := svc.Start(t.Context()); err == nil {
		t.Error("Start without a store succeeded")
	}
	rec := doRequest(svc, "GET", "/v1/backup/health", nil, reqOpts{})
	if rec.Code != 200 {
		t.Errorf("/health = %d", rec.Code)
	}
	rec = doRequest(svc, "GET", "/v1/backup/jobs", nil, reqOpts{})
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), string(ErrStoreUnavailable)) {
		t.Errorf("/jobs without store = %d %s", rec.Code, rec.Body.String())
	}
}

func TestJobRevisions(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	j, err := st.CreateJob(sampleJob("A"), now)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^bk_[0-9a-f]{12}$`).MatchString(j.ID) || j.Revision != 1 || j.DestFolderID == "" {
		t.Fatalf("created job: id %q rev %d folder %q", j.ID, j.Revision, j.DestFolderID)
	}
	edit := j
	edit.Name = "B"
	edit.DestFolderID = "forged" // server-owned: kept
	u, err := st.UpdateJob(j.ID, 1, edit, now.Add(time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if u.Revision != 2 || u.Name != "B" || u.DestFolderID != j.DestFolderID || !u.CreatedAt.Equal(now) {
		t.Fatalf("updated job: %+v", u)
	}
	// A stale editor gets the current job back.
	_, err = st.UpdateJob(j.ID, 1, edit, now, nil)
	var rc *RevisionConflict
	if !errors.As(err, &rc) || rc.Current.Revision != 2 || rc.Current.Name != "B" {
		t.Fatalf("stale update: %v", err)
	}
	// Server bookkeeping doesn't bump the revision.
	m, err := st.MutateJob(j.ID, now, false, func(j *Job) error { j.NeedsAttention = AttentionDestChanged; return nil })
	if err != nil || m.Revision != 2 || m.NeedsAttention != AttentionDestChanged {
		t.Fatalf("bookkeeping mutate: %+v %v", m, err)
	}
	// ... and an edit keeps it (only the API clears attention flags).
	u, err = st.UpdateJob(j.ID, 2, edit, now, nil)
	if err != nil || u.NeedsAttention != AttentionDestChanged || u.Revision != 3 {
		t.Fatalf("edit after mutate: %+v %v", u, err)
	}
	if _, err := st.GetJob("bk_000000000000"); !isNoRecord(err) {
		t.Errorf("missing job: %v", err)
	}
	if _, err := st.MutateJob("bk_000000000000", now, true, func(*Job) error { return nil }); !isNoRecord(err) {
		t.Errorf("mutate missing job: %v", err)
	}
}

func TestDeleteJobKeepsOneRun(t *testing.T) {
	st := openTestStore(t)
	j, _ := st.CreateJob(sampleJob("A"), time.Now())
	var ids []string
	for i := 0; i < 3; i++ {
		id := NewRunID(time.Now())
		ids = append(ids, id)
		if err := st.CreateRun(RunRow{ID: id, JobID: j.ID, Kind: string(KindBackup), Status: string(StatusSuccess), QueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpdateJobState(j.ID, func(s *JobState) { s.SizeBytes = 5 }); err != nil {
		t.Fatal(err)
	}
	removed, err := st.DeleteJob(j.ID, ids[2])
	if err != nil || len(removed) != 2 {
		t.Fatalf("DeleteJob = %d runs, %v", len(removed), err)
	}
	if _, err := st.GetRun(ids[2]); err != nil {
		t.Errorf("kept run gone: %v", err)
	}
	if _, err := st.GetRun(ids[0]); !isNoRecord(err) {
		t.Errorf("removed run still there: %v", err)
	}
	if _, ok, _ := st.GetMeta(jobStateKey(j.ID)); ok {
		t.Error("job state left behind")
	}
	if _, err := st.DeleteJob(j.ID, ""); !isNoRecord(err) {
		t.Errorf("second delete: %v", err)
	}
}

func TestMigratedFromIsUnique(t *testing.T) {
	st := openTestStore(t)
	task := "task_abc"
	j := sampleJob("A")
	j.MigratedFrom = &task
	if _, err := st.CreateJob(j, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateJob(j, time.Now()); err == nil {
		t.Fatal("a second job from the same task was stored")
	}
	got, err := st.JobByMigratedFrom(task)
	if err != nil || got.Name != "A" {
		t.Fatalf("JobByMigratedFrom: %+v %v", got, err)
	}
	// Jobs not from a migration have NULL there, which UNIQUE allows.
	for i := 0; i < 2; i++ {
		if _, err := st.CreateJob(sampleJob("plain"), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunIDsSortInCreationOrder(t *testing.T) {
	re := regexp.MustCompile(`^run_[0-9A-HJKMNP-TV-Z]{26}$`)
	now := time.Now()
	var ids []string
	for i := 0; i < 500; i++ {
		// Same millisecond, and a clock that steps back, must still sort.
		at := now
		if i%100 == 99 {
			at = now.Add(-time.Second)
		}
		ids = append(ids, NewRunID(at))
	}
	for i, id := range ids {
		if !re.MatchString(id) {
			t.Fatalf("run id %q", id)
		}
		if i > 0 && ids[i-1] >= id {
			t.Fatalf("run ids out of order at %d: %s >= %s", i, ids[i-1], id)
		}
	}
	later := NewRunID(now.Add(time.Hour))
	if later <= ids[len(ids)-1] {
		t.Errorf("a later millisecond sorts first: %s", later)
	}
}

func TestListRunsFilterAndPaging(t *testing.T) {
	st := openTestStore(t)
	statuses := []RunStatus{StatusSuccess, StatusFailed, StatusQueued, StatusSuccess, StatusSkipped}
	var ids []string
	for i, s := range statuses {
		id := NewRunID(time.Now())
		ids = append(ids, id)
		job := "bk_a"
		if i == 4 {
			job = "bk_b"
		}
		if err := st.CreateRun(RunRow{ID: id, JobID: job, Kind: string(KindBackup), Status: string(s)}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := st.ListRuns(RunFilter{})
	if len(all) != 5 || all[0].ID != ids[4] {
		t.Fatalf("newest first: %v", all)
	}
	page, _ := st.ListRuns(RunFilter{JobID: "bk_a", Limit: 2})
	if len(page) != 2 || page[0].ID != ids[3] || page[1].ID != ids[2] {
		t.Fatalf("page 1: %+v", page)
	}
	page, _ = st.ListRuns(RunFilter{JobID: "bk_a", Before: page[1].ID})
	if len(page) != 2 || page[0].ID != ids[1] {
		t.Fatalf("page 2: %+v", page)
	}
	ok, _ := st.ListRuns(RunFilter{Statuses: []RunStatus{StatusSuccess}})
	if len(ok) != 2 {
		t.Errorf("status filter: %d", len(ok))
	}
	last, err := st.LastRun("bk_a", []RunKind{KindBackup}, []RunStatus{StatusFailed})
	if err != nil || last == nil || last.ID != ids[1] {
		t.Errorf("LastRun: %+v %v", last, err)
	}
	none, err := st.LastRun("bk_c", nil, nil)
	if err != nil || none != nil {
		t.Errorf("LastRun of nothing: %+v %v", none, err)
	}
}

func TestSaveRunKeepsCoalesced(t *testing.T) {
	st := openTestStore(t)
	r := RunRow{ID: NewRunID(time.Now()), JobID: "bk_a", Status: string(StatusRunning)}
	if err := st.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	if err := st.IncCoalesced(r.ID); err != nil {
		t.Fatal(err)
	}
	// A worker saving its older copy must not undo the coalesced trigger.
	r.Phase = string(PhaseTransfer)
	if err := st.SaveRun(r); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetRun(r.ID)
	if got.Coalesced != 1 || got.Phase != string(PhaseTransfer) {
		t.Errorf("run after save: coalesced %d phase %q", got.Coalesced, got.Phase)
	}
	if err := st.IncCoalesced("run_missing"); !isNoRecord(err) {
		t.Errorf("IncCoalesced of a missing run: %v", err)
	}
}

func TestMetaAndSettings(t *testing.T) {
	st := openTestStore(t)
	s, err := st.Settings()
	if err != nil || s != DefaultAppSettings() {
		t.Fatalf("fresh settings: %+v %v", s, err)
	}
	s.MaxConcurrent = 3
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Settings(); got.MaxConcurrent != 3 {
		t.Errorf("settings not saved: %+v", got)
	}
	// LIKE wildcards in a prefix are literal.
	for _, k := range []string{"usb.A_B", "usb.AxB", "usbX"} {
		if err := st.SetMeta(k, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.MetaWithPrefix("usb.A_")
	if len(rows) != 1 || rows[0].Key != "usb.A_B" {
		t.Errorf("prefix with _: %+v", rows)
	}
	if err := st.DeleteMeta("never-set"); err != nil {
		t.Error(err)
	}
}

func TestRememberedDrives(t *testing.T) {
	st := openTestStore(t)
	ep := Endpoint{Kind: EPUSB, RefID: "3A4F-1C22", Match: &DevMatch{Serial: "S1"}, Label: "Sandisk"}
	if err := st.RememberDrive(ep, nil); err != nil {
		t.Fatal(err)
	}
	// A user rename survives the next sighting.
	var d RememberedDrive
	st.GetMetaJSON(MetaRememberedDrive+ep.RefID, &d)
	d.Label = "Offsite stick"
	st.SetMetaJSON(MetaRememberedDrive+ep.RefID, d)
	seen := time.Now().UTC().Truncate(time.Second)
	if err := st.RememberDrive(ep, &seen); err != nil {
		t.Fatal(err)
	}
	older := seen.Add(-time.Hour)
	st.RememberDrive(ep, &older)
	drives, err := st.RememberedDrives()
	if err != nil {
		t.Fatal(err)
	}
	got := drives[ep.RefID]
	if got.Label != "Offsite stick" || !got.LastSeen.Equal(seen) || got.Endpoint.Match == nil {
		t.Errorf("remembered drive: %+v", got)
	}
	// Non-USB endpoints are not remembered.
	st.RememberDrive(Endpoint{Kind: EPVolume, RefID: "x"}, nil)
	if drives, _ := st.RememberedDrives(); len(drives) != 1 {
		t.Errorf("drives: %v", drives)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := writeFileAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(p, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "two" || fi.Mode().Perm() != 0o640 {
		t.Errorf("content %q mode %v", b, fi.Mode().Perm())
	}
	// No temp files left behind, also after a failure.
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), []byte("x"), 0o600); err == nil {
		t.Error("write into a missing folder succeeded")
	}
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "f.json" {
		t.Errorf("folder holds %v", names)
	}
}

func TestNewUUID4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		u := NewUUID4()
		if !re.MatchString(u) || seen[u] {
			t.Fatalf("uuid %q", u)
		}
		seen[u] = true
	}
}
