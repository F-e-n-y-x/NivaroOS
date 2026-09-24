package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoVolumes is the usual setup: a source drive with some files and an
// empty destination drive.
func twoVolumes(t *testing.T) (*testSys, *Engine, *fakeMount, *fakeMount) {
	s := newTestSys(t)
	src := s.addVolume("src", "11111111-aaaa-4bbb-8ccc-000000000001", "ext4")
	dst := s.addVolume("dst", "22222222-aaaa-4bbb-8ccc-000000000002", "ext4")
	return s, s.engine(), src, dst
}

func TestCopyNeverDeletes(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "photos"), map[string]string{"a.jpg": "A", "sub/b.jpg": "B"})
	req := baseReq(OpCopy, ep(src, "photos"), ep(dst, "backup"))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if !st.Result.MarkerWritten {
		t.Error("first run did not write the marker")
	}
	got := readTree(t, filepath.Join(dst.MountPoint, "backup"))
	if got["a.jpg"] != "A" || got["sub/b.jpg"] != "B" {
		t.Fatalf("destination = %v", got)
	}
	if _, ok := got[MarkerFile]; !ok {
		t.Fatal("no marker in the destination")
	}
	// Delete a source file: the copy must keep it.
	must(t, os.Remove(filepath.Join(src.MountPoint, "photos", "a.jpg")))
	req.FirstRun = false
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if got := readTree(t, filepath.Join(dst.MountPoint, "backup")); got["a.jpg"] != "A" {
		t.Fatalf("copy deleted a destination file: %v", got)
	}
	if st.Result.Counts.Deleted != 0 {
		t.Errorf("copy counted %d deletions", st.Result.Counts.Deleted)
	}
}

func TestMirrorRecyclesDeletionsAndKeepsItsFolders(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "docs")
	files := map[string]string{}
	for i := 0; i < 40; i++ {
		files[filepathJoin("f", i)] = "v1"
	}
	writeTree(t, root, files)
	req := baseReq(OpSync, ep(src, "docs"), ep(dst, "mirror"))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")

	// Remove one file and change another: 2 of 40 = 5 %, under both guards.
	must(t, os.Remove(filepath.Join(root, "f00")))
	must(t, os.WriteFile(filepath.Join(root, "f01"), []byte("v2 changed"), 0o644))
	req.FirstRun = false
	req.Baseline = &Baseline{SourceFiles: 40, DestFiles: 40}
	id, err := e.StartJob(context.Background(), withRun(req, "run_second"))
	must(t, err)
	st = waitJob(t, e, id)
	expectState(t, st, JobDone, "")
	destRoot := filepath.Join(dst.MountPoint, "mirror")
	got := readTree(t, destRoot)
	if _, ok := got["f00"]; ok {
		t.Error("mirror kept a file deleted from the source")
	}
	if got["f01"] != "v2 changed" {
		t.Errorf("f01 = %q", got["f01"])
	}
	versions, err := os.ReadDir(filepath.Join(destRoot, VersionsDir))
	must(t, err)
	if len(versions) != 1 {
		t.Fatalf("want one recycle folder, got %d", len(versions))
	}
	if _, err := time.Parse(VersionTimeLayout, versions[0].Name()); err != nil {
		t.Errorf("recycle folder name %q is not a timestamp", versions[0].Name())
	}
	recycled := readTree(t, filepath.Join(destRoot, VersionsDir, versions[0].Name()))
	if recycled["f00"] != "v1" || recycled["f01"] != "v1" {
		t.Fatalf("recycle folder = %v, want the deleted f00 and the old f01", recycled)
	}
	if st.Result.Counts.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", st.Result.Counts.Deleted)
	}
	if _, ok := got[MarkerFile]; !ok {
		t.Error("mirror deleted its marker")
	}
	// The log names the files.
	var copied, updated, recycledLines int
	for _, l := range jobLog(t, e, id) {
		switch l.Code {
		case "copied":
			copied++
		case "updated":
			updated++
		case "recycled":
			recycledLines++
		}
		if l.MsgKey != "" && !strings.HasPrefix(l.MsgKey, "backup.log.") {
			t.Errorf("log key %q", l.MsgKey)
		}
	}
	if updated != 1 || recycledLines != 1 {
		t.Errorf("log: %d copied, %d updated, %d recycled", copied, updated, recycledLines)
	}
	// A third run with nothing changed must not touch the recycle folder.
	req.Baseline = &Baseline{SourceFiles: 39, DestFiles: 39}
	st = runJob(t, e, withRun(req, "run_third"))
	expectState(t, st, JobDone, "")
	if _, err := os.Stat(filepath.Join(destRoot, VersionsDir, versions[0].Name(), "f00")); err != nil {
		t.Errorf("an unchanged run touched the recycle folder: %v", err)
	}
}

func withRun(r JobRequest, id string) JobRequest {
	r.RunID = id
	return r
}

func filepathJoin(prefix string, i int) string {
	return prefix + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestDestinationUnderSourceIsExcluded(t *testing.T) {
	s := newTestSys(t)
	vol := s.addVolume("data", "33333333-aaaa-4bbb-8ccc-000000000003", "ext4")
	e := s.engine()
	writeTree(t, vol.MountPoint, map[string]string{"docs/a.txt": "a", "photos/b.jpg": "b"})
	req := baseReq(OpSync, ep(vol, ""), ep(vol, "Backup"))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	got := readTree(t, filepath.Join(vol.MountPoint, "Backup"))
	if got["docs/a.txt"] != "a" || got["photos/b.jpg"] != "b" {
		t.Fatalf("backup = %v", got)
	}
	for p := range got {
		if strings.HasPrefix(p, "Backup/") {
			t.Fatalf("the backup contains itself: %s", p)
		}
	}
	var excluded bool
	for _, c := range st.Result.Checks {
		if c.ID == CheckNotInside && c.Status == CheckPass && c.Args["excluded"] == "Backup" {
			excluded = true
		}
	}
	if !excluded {
		t.Errorf("not_inside check didn't report the exclude: %+v", st.Result.Checks)
	}
	// A second run must not recycle the destination's own files.
	req.FirstRun = false
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Deleted != 0 {
		t.Errorf("second run deleted %d files", st.Result.Counts.Deleted)
	}
}

func TestSourceInsideDestinationFails(t *testing.T) {
	s := newTestSys(t)
	vol := s.addVolume("data", "33333333-aaaa-4bbb-8ccc-000000000004", "ext4")
	e := s.engine()
	writeTree(t, vol.MountPoint, map[string]string{"Backup/docs/a.txt": "a"})
	st := runJob(t, e, baseReq(OpSync, ep(vol, "Backup/docs"), ep(vol, "Backup")))
	expectState(t, st, JobError, CodeDestInsideSource)
	st = runJob(t, e, baseReq(OpSync, ep(vol, "Backup"), ep(vol, "Backup")))
	expectState(t, st, JobError, CodeDestInsideSource)
}

// guardSetup mirrors 20 files, then lets the test change the source.
func guardSetup(t *testing.T) (*Engine, *fakeMount, *fakeMount, JobRequest, string) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "data")
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		files[filepathJoin("f", i)] = "original"
	}
	writeTree(t, root, files)
	req := baseReq(OpSync, ep(src, "data"), ep(dst, "m"))
	expectState(t, runJob(t, e, req), JobDone, "")
	req.FirstRun = false
	// No baseline (it is only skipped for small previous runs): the
	// planning pass runs.
	req.Baseline = nil
	return e, src, dst, req, root
}

func destSnapshot(t *testing.T, dst *fakeMount) map[string]string {
	return readTree(t, filepath.Join(dst.MountPoint, "m"))
}

func TestDeleteGuardTripsBeforeAnyWrite(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	for i := 0; i < 5; i++ { // 25 % > 10 %
		must(t, os.Remove(filepath.Join(root, filepathJoin("f", i))))
	}
	must(t, os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0o644))
	before := destSnapshot(t, dst)
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeDeleteGuard)
	g := st.Result.Guard
	if g == nil || g.Guard != guardDelete || g.Count != 5 || g.Total != 20 || g.Pct != 25 || g.Limit != 10 || len(g.Sample) != 5 {
		t.Fatalf("guard = %+v", g)
	}
	after := destSnapshot(t, dst)
	if len(after) != len(before) || after["new.txt"] != "" {
		t.Fatalf("the destination changed before the guard: %v", after)
	}
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "m", VersionsDir)); err == nil {
		t.Error("a recycle folder was created")
	}
	// The user accepts: the same run proceeds.
	req.GuardOverride = []string{guardDelete}
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Deleted != 5 {
		t.Errorf("deleted %d, want 5", st.Result.Counts.Deleted)
	}
}

func TestChangeGuardTripsBeforeAnyWrite(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	for i := 0; i < 8; i++ { // 40 % rewritten > 30 %, nothing deleted
		must(t, os.WriteFile(filepath.Join(root, filepathJoin("f", i)), []byte("encrypted!"), 0o644))
	}
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeChangeGuard)
	if g := st.Result.Guard; g == nil || g.Guard != guardChange || g.Count != 8 || g.Pct != 40 {
		t.Fatalf("guard = %+v", st.Result.Guard)
	}
	for p, c := range destSnapshot(t, dst) {
		if c == "encrypted!" {
			t.Fatalf("%s was overwritten before the guard", p)
		}
	}
	// Copy jobs run the change guard too.
	creq := req
	creq.Op = OpCopy
	st = runJob(t, e, creq)
	expectState(t, st, JobError, CodeChangeGuard)
}

// A small job plans too: the guard trips before any write (the spec's
// "skip the plan below 1000 files" let MaxDelete delete up to its limit
// and copy new files first).
func TestSmallJobsPlanBeforeAnyWrite(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	req.Baseline = &Baseline{SourceFiles: 20, DestFiles: 20}
	for i := 0; i < 6; i++ {
		must(t, os.Remove(filepath.Join(root, filepathJoin("f", i))))
	}
	must(t, os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0o644))
	before := destSnapshot(t, dst)
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeDeleteGuard)
	if g := st.Result.Guard; g == nil || g.Count != 6 || len(g.Sample) != 6 {
		t.Fatalf("guard = %+v (want the plan's count and sample)", st.Result.Guard)
	}
	after := destSnapshot(t, dst)
	if len(after) != len(before) || after["new.txt"] != "" {
		t.Fatalf("the destination changed before the guard: %v", after)
	}
}

// When the planning pass runs out of time, MaxDelete is the barrier left:
// it stops the deletions at the limit.
func TestMaxDeleteIsTheSecondBarrier(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	saved := planTimeout
	planTimeout = time.Nanosecond
	t.Cleanup(func() { planTimeout = saved })
	req.Baseline = &Baseline{SourceFiles: 20, DestFiles: 20}
	for i := 0; i < 6; i++ {
		must(t, os.Remove(filepath.Join(root, filepathJoin("f", i))))
	}
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeDeleteGuard)
	left := destSnapshot(t, dst)
	var present int
	for i := 0; i < 6; i++ {
		if _, ok := left[filepathJoin("f", i)]; ok {
			present++
		}
	}
	if present < 4 {
		t.Fatalf("only %d of the 6 deleted files are still in the destination; the limit is 2 deletions", present)
	}
}

func TestPlanChangesNothingAndWritesPreview(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	must(t, os.Remove(filepath.Join(root, "f00")))
	must(t, os.WriteFile(filepath.Join(root, "f01"), []byte("changed"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "added.txt"), []byte("new"), 0o644))
	before := destSnapshot(t, dst)
	preview := filepath.Join(t.TempDir(), "preview.ndjson")
	req.Op, req.PlanOp, req.PreviewFile = OpPlan, OpSync, preview
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	c := st.Result.Counts
	if c.Added != 1 || c.Changed != 1 || c.Deleted != 1 || c.DestFiles != 20 {
		t.Fatalf("counts = %+v", c)
	}
	after := destSnapshot(t, dst)
	if len(after) != len(before) || after["f01"] != "original" {
		t.Fatalf("a plan changed the destination: %v", after)
	}
	f, err := os.Open(preview)
	must(t, err)
	defer f.Close()
	ops := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var it PreviewItem
		must(t, json.Unmarshal(sc.Bytes(), &it))
		ops[it.Path] = it.Op
	}
	if ops["added.txt"] != "add" || ops["f01"] != "update" || ops["f00"] != "delete" || len(ops) != 3 {
		t.Fatalf("preview = %v", ops)
	}
}

func TestStopCancels(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "big")
	files := map[string]string{}
	for i := 0; i < 60; i++ {
		files[filepathJoin("f", i)] = strings.Repeat("x", 1024)
	}
	writeTree(t, root, files)
	started := make(chan struct{})
	release := make(chan struct{})
	var once bool
	testHookBeforeWrite = func() {
		if !once {
			once = true
			close(started)
			<-release
		}
	}
	defer func() { testHookBeforeWrite = nil }()
	req := baseReq(OpCopy, ep(src, "big"), ep(dst, "b"))
	req.RunID = "run_stop"
	id, err := e.StartJob(context.Background(), req)
	must(t, err)
	<-started
	must(t, e.StopJob(context.Background(), id))
	close(release)
	st := waitJob(t, e, id)
	expectState(t, st, JobError, CodeCancelledByUser)
	if n := len(readTree(t, filepath.Join(dst.MountPoint, "b"))); n >= 60 {
		t.Errorf("%d files copied after the stop", n)
	}
	// Stopping a finished job is a no-op.
	must(t, e.StopJob(context.Background(), id))
}

func TestMetadataAndSymlinksSurviveLocally(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "app")
	writeTree(t, root, map[string]string{"conf/app.ini": "x=1", "data/db": "rows"})
	must(t, os.Chmod(filepath.Join(root, "conf", "app.ini"), 0o640))
	must(t, os.Symlink("../data/db", filepath.Join(root, "conf", "db-link")))
	old := time.Date(2020, 5, 6, 7, 8, 9, 0, time.UTC)
	must(t, os.Chtimes(filepath.Join(root, "data", "db"), old, old))
	st := runJob(t, e, baseReq(OpSync, ep(src, "app"), ep(dst, "app")))
	expectState(t, st, JobDone, "")
	out := filepath.Join(dst.MountPoint, "app")
	fi, err := os.Stat(filepath.Join(out, "conf", "app.ini"))
	must(t, err)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	link, err := os.Readlink(filepath.Join(out, "conf", "db-link"))
	if err != nil || link != "../data/db" {
		t.Errorf("symlink = %q, %v", link, err)
	}
	fi, err = os.Stat(filepath.Join(out, "data", "db"))
	must(t, err)
	if !fi.ModTime().Equal(old) {
		t.Errorf("mtime = %v, want %v", fi.ModTime(), old)
	}
	for _, c := range st.Result.Checks {
		if c.ID == CheckMetadata && c.Status != CheckPass {
			t.Errorf("metadata check = %s", c.Status)
		}
	}
}

func TestMarkerIdentityBeforeAnyWrite(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})
	req := baseReq(OpCopy, ep(src, "s"), ep(dst, "d"))
	expectState(t, runJob(t, e, req), JobDone, "")
	must(t, os.WriteFile(filepath.Join(src.MountPoint, "s", "b"), []byte("2"), 0o644))

	// Another job's folder id.
	other := req
	other.FirstRun = false
	other.DestFolderID = "00000000-0000-4000-8000-000000000000"
	expectState(t, runJob(t, e, other), JobError, CodeDestMarkerMismatch)
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "d", "b")); err == nil {
		t.Fatal("wrote into a folder with another job's marker")
	}
	// No marker at all on a later run (a different, empty drive mounted).
	must(t, os.Remove(filepath.Join(dst.MountPoint, "d", MarkerFile)))
	later := req
	later.FirstRun = false
	expectState(t, runJob(t, e, later), JobError, CodeDestMarkerMismatch)
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "d", "b")); err == nil {
		t.Fatal("wrote into a folder without a marker")
	}
}

func TestEmptySourceSentinel(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	must(t, os.MkdirAll(filepath.Join(src.MountPoint, "empty"), 0o755))
	req := baseReq(OpSync, ep(src, "empty"), ep(dst, "d"))
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeEmptySource)
	if st.Result.Guard == nil || st.Result.Guard.Guard != guardEmptySource {
		t.Fatalf("guard = %+v", st.Result.Guard)
	}
	req.Guards.AllowEmptySrc = true
	expectState(t, runJob(t, e, req), JobDone, "")

	// A source that lost most of its files since the last success.
	writeTree(t, filepath.Join(src.MountPoint, "shrunk"), map[string]string{"one": "1"})
	req = baseReq(OpCopy, ep(src, "shrunk"), ep(dst, "d2"))
	req.Baseline = &Baseline{SourceFiles: 10, DestFiles: 10}
	st = runJob(t, e, req)
	expectState(t, st, JobError, CodeEmptySource)
	if g := st.Result.Guard; g == nil || g.Pct != 90 || g.Count != 9 {
		t.Fatalf("guard = %+v", g)
	}
	req.GuardOverride = []string{guardEmptySource}
	expectState(t, runJob(t, e, req), JobDone, "")
}

func TestOutsideAllowedRootsRefused(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "44444444-aaaa-4bbb-8ccc-000000000005", "ext4")
	// A volume mounted outside the allowed roots.
	outside := s.addVolume("x", "55555555-aaaa-4bbb-8ccc-000000000006", "ext4", func(m *fakeMount) {
		m.MountPoint = filepath.Join(s.root, "elsewhere")
	})
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "a"), map[string]string{"f": "1"})
	st := runJob(t, e, baseReq(OpCopy, ep(src, "a"), ep(outside, "")))
	expectState(t, st, JobError, CodePathNotAllowed)
	// A symlink out of the drive.
	must(t, os.Symlink(s.root, filepath.Join(src.MountPoint, "escape")))
	st = runJob(t, e, baseReq(OpCopy, ep(src, "escape"), ep(src, "b")))
	expectState(t, st, JobError, CodePathNotAllowed)
	// ".." in a sub-path.
	_, err := e.StartJob(context.Background(), baseReq(OpCopy, ep(src, "../x"), ep(src, "b")))
	if err == nil {
		st = runJob(t, e, baseReq(OpCopy, ep(src, "../x"), ep(src, "b")))
		expectState(t, st, JobError, CodePathNotAllowed)
	}
}

func TestVerifyAfterCopy(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "v"), map[string]string{"a": "1", "b/c": "2"})
	req := baseReq(OpCopy, ep(src, "v"), ep(dst, "v"))
	req.Options.Verify = true
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if len(st.Result.FileErrors) != 0 {
		t.Fatalf("verify found %v", st.Result.FileErrors)
	}
	// Corrupt the copy; a standalone check finds it.
	must(t, os.WriteFile(filepath.Join(dst.MountPoint, "v", "a"), []byte("X"), 0o644))
	creq := req
	creq.Op, creq.FirstRun = OpCheck, false
	st = runJob(t, e, creq)
	expectState(t, st, JobDone, "")
	if len(st.Result.FileErrors) != 1 || st.Result.FileErrors[0].Path != "a" {
		t.Fatalf("check found %v", st.Result.FileErrors)
	}
}

// TestMirrorToCloudRemote mirrors to a remote from the shared rclone
// config: deletions go to the recycle folder on the remote too, and the
// marker is checked before them.
func TestMirrorToCloudRemote(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "12121212-aaaa-4bbb-8ccc-000000000040", "ext4")
	cloudRoot := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloudRoot, 0o755))
	s.writeRcloneConf("[box]\ntype = alias\nremote = " + cloudRoot + "\n")
	e := s.engine()
	root := filepath.Join(src.MountPoint, "d")
	writeTree(t, root, map[string]string{"a": "1", "b": "2", "c": "3"})
	dest := Endpoint{Kind: EPCloud, RefID: "box", SubPath: "Backups/d"}
	req := baseReq(OpSync, ep(src, "d"), dest)
	req.Guards.DeletePct, req.Guards.ChangePct = 50, 50
	st := runJob(t, e, withRun(req, "run_c1"))
	expectState(t, st, JobDone, "")
	if st.Result.MountID != 0 || !st.Result.MarkerWritten {
		t.Errorf("result = %+v", st.Result)
	}
	for _, c := range st.Result.Checks {
		if c.ID == CheckMetadata && c.Status != CheckSkip {
			t.Errorf("metadata on a cloud destination: %s", c.Status)
		}
	}
	must(t, os.Remove(filepath.Join(root, "a")))
	req.FirstRun = false
	expectState(t, runJob(t, e, withRun(req, "run_c2")), JobDone, "")
	out := filepath.Join(cloudRoot, "Backups", "d")
	got := readTree(t, out)
	if _, ok := got["a"]; ok {
		t.Fatal("deleted file still in the mirror")
	}
	var recycled bool
	for p, c := range got {
		if strings.HasPrefix(p, VersionsDir+"/") && strings.HasSuffix(p, "/a") && c == "1" {
			recycled = true
		}
	}
	if !recycled {
		t.Fatalf("no recycled copy of a: %v", keys(got))
	}
	// The marker is replaced by someone else's: the next deletion is refused.
	must(t, os.WriteFile(filepath.Join(out, MarkerFile), []byte(`{"v":1,"job_id":"bk_x","dest_folder_id":"other"}`), 0o600))
	must(t, os.Remove(filepath.Join(root, "b")))
	expectState(t, runJob(t, e, withRun(req, "run_c3")), JobError, CodeDestMarkerMismatch)
	if _, err := os.Stat(filepath.Join(out, "b")); err != nil {
		t.Fatal("deleted from a folder that isn't the job's")
	}
}
