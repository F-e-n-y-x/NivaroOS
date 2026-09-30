package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMountinfo(t *testing.T, s string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	old := MountinfoPath
	MountinfoPath = p
	t.Cleanup(func() { MountinfoPath = old })
}

func TestTopMountDeviceStacked(t *testing.T) {
	writeMountinfo(t, `22 1 8:1 / / rw - ext4 /dev/sda1 rw
40 22 0:51 / /mnt/g rw - fuse.rclone g: rw
41 40 0:57 / /mnt/g rw - fuse.rclone g: rw
42 22 0:60 / /mnt/with\040space rw - fuse.rclone s: rw
`)
	if dev, ok := TopMountDevice("/mnt/g"); !ok || dev != "0:57" {
		t.Fatalf("top of /mnt/g = %q %v, want 0:57", dev, ok)
	}
	if dev, ok := TopMountDevice("/mnt/with space"); !ok || dev != "0:60" {
		t.Fatalf("escaped path = %q %v", dev, ok)
	}
	if _, ok := TopMountDevice("/mnt/none"); ok {
		t.Fatal("nothing is mounted at /mnt/none")
	}
}

func TestStillOurs(t *testing.T) {
	writeMountinfo(t, "40 22 0:57 / /mnt/g rw - fuse.rclone g: rw\n")
	if !StillOurs("/mnt/g", "0:57") {
		t.Fatal("0:57 is on top")
	}
	// An older mount (0:51) finishing late must not unmount the new one.
	if StillOurs("/mnt/g", "0:51") {
		t.Fatal("0:51 is gone; a newer mount is at the path")
	}
	if StillOurs("/mnt/other", "0:51") {
		t.Fatal("nothing mounted: not ours")
	}
	if !StillOurs("/mnt/g", "") {
		t.Fatal("unknown device keeps the old by-path behaviour")
	}
}
