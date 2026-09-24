package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolveOK(t *testing.T, e *Engine, ep Endpoint) Resolved {
	t.Helper()
	r, err := e.Resolve(context.Background(), ResolveRequest{Endpoint: ep})
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", ep, err)
	}
	return r
}

func resolveCode(t *testing.T, e *Engine, ep Endpoint) ErrorCode {
	t.Helper()
	_, err := e.Resolve(context.Background(), ResolveRequest{Endpoint: ep})
	return CodeOf(err)
}

func TestParseMountInfoEscapesAndOptionalFields(t *testing.T) {
	raw := "36 25 8:33 / /DATA/My\\040Drive rw,noatime shared:1 master:2 - ext4 /dev/sdc1 rw,errors=remount-ro\n" +
		"37 25 0:45 /@data /DATA/pool rw - btrfs /dev/sdd1 rw,subvol=/@data\n"
	entries, err := parseMountInfo(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	m := entries[0]
	if m.ID != 36 || m.Parent != 25 || m.MajMin != "8:33" || m.MountPoint != "/DATA/My Drive" || m.FSType != "ext4" || m.Source != "/dev/sdc1" {
		t.Fatalf("entry = %+v", m)
	}
	if entries[1].Root != "/@data" || entries[1].FSType != "btrfs" {
		t.Fatalf("entry = %+v", entries[1])
	}
	if _, err := parseMountInfo(strings.NewReader("garbage line\n")); err == nil {
		t.Fatal("a malformed table was accepted")
	}
}

func TestResolveVolumeByUUID(t *testing.T) {
	s := newTestSys(t)
	m := s.addVolume("tower", "6c1e0000-aaaa-4bbb-8ccc-000000000001", "ntfs3")
	e := s.engine()
	must(t, os.MkdirAll(filepath.Join(m.MountPoint, "Backups"), 0o755))
	r := resolveOK(t, e, ep(m, "Backups"))
	if !r.Online || r.MountID != m.ID || r.Root != filepath.Join(m.MountPoint, "Backups") {
		t.Fatalf("resolved = %+v", r)
	}
	if r.Marker != nil {
		t.Errorf("marker = %+v on a new folder", r.Marker)
	}
	want := []Quirk{QuirkCaseInsensitive, QuirkNTFSChars}
	if strings.Join(quirkStrings(r.Quirks), ",") != strings.Join(quirkStrings(want), ",") {
		t.Errorf("quirks = %v, want %v", r.Quirks, want)
	}
	// Case of a FAT-style UUID doesn't matter.
	upper := ep(m, "")
	upper.RefID = strings.ToUpper(upper.RefID)
	if r := resolveOK(t, e, upper); !r.Online {
		t.Error("upper-case UUID did not resolve")
	}
	// Unmounted: offline, not an error.
	s.unmount(m)
	if r := resolveOK(t, e, ep(m, "Backups")); r.Online {
		t.Fatal("an unmounted volume resolved online")
	}
	// Mounted again (new mount ID): found again.
	s.remount(m)
	if r := resolveOK(t, e, ep(m, "Backups")); !r.Online || r.MountID != m.ID {
		t.Fatalf("after remount = %+v", r)
	}
}

func quirkStrings(q []Quirk) []string {
	out := make([]string, len(q))
	for i, x := range q {
		out[i] = string(x)
	}
	return out
}

func TestResolveRootFilesystemOnlyUnderAllowedRoots(t *testing.T) {
	s := newTestSys(t)
	// The "root filesystem" holds the allowed root, like / holds /DATA.
	root := s.addVolume("system", "77777777-aaaa-4bbb-8ccc-000000000007", "ext4", func(m *fakeMount) { m.MountPoint = s.root })
	e := s.engine()
	rel, _ := filepath.Rel(s.root, filepath.Join(s.allowed, "DATA", "Documents"))
	must(t, os.MkdirAll(filepath.Join(s.root, rel), 0o755))
	r := resolveOK(t, e, ep(root, filepath.ToSlash(rel)))
	if !r.Online || r.Root != filepath.Join(s.allowed, "DATA", "Documents") {
		t.Fatalf("resolved = %+v", r)
	}
	if c := resolveCode(t, e, ep(root, "spool")); c != CodePathNotAllowed {
		t.Fatalf("outside the allowed roots: %q, want path_not_allowed", c)
	}
	if c := resolveCode(t, e, ep(root, "")); c != CodePathNotAllowed {
		t.Fatalf("the whole root filesystem: %q, want path_not_allowed", c)
	}
}

func TestResolveBindMountAndSubvolume(t *testing.T) {
	s := newTestSys(t)
	disk := s.addVolume("disk", "88888888-aaaa-4bbb-8ccc-000000000008", "ext4")
	bind := s.bind(disk, "/photos", filepath.Join(s.allowed, "photos-bind"))
	// A btrfs subvolume: st_dev in mountinfo is anonymous, the device is
	// found through the mount source.
	btr := s.addVolume("btr", "99999999-aaaa-4bbb-8ccc-000000000009", "btrfs", func(m *fakeMount) {
		m.ShownMajMin, m.Root = "0:45", "/@data"
	})
	must(t, os.WriteFile(filepath.Join(s.dev, btr.Dev), nil, 0o600))
	e := s.engine()

	// A path inside the bound folder is served by the bind mount (the
	// most specific mount of that part of the filesystem) ...
	must(t, os.MkdirAll(filepath.Join(bind.MountPoint, "2024"), 0o755))
	if r := resolveOK(t, e, ep(disk, "photos/2024")); r.Root != filepath.Join(bind.MountPoint, "2024") || r.MountID != bind.ID {
		t.Fatalf("bind: %+v", r)
	}
	// ... anything else by the whole-filesystem mount.
	if r := resolveOK(t, e, ep(disk, "music")); r.Root != filepath.Join(disk.MountPoint, "music") || r.MountID != disk.ID {
		t.Fatalf("disk: %+v", r)
	}
	// Losing the bind mount falls back to the full mount.
	s.unmount(bind)
	if r := resolveOK(t, e, ep(disk, "photos/2024")); r.Root != filepath.Join(disk.MountPoint, "photos", "2024") {
		t.Fatalf("after unbind: %+v", r)
	}

	// Subvolume: its sub-paths are filesystem paths ("@data/...").
	if r := resolveOK(t, e, ep(btr, "@data/docs")); !r.Online || r.Root != filepath.Join(btr.MountPoint, "docs") {
		t.Fatalf("subvolume: %+v", r)
	}
	// A different subvolume that isn't mounted anywhere is offline.
	if r := resolveOK(t, e, ep(btr, "@home")); r.Online {
		t.Fatalf("unmounted subvolume resolved: %+v", r)
	}
	// ResolvePath gives the same endpoint back.
	must(t, os.MkdirAll(filepath.Join(btr.MountPoint, "docs"), 0o755))
	res, err := e.ResolvePath(context.Background(), ResolvePathRequest{Path: filepath.Join(btr.MountPoint, "docs")})
	must(t, err)
	if !res.OK || res.Endpoint.Kind != EPVolume || res.Endpoint.RefID != btr.UUID || res.Endpoint.SubPath != "@data/docs" {
		t.Fatalf("ResolvePath = %+v %+v", res, res.Endpoint)
	}
}

func TestResolveMergePool(t *testing.T) {
	s := newTestSys(t)
	b1, b2 := filepath.Join(s.root, "b1"), filepath.Join(s.root, "b2")
	must(t, os.MkdirAll(b1, 0o755))
	must(t, os.MkdirAll(b2, 0o755))
	pool := s.addMerge("pool", b1, b2)
	e := s.engine()
	r := resolveOK(t, e, Endpoint{Kind: EPMerge, RefID: pool, SubPath: "media"})
	if !r.Online || r.Root != filepath.Join(pool, "media") || r.Free == nil {
		t.Fatalf("merge = %+v", r)
	}
	if len(r.Quirks) != 1 || r.Quirks[0] != QuirkPerBranchFree {
		t.Errorf("quirks = %v", r.Quirks)
	}
	if got := mergeBranches(mountEntry{Source: b1 + "=RW:" + b2 + "=NC"}); len(got) != 2 || got[0] != b1 || got[1] != b2 {
		t.Errorf("branches = %v", got)
	}
	// A folder that is not a pool is not a merge endpoint.
	if r := resolveOK(t, e, Endpoint{Kind: EPMerge, RefID: filepath.Join(s.allowed, "nothing")}); r.Online {
		t.Fatal("a non-pool resolved as a merge pool")
	}
	res, err := e.ResolvePath(context.Background(), ResolvePathRequest{Path: filepath.Join(pool, "media")})
	must(t, err)
	if !res.OK || res.Endpoint.Kind != EPMerge || res.Endpoint.RefID != pool || res.Endpoint.SubPath != "media" {
		t.Fatalf("ResolvePath = %+v", res.Endpoint)
	}
}

func TestResolveUSBMatchAndClones(t *testing.T) {
	s := newTestSys(t)
	usb := s.addVolume("stick", "3A4F-1C22", "exfat", func(m *fakeMount) { m.USB = true })
	e := s.engine()
	if r := resolveOK(t, e, ep(usb, "")); !r.Online {
		t.Fatal("USB drive did not resolve")
	}
	// Same UUID, other serial: another stick with a cloned filesystem.
	other := ep(usb, "")
	other.Match = &DevMatch{Serial: "SOMEONEELSE", SizeBytes: usb.Size}
	if r := resolveOK(t, e, other); r.Online {
		t.Fatal("a different drive with the same UUID was accepted")
	}
	// Two drives with that UUID plugged in at once, and no serial to tell
	// them apart: ambiguous.
	s.addVolume("clone", "3A4F-1C22", "exfat", func(m *fakeMount) { m.USB, m.Serial = true, "CLONESERIAL" })
	noMatch := ep(usb, "")
	noMatch.Match = nil
	if c := resolveCode(t, e, noMatch); c != CodeAmbiguousDevice {
		t.Fatalf("clone: %q, want ambiguous_device", c)
	}
	// The pinned serial still picks the right one.
	if r := resolveOK(t, e, ep(usb, "")); !r.Online || r.MountID != usb.ID {
		t.Fatalf("pinned serial: %+v", r)
	}
}

func TestResolveRejectsBadSubPaths(t *testing.T) {
	s := newTestSys(t)
	m := s.addVolume("v", "aaaaaaaa-aaaa-4bbb-8ccc-00000000000a", "ext4")
	e := s.engine()
	for _, sub := range []string{"../etc", "a/../../b", "/abs", "nul\x00byte"} {
		if c := resolveCode(t, e, ep(m, sub)); c != CodePathNotAllowed {
			t.Errorf("sub-path %q: %q, want path_not_allowed", sub, c)
		}
	}
	// A symlink leading off the drive.
	must(t, os.Symlink(s.root, filepath.Join(m.MountPoint, "out")))
	if c := resolveCode(t, e, ep(m, "out/x")); c != CodePathNotAllowed {
		t.Errorf("symlink escape: %q", c)
	}
	// Unknown kinds and empty UUIDs can never resolve.
	if c := resolveCode(t, e, Endpoint{Kind: "tape", RefID: "x"}); c != CodeEndpointUnknown {
		t.Errorf("unknown kind: %q", c)
	}
	if c := resolveCode(t, e, Endpoint{Kind: EPVolume}); c != CodeEndpointUnknown {
		t.Errorf("empty uuid: %q", c)
	}
	// A folder that is another drive's mount point belongs to that drive.
	inner := s.addVolume("inner", "bbbbbbbb-aaaa-4bbb-8ccc-00000000000b", "ext4", func(x *fakeMount) {
		x.MountPoint = filepath.Join(m.MountPoint, "nested")
	})
	_ = inner
	if c := resolveCode(t, e, ep(m, "nested/deeper")); c != CodePathNotAllowed {
		t.Errorf("path on a nested drive: %q", c)
	}
}

func TestResolveReadsTheMarker(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})
	expectState(t, runJob(t, e, baseReq(OpCopy, ep(src, "s"), ep(dst, "d"))), JobDone, "")
	r := resolveOK(t, e, ep(dst, "d"))
	if r.Marker == nil || r.Marker.DestFolderID != testFolderID || r.Marker.JobID != "bk_test0000001" || r.Marker.FSUUID != dst.UUID || len(r.Marker.Host) != 16 {
		t.Fatalf("marker = %+v", r.Marker)
	}
	fi, err := os.Stat(filepath.Join(dst.MountPoint, "d", MarkerFile))
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("marker mode %v, want 0600", fi.Mode().Perm())
	}
	// Someone else's file where the marker should be: mismatch, not a crash.
	must(t, os.WriteFile(filepath.Join(dst.MountPoint, "d", MarkerFile), []byte("not json"), 0o600))
	if c := resolveCode(t, e, ep(dst, "d")); c != CodeDestMarkerMismatch {
		t.Fatalf("foreign marker: %q", c)
	}
}

func TestResolvePathMapsCloudMounts(t *testing.T) {
	s := newTestSys(t)
	cloudRoot := filepath.Join(s.root, "cloudroot")
	must(t, os.MkdirAll(cloudRoot, 0o755))
	s.writeRcloneConf("[drive_gd]\ntype = alias\nremote = " + cloudRoot + "\nusername = My Drive\nmount_point = /mnt/drive_gd\n")
	e := s.engine()
	res, err := e.ResolvePath(context.Background(), ResolvePathRequest{Path: "/mnt/drive_gd/Photos/2024"})
	must(t, err)
	if !res.OK || res.Endpoint.Kind != EPCloud || res.Endpoint.RefID != "drive_gd" || res.Endpoint.SubPath != "Photos/2024" || res.Endpoint.Label != "My Drive" {
		t.Fatalf("ResolvePath = %+v %+v", res, res.Endpoint)
	}
	// The remote resolves through rclone, never the FUSE mount.
	r := resolveOK(t, e, *res.Endpoint)
	if !r.Online || r.Root != "drive_gd:Photos/2024" {
		t.Fatalf("cloud resolve = %+v", r)
	}
	for _, p := range []string{"relative/path", "/proc/1", filepath.Join(s.root, "spool")} {
		res, err := e.ResolvePath(context.Background(), ResolvePathRequest{Path: p})
		must(t, err)
		if res.OK || res.Reason != CodePathNotAllowed {
			t.Errorf("ResolvePath(%q) = %+v, want path_not_allowed", p, res)
		}
	}
	// An account that isn't in the config can never resolve.
	if c := resolveCode(t, e, Endpoint{Kind: EPCloud, RefID: "gone"}); c != CodeEndpointUnknown {
		t.Fatalf("unknown remote: %q", c)
	}
}

func TestResolveSMBNeedsCredentials(t *testing.T) {
	s := newTestSys(t)
	e := s.engine()
	_, err := e.Resolve(context.Background(), ResolveRequest{Endpoint: Endpoint{Kind: EPSMB, RefID: "3"}})
	if CodeOf(err) != CodeEndpointUnknown {
		t.Fatalf("no creds: %v", err)
	}
	c := SMBCreds{Host: "192.0.2.1", Share: "backup", User: "u", Password: "s3cret-pass"}
	opts, err := smbFsOptions(c)
	must(t, err)
	if opts["pass"] == "" || opts["pass"] == c.Password {
		t.Fatalf("password is not obscured: %q", opts["pass"])
	}
	if name := smbFsName(c); strings.Contains(name, c.Password) || !strings.HasPrefix(name, ":smb{") {
		t.Fatalf("fs name %q", name)
	}
	if got := redact("login to 192.0.2.1 with s3cret-pass failed", &c); strings.Contains(got, c.Password) {
		t.Fatalf("redact = %q", got)
	}
	if d := smbDisplay(c, "sub"); d != "smb://192.0.2.1/backup/sub" {
		t.Fatalf("display = %q", d)
	}
}

// Under the service unit's sandbox (ProtectSystem=strict with
// ReadWritePaths=/DATA ...), systemd binds each allowed root onto itself.
// That hides the original mounts below it, and propagation adds visible
// copies under the bind: the table then lists every data drive twice.
// Only the visible copy may serve a path, or every write would be
// refused as "on another drive".
func TestResolveIgnoresShadowedMounts(t *testing.T) {
	s := newTestSys(t)
	disk := s.addVolume("disk", "12121212-aaaa-4bbb-8ccc-000000000012", "ext4")
	s.mu.Lock()
	s.extra = append(s.extra,
		fmt.Sprintf("900 1 0:1 /allowed %s rw,relatime shared:2 - tmpfs tmpfs rw", escapeMI(s.allowed)),
		fmt.Sprintf("901 900 %s / %s rw,relatime shared:1 - ext4 /dev/%s rw", disk.MajMin, escapeMI(disk.MountPoint), disk.Dev),
	)
	s.mu.Unlock()
	s.write()
	e := s.engine()

	r := resolveOK(t, e, ep(disk, "photos"))
	if !r.Online || r.MountID != 901 || r.Root != filepath.Join(disk.MountPoint, "photos") {
		t.Fatalf("resolved to the hidden mount or not at all: %+v", r)
	}
	vols, err := e.Volumes(context.Background())
	must(t, err)
	for _, v := range vols {
		if v.MountID == disk.ID {
			t.Fatalf("the shadowed mount %d is listed as a volume: %+v", disk.ID, vols)
		}
	}
	locs, err := e.Locations(context.Background())
	must(t, err)
	n := 0
	for _, l := range locs {
		if l.RefID == disk.UUID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the drive is listed %d times: %+v", n, locs)
	}
}

func TestMountTableShadowed(t *testing.T) {
	// The real sandbox layout, trimmed (IDs as systemd produced them).
	raw := strings.Join([]string{
		"1863 1800 8:2 / / ro,relatime - ext4 /dev/sda2 rw",
		"2105 1863 8:33 / /DATA/tank rw,relatime - ext4 /dev/sdc1 rw",
		"2159 1863 8:2 /DATA /DATA rw,relatime - ext4 /dev/sda2 rw",
		"2160 2159 8:33 / /DATA/tank rw,relatime - ext4 /dev/sdc1 rw",
		"2161 2160 0:50 / /DATA/tank/sub rw,relatime - tmpfs tmpfs rw",
		"1865 1863 8:2 /media /media rw,relatime - ext4 /dev/sda2 rw",
	}, "\n") + "\n"
	entries, err := parseMountInfo(strings.NewReader(raw))
	must(t, err)
	tbl := newMountTable(entries)
	want := map[int]bool{1863: false, 2105: true, 2159: false, 2160: false, 2161: false, 1865: false}
	for i, m := range tbl.entries {
		if got := tbl.shadowed(i); got != want[m.ID] {
			t.Errorf("mount %d at %s: shadowed=%v, want %v", m.ID, m.MountPoint, got, want[m.ID])
		}
	}
}
