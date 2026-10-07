package engine

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func need7z(t *testing.T) {
	t.Helper()
	if SevenZip() == "" {
		t.Skip("7z is not installed")
	}
}

func archiveEnc(pw string, vol int64) *Encryption {
	return &Encryption{Mode: EncryptArchive, VolumeBytes: vol, Keys: &CryptKeys{Password: pw, KeyFile: []byte(`{"v":1,"mode":"archive"}` + "\n")}}
}

func TestEncryptedArchiveSplitBrowseRestoreAndPrune(t *testing.T) {
	need7z(t)
	s := newTestSys(t)
	src := s.addVolume("src", "e5e5e5e5-aaaa-4bbb-8ccc-000000000055", "ext4")
	cloud := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloud, 0o755))
	s.writeRcloneConf("[tera]\ntype = alias\nremote = " + cloud + "\n")
	e := s.engine()
	root := filepath.Join(src.MountPoint, "vault")
	noise := make([]byte, 300<<10) // incompressible: several 64 KiB volumes
	_, _ = rand.Read(noise)
	writeTree(t, root, map[string]string{"passport-scan.pdf": string(noise), "letters/to-mum.txt": "dear mum"})

	d := Endpoint{Kind: EPCloud, RefID: "tera", SubPath: "bk"}
	d.Encryption = archiveEnc("Tr0ub4dor&3", 64<<10)
	req := archiveReq(ep(src, "vault"), d)
	req.Options.Verify = true
	st := runJob(t, e, withRun(req, "run_z1"))
	expectState(t, st, JobDone, "")
	set := st.Result.ArchiveName
	if !strings.HasSuffix(set, ".7z") || st.Result.Counts.Added != 2 {
		t.Fatalf("result %+v", st.Result)
	}
	dir := filepath.Join(cloud, "bk")
	var vols int
	for _, p := range plainNames(t, dir) {
		fi, err := os.Stat(filepath.Join(dir, p))
		must(t, err)
		if strings.HasPrefix(p, set+".") {
			vols++
			if fi.Size() > 64<<10 {
				t.Errorf("volume %s is %d bytes, over the volume size", p, fi.Size())
			}
		}
	}
	if vols < 4 {
		t.Fatalf("%d volumes, want the archive split: %v", vols, plainNames(t, dir))
	}
	assertUnreadable(t, dir, "passport", "letters", "to-mum", "dear mum")

	vers, err := e.ListVersions(context.Background(), VersionsRequest{JobID: req.JobID, JobType: jobTypeArchive, Dest: d})
	must(t, err)
	if len(vers) != 1 || vers[0].ID != "a_"+set || vers[0].Files != 2 {
		t.Fatalf("versions %+v", vers)
	}
	br, err := e.Browse(context.Background(), BrowseRequest{Endpoint: d, VersionID: vers[0].ID, Path: "letters"})
	must(t, err)
	if len(br.Entries) != 1 || br.Entries[0].Name != "to-mum.txt" {
		t.Fatalf("browse %+v", br.Entries)
	}
	spec := &RestoreSpec{VersionID: vers[0].ID, Paths: []string{"passport-scan.pdf"}, Target: ep(src, "back"), Conflict: ConflictOverwrite}
	expectState(t, runJob(t, e, JobRequest{Op: OpRestore, RunID: "run_z2", JobID: req.JobID, Dest: d, DestFolderID: testFolderID, Restore: spec}), JobDone, "")
	if got := readTree(t, filepath.Join(src.MountPoint, "back")); got["passport-scan.pdf"] != string(noise) || len(got) != 1 {
		t.Fatalf("restored %d files", len(got))
	}

	// The wrong password opens nothing.
	wrong := d
	wrong.Encryption = archiveEnc("guess", 64<<10)
	if _, err := e.Browse(context.Background(), BrowseRequest{Endpoint: wrong, VersionID: vers[0].ID}); CodeOf(err) != CodeWrongPassword {
		t.Fatalf("wrong password browse: %v", err)
	}
	spec.Target = ep(src, "nope")
	st = runJob(t, e, JobRequest{Op: OpRestore, RunID: "run_z3", JobID: req.JobID, Dest: wrong, DestFolderID: testFolderID, Restore: spec})
	if resultCode(st) != CodeWrongPassword {
		t.Fatalf("wrong password restore: %s %s", resultCode(st), resultDetail(st))
	}

	// Check backup: 7z decrypts and checks the newest archive.
	creq := archiveReq(ep(src, "vault"), d)
	creq.Op, creq.PlanOp, creq.FirstRun = OpCheck, OpArchive, false
	st = runJob(t, e, withRun(creq, "run_z4"))
	expectState(t, st, JobDone, "")
	creq.Dest = wrong
	if st := runJob(t, e, withRun(creq, "run_z5")); resultCode(st) != CodeWrongPassword {
		t.Fatalf("check with the wrong password: %s", resultCode(st))
	}

	// A second run is another full archive; an unfinished set (volumes,
	// no index) is removed; keep_last 1 prunes the first archive whole.
	stray := strings.Replace(set, "_20", "_19", 1) + ".001"
	must(t, os.WriteFile(filepath.Join(dir, stray), []byte("half"), 0o644))
	time.Sleep(1100 * time.Millisecond)
	req.FirstRun = false
	st = runJob(t, e, withRun(req, "run_z6"))
	expectState(t, st, JobDone, "")
	if _, err := os.Stat(filepath.Join(dir, stray)); !os.IsNotExist(err) {
		t.Error("the unfinished set was not removed")
	}
	preq := JobRequest{Op: OpPurgeArchives, RunID: "run_z7", JobID: req.JobID, Dest: d, DestFolderID: testFolderID, Retention: Retention{KeepLast: 1}}
	expectState(t, runJob(t, e, preq), JobDone, "")
	for _, p := range plainNames(t, dir) {
		if strings.HasPrefix(p, strings.TrimSuffix(set, ".7z")) {
			t.Errorf("pruned archive left %s", p)
		}
	}
	vers, err = e.ListVersions(context.Background(), VersionsRequest{JobID: req.JobID, JobType: jobTypeArchive, Dest: d})
	must(t, err)
	if len(vers) != 1 || vers[0].ID != "a_"+st.Result.ArchiveName {
		t.Fatalf("after prune %+v", vers)
	}
}

func TestEncryptedArchiveOnLocalDrive(t *testing.T) {
	need7z(t)
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a/b.txt": "bee"})
	d := ep(dst, "z")
	d.Encryption = archiveEnc("pw-local", 0)
	st := runJob(t, e, withRun(archiveReq(ep(src, "s"), d), "run_l1"))
	expectState(t, st, JobDone, "")
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "z", st.Result.ArchiveName+".001")); err != nil {
		t.Fatal(err)
	}
	br, err := e.Browse(context.Background(), BrowseRequest{Endpoint: d, VersionID: "a_" + st.Result.ArchiveName, Path: "a"})
	must(t, err)
	if len(br.Entries) != 1 || br.Entries[0].Name != "b.txt" {
		t.Fatalf("browse %+v", br.Entries)
	}
	if left, _ := os.ReadDir(e.cfg.StagingDir); len(left) != 0 {
		t.Fatalf("staging not cleaned: %v", left)
	}
}
