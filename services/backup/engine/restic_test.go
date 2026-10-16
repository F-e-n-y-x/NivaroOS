package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plantPartial writes an interrupted upload's leftover, age old.
func plantPartial(t *testing.T, repo, rel string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(repo, rel)
	must(t, os.MkdirAll(filepath.Dir(p), 0o700))
	must(t, os.WriteFile(p, []byte("cut off"), 0o600))
	ts := time.Now().Add(-age)
	must(t, os.Chtimes(p, ts, ts))
	return p
}

// needRestic skips a test when restic isn't installed (set NIVAROOS_RESTIC
// to a restic binary to run them).
func needRestic(t *testing.T) {
	t.Helper()
	if Restic() == "" {
		t.Skip("restic not installed; set NIVAROOS_RESTIC")
	}
}

func resticReq(op Op, dst Endpoint, srcs ...Endpoint) JobRequest {
	r := baseReq(op, Endpoint{}, dst)
	r.JobID, r.Sources = "bk_restic00001", srcs
	return r
}

func versions(t *testing.T, e *Engine, dst Endpoint) []Version {
	t.Helper()
	vs, err := e.ListVersions(context.Background(), VersionsRequest{JobID: "bk_restic00001", JobType: jobTypeBackup, Dest: dst})
	must(t, err)
	return vs
}

func TestResticBackupDedupsBrowsesRestoresAndForgets(t *testing.T) {
	needRestic(t)
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "docs")
	writeTree(t, root, awkwardNames)
	writeTree(t, root, map[string]string{"skip.tmp": "x", "keep/a.txt": "A"})
	big := make([]byte, 4<<20)
	_, _ = rand.Read(big)
	must(t, os.WriteFile(filepath.Join(root, "big.bin"), big, 0o644))
	srcEP, dstEP := ep(src, "docs"), ep(dst, "repo")

	req := resticReq(OpBackup, dstEP, srcEP)
	req.Filters.ExcludePresets = []string{ExcludePresetTemp}
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if !st.Result.MarkerWritten {
		t.Error("first run did not write the marker")
	}
	for _, f := range []string{MarkerFile, "config", "keys", "data", "snapshots"} {
		if _, err := os.Stat(filepath.Join(dst.MountPoint, "repo", f)); err != nil {
			t.Errorf("repository: %v", err)
		}
	}
	first := st.Result.Counts
	if first.Added != int64(len(awkwardNames))+2 || first.BytesTransferred < 4<<20 {
		t.Fatalf("first run counts %+v", first)
	}

	// Uploads a crash cut off: stale ones go at the next run, a fresh one
	// (maybe a running upload) and files that aren't restic's stay.
	repoDir := filepath.Join(dst.MountPoint, "repo")
	stalePartial := plantPartial(t, repoDir, "data/ab/ab01.partial", 25*time.Hour)
	staleConfig := plantPartial(t, repoDir, "config.partial", 25*time.Hour)
	fresh := plantPartial(t, repoDir, "data/ab/ab02.partial", time.Hour)
	foreign := plantPartial(t, repoDir, "notes.partial", 25*time.Hour)

	// Second run: one small change; only it is uploaded.
	must(t, os.WriteFile(filepath.Join(root, "keep/a.txt"), []byte("AA"), 0o644))
	req.FirstRun = false
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	for p, keep := range map[string]bool{stalePartial: false, staleConfig: false, fresh: true, foreign: true} {
		if _, err := os.Stat(p); os.IsNotExist(err) == keep {
			t.Errorf("%s: kept=%v, want %v", p, !keep, keep)
		}
	}
	forgetPartial := plantPartial(t, repoDir, "index/cd03.partial", 25*time.Hour)
	if c := st.Result.Counts; c.Changed != 1 || c.Added != 0 || c.BytesTransferred > 1<<20 {
		t.Fatalf("second run counts %+v: it should upload only the change", c)
	}
	vs := versions(t, e, dstEP)
	if len(vs) != 2 || vs[0].Kind != VersionSnapshot || !vs[0].Time.After(*vs[1].Time) {
		t.Fatalf("versions %+v", vs)
	}

	br, err := e.Browse(context.Background(), BrowseRequest{Endpoint: dstEP, VersionID: vs[0].ID, Path: "keep"})
	must(t, err)
	if len(br.Entries) != 1 || br.Entries[0].Name != "a.txt" || br.Entries[0].Size != 2 {
		t.Fatalf("browse keep: %+v", br.Entries)
	}
	br, err = e.Browse(context.Background(), BrowseRequest{Endpoint: dstEP, VersionID: vs[0].ID})
	must(t, err)
	for _, en := range br.Entries {
		if en.Name == "skip.tmp" {
			t.Error("the temp-files preset was not applied")
		}
	}

	// Everything of the older snapshot, to a new folder.
	restore := func(version string, paths []string, target Endpoint, conflict string) JobStatus {
		t.Helper()
		r := JobRequest{Op: OpRestore, RunID: "run_restore_" + version[:6] + conflict, JobID: "bk_restic00001", Dest: dstEP,
			DestFolderID: testFolderID, Restore: &RestoreSpec{VersionID: version, Paths: paths, Target: target, Conflict: conflict}}
		st := runJob(t, e, r)
		expectState(t, st, JobDone, "")
		return st
	}
	restore(vs[1].ID, nil, ep(dst, "out"), ConflictKeepBoth)
	got := readTree(t, filepath.Join(dst.MountPoint, "out"))
	for p, c := range awkwardNames {
		if got[p] != c {
			t.Errorf("%q = %q, want %q", p, got[p], c)
		}
	}
	if got["keep/a.txt"] != "A" || got["big.bin"] != string(big) {
		t.Errorf("restored old snapshot wrong: a.txt=%q big=%d bytes", got["keep/a.txt"], len(got["big.bin"]))
	}

	// One file of the old snapshot back to the original place: kept beside.
	st = restore(vs[1].ID, []string{"keep/a.txt"}, srcEP, ConflictKeepBoth)
	if st.Result.Counts.Added != 1 {
		t.Errorf("restore one file: %+v", st.Result.Counts)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "keep", "a (restored *).txt"))
	if len(matches) != 1 {
		t.Fatalf("keep-both copy missing: %v", matches)
	}
	if raw, _ := os.ReadFile(matches[0]); string(raw) != "A" {
		t.Errorf("kept copy = %q", raw)
	}

	// Downloads: one file raw, a folder as a zip.
	d, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: dstEP, VersionID: vs[0].ID, Paths: []string{"keep/a.txt"}})
	must(t, err)
	raw, err := io.ReadAll(d.Body)
	d.Body.Close()
	if err != nil || string(raw) != "AA" {
		t.Fatalf("download file = %q, %v", raw, err)
	}
	d, err = e.OpenDownload(context.Background(), DownloadRequest{Dest: dstEP, VersionID: vs[0].ID, Paths: []string{"keep", "big.bin"}})
	must(t, err)
	raw, err = io.ReadAll(d.Body)
	d.Body.Close()
	must(t, err)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	must(t, err)
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "keep/a.txt,big.bin" {
		t.Errorf("zip holds %v", names)
	}

	// Check reads data back; forget keeps the newest one.
	chk := resticReq(OpCheck, dstEP)
	chk.PlanOp, chk.Sources = OpBackup, nil
	st = runJob(t, e, chk)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Errored != 0 || st.Result.Counts.Skipped != 2 {
		t.Errorf("check counts %+v", st.Result.Counts)
	}
	fg := resticReq(OpForget, dstEP)
	fg.Sources, fg.Retention = nil, Retention{KeepLast: 1}
	st = runJob(t, e, fg)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Deleted != 1 {
		t.Errorf("forget deleted %d", st.Result.Counts.Deleted)
	}
	if _, err := os.Stat(forgetPartial); !os.IsNotExist(err) {
		t.Errorf("forget left a stale partial: %v", err)
	}
	if vs := versions(t, e, dstEP); len(vs) != 1 {
		t.Errorf("after forget: %d versions", len(vs))
	}
}

func TestResticPasswordAndMissingRepo(t *testing.T) {
	needRestic(t)
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})
	enc := func(pw string) *Encryption {
		return &Encryption{Mode: EncryptRestic, Keys: &CryptKeys{Password: pw}}
	}
	dstEP := ep(dst, "r")
	dstEP.Encryption = enc("correct horse")
	st := runJob(t, e, resticReq(OpBackup, dstEP, ep(src, "s")))
	expectState(t, st, JobDone, "")

	dstEP.Encryption = enc("wrong password")
	req := resticReq(OpBackup, dstEP, ep(src, "s"))
	req.FirstRun = false
	expectState(t, runJob(t, e, req), JobError, CodeWrongPassword)

	dstEP.Encryption = nil // a repository with a password, opened without one
	req = resticReq(OpBackup, dstEP, ep(src, "s"))
	req.FirstRun = false
	expectState(t, runJob(t, e, req), JobError, CodeWrongPassword)
}

func TestResticExcludesAnchorEachSource(t *testing.T) {
	p := &prepared{
		in:       precheckInput{filters: Filters{Exclude: []string{"/cache/**", "*.log"}}},
		sources:  []*target{{local: true, path: "/DATA/a [1]"}, {local: true, path: "/mnt/b"}},
		excludes: [][]string{{"inner"}, nil},
	}
	got, err := resticExcludes(p)
	must(t, err)
	want := []string{`/DATA/a \[1\]/inner/**`, `/DATA/a \[1\]/cache/**`, "*.log", "/mnt/b/cache/**"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("excludes\n got %q\nwant %q", got, want)
	}
	p.in.filters.Include = []string{"*.jpg"}
	if _, err := resticExcludes(p); CodeOf(err) != CodeInvalidFilter {
		t.Fatalf("include rules: %v", err)
	}
}
