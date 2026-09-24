package main

import (
	"os"
	"path/filepath"
	"testing"
)

// No AMD/Intel hardware exists in CI/dev sandboxes to verify queryAMD()
// against - this builds a fake sysfs tree matching the real, documented
// amdgpu/hwmon kernel ABI layout instead, so the parsing and unit
// conversions (bytes->MiB, millidegrees->C, microwatts->W) are at least
// verified against realistic input, even without real hardware.
func TestQueryAMD(t *testing.T) {
	base := t.TempDir()
	card := filepath.Join(base, "card0")
	device := filepath.Join(card, "device")
	hwmon := filepath.Join(device, "hwmon", "hwmon3")
	if err := os.MkdirAll(hwmon, 0o755); err != nil {
		t.Fatal(err)
	}

	// A real PCI device dir would be a symlink to something like
	// /sys/devices/pci0000:00/.../0000:03:00.0 - just create a real dir
	// with that name and symlink device -> it, matching what
	// os.Readlink(.../device) expects to resolve.
	pciTarget := filepath.Join(base, "0000:03:00.0")
	if err := os.MkdirAll(pciTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(device); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pciTarget, device); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pciTarget, "hwmon", "hwmon3"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(pciTarget, "vendor"), "0x1002\n")
	writeFile(filepath.Join(pciTarget, "gpu_busy_percent"), "42\n")
	writeFile(filepath.Join(pciTarget, "mem_info_vram_used"), "2147483648\n")   // 2 GiB
	writeFile(filepath.Join(pciTarget, "mem_info_vram_total"), "8589934592\n") // 8 GiB
	writeFile(filepath.Join(pciTarget, "hwmon", "hwmon3", "temp1_input"), "55000\n")     // 55.0 C
	writeFile(filepath.Join(pciTarget, "hwmon", "hwmon3", "power1_average"), "45000000\n") // 45.0 W

	old := drmBasePath
	drmBasePath = base
	defer func() { drmBasePath = old }()

	dir, pciAddr, ok := findDRMCard(vendorAMD)
	if !ok {
		t.Fatalf("findDRMCard(amd) did not find the fake card")
	}
	if dir != card {
		t.Errorf("dir = %q, want %q", dir, card)
	}
	if pciAddr != "0000:03:00.0" {
		t.Errorf("pciAddr = %q, want %q", pciAddr, "0000:03:00.0")
	}

	stats, err := queryAMD()
	if err != nil {
		t.Fatalf("queryAMD() error = %v", err)
	}
	if stats.UtilizationPercent != 42 {
		t.Errorf("UtilizationPercent = %v, want 42", stats.UtilizationPercent)
	}
	if stats.MemoryUsedMiB != 2048 {
		t.Errorf("MemoryUsedMiB = %v, want 2048", stats.MemoryUsedMiB)
	}
	if stats.MemoryTotalMiB != 8192 {
		t.Errorf("MemoryTotalMiB = %v, want 8192", stats.MemoryTotalMiB)
	}
	if stats.TemperatureC != 55 {
		t.Errorf("TemperatureC = %v, want 55", stats.TemperatureC)
	}
	if stats.PowerDrawW == nil || *stats.PowerDrawW != 45 {
		t.Errorf("PowerDrawW = %v, want 45", stats.PowerDrawW)
	}
}

func TestQueryAMDNoCard(t *testing.T) {
	old := drmBasePath
	drmBasePath = t.TempDir() // empty - no cards at all
	defer func() { drmBasePath = old }()

	if _, err := queryAMD(); err == nil {
		t.Error("expected an error when no AMD card is present, got nil")
	}
}

// A vendor match alone isn't enough - queryAMD() must fail (not return
// fake/zero stats) when the card is present but gpu_busy_percent can't be
// read, which is exactly the amdgpu-kernel-module-loaded-but-userspace-
// stack-missing gap this whole feature was built to close: a naive
// "is the module loaded" check would call this "working" even though the
// widget genuinely has no real stat to show.
func TestQueryAMDCardPresentButNoStat(t *testing.T) {
	base := t.TempDir()
	device := filepath.Join(base, "card0", "device")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "vendor"), []byte("0x1002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately no gpu_busy_percent file.

	old := drmBasePath
	drmBasePath = base
	defer func() { drmBasePath = old }()

	if _, err := queryAMD(); err == nil {
		t.Error("queryAMD() returned no error with a vendor match but no gpu_busy_percent file")
	}
}
