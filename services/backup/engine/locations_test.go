package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func locByRef(locs []Location, ref string) (Location, bool) {
	for _, l := range locs {
		if l.RefID == ref {
			return l, true
		}
	}
	return Location{}, false
}

func TestLocationsListsEverythingPresent(t *testing.T) {
	s := newTestSys(t)
	sys := s.addVolume("system", "5y5-0000-aaaa-4bbb-8ccc-000000000030", "ext4", func(m *fakeMount) { m.MountPoint = s.root })
	tower := s.addVolume("tower", "70e7-0000-aaaa-4bbb-8ccc-000000000031", "ntfs3", func(m *fakeMount) { m.Options = "rw,umask=000" })
	s.bind(tower, "/", filepath.Join(s.allowed, "tower-bind")) // same filesystem, listed once
	stick := s.addVolume("stick", "3A4F-1C22", "exfat", func(m *fakeMount) { m.USB = true })
	b1 := filepath.Join(s.root, "branch1")
	must(t, os.MkdirAll(b1, 0o755))
	pool := s.addMerge("pool", b1)
	cloudRoot := filepath.Join(s.root, "cloud")
	must(t, os.MkdirAll(cloudRoot, 0o755))
	s.writeRcloneConf("[gd]\ntype = alias\nremote = " + cloudRoot + "\nusername = Family Drive\nmount_point = /mnt/alias_gd\n")
	data := filepath.Join(s.allowed, "DATA")
	for _, d := range []string{"Documents", "AppData/immich", "AppData/.hidden", "VMs/win11", "lost+found"} {
		must(t, os.MkdirAll(filepath.Join(data, d), 0o755))
	}
	e := s.engine()

	locs, err := e.Locations(context.Background())
	must(t, err)
	n := 0
	for _, l := range locs {
		if l.RefID == tower.UUID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tower listed %d times", n)
	}
	l, ok := locByRef(locs, tower.UUID)
	if !ok || l.Kind != EPVolume || l.MountPoint != tower.MountPoint || !l.Online || l.Free == nil || l.Removable || l.FSType != "ntfs3" {
		t.Fatalf("tower = %+v", l)
	}
	if !hasWarning(l.Warnings, WarnWorldWritable) {
		t.Errorf("umask=000 NTFS has no world-writable warning: %v", l.Warnings)
	}
	u, ok := locByRef(locs, stick.UUID)
	if !ok || u.Kind != EPUSB || !u.Removable || u.Match == nil || u.Match.Serial != stick.Serial || u.Match.SizeBytes != stick.Size {
		t.Fatalf("usb = %+v", u)
	}
	p, ok := locByRef(locs, pool)
	if !ok || p.Kind != EPMerge || p.FSType != fsTypeMergerFS {
		t.Fatalf("pool = %+v", p)
	}
	c, ok := locByRef(locs, "gd")
	if !ok || c.Kind != EPCloud || c.Label != "Family Drive" || c.Provider != "alias" || c.MountPoint != "/mnt/alias_gd" {
		t.Fatalf("cloud = %+v", c)
	}
	// The /DATA folders are presets of the volume holding them.
	root, ok := locByRef(locs, sys.UUID)
	if !ok {
		t.Fatal("the system volume is missing")
	}
	presets := map[string]string{}
	for _, pr := range root.Presets {
		presets[pr.ID] = pr.SubPath
	}
	rel, _ := filepath.Rel(s.root, data)
	for id, sub := range map[string]string{
		"data:Documents": rel + "/Documents", "appdata:immich": rel + "/AppData/immich", "vm:win11": rel + "/VMs/win11",
	} {
		if presets[id] != filepath.ToSlash(sub) {
			t.Errorf("preset %s = %q, want %q (all: %v)", id, presets[id], sub, presets)
		}
	}
	for _, bad := range []string{"appdata:.hidden", "data:lost+found"} {
		if _, ok := presets[bad]; ok {
			t.Errorf("preset %s should not be offered", bad)
		}
	}
	for _, loc := range locs {
		if loc.Quirks == nil || loc.Warnings == nil {
			t.Errorf("%s: nil quirks/warnings (must be [] in JSON)", loc.RefID)
		}
	}

	// A remote added by local-storage appears without a restart; one it
	// removed disappears.
	time.Sleep(10 * time.Millisecond) // a new modtime for the config file
	s.writeRcloneConf("[od]\ntype = alias\nremote = " + cloudRoot + "\n")
	locs, err = e.Locations(context.Background())
	must(t, err)
	if _, ok := locByRef(locs, "od"); !ok {
		t.Fatal("new remote not listed")
	}
	if _, ok := locByRef(locs, "gd"); ok {
		t.Fatal("removed remote still listed")
	}
	vols, err := e.Volumes(context.Background())
	must(t, err)
	if len(vols) != 5 { // system, tower, tower bind, stick, pool
		t.Fatalf("volumes = %+v", vols)
	}
}

func hasWarning(ws []Warning, w Warning) bool {
	for _, x := range ws {
		if x == w {
			return true
		}
	}
	return false
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err  error
		want ErrorCode
	}{
		{context.Canceled, CodeCancelledByUser},
		{context.DeadlineExceeded, CodeMaxDuration},
		{&os.PathError{Op: "write", Path: "/x", Err: syscall.ENOSPC}, CodeNoSpace},
		{&os.PathError{Op: "stat", Path: "/x", Err: syscall.ENOENT}, CodeIOError},
		{&os.PathError{Op: "open", Path: "/x", Err: syscall.EACCES}, CodeIOError},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, CodeNetworkUnreachable},
		{&net.DNSError{Err: "no such host", Name: "x"}, CodeNetworkUnreachable},
		{errors.New("HTTP error 429 (429 Too Many Requests)"), CodeCloudRateLimited},
		{errors.New("oauth2: cannot fetch token: 400 Bad Request invalid_grant"), CodeCloudAuth},
		{errors.New("googleapi: Error 403: The user's Drive storage quota has been exceeded., storageQuotaExceeded"), CodeNoSpace},
		{errors.New("failed to copy: upload limit exceeded"), CodeCloudQuotaDaily},
		{errors.New("--max-delete threshold reached"), CodeDeleteGuard},
		{errors.New("file 429.jpg: checksum mismatch"), CodeIOError},
		{Errorf(CodeCaseCollision, "x"), CodeCaseCollision},
		{fmt.Errorf("wrapped: %w", Errorf(CodeDestOffline, "gone")), CodeDestOffline},
	}
	for _, c := range cases {
		if got := classifyError(c.err); got != c.want {
			t.Errorf("classifyError(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
