package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkByID(cs []Check, id string) Check {
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestModifyWindowPerFSType(t *testing.T) {
	cases := map[string]time.Duration{
		"vfat": 2 * time.Second, "exfat": 2 * time.Second,
		"ntfs": time.Microsecond, "ntfs3": time.Microsecond, "fuseblk": time.Microsecond,
		"ext4": time.Nanosecond, "xfs": time.Nanosecond, "btrfs": time.Nanosecond,
	}
	for fstype, want := range cases {
		q := localQuirks(fstype)
		if q.ModifyWindow != want {
			t.Errorf("%s: window %v, want %v", fstype, q.ModifyWindow, want)
		}
		if !q.Local {
			t.Errorf("%s: not local", fstype)
		}
	}
	for _, fstype := range []string{"vfat", "exfat", "ntfs3"} {
		q := localQuirks(fstype)
		if !q.WinEncoding || !q.CaseInsensitive {
			t.Errorf("%s: encoding %v, case-insensitive %v", fstype, q.WinEncoding, q.CaseInsensitive)
		}
		if o := localFsOptions(q, false, false); o["encoding"] != windowsEncoding || o["case_insensitive"] != "true" {
			t.Errorf("%s: local options %v", fstype, o)
		}
	}
	if !hasQuirk(localQuirks("vfat").Quirks, QuirkMaxFile4G) || hasQuirk(localQuirks("exfat").Quirks, QuirkMaxFile4G) {
		t.Error("only FAT32 has the 4 GiB limit")
	}
	if o := localFsOptions(localQuirks("ext4"), true, true); o["links"] != "true" || o["one_file_system"] != "true" || o["encoding"] != "" {
		t.Errorf("ext4 options %v", o)
	}
}

func TestRemoteQuirks(t *testing.T) {
	tb := remoteQuirks("terabox", nil)
	if !tb.SizeOnly || !tb.NoHash || !hasQuirk(tb.Quirks, QuirkNoModTime) || !hasQuirk(tb.Quirks, QuirkNoHash) {
		t.Fatalf("terabox = %+v", tb)
	}
	if w := warningsFor(tb.Quirks); len(w) != 1 || w[0] != WarnLimitedChangeDetection {
		t.Errorf("terabox warnings %v", w)
	}
	gd := remoteQuirks("drive", nil)
	if !gd.Drive || !hasQuirk(gd.Quirks, QuirkDailyQuota) {
		t.Fatalf("drive = %+v", gd)
	}
	if o := cloudFsOptions(gd); o["skip_gdocs"] != "true" || o["stop_on_upload_limit"] != "true" {
		t.Errorf("drive options %v", o)
	}
	if od := remoteQuirks("onedrive", nil); !od.CaseInsensitive {
		t.Errorf("onedrive = %+v", od)
	}
	for _, p := range []string{"drive", "s3", "terabox", "smb"} {
		if !hasQuirk(remoteQuirks(p, nil).Quirks, QuirkNoMetadata) {
			t.Errorf("%s keeps metadata?", p)
		}
	}
	// Size-only reaches the transfer config, and verify is skipped.
	p := &prepared{dest: &target{q: tb}}
	_, ci := transferConfig(context.Background(), p, Options{LowPriority: true})
	if !ci.SizeOnly || ci.Transfers != 2 {
		t.Errorf("config: size-only %v, transfers %d", ci.SizeOnly, ci.Transfers)
	}
}

func TestFAT32RefusesFilesOf4GiB(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "11110000-aaaa-4bbb-8ccc-000000000011", "ext4")
	stick := s.addVolume("stick", "2B3C-4D5E", "vfat", func(m *fakeMount) { m.USB = true })
	e := s.engine()
	root := filepath.Join(src.MountPoint, "videos")
	writeTree(t, root, map[string]string{"small.mp4": "x"})
	big, err := os.Create(filepath.Join(root, "huge.mkv"))
	must(t, err)
	must(t, big.Truncate(4<<30)) // sparse: costs no disk
	must(t, big.Close())

	res, err := e.Precheck(context.Background(), PrecheckRequest{
		JobType: jobTypeCopy, Sources: []Endpoint{ep(src, "videos")}, Dest: ep(stick, "b"), FirstRun: true,
		Guards: Guards{EmptySourcePct: 50, DeletePct: 10, ChangePct: 30},
	})
	must(t, err)
	c := checkByID(res.Checks, CheckFSQuirks)
	if res.OK || c.Status != CheckFail || c.Code != CodeFat32FileTooLarge || c.Args["paths"] != "huge.mkv" {
		t.Fatalf("precheck = %+v, fs_quirks %+v", res.OK, c)
	}
	// A run refuses before writing anything.
	st := runJob(t, e, baseReq(OpCopy, ep(src, "videos"), ep(stick, "b")))
	expectState(t, st, JobError, CodeFat32FileTooLarge)
	if _, err := os.Stat(filepath.Join(stick.MountPoint, "b", "small.mp4")); err == nil {
		t.Fatal("copied before failing the check")
	}
	// Excluding the file makes the job possible.
	req := baseReq(OpCopy, ep(src, "videos"), ep(stick, "b"))
	req.Filters.Exclude = []string{"*.mkv"}
	expectState(t, runJob(t, e, req), JobDone, "")
}

func TestCaseCollisionOnCaseInsensitiveDest(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "11110000-aaaa-4bbb-8ccc-000000000012", "ext4")
	ex := s.addVolume("ex", "5E6F-7A8B", "exfat")
	lin := s.addVolume("lin", "11110000-aaaa-4bbb-8ccc-000000000013", "ext4")
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "d"), map[string]string{"Report.txt": "1", "report.TXT": "2", "sub/a": "x", "sub/A": "y", "ok": "z"})
	st := runJob(t, e, baseReq(OpCopy, ep(src, "d"), ep(ex, "d")))
	expectState(t, st, JobError, CodeCaseCollision)
	c := checkByID(st.Result.Checks, CheckFSQuirks)
	if c.Args["count"] != int64(2) || !strings.Contains(c.Args["paths"].(string), "sub/a") {
		t.Fatalf("collision check = %+v", c)
	}
	// A case-sensitive destination takes the same tree.
	expectState(t, runJob(t, e, baseReq(OpCopy, ep(src, "d"), ep(lin, "d"))), JobDone, "")
}

func TestWindowsNamesAreEncodedOnNTFS(t *testing.T) {
	s := newTestSys(t)
	src := s.addVolume("src", "11110000-aaaa-4bbb-8ccc-000000000014", "ext4")
	nt := s.addVolume("nt", "0123456789ABCDEF", "ntfs3")
	e := s.engine()
	writeTree(t, filepath.Join(src.MountPoint, "d"), map[string]string{"what?.txt": "1", "a:b": "2"})
	expectState(t, runJob(t, e, baseReq(OpCopy, ep(src, "d"), ep(nt, "d"))), JobDone, "")
	got := readTree(t, filepath.Join(nt.MountPoint, "d"))
	if _, ok := got["what?.txt"]; ok {
		t.Fatal("a Windows-reserved character was written as is")
	}
	if got["what？.txt"] != "1" || got["a：b"] != "2" {
		t.Fatalf("encoded names = %v", got)
	}
}

func TestWorldWritableWarning(t *testing.T) {
	if !worldWritable(mountEntry{Options: "rw", SuperOpts: "rw,uid=0,gid=0,fmask=0000,dmask=0000"}) {
		t.Error("fmask=0000 is world-writable")
	}
	if worldWritable(mountEntry{Options: "rw", SuperOpts: "rw,fmask=0022,dmask=0022"}) {
		t.Error("0022 is not world-writable")
	}
}
