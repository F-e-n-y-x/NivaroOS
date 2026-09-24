package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	rfs "github.com/rclone/rclone/fs"
)

// noPutStreamFs hides PutStream, like backends that need the size of an
// upload up front.
type noPutStreamFs struct{ rfs.Fs }

func (n *noPutStreamFs) Features() *rfs.Features {
	ft := *n.Fs.Features()
	ft.PutStream = nil
	return &ft
}

func mustSpool(t *testing.T, e *Engine) *logSpool {
	t.Helper()
	sp, err := newLogSpool(e.cfg.SpoolDir, 424242, t.Logf)
	must(t, err)
	t.Cleanup(sp.remove)
	return sp
}

// awkwardNames are file names a shell-built archive would have broken.
var awkwardNames = map[string]string{
	`it's "quoted".txt`:       "quotes",
	"$(rm -rf nothing).sh":    "subshell",
	"with space/and more.txt": "spaces",
	"new\nline.txt":           "newline",
	"back\\slash;&|.md":       "punctuation",
	"ünïcødé/日本.txt":          "unicode",
}

func archiveReq(src, dst Endpoint) JobRequest {
	r := baseReq(OpArchive, src, dst)
	r.JobID = "bk_arch0000001"
	return r
}

func TestArchiveRoundTripKeepsNamesModesLinksAndHoles(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "app")
	writeTree(t, root, awkwardNames)
	must(t, os.Chmod(filepath.Join(root, "$(rm -rf nothing).sh"), 0o751))
	must(t, os.Symlink("with space/and more.txt", filepath.Join(root, "link")))
	// A sparse file: 8 MiB with two small data blocks.
	sparse := filepath.Join(root, "disk.img")
	f, err := os.Create(sparse)
	must(t, err)
	_, err = f.WriteAt([]byte("head"), 0)
	must(t, err)
	_, err = f.WriteAt([]byte("tail"), 8<<20-4)
	must(t, err)
	must(t, f.Close())
	old := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	must(t, os.Chtimes(filepath.Join(root, "new\nline.txt"), old, old))
	asRoot := os.Geteuid() == 0
	if asRoot {
		must(t, os.Lchown(filepath.Join(root, "with space/and more.txt"), 1234, 4321))
	}

	st := runJob(t, e, archiveReq(ep(src, "app"), ep(dst, "archives")))
	expectState(t, st, JobDone, "")
	name := st.Result.ArchiveName
	if _, ok := parseArchiveName("bk_arch0000001", name); !ok {
		t.Fatalf("archive name %q", name)
	}
	dir := filepath.Join(dst.MountPoint, "archives")
	entries, err := os.ReadDir(dir)
	must(t, err)
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	sort.Strings(names)
	want := []string{MarkerFile, name, name + archiveIndexSuffix}
	sort.Strings(want)
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("destination holds %v, want %v", names, want)
	}
	if st.Result.Counts.Added != int64(len(awkwardNames)+1) { // + disk.img; links aren't files
		t.Errorf("added = %d", st.Result.Counts.Added)
	}

	// Restore everything into an empty folder.
	out := filepath.Join(dst.MountPoint, "restored")
	req := JobRequest{Op: OpRestore, RunID: "run_restore_arch", JobID: "bk_arch0000001", Dest: ep(dst, "archives"),
		DestFolderID: testFolderID,
		Restore:      &RestoreSpec{VersionID: "a_" + name, Target: ep(dst, "restored"), Conflict: ConflictKeepBoth}}
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	got := readTree(t, out)
	for p, c := range awkwardNames {
		if got[p] != c {
			t.Errorf("%q = %q, want %q", p, got[p], c)
		}
	}
	fi, err := os.Stat(filepath.Join(out, "$(rm -rf nothing).sh"))
	must(t, err)
	if fi.Mode().Perm() != 0o751 {
		t.Errorf("mode %v, want 0751", fi.Mode().Perm())
	}
	if l, err := os.Readlink(filepath.Join(out, "link")); err != nil || l != "with space/and more.txt" {
		t.Errorf("link = %q, %v", l, err)
	}
	fi, err = os.Stat(filepath.Join(out, "new\nline.txt"))
	must(t, err)
	if !fi.ModTime().Equal(old) {
		t.Errorf("mtime %v, want %v", fi.ModTime(), old)
	}
	if asRoot {
		fi, err = os.Lstat(filepath.Join(out, "with space/and more.txt"))
		must(t, err)
		if st := fi.Sys().(*syscall.Stat_t); st.Uid != 1234 || st.Gid != 4321 {
			t.Errorf("owner %d:%d, want 1234:4321", st.Uid, st.Gid)
		}
	}
	fi, err = os.Stat(filepath.Join(out, "disk.img"))
	must(t, err)
	blocks := fi.Sys().(*syscall.Stat_t).Blocks * 512
	if fi.Size() != 8<<20 {
		t.Fatalf("sparse file size %d", fi.Size())
	}
	if blocks >= fi.Size() {
		t.Errorf("sparse file came back fully allocated (%d of %d bytes)", blocks, fi.Size())
	}
	raw, err := os.ReadFile(filepath.Join(out, "disk.img"))
	must(t, err)
	if string(raw[:4]) != "head" || string(raw[len(raw)-4:]) != "tail" {
		t.Error("sparse file content changed")
	}

	// Restoring again with keep_both: identical files are skipped, a
	// changed one comes back beside the local copy.
	must(t, os.WriteFile(filepath.Join(out, "with space/and more.txt"), []byte("edited"), 0o644))
	req.RunID = "run_restore_arch2"
	st = runJob(t, e, req)
	expectState(t, st, JobDone, "")
	got = readTree(t, out)
	if got["with space/and more.txt"] != "edited" {
		t.Error("keep_both overwrote the local file")
	}
	day := time.Now().Format("2006-01-02")
	if got["with space/and more (restored "+day+").txt"] != "spaces" {
		t.Errorf("keep_both copy missing: %v", keys(got))
	}
	if st.Result.Counts.Skipped < int64(len(awkwardNames)-1) {
		t.Errorf("skipped %d identical files", st.Result.Counts.Skipped)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestArchiveMultipleSourcesAndPartials(t *testing.T) {
	s := newTestSys(t)
	a := s.addVolume("a", "a1a1a1a1-aaaa-4bbb-8ccc-000000000021", "ext4")
	b := s.addVolume("b", "b2b2b2b2-aaaa-4bbb-8ccc-000000000022", "ext4")
	dst := s.addVolume("dst", "c3c3c3c3-aaaa-4bbb-8ccc-000000000023", "ext4")
	e := s.engine()
	writeTree(t, filepath.Join(a.MountPoint, "Photos"), map[string]string{"p.jpg": "P"})
	writeTree(t, filepath.Join(b.MountPoint, "Photos"), map[string]string{"q.jpg": "Q"})
	writeTree(t, filepath.Join(b.MountPoint, "Docs"), map[string]string{"d.txt": "D"})
	// A crash left a half-written archive of this job, and another job's
	// file sits in the folder too.
	dir := filepath.Join(dst.MountPoint, "arch")
	stale := archiveName("bk_arch0000001", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) + archivePartial
	foreign := archiveName("bk_other000001", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	writeTree(t, dir, map[string]string{stale: "half", foreign: "theirs"})

	req := archiveReq(ep(a, "Photos"), ep(dst, "arch"))
	req.Sources = append(req.Sources, ep(b, "Photos"), ep(b, "Docs"))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if _, err := os.Stat(filepath.Join(dir, stale)); !os.IsNotExist(err) {
		t.Error("the stale partial archive is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, foreign)); err != nil {
		t.Error("another job's archive was removed")
	}
	res, err := e.Browse(context.Background(), BrowseRequest{Endpoint: ep(dst, "arch"), VersionID: "a_" + st.Result.ArchiveName})
	must(t, err)
	var top []string
	for _, en := range res.Entries {
		top = append(top, en.Name)
		if !en.Dir {
			t.Errorf("%s is not a folder", en.Name)
		}
	}
	if strings.Join(top, "|") != "Docs|Photos|Photos (2)" {
		t.Fatalf("archive top level = %v", top)
	}
	res, err = e.Browse(context.Background(), BrowseRequest{Endpoint: ep(dst, "arch"), VersionID: "a_" + st.Result.ArchiveName, Path: "Photos (2)"})
	must(t, err)
	if len(res.Entries) != 1 || res.Entries[0].Name != "q.jpg" || res.Entries[0].Size != 1 {
		t.Fatalf("Photos (2) = %+v", res.Entries)
	}
	// Verify decodes the whole archive.
	req.Options.Verify = true
	req.FirstRun = false
	req.RunID = "run_verify_arch"
	expectState(t, runJob(t, e, req), JobDone, "")
}

func TestArchiveDownloads(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"one.txt": "first", "dir/two.txt": "second"})
	st := runJob(t, e, archiveReq(ep(src, "s"), ep(dst, "a")))
	expectState(t, st, JobDone, "")
	vid := "a_" + st.Result.ArchiveName
	d, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: ep(dst, "a"), VersionID: vid, Paths: []string{"one.txt"}})
	must(t, err)
	raw, err := io.ReadAll(d.Body)
	must(t, err)
	must(t, d.Body.Close())
	if string(raw) != "first" || d.Name != "one.txt" || d.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("raw download %q %+v", raw, d)
	}
	d, err = e.OpenDownload(context.Background(), DownloadRequest{Dest: ep(dst, "a"), VersionID: vid, Paths: []string{"dir", "one.txt"}})
	must(t, err)
	raw, err = io.ReadAll(d.Body)
	must(t, err)
	must(t, d.Body.Close())
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	must(t, err)
	var got []string
	for _, f := range zr.File {
		got = append(got, f.Name)
	}
	sort.Strings(got)
	if d.ContentType != "application/zip" || strings.Join(got, "|") != "dir/two.txt|one.txt" {
		t.Fatalf("zip download %v (%s)", got, d.ContentType)
	}
	if _, err := e.OpenDownload(context.Background(), DownloadRequest{Dest: ep(dst, "a"), VersionID: vid, Paths: []string{"../x"}}); CodeOf(err) != CodePathNotAllowed {
		t.Fatalf("escape: %v", err)
	}
}

func TestArchiveKeepLastByName(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"f": "1"})
	st := runJob(t, e, archiveReq(ep(src, "s"), ep(dst, "a")))
	expectState(t, st, JobDone, "")
	dir := filepath.Join(dst.MountPoint, "a")
	// Older archives whose modtimes lie: the oldest name has the newest
	// mtime. Retention goes by the name only.
	var made []string
	for i, day := range []int{1, 2, 3, 4} {
		n := archiveName("bk_arch0000001", time.Date(2025, 6, day, 3, 0, 0, 0, time.UTC))
		must(t, os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o600))
		mt := time.Now().Add(-time.Duration(i) * time.Hour)
		must(t, os.Chtimes(filepath.Join(dir, n), mt, mt))
		made = append(made, n)
	}
	partial := archiveName("bk_arch0000001", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) + archivePartial
	must(t, os.WriteFile(filepath.Join(dir, partial), []byte("in progress"), 0o600))

	vers, err := e.ListVersions(context.Background(), VersionsRequest{JobID: "bk_arch0000001", JobType: jobTypeArchive, Dest: ep(dst, "a")})
	must(t, err)
	if len(vers) != 5 || vers[0].ID != "a_"+st.Result.ArchiveName || vers[0].Files != 1 || vers[4].ID != "a_"+made[0] {
		t.Fatalf("versions = %+v", vers)
	}

	req := archiveReq(ep(src, "s"), ep(dst, "a"))
	req.Op, req.FirstRun, req.Retention = OpPurgeArchives, false, Retention{KeepLast: 2}
	st2 := runJob(t, e, req)
	expectState(t, st2, JobDone, "")
	if st2.Result.Counts.Deleted != 3 {
		t.Errorf("deleted %d, want 3", st2.Result.Counts.Deleted)
	}
	for i, n := range made {
		_, err := os.Stat(filepath.Join(dir, n))
		if keep := i == 3; keep != (err == nil) {
			t.Errorf("%s: kept=%v, want %v", n, err == nil, keep)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, st.Result.ArchiveName)); err != nil {
		t.Error("the newest archive was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, st.Result.ArchiveName+archiveIndexSuffix)); err != nil {
		t.Error("the newest archive's index was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, partial)); err != nil {
		t.Error("a partial archive was counted and removed")
	}
}

func TestArchiveToRemoteWithoutStreamingUploads(t *testing.T) {
	// A remote whose backend can't take uploads of unknown size: the
	// archive is built in the staging folder and uploaded from there,
	// never in the system temp folder.
	s := newTestSys(t)
	src := s.addVolume("src", "d4d4d4d4-aaaa-4bbb-8ccc-000000000024", "ext4")
	cloudRoot := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloudRoot, 0o755))
	s.writeRcloneConf("[box]\ntype = alias\nremote = " + cloudRoot + "\n")
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"f": "1"})
	dst, err := e.resolve(context.Background(), Endpoint{Kind: EPCloud, RefID: "box", SubPath: "a"}, nil)
	must(t, err)
	f, err := dst.fsAt(context.Background(), "", fsOpts{})
	must(t, err)
	must(t, f.Mkdir(context.Background(), ""))
	noStream := &noPutStreamFs{Fs: f}
	srcT, err := e.resolve(context.Background(), ep(src, "s"), nil)
	must(t, err)
	fi, err := newFilter(filterSpec{})
	must(t, err)
	j := &job{log: mustSpool(t, e), group: "bk_stage"}
	idx, n, err := e.streamArchive(context.Background(), j, noStream, "x.tar.zst", []archiveSource{{t: srcT, filter: fi}}, false)
	must(t, err)
	if idx.header.Files != 1 || n <= 0 {
		t.Fatalf("staged archive: files %d, %d bytes", idx.header.Files, n)
	}
	if _, err := os.Stat(filepath.Join(cloudRoot, "a", "x.tar.zst")); err != nil {
		t.Fatalf("not uploaded: %v", err)
	}
	left, _ := os.ReadDir(e.cfg.StagingDir)
	if len(left) != 0 {
		t.Fatalf("staging folder not cleaned: %v", left)
	}
}
