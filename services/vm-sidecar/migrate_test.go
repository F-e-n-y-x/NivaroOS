package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A VM exactly as older versions created it: disk and raw UEFI vars loose
// in /DATA/VMs, installer CD in the old lowercase isos/ folder.
const legacyVMXML = `<domain type='kvm'>
  <name>mint</name>
  <os>
    <loader readonly='yes' type='pflash' format='raw'>/usr/share/OVMF/OVMF_CODE_4M.fd</loader>
    <nvram template='/usr/share/OVMF/OVMF_VARS_4M.fd' templateFormat='raw' format='raw'>/DATA/VMs/mint_VARS.fd</nvram>
  </os>
  <devices>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='/DATA/VMs/isos/mint.iso'/>
      <target dev='sda' bus='sata'/>
    </disk>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='/DATA/VMs/mint.qcow2'/>
      <target dev='sdb' bus='sata'/>
    </disk>
    <disk type='file' device='disk'>
      <source file='/mnt/other/data.qcow2'/>
      <target dev='sdc' bus='sata'/>
    </disk>
  </devices>
</domain>`

func existsIn(files ...string) func(string) bool {
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	return func(p string) bool { return set[p] }
}

func TestAnOldFlatVMMovesIntoItsOwnFolder(t *testing.T) {
	exists := existsIn("/DATA/VMs/mint.qcow2", "/DATA/VMs/mint_VARS.fd", "/DATA/VMs/isos/mint.iso", "/mnt/other/data.qcow2")
	out, ops := planLayoutMigration("mint", legacyVMXML, "/DATA/VMs", "/DATA/VMs/ISOs", exists)

	for _, want := range []string{
		"<source file='/DATA/VMs/mint/mint.qcow2'",
		"<source file='/DATA/VMs/ISOs/mint.iso'",
		"format='qcow2'>/DATA/VMs/mint/mint_VARS.qcow2</nvram>",
		"templateFormat='raw'",
		// A disk that lives elsewhere (another drive) is left alone.
		"<source file='/mnt/other/data.qcow2'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("migrated XML lacks %q:\n%s", want, out)
		}
	}
	got := map[string]string{}
	for _, op := range ops {
		got[op.kind+" "+op.from] = op.to
	}
	if got["move /DATA/VMs/mint.qcow2"] != "/DATA/VMs/mint/mint.qcow2" ||
		got["move /DATA/VMs/isos/mint.iso"] != "/DATA/VMs/ISOs/mint.iso" ||
		got["convert-nvram /DATA/VMs/mint_VARS.fd"] != "/DATA/VMs/mint/mint_VARS.qcow2" || len(ops) != 3 {
		t.Fatalf("ops: %+v", ops)
	}
}

func TestAMigratedVMIsLeftAlone(t *testing.T) {
	exists := existsIn("/DATA/VMs/mint.qcow2", "/DATA/VMs/mint_VARS.fd", "/DATA/VMs/isos/mint.iso")
	first, _ := planLayoutMigration("mint", legacyVMXML, "/DATA/VMs", "/DATA/VMs/ISOs", exists)
	after := existsIn("/DATA/VMs/mint/mint.qcow2", "/DATA/VMs/mint/mint_VARS.qcow2", "/DATA/VMs/ISOs/mint.iso")
	second, ops := planLayoutMigration("mint", first, "/DATA/VMs", "/DATA/VMs/ISOs", after)
	if len(ops) != 0 || second != first {
		t.Fatalf("second run should do nothing, got %+v", ops)
	}
}

// If an ISO of that name already sits in ISOs/, the CD is just repointed;
// a file that's missing on disk isn't planned at all.
func TestCDAlreadyInISOsIsOnlyRepointed(t *testing.T) {
	exists := existsIn("/DATA/VMs/ISOs/mint.iso")
	out, ops := planLayoutMigration("mint", legacyVMXML, "/DATA/VMs", "/DATA/VMs/ISOs", exists)
	if len(ops) != 0 {
		t.Fatalf("nothing to move, got %+v", ops)
	}
	if !strings.Contains(out, "/DATA/VMs/ISOs/mint.iso") || !strings.Contains(out, "<source file='/DATA/VMs/mint.qcow2'") {
		t.Fatalf("unexpected XML:\n%s", out)
	}
}

func TestOldFoldersAreTidiedWithoutLosingFiles(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "VMs")
	iso := filepath.Join(storage, "ISOs")
	legacy := filepath.Join(storage, "isos")
	shares := filepath.Join(root, "VM-Shares")
	for _, d := range []string{legacy, filepath.Join(storage, "Disks"), filepath.Join(storage, "Images"), iso, shares} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(legacy, "a.iso"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(legacy, "b.iso"), []byte("old b"), 0o644)
	os.WriteFile(filepath.Join(iso, "b.iso"), []byte("new b"), 0o644)
	os.WriteFile(filepath.Join(storage, "Images", "keep.img"), []byte("x"), 0o644)

	migrateLegacyDirs(storage, iso, shares)

	if b, _ := os.ReadFile(filepath.Join(iso, "a.iso")); string(b) != "a" {
		t.Fatal("a.iso wasn't moved into ISOs/")
	}
	if b, _ := os.ReadFile(filepath.Join(iso, "b.iso")); string(b) != "new b" {
		t.Fatal("an ISO in ISOs/ was overwritten")
	}
	if !fileExists(filepath.Join(legacy, "b.iso")) {
		t.Fatal("the conflicting old ISO was deleted")
	}
	if fileExists(filepath.Join(storage, "Disks")) || fileExists(shares) {
		t.Fatal("empty legacy folders should be removed")
	}
	if !fileExists(filepath.Join(storage, "Images", "keep.img")) {
		t.Fatal("a non-empty legacy folder must stay")
	}
}
