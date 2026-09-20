// vendor.go adds AMD and Intel GPU stats, alongside main.go's existing
// NVIDIA (nvidia-smi) path. The frontend widget (ui/src/shell/widgets/
// Gpu.vue) and the gpuStats JSON shape are already vendor-agnostic - it
// just renders whatever numbers this service returns - so making this work
// for AMD/Intel is entirely a backend job, nothing to change on the UI
// side.
//
// AMD and Intel don't have an nvidia-smi equivalent (a stable CLI tool
// installed by the driver itself), so both read directly from the kernel's
// DRM sysfs ABI instead - no extra package required beyond what's already
// on any system with a working driver.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var errNoDevice = errors.New("no matching GPU device found")

// drmBasePath is a var, not a const, purely so a test can point it at a
// fake sysfs tree instead of the real /sys/class/drm - there's no AMD/Intel
// hardware in CI/dev sandboxes to verify this against otherwise.
var drmBasePath = "/sys/class/drm"

type gpuVendor string

const (
	vendorNone   gpuVendor = ""
	vendorNVIDIA gpuVendor = "nvidia"
	vendorAMD    gpuVendor = "amd"
	vendorIntel  gpuVendor = "intel"
)

// pciVendorID matches gpu-driver-install.sh's own detection - the PCI
// [vendor-id] tag, not a name string (which varies across chipsets/OEMs).
func pciVendorID(v gpuVendor) string {
	switch v {
	case vendorNVIDIA:
		return "0x10de"
	case vendorAMD:
		return "0x1002"
	case vendorIntel:
		return "0x8086"
	}
	return ""
}

// findDRMCard returns the /sys/class/drm/cardN directory whose PCI vendor
// matches, and that card's PCI address (e.g. "0000:03:00.0", the basename
// of the device symlink's target) for looking up its human-readable name.
// Skips connector pseudo-entries like "card0-DP-1", which also match the
// cardN* glob but aren't GPU devices themselves.
func findDRMCard(v gpuVendor) (dir string, pciAddr string, ok bool) {
	want := pciVendorID(v)
	if want == "" {
		return "", "", false
	}
	matches, _ := filepath.Glob(filepath.Join(drmBasePath, "card[0-9]*"))
	for _, m := range matches {
		if strings.Contains(filepath.Base(m), "-") {
			continue
		}
		vendorBytes, err := os.ReadFile(filepath.Join(m, "device", "vendor"))
		if err != nil || strings.TrimSpace(string(vendorBytes)) != want {
			continue
		}
		if target, err := os.Readlink(filepath.Join(m, "device")); err == nil {
			pciAddr = filepath.Base(target)
		}
		return m, pciAddr, true
	}
	return "", "", false
}

// pciDescription returns lspci's own human-readable description for a
// device address (e.g. "Advanced Micro Devices, Inc. [AMD/ATI] Renoir")
// rather than re-deriving a name from raw PCI IDs ourselves.
func pciDescription(pciAddr string) string {
	if pciAddr == "" {
		return ""
	}
	out, err := exec.Command("lspci", "-s", pciAddr).Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if idx := strings.Index(line, ": "); idx != -1 {
		return line[idx+2:]
	}
	return line
}

func readSysfsInt(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// hwmonPath finds the first sibling hwmon device's file for a card
// (temp/power live under .../device/hwmon/hwmonN/<file>, where N is
// whatever index the kernel happened to assign - not fixed).
func hwmonPath(cardDir, file string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(cardDir, "device", "hwmon", "hwmon*", file))
	if len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

func queryAMD() (gpuStats, error) {
	dir, pciAddr, ok := findDRMCard(vendorAMD)
	if !ok {
		return gpuStats{}, errNoDevice
	}

	stats := gpuStats{Name: pciDescription(pciAddr)}

	if busy, ok := readSysfsInt(filepath.Join(dir, "device", "gpu_busy_percent")); ok {
		stats.UtilizationPercent = busy
	} else {
		return gpuStats{}, errNoDevice
	}

	if used, ok := readSysfsInt(filepath.Join(dir, "device", "mem_info_vram_used")); ok {
		stats.MemoryUsedMiB = used / (1024 * 1024)
	}
	if total, ok := readSysfsInt(filepath.Join(dir, "device", "mem_info_vram_total")); ok {
		stats.MemoryTotalMiB = total / (1024 * 1024)
	}
	// An APU (integrated graphics, e.g. Ryzen "G"/APU chips) has no
	// dedicated VRAM - mem_info_vram_total reads back near-zero/absent on
	// those, which is expected, not a fault; the utilization/temp/power
	// readings above are still real and meaningful either way.

	if tempPath, ok := hwmonPath(dir, "temp1_input"); ok {
		if milliC, ok := readSysfsInt(tempPath); ok {
			stats.TemperatureC = milliC / 1000
		}
	}
	if powerPath, ok := hwmonPath(dir, "power1_average"); ok {
		if microW, ok := readSysfsInt(powerPath); ok {
			stats.PowerDrawW = microW / 1_000_000
		}
	}

	// Per-process GPU utilization has no sysfs equivalent to nvidia-smi's
	// pmon on AMD without extra tooling (e.g. rocm-smi, not present on a
	// plain driver install) - Processes stays empty rather than guessing.
	return stats, nil
}

// queryIntel deliberately always returns an error (never a "successful"
// 200 with placeholder/zero stats) even though a card is genuinely
// detected: i915's sysfs ABI doesn't expose a stable, documented
// utilization/VRAM/temp/power surface the way amdgpu's does (real-time
// frequency/utilization needs intel_gpu_top from intel-gpu-tools, which
// also requires perf_event_paranoid access this service doesn't assume it
// has). Returning a fake-success response with all-zero numbers would look
// like a real (but broken) GPU to the widget instead of an honestly
// unsupported one - falling into the same "unavailable" path the widget
// already handles for no-GPU-at-all is the more truthful result until
// real Intel monitoring is implemented.
func queryIntel() (gpuStats, error) {
	if _, _, ok := findDRMCard(vendorIntel); !ok {
		return gpuStats{}, errNoDevice
	}
	return gpuStats{}, errors.New("Intel GPU detected, but live monitoring isn't implemented yet - only NVIDIA and AMD are fully supported")
}
