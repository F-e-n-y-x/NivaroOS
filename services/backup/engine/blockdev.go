package engine

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// blockDev is what the engine knows about one block device (disk or
// partition), read from sysfs and the udev database - the same sources
// lsblk uses - without running any program.
type blockDev struct {
	Name      string // kernel name: "sdc1", "nvme0n1p2", "dm-0"
	MajMin    string // "8:33"
	Disk      string // the whole disk: "sdc" (the device itself when not a partition)
	UUID      string // filesystem UUID (udev ID_FS_UUID, else /dev/disk/by-uuid)
	Label     string // filesystem label
	FSType    string // udev ID_FS_TYPE
	Serial    string // udev ID_SERIAL_SHORT (of the disk)
	Size      int64  // bytes
	Tran      string // "usb", "sata", "nvme", "mmc", "virtio", "" (unknown)
	Removable bool   // sysfs removable flag of the disk, or on a USB bus
}

// blockDevs is a snapshot of all block devices, indexed.
type blockDevs struct {
	byName   map[string]*blockDev
	byMajMin map[string]*blockDev
}

// scanBlockDevs reads every device under sysBlockDir (/sys/class/block),
// with udev properties from udevDataDir (/run/udev/data) and the
// /dev/disk/by-uuid and by-label links under devDir as a fallback when
// udev has no record (containers, early boot).
func scanBlockDevs(sysBlockDir, udevDataDir, devDir string) *blockDevs {
	out := &blockDevs{byName: map[string]*blockDev{}, byMajMin: map[string]*blockDev{}}
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		dir := filepath.Join(sysBlockDir, name)
		majmin := readTrimmed(filepath.Join(dir, "dev"))
		if majmin == "" {
			continue
		}
		d := &blockDev{Name: name, MajMin: majmin, Disk: name}
		if sectors, err := strconv.ParseInt(readTrimmed(filepath.Join(dir, "size")), 10, 64); err == nil {
			d.Size = sectors * 512
		}
		real, _ := filepath.EvalSymlinks(dir)
		if real == "" {
			real = dir
		}
		if _, err := os.Stat(filepath.Join(real, "partition")); err == nil {
			d.Disk = filepath.Base(filepath.Dir(real))
		}
		diskDir := filepath.Join(sysBlockDir, d.Disk)
		d.Removable = readTrimmed(filepath.Join(diskDir, "removable")) == "1"
		d.Tran = tranFromSysPath(real)

		props := readUdevData(filepath.Join(udevDataDir, "b"+majmin))
		d.UUID = props["ID_FS_UUID"]
		d.FSType = props["ID_FS_TYPE"]
		d.Label = decodeUdevEnc(props["ID_FS_LABEL_ENC"])
		if d.Label == "" {
			d.Label = props["ID_FS_LABEL"]
		}
		d.Serial = props["ID_SERIAL_SHORT"]
		if d.Serial == "" {
			d.Serial = readTrimmed(filepath.Join(diskDir, "device", "serial"))
		}
		switch bus := props["ID_BUS"]; bus {
		case "usb":
			d.Tran = "usb"
		case "ata":
			if d.Tran == "" {
				d.Tran = "sata"
			}
		case "nvme", "mmc", "virtio":
			if d.Tran == "" {
				d.Tran = bus
			}
		}
		if d.Tran == "usb" {
			d.Removable = true
		}
		out.byName[name] = d
		out.byMajMin[majmin] = d
	}
	// Fallback identities from /dev/disk links, for devices udev didn't
	// describe.
	fillFromLinks(out, filepath.Join(devDir, "disk", "by-uuid"), func(d *blockDev, v string) {
		if d.UUID == "" {
			d.UUID = v
		}
	})
	fillFromLinks(out, filepath.Join(devDir, "disk", "by-label"), func(d *blockDev, v string) {
		if d.Label == "" {
			d.Label = decodeUdevEnc(v)
		}
	})
	return out
}

func fillFromLinks(devs *blockDevs, dir string, set func(*blockDev, string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if d, ok := devs.byName[filepath.Base(target)]; ok {
			set(d, e.Name())
		}
	}
}

// tranFromSysPath derives lsblk's TRAN from the device's sysfs path
// (/sys/devices/pci.../usb2/.../block/sdc/sdc1).
func tranFromSysPath(real string) string {
	switch {
	case strings.Contains(real, "/usb"):
		return "usb"
	case strings.Contains(real, "/nvme"):
		return "nvme"
	case strings.Contains(real, "/ata"):
		return "sata"
	case strings.Contains(real, "/mmc"):
		return "mmc"
	case strings.Contains(real, "/virtio"):
		return "virtio"
	}
	return ""
}

// readUdevData parses the "E:KEY=value" lines of a udev database record.
func readUdevData(path string) map[string]string {
	props := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return props
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "E:") {
			continue
		}
		if k, v, ok := strings.Cut(line[2:], "="); ok {
			props[k] = v
		}
	}
	return props
}

// decodeUdevEnc undoes udev's \xNN encoding (ID_FS_LABEL_ENC, by-label
// link names).
func decodeUdevEnc(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func readTrimmed(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// deviceFor finds the block device a mount lives on: by its st_dev
// (mountinfo major:minor), else - for btrfs and others that report an
// anonymous 0:NN device - by the source path (/dev/sdc1, /dev/mapper/x),
// resolved inside devDir.
func (b *blockDevs) deviceFor(m mountEntry, devDir string) *blockDev {
	if d, ok := b.byMajMin[m.MajMin]; ok {
		return d
	}
	if !strings.HasPrefix(m.Source, "/dev/") {
		return nil
	}
	p := filepath.Join(devDir, strings.TrimPrefix(m.Source, "/dev/"))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	if d, ok := b.byName[filepath.Base(p)]; ok {
		return d
	}
	return nil
}
