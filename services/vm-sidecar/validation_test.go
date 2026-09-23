package main

import (
	"encoding/xml"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

func TestXMLEscape(t *testing.T) {
	got := xmlEscape(`/a'b"<c>&d`)
	want := "/a&#39;b&#34;&lt;c&gt;&amp;d"
	if got != want {
		t.Fatalf("xmlEscape = %q, want %q", got, want)
	}
}

// Even if a hostile value got past validation, the rendered domain must
// stay one well-formed document with only the devices the template wrote.
func TestDomainTemplate_EscapesInterpolatedValues(t *testing.T) {
	evil := `/x.qcow2'/></disk><disk type='block' device='disk'><source dev='/dev/sda'/>`
	data := domainXMLData{
		Name: "vm", VCPUs: 1, MemoryMiB: 256,
		Disks:    []renderedDisk{{Path: evil, Target: "vda", Bus: "virtio"}},
		ISO:      &renderedISO{Path: evil},
		Networks: []renderedNIC{{Mode: "bridge", BridgeName: "br0'/><x", Model: "virtio", MAC: "a'", LinkState: "up"}},
		Share:    &shareDeviceData{Binary: "/b'", Dir: "/s'<", Tag: "share"},
		Firmware: "uefi", OVMFCodePath: "/c'", NVRAMPath: "/n<", NVRAMFormat: "qcow2'",
		UseOSBoot: true,
	}
	var buf strings.Builder
	if err := domainTemplate.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	var parsed domainXML
	if err := xml.Unmarshal([]byte(buf.String()), &parsed); err != nil {
		t.Fatalf("rendered XML doesn't parse: %v\n%s", err, buf.String())
	}
	if len(parsed.Devices.Disks) != 2 {
		t.Fatalf("expected exactly the data disk and the cdrom, got %d disks", len(parsed.Devices.Disks))
	}
	if parsed.Devices.Disks[0].Source.File != evil {
		t.Fatalf("expected the path to round-trip literally, got %q", parsed.Devices.Disks[0].Source.File)
	}
	if strings.Contains(buf.String(), "/dev/sda'") {
		t.Fatalf("unescaped injection in output:\n%s", buf.String())
	}
}

func TestValidateNIC(t *testing.T) {
	good := []NICSpec{
		{Mode: "nat"},
		{Mode: ""},
		{Mode: "bridge", BridgeName: "br0", Model: "e1000e", MAC: "52:54:00:AB:cd:01", LinkState: "down"},
	}
	for i, n := range good {
		if err := validateNIC(i, n); err != nil {
			t.Errorf("%+v: unexpected error %v", n, err)
		}
	}
	bad := []NICSpec{
		{Mode: "bridge"},
		{Mode: "bridge", BridgeName: "br0'/><x"},
		{Mode: "bridge", BridgeName: "averyveryverylongname"},
		{Mode: "direct"},
		{Model: "ne2k_pci"},
		{MAC: "52:54:00:ab:cd"},
		{MAC: "52:54:00:ab:cd:01'/>"},
		{LinkState: "sideways"},
	}
	for i, n := range bad {
		if err := validateNIC(i, n); err == nil || errorStatus(err, 0) != http.StatusBadRequest {
			t.Errorf("%+v: expected a 400 error, got %v", n, err)
		}
	}
}

func TestValidateResourcesAndFirmware(t *testing.T) {
	if err := validateResources(0, 512); err == nil {
		t.Error("expected vcpus=0 to be rejected")
	}
	if err := validateResources(1, 64); err == nil {
		t.Error("expected 64 MiB to be rejected")
	}
	if err := validateResources(1, 128); err != nil {
		t.Errorf("unexpected error for 1 vCPU / 128 MiB: %v", err)
	}
	if err := validateFirmware("coreboot"); err == nil {
		t.Error("expected unknown firmware to be rejected")
	}
}

// withStorageRoots points the disk and ISO roots at fresh temp dirs for
// one test.
func withStorageRoots(t *testing.T) (storage, isos string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storage, isos = filepath.Join(root, "VMs"), filepath.Join(root, "VMs", "ISOs")
	if err := os.MkdirAll(isos, 0755); err != nil {
		t.Fatal(err)
	}
	origStorage, origISO := defaultStorageDir, defaultISODir
	defaultStorageDir, defaultISODir = storage, isos
	t.Cleanup(func() { defaultStorageDir, defaultISODir = origStorage, origISO })
	return storage, isos
}

func TestValidateDiskPath(t *testing.T) {
	storage, _ := withStorageRoots(t)
	origShare := defaultAutoShareDir
	defaultAutoShareDir = filepath.Join(storage, "share")
	t.Cleanup(func() { defaultAutoShareDir = origShare })
	outside := t.TempDir()

	if got, err := validateDiskPath(filepath.Join(storage, "new.qcow2")); err != nil || got != filepath.Join(storage, "new.qcow2") {
		t.Errorf("new disk inside storage: got %q, %v", got, err)
	}
	if _, err := validateDiskPath(filepath.Join(storage, "sub", "deeper", "new.qcow2")); err != nil {
		t.Errorf("not-yet-existing subdirectory inside storage: %v", err)
	}
	for _, p := range []string{
		"/dev/sda",
		"/etc/passwd",
		storage,
		filepath.Join(storage, "..", "escape.qcow2"),
		"relative.qcow2",
		filepath.Join(outside, "x.qcow2"),
		filepath.Join(defaultISODir, "x.qcow2"),
		filepath.Join(storage, "share", "planted.qcow2"),
	} {
		if _, err := validateDiskPath(p); err == nil {
			t.Errorf("%q: expected rejection", p)
		}
	}

	// A symlink inside storage pointing out of it is judged by its target.
	link := filepath.Join(storage, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDiskPath(filepath.Join(link, "x.qcow2")); err == nil {
		t.Error("expected a symlink escaping storage to be rejected")
	}
	dangling := filepath.Join(storage, "dangling.qcow2")
	if err := os.Symlink(filepath.Join(outside, "nothing-here"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDiskPath(dangling); err == nil {
		t.Error("expected a dangling symlink to be rejected")
	}
}

func TestValidateISOPath(t *testing.T) {
	storage, isos := withStorageRoots(t)
	if _, err := validateISOPath(filepath.Join(isos, "debian.iso")); err != nil {
		t.Errorf("ISO inside the ISO dir: %v", err)
	}
	for _, p := range []string{filepath.Join(storage, "vm.qcow2"), "/dev/sr0", "/etc/shadow", filepath.Join(isos, "..", "x.iso")} {
		if _, err := validateISOPath(p); err == nil {
			t.Errorf("%q: expected rejection", p)
		}
	}
}

func TestAutoDiskPath_SkipsExistingFiles(t *testing.T) {
	storage, _ := withStorageRoots(t)
	if err := os.MkdirAll(filepath.Join(storage, "vm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "vm", "vm.qcow2"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "vm", "vm-disk2.qcow2"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if got := autoDiskPath("vm", 0, nil); got != filepath.Join(storage, "vm", "vm-disk3.qcow2") {
		t.Errorf("index 0 with vm.qcow2 and vm-disk2 taken: got %q", got)
	}
	taken := map[string]bool{filepath.Join(storage, "vm", "vm-disk3.qcow2"): true}
	if got := autoDiskPath("vm", 1, taken); got != filepath.Join(storage, "vm", "vm-disk4.qcow2") {
		t.Errorf("index 1 with vm-disk3 already chosen: got %q", got)
	}
}

func TestPlanNewDisks_RefusesExistingPathUnlessExplicit(t *testing.T) {
	storage, _ := withStorageRoots(t)
	existing := filepath.Join(storage, "old.qcow2")
	if err := os.WriteFile(existing, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := planNewDisks("vm", []DiskSpec{{Path: existing, GiB: 1}}, nil); errorStatus(err, 0) != http.StatusConflict {
		t.Fatalf("expected 409 for a new disk at an existing path, got %v", err)
	}
	planned, err := planNewDisks("vm", []DiskSpec{{Path: existing, Existing: true}}, nil)
	if err != nil || planned[0].Create {
		t.Fatalf("expected an explicit existing attach to be planned without create, got %+v, %v", planned, err)
	}
	if _, err := planNewDisks("vm", []DiskSpec{{Path: filepath.Join(storage, "missing.qcow2"), Existing: true}}, nil); err == nil {
		t.Fatal("expected an error attaching a missing existing image")
	}
	if _, err := planNewDisks("vm", []DiskSpec{{Path: "/dev/sda", GiB: 1}}, nil); errorStatus(err, 0) != http.StatusBadRequest {
		t.Fatalf("expected 400 for /dev/sda, got %v", err)
	}
}

func TestCreateVM_RejectsUnsafeInputs(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	base := CreateVMRequest{Name: "unsafe-vm", VCPUs: 1, MemoryMiB: 256, Disks: []DiskSpec{{Path: dir + "/d.qcow2", GiB: 1}}}
	cases := map[string]func(r *CreateVMRequest){
		"block device disk": func(r *CreateVMRequest) { r.Disks = []DiskSpec{{Path: "/dev/sda", GiB: 1}} },
		"ISO outside dir":   func(r *CreateVMRequest) { r.ISOPath = "/etc/passwd" },
		"bad MAC":           func(r *CreateVMRequest) { r.Networks = []NICSpec{{Mode: "nat", MAC: "x'/>"}} },
		"bad model":         func(r *CreateVMRequest) { r.Networks = []NICSpec{{Mode: "nat", Model: "virtio'/>"}} },
		"bad bridge":        func(r *CreateVMRequest) { r.Networks = []NICSpec{{Mode: "bridge", BridgeName: "br0\nup reboot"}} },
		"zero vcpus":        func(r *CreateVMRequest) { r.VCPUs = 0 },
		"custom share":      func(r *CreateVMRequest) { r.SharedFolders = []SharedFolderSpec{{SourceDir: dir, TargetTag: "share"}} },
	}
	for name, mutate := range cases {
		req := base
		mutate(&req)
		if _, err := store.CreateVM(req); errorStatus(err, 0) != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %v", name, err)
		}
	}
	if _, err := os.Stat(dir + "/d.qcow2"); !os.IsNotExist(err) {
		t.Errorf("a rejected create must not leave a disk behind (stat: %v)", err)
	}
	if _, err := store.CreateVM(CreateVMRequest{Name: "test", VCPUs: 1, MemoryMiB: 256}); errorStatus(err, 0) != http.StatusConflict {
		t.Errorf("duplicate name: expected 409, got %v", err)
	}
}

func createStoppedVM(t *testing.T, store *LibvirtStore, req CreateVMRequest) {
	t.Helper()
	if _, err := store.CreateVM(req); err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	t.Cleanup(func() {
		_ = store.ForceOffVM(req.Name)
		_ = store.DeleteVM(req.Name, true)
	})
	if err := store.ForceOffVM(req.Name); err != nil {
		t.Fatalf("ForceOffVM: %v", err)
	}
}

func domainUUID(t *testing.T, store *LibvirtStore, name string) string {
	t.Helper()
	dom, err := store.lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	defer dom.Free()
	uuid, err := dom.GetUUIDString()
	if err != nil {
		t.Fatal(err)
	}
	return uuid
}

func TestUpdateVM_KeepsUUIDBootOrderAndSurvivesDefineFailure(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	disk := dir + "/keep.qcow2"
	createStoppedVM(t, store, CreateVMRequest{
		Name: "keep-vm", VCPUs: 1, MemoryMiB: 256,
		Disks: []DiskSpec{{Path: disk, GiB: 1}}, Networks: []NICSpec{{Mode: "nat"}},
		BootOrder: []string{"vda", "cdrom"},
	})
	uuid := domainUUID(t, store, "keep-vm")

	vm, err := store.UpdateVM("keep-vm", UpdateVMRequest{VCPUs: 2, MemoryMiB: 512, Disks: []DiskSpec{{Path: disk}}, Networks: []NICSpec{{Mode: "nat"}}})
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if got := domainUUID(t, store, "keep-vm"); got != uuid {
		t.Errorf("UUID changed across update: %s -> %s", uuid, got)
	}
	if strings.Join(vm.BootOrder, ",") != "vda,cdrom" {
		t.Errorf("expected boot order to be kept when omitted, got %v", vm.BootOrder)
	}

	// A define libvirt itself rejects (a multicast MAC passes the format
	// check) must leave the VM defined and unchanged - it used to be
	// undefined first, so this deleted it.
	if _, err := store.UpdateVM("keep-vm", UpdateVMRequest{VCPUs: 4, MemoryMiB: 256, Disks: []DiskSpec{{Path: disk}}, Networks: []NICSpec{{Mode: "nat", MAC: "01:00:00:00:00:01"}}}); err == nil {
		t.Fatal("expected libvirt to reject a multicast MAC")
	}
	if after, err := store.GetVM("keep-vm"); err != nil || after.VCPUs != 2 {
		t.Fatalf("VM must still exist, unchanged, after a failed update: %+v %v", after, err)
	}
}

func TestUpdateVM_RunningRejectsStoppedOnlyChangesWith409(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	disk := dir + "/live.qcow2"
	if _, err := store.CreateVM(CreateVMRequest{
		Name: "live-vm", VCPUs: 1, MemoryMiB: 256,
		Disks: []DiskSpec{{Path: disk, GiB: 1}}, Networks: []NICSpec{{Mode: "nat"}},
	}); err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	t.Cleanup(func() { _ = store.ForceOffVM("live-vm"); _ = store.DeleteVM("live-vm", true) })

	_, err := store.UpdateVM("live-vm", UpdateVMRequest{
		VCPUs: 1, MemoryMiB: 256, Disks: []DiskSpec{{Path: disk, GiB: 1}, {GiB: 1}},
	})
	if errorStatus(err, 0) != http.StatusConflict || !strings.Contains(err.Error(), "disks") {
		t.Fatalf("expected a 409 naming disks, got %v", err)
	}
	if err := store.DeleteVM("live-vm", false); errorStatus(err, 0) != http.StatusConflict {
		t.Fatalf("deleting a running VM: expected 409, got %v", err)
	}
}

func TestStoppedOnlyChanges(t *testing.T) {
	current := VM{VCPUs: 2, MemoryMiB: 1024, Firmware: "uefi", Disks: []DiskInfo{{Path: "/d", GiB: 10, Bus: "virtio"}}, BootOrder: []string{"vda"}}
	same := UpdateVMRequest{VCPUs: 2, MemoryMiB: 1024, Firmware: "uefi", Disks: []DiskSpec{{Path: "/d"}}, BootOrder: []string{"vda"}}
	if got := stoppedOnlyChanges(current, same); len(got) != 0 {
		t.Errorf("expected no changes, got %v", got)
	}
	changed := same
	changed.USBDevices = []USBDeviceSpec{{VendorID: "1a2c", ProductID: "212a"}}
	changed.DisplayWidth, changed.DisplayHeight = 1920, 1080
	got := strings.Join(stoppedOnlyChanges(current, changed), ",")
	if got != "USB devices,display resolution" {
		t.Errorf("got %q", got)
	}
}

func TestNICMatches(t *testing.T) {
	info := NICInfo{Mode: "bridge", BridgeName: "br0", Model: "virtio", MAC: "52:54:00:00:00:01", LinkState: "up"}
	if !nicMatches(info, NICSpec{Mode: "bridge", BridgeName: "br0", MAC: "52:54:00:00:00:01"}) {
		t.Error("expected defaults (model virtio, link up) to match")
	}
	if nicMatches(info, NICSpec{Mode: "nat", MAC: "52:54:00:00:00:01"}) {
		t.Error("mode change must not match")
	}
}

func TestParseBootOrder(t *testing.T) {
	var parsed domainXML
	doc := `<domain><devices>
		<disk device='disk'><target dev='vdb'/><boot order='2'/></disk>
		<disk device='cdrom'><target dev='sda'/><boot order='1'/></disk>
		<disk device='disk'><target dev='vda'/></disk>
		<interface type='network'><boot order='3'/></interface>
	</devices></domain>`
	if err := xml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(parseBootOrder(parsed), ","); got != "cdrom,vdb,network" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateVM_UEFIUsesQcow2NVRAMAndUpdateKeepsIt(t *testing.T) {
	if _, err := os.Stat(ovmfVarsTemplate); err != nil {
		t.Skipf("OVMF not installed on this machine (%v)", err)
	}
	store := newTestStore(t)
	dir := t.TempDir()
	disk := dir + "/nv.qcow2"
	createStoppedVM(t, store, CreateVMRequest{
		Name: "nv-vm", VCPUs: 1, MemoryMiB: 256, Firmware: "uefi",
		Disks: []DiskSpec{{Path: disk, GiB: 1}}, Networks: []NICSpec{{Mode: "nat"}},
	})
	readOS := func() domainXML {
		dom, err := store.lookup("nv-vm")
		if err != nil {
			t.Fatal(err)
		}
		defer dom.Free()
		x, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			t.Fatal(err)
		}
		var parsed domainXML
		if err := xml.Unmarshal([]byte(x), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	before := readOS()
	if before.OS.NVRAM.Format != "qcow2" || strings.TrimSpace(before.OS.NVRAM.Path) != nvramPathFor(vmDirFor("nv-vm"), "nv-vm") {
		t.Fatalf("expected qcow2 NVRAM at %s, got %+v", nvramPathFor(vmDirFor("nv-vm"), "nv-vm"), before.OS.NVRAM)
	}
	out, err := osexec.Command("qemu-img", "info", nvramPathFor(vmDirFor("nv-vm"), "nv-vm")).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "file format: qcow2") {
		t.Fatalf("NVRAM file isn't qcow2: %v %s", err, out)
	}

	// Firmware omitted: must stay UEFI with the very same NVRAM.
	vm, err := store.UpdateVM("nv-vm", UpdateVMRequest{VCPUs: 2, MemoryMiB: 256, Disks: []DiskSpec{{Path: disk}}, Networks: []NICSpec{{Mode: "nat"}}})
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	after := readOS()
	if vm.Firmware != "uefi" || after.OS.NVRAM != before.OS.NVRAM || after.UUID != before.UUID {
		t.Fatalf("firmware/NVRAM/UUID not preserved: %q %+v -> %+v", vm.Firmware, before.OS.NVRAM, after.OS.NVRAM)
	}
	if err := checkSnapshotNVRAM(mustXML(t, store, "nv-vm")); err != nil {
		t.Fatalf("a qcow2-NVRAM VM should be snapshot-able: %v", err)
	}
}

func mustXML(t *testing.T, store *LibvirtStore, name string) string {
	t.Helper()
	dom, err := store.lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	defer dom.Free()
	x, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestDeleteVM_WipeOnlyRemovesDisksInsideStorage(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	disk := dir + "/wipe.qcow2"
	createStoppedVM(t, store, CreateVMRequest{Name: "wipe-vm", VCPUs: 1, MemoryMiB: 256, Disks: []DiskSpec{{Path: disk, GiB: 1}}})

	// Move the storage root elsewhere: the VM's disk is now "outside".
	withStorageRoots(t)
	if err := store.DeleteVM("wipe-vm", true); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("a disk outside the storage dir must never be removed: %v", err)
	}
}

func TestUpdateNetworkAdapter_StoppedKeepsOtherSettings(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	createStoppedVM(t, store, CreateVMRequest{
		Name: "nic-vm", VCPUs: 1, MemoryMiB: 256, Disks: []DiskSpec{{Path: dir + "/n.qcow2", GiB: 1}},
		Networks: []NICSpec{{Mode: "nat"}}, DisplayWidth: 1280, DisplayHeight: 720,
		SharedFolders: []SharedFolderSpec{{SourceDir: defaultAutoShareDir, TargetTag: "share", ReadOnly: true}},
	})
	if err := store.UpdateNetworkAdapter("nic-vm", "", NICSpec{Mode: "bridge", BridgeName: "br7", Model: "e1000e"}); err != nil {
		t.Fatalf("UpdateNetworkAdapter: %v", err)
	}
	vm, err := store.GetVM("nic-vm")
	if err != nil {
		t.Fatal(err)
	}
	if len(vm.Networks) != 1 || vm.Networks[0].BridgeName != "br7" || vm.Networks[0].Model != "e1000e" {
		t.Errorf("adapter not updated: %+v", vm.Networks)
	}
	if vm.DisplayWidth != 1280 || len(vm.SharedFolders) != 1 || !vm.SharedFolders[0].ReadOnly {
		t.Errorf("other settings lost: display=%dx%d shares=%+v", vm.DisplayWidth, vm.DisplayHeight, vm.SharedFolders)
	}
	if err := store.UpdateNetworkAdapter("nic-vm", "", NICSpec{Mode: "nat", MAC: "zz"}); errorStatus(err, 0) != http.StatusBadRequest {
		t.Errorf("expected 400 for a bad MAC, got %v", err)
	}
}

func TestCreateVM_NewVMGetsItsOwnFolderRemovedOnWipe(t *testing.T) {
	storage, _ := withStorageRoots(t)
	store := newTestStore(t)
	createStoppedVM(t, store, CreateVMRequest{Name: "folder-vm", VCPUs: 1, MemoryMiB: 256, Disks: []DiskSpec{{GiB: 1}, {GiB: 1}}})
	vm, err := store.GetVM("folder-vm")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(storage, "folder-vm", "folder-vm.qcow2"), filepath.Join(storage, "folder-vm", "folder-vm-disk2.qcow2")}
	if len(vm.Disks) != 2 || vm.Disks[0].Path != want[0] || vm.Disks[1].Path != want[1] {
		t.Fatalf("expected disks %v, got %+v", want, vm.Disks)
	}
	if err := store.DeleteVM("folder-vm", true); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage, "folder-vm")); !os.IsNotExist(err) {
		t.Fatalf("expected the emptied VM folder to be removed, stat: %v", err)
	}
}
