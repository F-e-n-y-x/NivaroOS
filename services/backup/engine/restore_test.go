package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mirrorWithHistory mirrors docs twice with a deletion and a change in
// between, so the destination has a current copy and one recycle folder.
func mirrorWithHistory(t *testing.T) (*Engine, *fakeMount, *fakeMount, JobRequest, string) {
	t.Helper()
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "docs")
	writeTree(t, root, map[string]string{"a.txt": "a1", "b.txt": "b1", "sub/c.txt": "c1"})
	req := baseReq(OpSync, ep(src, "docs"), ep(dst, "m"))
	req.Guards = Guards{EmptySourcePct: 50, DeletePct: 100, ChangePct: 100}
	expectState(t, runJob(t, e, withRun(req, "run_h1")), JobDone, "")
	must(t, os.Remove(filepath.Join(root, "b.txt")))
	must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a2"), 0o644))
	req.FirstRun = false
	expectState(t, runJob(t, e, withRun(req, "run_h2")), JobDone, "")
	return e, src, dst, req, root
}

func TestVersionsBrowseAndRestoreFromRecycle(t *testing.T) {
	e, src, dst, req, root := mirrorWithHistory(t)
	vers, err := e.ListVersions(context.Background(), VersionsRequest{JobID: req.JobID, JobType: jobTypeMirror, Dest: req.Dest})
	must(t, err)
	if len(vers) != 2 || vers[0].ID != "current" || vers[1].Kind != VersionRecycle || vers[1].Files != 2 || vers[1].Time == nil {
		t.Fatalf("versions = %+v", vers)
	}
	recycle := vers[1].ID

	// Browsing the current copy hides the engine's own files.
	cur, err := e.Browse(context.Background(), BrowseRequest{Endpoint: req.Dest})
	must(t, err)
	var names []string
	for _, en := range cur.Entries {
		names = append(names, en.Name)
	}
	if strings.Join(names, "|") != "sub|a.txt" {
		t.Fatalf("current = %v", names)
	}
	old, err := e.Browse(context.Background(), BrowseRequest{Endpoint: req.Dest, VersionID: recycle})
	must(t, err)
	if len(old.Entries) != 2 {
		t.Fatalf("recycle folder = %+v", old.Entries)
	}
	if _, err := e.Browse(context.Background(), BrowseRequest{Endpoint: req.Dest, VersionID: "v_yesterday"}); CodeOf(err) != CodeNotFound {
		t.Fatalf("bad version id: %v", err)
	}
	if _, err := e.Browse(context.Background(), BrowseRequest{Endpoint: req.Dest, Path: "../.."}); CodeOf(err) != CodePathNotAllowed {
		t.Fatalf("browse escape: %v", err)
	}
	dirs, err := e.Browse(context.Background(), BrowseRequest{Endpoint: req.Dest, DirsOnly: true})
	must(t, err)
	if len(dirs.Entries) != 1 || dirs.Entries[0].Name != "sub" {
		t.Fatalf("dirs only = %+v", dirs.Entries)
	}

	// Restore the deleted b.txt and the old a.txt to the original place,
	// overwriting.
	spec := &RestoreSpec{VersionID: recycle, Paths: []string{"a.txt", "b.txt"}, Target: ep(src, "docs"), Conflict: ConflictOverwrite}
	rreq := JobRequest{Op: OpRestore, RunID: "run_r1", JobID: req.JobID, Dest: req.Dest, DestFolderID: testFolderID, Restore: spec}
	st := runJob(t, e, rreq)
	expectState(t, st, JobDone, "")
	got := readTree(t, root)
	if got["a.txt"] != "a1" || got["b.txt"] != "b1" || got["sub/c.txt"] != "c1" {
		t.Fatalf("after restore %v", got)
	}
	if st.Result.Counts.Added != 1 || st.Result.Counts.Changed != 1 {
		t.Errorf("counts = %+v", st.Result.Counts)
	}

	// skip leaves an existing different file alone.
	must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("mine"), 0o644))
	spec.Conflict = ConflictSkip
	rreq.RunID = "run_r2"
	st = runJob(t, e, rreq)
	expectState(t, st, JobDone, "")
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "mine" {
		t.Fatalf("skip overwrote: %q", b)
	}
	// A dry run counts but writes nothing.
	spec.Conflict, spec.DryRun = ConflictOverwrite, true
	rreq.RunID = "run_r3"
	st = runJob(t, e, rreq)
	expectState(t, st, JobDone, "")
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "mine" || st.Result.Counts.Changed != 1 {
		t.Fatalf("dry run: %q, counts %+v", b, st.Result.Counts)
	}
	// Restoring into the backup itself is refused.
	spec.DryRun, spec.Target = false, ep(dst, "m/sub")
	rreq.RunID = "run_r4"
	expectState(t, runJob(t, e, rreq), JobError, CodeDestInsideSource)
	// A destination that isn't the job's (marker mismatch) is refused.
	rreq.DestFolderID, rreq.RunID, spec.Target = "00000000-0000-4000-8000-00000000abcd", "run_r5", ep(src, "docs")
	expectState(t, runJob(t, e, rreq), JobError, CodeDestMarkerMismatch)
}

func TestRestoreCurrentToNewFolderAndDownload(t *testing.T) {
	e, src, _, req, _ := mirrorWithHistory(t)
	spec := &RestoreSpec{VersionID: "current", Target: ep(src, "restored"), Conflict: ConflictKeepBoth}
	st := runJob(t, e, JobRequest{Op: OpRestore, RunID: "run_rc", JobID: req.JobID, Dest: req.Dest, DestFolderID: testFolderID, Restore: spec})
	expectState(t, st, JobDone, "")
	got := readTree(t, filepath.Join(src.MountPoint, "restored"))
	if len(got) != 2 || got["a.txt"] != "a2" || got["sub/c.txt"] != "c1" {
		t.Fatalf("restored %v (the recycle folder and marker must not come along)", got)
	}
	d, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: req.Dest, VersionID: "current", Paths: []string{"sub/c.txt"}})
	must(t, err)
	raw, err := io.ReadAll(d.Body)
	must(t, err)
	must(t, d.Body.Close())
	if string(raw) != "c1" || d.Size != 2 {
		t.Fatalf("download %q %+v", raw, d)
	}
	if _, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: req.Dest, VersionID: "current", Paths: []string{MarkerFile}}); err != nil {
		t.Fatal(err)
	}
}

func TestKeepBothName(t *testing.T) {
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	taken := map[string]bool{"a/report (restored 2026-09-24).pdf": true}
	exists := func(p string) bool { return taken[p] }
	if got := keepBothName("a/report.pdf", day, exists); got != "a/report (restored 2026-09-24 2).pdf" {
		t.Errorf("got %q", got)
	}
	if got := keepBothName(".bashrc", day, exists); got != ".bashrc (restored 2026-09-24)" {
		t.Errorf("got %q", got)
	}
}

func TestPurgeVersionsByFolderName(t *testing.T) {
	e, _, dst, req, _ := mirrorWithHistory(t)
	vdir := filepath.Join(dst.MountPoint, "m", VersionsDir)
	now := time.Now().UTC()
	oldName := now.Add(-40 * 24 * time.Hour).Format(VersionTimeLayout)
	newName := now.Add(-2 * 24 * time.Hour).Format(VersionTimeLayout)
	for _, n := range []string{oldName, newName, "not-a-timestamp"} {
		writeTree(t, filepath.Join(vdir, n), map[string]string{"f": "x"})
	}
	// Folder mtimes say the opposite of the names; only names count.
	fresh := time.Now()
	must(t, os.Chtimes(filepath.Join(vdir, oldName), fresh, fresh))
	ancient := time.Now().Add(-365 * 24 * time.Hour)
	must(t, os.Chtimes(filepath.Join(vdir, newName), ancient, ancient))

	preq := req
	preq.Op, preq.Retention, preq.RunID = OpPurgeVersions, Retention{VersionsDays: 30}, "run_prune"
	st := runJob(t, e, preq)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Deleted != 1 {
		t.Errorf("deleted %d", st.Result.Counts.Deleted)
	}
	for n, keep := range map[string]bool{oldName: false, newName: true, "not-a-timestamp": true} {
		if _, err := os.Stat(filepath.Join(vdir, n)); (err == nil) != keep {
			t.Errorf("%s kept=%v, want %v", n, err == nil, keep)
		}
	}
	// 0 days keeps everything.
	preq.Retention.VersionsDays, preq.RunID = 0, "run_prune0"
	expectState(t, runJob(t, e, preq), JobDone, "")
	if _, err := os.Stat(filepath.Join(vdir, newName)); err != nil {
		t.Error("keep-forever removed a version")
	}
	// Someone else's folder: nothing is pruned.
	preq.DestFolderID, preq.Retention.VersionsDays, preq.RunID = "00000000-0000-4000-8000-00000000abcd", 1, "run_prune_x"
	expectState(t, runJob(t, e, preq), JobError, CodeDestMarkerMismatch)
	if _, err := os.Stat(filepath.Join(vdir, newName)); err != nil {
		t.Error("pruned a folder that isn't the job's")
	}
}

func TestPurgeDest(t *testing.T) {
	e, _, dst, req, _ := mirrorWithHistory(t)
	preq := req
	preq.Op, preq.RunID, preq.Sources = OpPurgeDest, "run_purge_x", nil
	preq.DestFolderID = "00000000-0000-4000-8000-00000000abcd"
	expectState(t, runJob(t, e, preq), JobError, CodeDestMarkerMismatch)
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "m", "a.txt")); err != nil {
		t.Fatal("a mismatched purge deleted data")
	}
	preq.DestFolderID, preq.RunID = testFolderID, "run_purge"
	st := runJob(t, e, preq)
	expectState(t, st, JobDone, "")
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "m")); !os.IsNotExist(err) {
		t.Fatalf("the backup folder is still there: %v", err)
	}
	if st.Result.Counts.Deleted < 3 {
		t.Errorf("deleted %d", st.Result.Counts.Deleted)
	}
}

func TestPurgeDestAtDriveRootKeepsOtherFiles(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"f": "1"})
	writeTree(t, dst.MountPoint, map[string]string{"someone-elses.txt": "keep me"})
	req := archiveReq(ep(src, "s"), ep(dst, ""))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	req.Op, req.FirstRun, req.RunID, req.Sources = OpPurgeDest, false, "run_purge_root", nil
	expectState(t, runJob(t, e, req), JobDone, "")
	got := readTree(t, dst.MountPoint)
	if len(got) != 1 || got["someone-elses.txt"] != "keep me" {
		t.Fatalf("drive root after purge: %v", keys(got))
	}
}
