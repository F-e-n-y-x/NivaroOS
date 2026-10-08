package service

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
)

// ntfsfix -n on the 2026-10-08 power-cut drive.
const ntfsfixDamaged = `Mounting volume... $MFTMirr does not match $MFT (record 3).
FAILED
Attempting to correct errors...
Processing $MFT and $MFTMirr...`

func TestClassifyDriveProblem(t *testing.T) {
	p := classify("tower", "ntfs", "sdb1", ntfsfixDamaged, errors.New("exit status 1"), "", "")
	if p.Reason != "damaged" || !p.Repairable || p.Detail != "$MFTMirr does not match $MFT (record 3)." {
		t.Fatalf("damaged: %+v", p)
	}
	klog := "[   13.5] ntfs3(sdb1): It is recommended to use chkdsk.\n[   13.5] ntfs3(sdb1): volume is dirty and \"force\" flag is not set!\n"
	p = classify("tower", "ntfs", "sdb1", "ok", nil, klog, "")
	if p.Reason != "dirty" || !p.Repairable || !strings.Contains(p.Message, "tower couldn't be mounted after an unclean shutdown") {
		t.Fatalf("dirty: %+v", p)
	}
	p = classify("media", "ext4", "sdc1", "", nil, "[1.0] EXT4-fs (sdc1): bad geometry\n", "")
	if p.Reason != "failed" || !p.Repairable || p.Detail != "EXT4-fs (sdc1): bad geometry" {
		t.Fatalf("ext4: %+v", p)
	}
	if p = classify("x", "xfs", "sdd1", "", nil, "", "mount: boom"); p.Repairable || p.Detail != "mount: boom" {
		t.Fatalf("xfs: %+v", p)
	}
}

func TestDriveProblemOnlyForDrivesThatShouldBeMounted(t *testing.T) {
	state, probes := "failed", 0
	oldState, oldRun, oldLog := mountUnitState, runTool, kernelLog
	defer func() { mountUnitState, runTool, kernelLog = oldState, oldRun, oldLog }()
	mountUnitState = func(string) string { return state }
	runTool = func(time.Duration, string, ...string) (string, error) { probes++; return "ok", nil }
	kernelLog = func() string { return "ntfs3(sdb1): volume is dirty" }

	e := &fstab.Entry{MountPoint: "/DATA/tower", FSType: "ntfs3", Options: "defaults,nofail", Managed: true, Enabled: true}
	blk := &model.LSBLKModel{Path: "/dev/sdb1", FsType: "ntfs"}

	if driveProblem(e, blk, true) != nil {
		t.Fatal("mounted drive has a problem")
	}
	if p := driveProblem(e, nil, false); p == nil || p.Reason != "missing" {
		t.Fatalf("missing drive: %+v", p)
	}
	if p := driveProblem(e, blk, false); p == nil || p.Reason != "dirty" {
		t.Fatalf("failed boot mount: %+v", p)
	}
	driveProblem(e, blk, false)
	if probes != 1 {
		t.Fatalf("ntfsfix -n ran %d times, want once (cached)", probes)
	}
	// Unmounted on purpose: the unit is inactive, our mount didn't fail.
	driveProblem(e, blk, true) // mounted again: forget the diagnosis
	state = "inactive"
	if p := driveProblem(e, blk, false); p != nil {
		t.Fatalf("drive unmounted on purpose: %+v", p)
	}
	setMountErr(e.MountPoint, "mount: wrong fs type")
	if p := driveProblem(e, blk, false); p == nil {
		t.Fatal("our failed mount not reported")
	}
	setMountErr(e.MountPoint, "")
	for _, x := range []*fstab.Entry{
		{MountPoint: "/a", Options: "nofail,noauto", Managed: true, Enabled: true},
		{MountPoint: "/b", Options: "nofail", Managed: true, Enabled: false},
		{MountPoint: "/c", Options: "nofail", Managed: false, Enabled: true},
	} {
		if driveProblem(x, nil, false) != nil {
			t.Fatalf("%s: not meant to be mounted at boot", x.MountPoint)
		}
	}
}

func TestRepairSteps(t *testing.T) {
	if s := repairSteps("ntfs", "/dev/sdb1"); len(s) != 2 || strings.Join(s[0], " ") != "ntfsfix /dev/sdb1" || strings.Join(s[1], " ") != "ntfsfix -d /dev/sdb1" {
		t.Fatalf("ntfs: %v", s)
	}
	if s := repairSteps("ext4", "/dev/sdc1"); len(s) != 1 || s[0][0] != "e2fsck" {
		t.Fatalf("ext4: %v", s)
	}
	if repairSteps("btrfs", "/dev/x") != nil {
		t.Fatal("btrfs repairable")
	}
	exit := func(code int) error { return exec.Command("sh", "-c", "exit "+string(rune('0'+code))).Run() }
	if !stepOK("e2fsck", exit(1)) || stepOK("e2fsck", exit(4)) || stepOK("ntfsfix", exit(1)) || !stepOK("ntfsfix", nil) {
		t.Fatal("exit codes")
	}
}

// The real thing on a loop device: an NTFS image left dirty and damaged
// the way the power cut left the owner's drive. ntfs3 refuses it, ntfs-3g
// too ($MFTMirr), Repair drive's steps fix it and ntfs3 mounts it.
// Root only, and opt-in: NIVAROOS_LOOP_TESTS=1.
func TestRepairDamagedNTFSOnLoopDevice(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("NIVAROOS_LOOP_TESTS") != "1" {
		t.Skip("needs root and NIVAROOS_LOOP_TESTS=1")
	}
	for _, tool := range []string{"mkntfs", "ntfsfix", "ntfs-3g", "losetup"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " missing")
		}
	}
	dir := t.TempDir()
	img, mnt := filepath.Join(dir, "drive.img"), filepath.Join(dir, "mnt")
	must := func(name string, args ...string) string {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	must("truncate", "-s", "64M", img)
	must("mkntfs", "-F", "-Q", "-L", "tower", img)
	must("ntfsfix", img) // leaves the volume marked dirty
	f, _ := os.OpenFile(img, os.O_RDWR, 0)
	boot := make([]byte, 512)
	f.ReadAt(boot, 0)
	cluster := int64(binary.LittleEndian.Uint16(boot[0x0B:])) * int64(boot[0x0D])
	off := int64(binary.LittleEndian.Uint64(boot[0x38:]))*cluster + 3*1024 + 0x10
	b := []byte{0}
	f.ReadAt(b, off)
	f.WriteAt([]byte{b[0] + 1}, off) // $MFTMirr record 3 no longer matches $MFT
	f.Close()
	os.Mkdir(mnt, 0o755)
	dev := strings.TrimSpace(must("losetup", "-f", "--show", img))
	defer exec.Command("losetup", "-d", dev).Run()
	defer exec.Command("umount", mnt).Run()

	if exec.Command("mount", "-t", "ntfs3", dev, mnt).Run() == nil {
		t.Fatal("ntfs3 mounted the dirty volume")
	}
	helper := "../build/sysroot/usr/share/nivaroos/shell/local-storage-helper.sh"
	if out, err := exec.Command("bash", helper, "mount_ntfs3g", dev, mnt, "defaults,noatime,nofail,uid=1000,gid=1000,umask=000,prealloc").CombinedOutput(); err == nil {
		t.Fatalf("ntfs-3g mounted the damaged volume: %s", out)
	}
	out, err := runTool(time.Minute, "ntfsfix", "-n", dev)
	if p := classify("tower", "ntfs", filepath.Base(dev), out, err, "", ""); p.Reason != "damaged" {
		t.Fatalf("diagnosis: %+v\n%s", p, out)
	}
	for _, s := range repairSteps("ntfs", dev) {
		if o, err := runTool(time.Minute, s[0], s[1:]...); !stepOK(s[0], err) {
			t.Fatalf("%v: %v\n%s", s, err, o)
		}
	}
	must("mount", "-t", "ntfs3", "-o", "noatime,uid=1000,gid=1000,umask=000", dev, mnt)
}

// A dirty (not damaged) volume: ntfs3 refuses it, the fallback mounts it
// through ntfs-3g with the ntfs3-only options dropped.
func TestNTFS3GFallbackMountsDirtyVolume(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("NIVAROOS_LOOP_TESTS") != "1" {
		t.Skip("needs root and NIVAROOS_LOOP_TESTS=1")
	}
	dir := t.TempDir()
	img, mnt := filepath.Join(dir, "drive.img"), filepath.Join(dir, "mnt")
	for _, c := range [][]string{{"truncate", "-s", "64M", img}, {"mkntfs", "-F", "-Q", img}, {"ntfsfix", img}} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Skipf("%v: %v %s", c, err, out)
		}
	}
	os.Mkdir(mnt, 0o755)
	out, _ := exec.Command("losetup", "-f", "--show", img).Output()
	dev := strings.TrimSpace(string(out))
	defer exec.Command("losetup", "-d", dev).Run()
	defer exec.Command("umount", mnt).Run()
	if exec.Command("mount", "-t", "ntfs3", dev, mnt).Run() == nil {
		t.Fatal("ntfs3 mounted the dirty volume")
	}
	helper := "../build/sysroot/usr/share/nivaroos/shell/local-storage-helper.sh"
	if out, err := exec.Command("bash", helper, "mount_ntfs3g", dev, mnt, "defaults,noatime,nofail,uid=1000,gid=1000,umask=000,iocharset=utf8,prealloc").CombinedOutput(); err != nil {
		t.Fatalf("fallback: %v %s", err, out)
	}
	if exec.Command("mountpoint", "-q", mnt).Run() != nil {
		t.Fatal("not mounted")
	}
}
