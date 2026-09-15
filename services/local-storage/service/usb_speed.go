package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// usbSpeedMbps returns the negotiated USB link speed, in Mbit/s, for the
// block device at devicePath (e.g. "/dev/sdb"). It resolves the device's
// real sysfs path (/sys/class/block/<name> is a symlink into the full
// .../usbN/N-M/... chain) and reads the nearest ancestor's "speed" file -
// the same value the kernel negotiated with the device and what `lsusb -t`
// reports. Returns (0, false) if devicePath isn't a USB-attached device, has
// been unplugged since LSBLK() ran, or its speed can't be read for any
// other reason - callers must treat that as "unknown", never as USB2.
func usbSpeedMbps(devicePath string) (int, bool) {
	name := filepath.Base(devicePath)
	if name == "" || name == "." || name == "/" {
		return 0, false
	}

	real, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", name))
	if err != nil {
		return 0, false
	}
	if !strings.Contains(real, "/usb") {
		// Not USB-attached at all (SATA/NVMe/etc.) - nothing to read.
		return 0, false
	}

	dir := real
	for i := 0; i < 20; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		data, err := os.ReadFile(filepath.Join(dir, "speed"))
		if err != nil {
			continue
		}
		mbps, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || mbps <= 0 {
			continue
		}
		// The first "speed" file found walking up from the block device is
		// the leaf USB device's (the drive/enclosure itself) negotiated
		// speed, not an upstream hub's - exactly the number that matters
		// for "is this plugged into a USB3 port at USB3 speed".
		return mbps, true
	}
	return 0, false
}

// usb3SpeedThresholdMbps is USB 3.x SuperSpeed (5 Gbit/s). USB 2.0's fastest
// mode, Hi-Speed, tops out at 480 Mbit/s, so anything at or above this is
// unambiguously USB 3.x (SuperSpeed, SuperSpeed+, or faster).
const usb3SpeedThresholdMbps = 5000

// classifyUSBTransport returns "usb3" for a USB-attached disk whose sysfs
// link speed is SuperSpeed or faster, otherwise "usb". A speed that can't be
// determined always falls back to plain "usb" - detection failure must
// never regress the existing usb-icon behavior.
func classifyUSBTransport(devicePath string) string {
	if mbps, ok := usbSpeedMbps(devicePath); ok && mbps >= usb3SpeedThresholdMbps {
		return "usb3"
	}
	return "usb"
}
