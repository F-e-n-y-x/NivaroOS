package service

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
)

func strp(s string) *string { return &s }

func blankEnv() GuardEnv {
	return GuardEnv{
		FstabMountPoints: map[string]bool{},
		FstabSources:     map[string]bool{},
		Aliases:          map[string][]string{},
		SystemSources:    map[string]bool{},
	}
}

var (
	systemDisk = model.LSBLKModel{Path: "/dev/nvme0n1", Type: "disk", Tran: "nvme", Children: []model.LSBLKModel{
		{Path: "/dev/nvme0n1p1", Type: "part", FsType: "vfat", MountPoint: "/boot/efi"},
		{Path: "/dev/nvme0n1p2", Type: "part", FsType: "ext4", MountPoint: "/"},
	}}
	blankDisk = model.LSBLKModel{Path: "/dev/sdb", Type: "disk", Tran: "sata"}
	dataDisk  = model.LSBLKModel{Path: "/dev/sdc", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{
		{Path: "/dev/sdc1", Type: "part", FsType: "ext4", UUID: "u-sdc1", PartUUID: "pu-sdc1", MountPoint: "/mnt/Storage_sdc1"},
	}}
)

func TestValidateBlockDevicePath(t *testing.T) {
	for _, p := range []string{"/dev/sda", "/dev/nvme0n1p2", "/dev/disk/by-id/ata-WDC_WD40EFRX-68N32N0_WD-WCC7K1234567", "/dev/mapper/vg-lv", "/dev/mmcblk0p1"} {
		if err := ValidateBlockDevicePath(p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range []string{"", "sda", "/dev/", "/dev/sda;reboot", "/dev/sda\nx", "/dev/../etc/passwd", "/dev/sda /mnt", "/dev/-rf", "/etc/passwd", "/dev/sda$(id)", "/dev//sda", "/dev/sda/"} {
		if err := ValidateBlockDevicePath(p); err == nil {
			t.Errorf("%q accepted", p)
		} else if !IsInvalidDeviceError(err) {
			t.Errorf("%q: wrong error type %T", p, err)
		}
	}
}

func TestFindBlockDevice(t *testing.T) {
	list := []model.LSBLKModel{systemDisk, dataDisk}
	d, n, ok := FindBlockDevice(list, "/dev/sdc1")
	if !ok || d.Path != "/dev/sdc" || n.Path != "/dev/sdc1" {
		t.Fatalf("got %v %v %v", d.Path, n.Path, ok)
	}
	if _, _, ok := FindBlockDevice(list, "/dev/sdz"); ok {
		t.Fatal("unlisted device found")
	}
}

func TestIsAvailableDisk(t *testing.T) {
	if !IsAvailableDisk(blankDisk, false) {
		t.Fatal("blank sata disk not available")
	}
	if IsAvailableDisk(blankDisk, true) {
		t.Fatal("fstab-managed disk available")
	}
	if IsAvailableDisk(dataDisk, false) || IsAvailableDisk(systemDisk, false) {
		t.Fatal("disk with filesystems available")
	}
	usb := model.LSBLKModel{Path: "/dev/sdd", Type: "disk", Tran: "usb"}
	if IsAvailableDisk(usb, false) {
		t.Fatal("usb disk offered for formatting")
	}
}

func expectRefused(t *testing.T, name string, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: allowed", name)
	}
	if !IsGuardError(err) {
		t.Fatalf("%s: not a guard error: %v", name, err)
	}
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("%s: message %q lacks %q", name, err.Error(), contains)
	}
}

func TestGuardRefusesSystemDiskForEveryOperation(t *testing.T) {
	for _, op := range []DiskOp{DiskOpCreate, DiskOpMount, DiskOpFormat, DiskOpUmount} {
		expectRefused(t, string(op), CheckDiskOperation(systemDisk, systemDisk, op, blankEnv()), "system disk")
		expectRefused(t, string(op)+" partition", CheckDiskOperation(systemDisk, systemDisk.Children[1], op, blankEnv()), "system disk")
	}
	// swap only
	swap := model.LSBLKModel{Path: "/dev/sde", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sde1", Type: "part", FsType: "swap", MountPoint: "[SWAP]"}}}
	expectRefused(t, "swap", CheckDiskOperation(swap, swap, DiskOpCreate, blankEnv()), "system disk")
	// lsblk shows no mount point, but the kernel mount table says / lives here
	hidden := model.LSBLKModel{Path: "/dev/sdf", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdf2", Type: "part", FsType: "btrfs"}}}
	env := blankEnv()
	env.SystemSources["/dev/sdf2"] = true
	expectRefused(t, "mountinfo root", CheckDiskOperation(hidden, hidden, DiskOpFormat, env), "system disk")
	// second mount in lsblk's mountpoints array
	multi := model.LSBLKModel{Path: "/dev/sdg", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdg1", Type: "part", FsType: "btrfs", MountPoint: "/mnt/x", MountPoints: []*string{strp("/mnt/x"), strp("/home")}}}}
	expectRefused(t, "mountpoints array", CheckDiskOperation(multi, multi, DiskOpUmount, blankEnv()), "system disk")
}

func TestGuardRefusesStackedStorage(t *testing.T) {
	cases := map[string]model.LSBLKModel{
		"LVM PV":   {Path: "/dev/sdb", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdb1", Type: "part", FsType: "LVM2_member"}}},
		"LVM LV":   {Path: "/dev/sdb", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdb1", Type: "part", Children: []model.LSBLKModel{{Path: "/dev/mapper/vg-lv", Type: "lvm"}}}}},
		"LUKS":     {Path: "/dev/sdb", Type: "disk", Tran: "sata", FsType: "crypto_LUKS"},
		"dm-crypt": {Path: "/dev/sdb", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdb1", Type: "part", Children: []model.LSBLKModel{{Path: "/dev/mapper/c", Type: "crypt"}}}}},
		"md":       {Path: "/dev/sdb", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/md0", Type: "raid1"}}},
		"md fs":    {Path: "/dev/sdb", Type: "disk", Tran: "sata", FsType: "linux_raid_member"},
		"ZFS":      {Path: "/dev/sdb", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdb1", Type: "part", FsType: "zfs_member"}}},
	}
	for name, d := range cases {
		for _, op := range []DiskOp{DiskOpCreate, DiskOpFormat, DiskOpUmount} {
			expectRefused(t, name+"/"+string(op), CheckDiskOperation(d, d, op, blankEnv()), "")
		}
	}
}

func TestGuardRefusesFstabDataPoolAndVM(t *testing.T) {
	env := blankEnv()
	env.FstabMountPoints["/mnt/Storage_sdc1"] = true
	expectRefused(t, "fstab mount point", CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpFormat, env), "Persistent Mounts")

	env = blankEnv()
	env.FstabSources["UUID=u-sdc1"] = true
	unmounted := dataDisk
	unmounted.Children = []model.LSBLKModel{{Path: "/dev/sdc1", Type: "part", FsType: "ext4", UUID: "u-sdc1"}}
	expectRefused(t, "fstab source (unmounted)", CheckDiskOperation(unmounted, unmounted.Children[0], DiskOpFormat, env), "fstab")

	onData := model.LSBLKModel{Path: "/dev/sdh", Type: "disk", Tran: "sata", Children: []model.LSBLKModel{{Path: "/dev/sdh1", Type: "part", FsType: "ext4", MountPoint: "/DATA/Media"}}}
	for _, op := range []DiskOp{DiskOpFormat, DiskOpUmount} {
		expectRefused(t, "/DATA "+string(op), CheckDiskOperation(onData, onData.Children[0], op, blankEnv()), "/DATA/Media")
	}

	env = blankEnv()
	env.PoolBranches = []string{"/var/lib/nivaroos/files", "/mnt/Storage_sdc1"}
	expectRefused(t, "pool branch", CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpUmount, env), "pool")
	env.PoolBranches = []string{"/mnt/Storage_sdc1/share"}
	expectRefused(t, "pool branch inside", CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpFormat, env), "pool")
	env.PoolBranches = []string{"/mnt/Storage_sdc10"}
	if err := CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpUmount, env); err != nil {
		t.Fatalf("prefix-similar branch treated as member: %v", err)
	}

	env = blankEnv()
	env.Aliases["/dev/sdb"] = []string{"/dev/disk/by-id/ata-DISK_123"}
	env.VMDefinitions = []string{`<domain><devices><disk type='block'><source dev='/dev/disk/by-id/ata-DISK_123'/></disk></devices></domain>`}
	expectRefused(t, "VM passthrough", CheckDiskOperation(blankDisk, blankDisk, DiskOpCreate, env), "virtual machine")

	env = blankEnv()
	env.FstabLookupFailed = true
	expectRefused(t, "fstab unreadable", CheckDiskOperation(blankDisk, blankDisk, DiskOpCreate, env), "fstab")
}

func TestGuardPerOperationRules(t *testing.T) {
	env := blankEnv()
	if err := CheckDiskOperation(blankDisk, blankDisk, DiskOpCreate, env); err != nil {
		t.Fatalf("blank disk create refused: %v", err)
	}
	expectRefused(t, "create on used disk", CheckDiskOperation(dataDisk, dataDisk, DiskOpCreate, env), "available disks")
	expectRefused(t, "create on partition", CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpCreate, env), "whole disk")

	if err := CheckDiskOperation(dataDisk, dataDisk.Children[0], DiskOpFormat, env); err != nil {
		t.Fatalf("format of a data partition refused: %v", err)
	}
	usb := model.LSBLKModel{Path: "/dev/sdi", Type: "disk", Tran: "usb", Children: []model.LSBLKModel{{Path: "/dev/sdi1", Type: "part", FsType: "vfat", MountPoint: "/media/USB"}}}
	expectRefused(t, "format usb", CheckDiskOperation(usb, usb.Children[0], DiskOpFormat, env), "supported")
	if err := CheckDiskOperation(usb, usb.Children[0], DiskOpUmount, env); err != nil {
		t.Fatalf("umount of a usb storage refused: %v", err)
	}

	expectRefused(t, "mount while mounted", CheckDiskOperation(dataDisk, dataDisk, DiskOpMount, env), "already mounted")
	if err := CheckDiskOperation(dataDisk, dataDisk, DiskOpUmount, env); err != nil {
		t.Fatalf("umount of /mnt storage refused: %v", err)
	}
}

func TestParseSwapAndMergerFSBranches(t *testing.T) {
	swaps := ParseSwapDevices("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/dev/sda3                               partition\t8388604\t\t0\t\t-2\n/swapfile file 1 0 -3\n")
	if !swaps["/dev/sda3"] || len(swaps) != 1 {
		t.Fatalf("swaps = %v", swaps)
	}
	got := ParseMergerFSBranches([]string{"/var/lib/nivaroos/files=RW", "/mnt/a/", " /mnt/b=NC ", ""})
	want := []string{"/var/lib/nivaroos/files", "/mnt/a", "/mnt/b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("branches = %v", got)
	}
}

// Two formats of the same disk could both pass the unlocked map check.
func TestBusySetIsAtomic(t *testing.T) {
	b := NewBusySet()
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.TryAcquire("/dev/sdb", "format") {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d concurrent acquisitions succeeded", wins)
	}
	if !b.IsBusy("/dev/sdb") || b.TryAcquire("/dev/sdb", "x") {
		t.Fatal("lock not held")
	}
	b.Release("/dev/sdb")
	if !b.TryAcquire("/dev/sdb", "x") {
		t.Fatal("lock not released")
	}
}

func TestValidateMountRequest(t *testing.T) {
	listed := []model.LSBLKModel{dataDisk}
	if err := ValidateMountRequest(listed, "/dev/sdc1", "/mnt/Storage_sdc1"); err != nil {
		t.Fatalf("valid request refused: %v", err)
	}
	bad := [][2]string{
		{"/dev/sdz1", "/mnt/x"},                 // not listed
		{"/dev/sdc1;id", "/mnt/x"},              // injection in device
		{"/dev/sdc1", "/mnt/x; reboot"},         // injection in mount point
		{"/dev/sdc1", "/mnt/$(touch /tmp/pwn)"}, // command substitution
		{"/dev/sdc1", "/etc"},                   // outside roots
		{"/dev/sdc1", "/mnt/../etc/cron.d"},     // traversal
	}
	for _, c := range bad {
		if err := ValidateMountRequest(listed, c[0], c[1]); err == nil {
			t.Errorf("%q %q accepted", c[0], c[1])
		}
	}
}

// The helper is run as `bash helper.sh fn arg...` - arguments are never
// joined into a shell string.
func TestHelperCommandKeepsArgumentsSeparate(t *testing.T) {
	cmd := HelperCommand(context.Background(), "/usr/share/nivaroos/shell", "do_mount", "/dev/sdb1", "/mnt/a b; reboot")
	want := []string{"bash", "/usr/share/nivaroos/shell/local-storage-helper.sh", "do_mount", "/dev/sdb1", "/mnt/a b; reboot"}
	if strings.Join(cmd.Args, "|") != strings.Join(want, "|") {
		t.Fatalf("argv = %q", cmd.Args)
	}
	if _, err := RunHelper("rm -rf /"); err == nil {
		t.Fatal("non-allow-listed helper function ran")
	}
}

// The script's own dispatcher refuses unknown functions and validates
// do_mount's arguments before touching anything (these calls never mount).
func TestHelperScriptDispatcher(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	script := "../build/sysroot/usr/share/nivaroos/shell/local-storage-helper.sh"
	run := func(args ...string) (string, int) {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	if _, code := run("GetDeviceTree;id"); code != 2 {
		t.Fatalf("unknown function: exit %d", code)
	}
	if out, code := run("do_mount", "/dev/null", "/mnt/x"); code == 0 || !strings.Contains(out, "not a block device") {
		t.Fatalf("char device accepted: %d %s", code, out)
	}
	if out, code := run("do_mount"); code == 0 || !strings.Contains(out, "usage") {
		t.Fatalf("missing args accepted: %d %s", code, out)
	}
}

func TestLSBLKMountPointsArrayParses(t *testing.T) {
	list, err := ParseBlockDevices([]byte(`{"blockdevices":[{"path":"/dev/sda","type":"disk","mountpoint":null,"mountpoints":[null],"children":[{"path":"/dev/sda1","type":"part","mountpoint":"/mnt/a","mountpoints":["/mnt/a","/home"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeMountPoints(list[0].Children[0]); strings.Join(got, ",") != "/mnt/a,/home" {
		t.Fatalf("mount points = %v", got)
	}
	if len(NodeMountPoints(list[0])) != 0 {
		t.Fatal("null mountpoint counted")
	}
	if !diskHoldsSystem(list[0]) {
		t.Fatal("/home in mountpoints array not seen as system")
	}
}
