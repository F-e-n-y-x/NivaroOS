package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func folderEnc(pw string) *Encryption {
	return &Encryption{Mode: EncryptFolder, Keys: &CryptKeys{Password: pw, Salt: "salt-" + pw, KeyFile: []byte(`{"v":1,"mode":"folder"}` + "\n")}}
}

// plainNames lists every path under root, for "nothing readable" checks.
func plainNames(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	must(t, filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	}))
	return out
}

// assertUnreadable fails when a destination shows any secret name or
// content: only the engine's own two files may be plain.
func assertUnreadable(t *testing.T, root string, secrets ...string) {
	t.Helper()
	for _, p := range plainNames(t, root) {
		if p == "." || p == MarkerFile || p == KeyFileName {
			continue
		}
		for _, s := range secrets {
			if strings.Contains(strings.ToLower(p), strings.ToLower(s)) {
				t.Errorf("destination path %q shows %q", p, s)
			}
		}
		if raw, err := os.ReadFile(filepath.Join(root, p)); err == nil {
			for _, s := range secrets {
				if strings.Contains(string(raw), s) {
					t.Errorf("destination file %q holds %q in clear", p, s)
				}
			}
		}
	}
}

func TestEncryptedFolderMirrorVersionsRestoreAndCheck(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "docs")
	writeTree(t, root, map[string]string{"taxes-2026.pdf": "secret-content-1", "diary/june.txt": "secret-content-2", "keep.txt": "secret-content-3"})
	d := ep(dst, "enc")
	d.Encryption = folderEnc("correct horse")
	req := baseReq(OpSync, ep(src, "docs"), d)
	req.Guards = Guards{EmptySourcePct: 50, DeletePct: 100, ChangePct: 100}
	req.Options.Verify = true
	st := runJob(t, e, withRun(req, "run_c1"))
	expectState(t, st, JobDone, "")
	destDir := filepath.Join(dst.MountPoint, "enc")
	assertUnreadable(t, destDir, "taxes", "diary", "june", "keep", "secret-content")
	if raw, err := os.ReadFile(filepath.Join(destDir, KeyFileName)); err != nil || !strings.Contains(string(raw), `"folder"`) {
		t.Fatalf("key file: %q %v", raw, err)
	}

	// Second run: a change and a deletion go to the (encrypted) recycle folder.
	must(t, os.WriteFile(filepath.Join(root, "taxes-2026.pdf"), []byte("secret-content-1b"), 0o644))
	must(t, os.Remove(filepath.Join(root, "keep.txt")))
	req.FirstRun = false
	expectState(t, runJob(t, e, withRun(req, "run_c2")), JobDone, "")
	assertUnreadable(t, destDir, "taxes", "diary", "keep", "secret-content", VersionsDir)

	vers, err := e.ListVersions(context.Background(), VersionsRequest{JobID: req.JobID, JobType: jobTypeMirror, Dest: d})
	must(t, err)
	if len(vers) != 2 || vers[1].Kind != VersionRecycle || vers[1].Files != 2 {
		t.Fatalf("versions = %+v", vers)
	}
	cur, err := e.Browse(context.Background(), BrowseRequest{Endpoint: d})
	must(t, err)
	var names []string
	for _, en := range cur.Entries {
		names = append(names, en.Name)
	}
	if strings.Join(names, "|") != "diary|taxes-2026.pdf" {
		t.Fatalf("browse current = %v", names)
	}

	// Restore the deleted file and the old version of the changed one.
	spec := &RestoreSpec{VersionID: vers[1].ID, Paths: []string{"keep.txt", "taxes-2026.pdf"}, Target: ep(src, "restored"), Conflict: ConflictOverwrite}
	expectState(t, runJob(t, e, JobRequest{Op: OpRestore, RunID: "run_c3", JobID: req.JobID, Dest: d, DestFolderID: testFolderID, Restore: spec}), JobDone, "")
	got := readTree(t, filepath.Join(src.MountPoint, "restored"))
	if got["keep.txt"] != "secret-content-3" || got["taxes-2026.pdf"] != "secret-content-1" || len(got) != 2 {
		t.Fatalf("restored %v", got)
	}
	dl, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: d, VersionID: "current", Paths: []string{"diary/june.txt"}})
	must(t, err)
	raw, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	if string(raw) != "secret-content-2" {
		t.Fatalf("download %q", raw)
	}

	// Check backup (cryptcheck) passes, then catches a damaged file.
	creq := baseReq(OpCheck, ep(src, "docs"), d)
	creq.FirstRun = false
	st = runJob(t, e, withRun(creq, "run_c4"))
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Errored != 0 || st.Result.Counts.Skipped != 2 {
		t.Fatalf("check counts %+v %+v", st.Result.Counts, st.Result.FileErrors)
	}
	var victim string
	for _, p := range plainNames(t, destDir) {
		fi, _ := os.Stat(filepath.Join(destDir, p))
		if fi.Mode().IsRegular() && !strings.HasPrefix(p, ".") && !strings.Contains(p, "/") {
			victim = p // the top-level encrypted file: taxes-2026.pdf
		}
	}
	raw, err = os.ReadFile(filepath.Join(destDir, victim))
	must(t, err)
	raw[len(raw)-1] ^= 0xff
	must(t, os.WriteFile(filepath.Join(destDir, victim), raw, 0o644))
	st = runJob(t, e, withRun(creq, "run_c5"))
	if st.Result == nil || st.Result.Counts.Errored != 1 {
		t.Fatalf("damaged file not found: %+v", st.Result)
	}

	// The wrong password sees nothing; no keys at all is refused.
	wrong := ep(dst, "enc")
	wrong.Encryption = folderEnc("wrong horse")
	cur, err = e.Browse(context.Background(), BrowseRequest{Endpoint: wrong})
	must(t, err)
	if len(cur.Entries) != 0 {
		t.Fatalf("wrong password lists %+v", cur.Entries)
	}
	locked := ep(dst, "enc")
	locked.Encryption = &Encryption{Mode: EncryptFolder}
	if _, err := e.Browse(context.Background(), BrowseRequest{Endpoint: locked}); CodeOf(err) != CodeEncryptionLocked {
		t.Fatalf("no keys: %v", err)
	}
	if st := runJob(t, e, withRun(JobRequest{Op: OpCopy, JobID: req.JobID, Sources: req.Sources, Dest: locked, DestFolderID: testFolderID}, "run_c6")); resultCode(st) != CodeEncryptionLocked {
		t.Fatalf("copy without keys: %s", resultCode(st))
	}
}

func TestEncryptedFolderOnCloudRemote(t *testing.T) {
	// An alias remote over a temp folder stands in for a cloud. Check
	// backup there decrypts a sample.
	s := newTestSys(t)
	src := s.addVolume("src", "d4d4d4d4-aaaa-4bbb-8ccc-000000000044", "ext4")
	cloud := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloud, 0o755))
	s.writeRcloneConf("[box]\ntype = alias\nremote = " + cloud + "\n")
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "p"), map[string]string{"holiday.jpg": "pixels", "sub/notes.md": "words"})
	d := Endpoint{Kind: EPCloud, RefID: "box", SubPath: "backups/p"}
	d.Encryption = folderEnc("pw")
	req := baseReq(OpCopy, ep(src, "p"), d)
	expectState(t, runJob(t, e, withRun(req, "run_r1")), JobDone, "")
	assertUnreadable(t, filepath.Join(cloud, "backups", "p"), "holiday", "notes", "sub", "pixels", "words")
	res, err := e.Resolve(context.Background(), ResolveRequest{Endpoint: d})
	must(t, err)
	if res.Marker == nil || len(res.KeyFile) == 0 {
		t.Fatalf("resolve: marker %v key file %q", res.Marker, res.KeyFile)
	}
	if st := runJob(t, e, withRun(baseReq(OpCheck, ep(src, "p"), d), "run_r2")); st.Result == nil || st.Result.Counts.Errored != 0 {
		t.Fatalf("check: %+v", st.Result)
	}
	// No hashes to compare (TeraBox, SMB): a sample is decrypted in full.
	dt, err := e.resolve(context.Background(), d, nil)
	must(t, err)
	df, err := dt.fsAt(context.Background(), "", fsOpts{})
	must(t, err)
	sf, err := newBackendFs(context.Background(), "local", ":local", filepath.Join(src.MountPoint, "p"), nil)
	must(t, err)
	diffs, n, err := decryptSample(context.Background(), sf, unwrapCrypt(df))
	if err != nil || n != 2 || len(diffs) != 0 {
		t.Fatalf("sample: %d checked, %v, %v", n, diffs, err)
	}
}
