package service

// One shared safety guard for every destructive/disruptive disk operation the
// v1 API offers (create = repartition+format a whole disk, mount, format a
// partition, umount/remove). Before this, only DELETE /v1/disks refused the
// system disk; POST /v1/storage (format=true) and PUT /v1/storage would wipe
// whatever path the client sent - including the disk / runs from.
//
// The decision itself (CheckDiskOperation) is a pure function of the lsblk
// tree plus a GuardEnv snapshot (fstab, mergerfs branches, VM definitions),
// so it is unit-tested without touching real disks.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mergerfs"
	"github.com/moby/sys/mountinfo"
)

type DiskOp string

const (
	DiskOpCreate DiskOp = "create" // POST /v1/storage format=true: new partition table + format
	DiskOpMount  DiskOp = "mount"  // POST /v1/storage format=false
	DiskOpFormat DiskOp = "format" // PUT /v1/storage: reformat one partition
	DiskOpUmount DiskOp = "umount" // DELETE /v1/disks, DELETE /v1/storage
)

// GuardError: the operation is refused because of the disk's current state
// (HTTP 409). Message is meant for the user.
type GuardError struct{ Msg string }

func (e *GuardError) Error() string { return e.Msg }

// InvalidDeviceError: the request named something that isn't an acceptable
// block device at all (HTTP 400).
type InvalidDeviceError struct{ Msg string }

func (e *InvalidDeviceError) Error() string { return e.Msg }

func IsGuardError(err error) bool {
	var g *GuardError
	return errors.As(err, &g)
}

func IsInvalidDeviceError(err error) bool {
	var g *InvalidDeviceError
	return errors.As(err, &g)
}

func refuse(format string, a ...interface{}) error {
	return &GuardError{Msg: fmt.Sprintf(format, a...)}
}

var blockDevicePathRe = regexp.MustCompile(`^/dev/[A-Za-z0-9][A-Za-z0-9/_.:+-]*$`)

// ValidateBlockDevicePath: syntactic check of a client-supplied device path -
// /dev/<name>, clean, no "..", no whitespace/newlines/shell metacharacters,
// no leading "-" (it goes to mount/smartctl/hdparm/parted as an argument).
func ValidateBlockDevicePath(p string) error {
	if p == "" || len(p) > 255 || filepath.Clean(p) != p || strings.Contains(p, "..") || !blockDevicePathRe.MatchString(p) {
		return &InvalidDeviceError{Msg: fmt.Sprintf("%q is not a valid block device path", p)}
	}
	return nil
}

// FindBlockDevice finds path in an lsblk tree: the top-level disk it belongs
// to and the node itself.
func FindBlockDevice(list []model.LSBLKModel, path string) (disk model.LSBLKModel, node model.LSBLKModel, ok bool) {
	var find func(m model.LSBLKModel) (model.LSBLKModel, bool)
	find = func(m model.LSBLKModel) (model.LSBLKModel, bool) {
		if m.Path == path {
			return m, true
		}
		for _, c := range m.Children {
			if n, ok := find(c); ok {
				return n, true
			}
		}
		return model.LSBLKModel{}, false
	}
	for _, d := range list {
		if n, ok := find(d); ok {
			return d, n, true
		}
	}
	return model.LSBLKModel{}, model.LSBLKModel{}, false
}

// IsAvailableDisk is the server's own "available disks" rule (GET /v1/disks
// "avail"): a supported, non-system disk with nothing on it - no mounts, no
// filesystem signatures on it or any partition, and no fstab entry.
func IsAvailableDisk(d model.LSBLKModel, fstabManaged bool) bool {
	if !IsDiskSupported(d) || diskHoldsSystem(d) || fstabManaged {
		return false
	}
	if d.MountPoint != "" || d.FsType != "" {
		return false
	}
	for _, v := range d.Children {
		if v.MountPoint != "" || v.FsType != "" {
			return false
		}
	}
	return true
}

// GuardEnv is the host state (outside lsblk) the guard looks at.
type GuardEnv struct {
	// FstabMountPoints / FstabSources: every /etc/fstab entry, active or
	// disabled-managed (Source as written: UUID=…, PARTUUID=…, LABEL=…, /dev/…).
	FstabMountPoints map[string]bool
	FstabSources     map[string]bool
	// PoolBranches: branch directories of every mergerfs pool (live mounts +
	// DB merge definitions).
	PoolBranches []string
	// VMDefinitions: raw text of VM definitions (libvirt domain XML).
	VMDefinitions []string
	// Aliases: device path -> other names for it (/dev/disk/by-id/…).
	Aliases map[string][]string
	// FstabLookupFailed: fstab couldn't be read - refuse destructive ops
	// rather than guess.
	FstabLookupFailed bool
	// SystemSources: devices the kernel mount table / /proc/swaps say back
	// /, /boot, /boot/efi, /usr, /var, … or swap (independent of what lsblk
	// reports as the mount point).
	SystemSources map[string]bool
}

var holderTypes = map[string]string{
	"lvm": "LVM", "crypt": "LUKS/dm-crypt", "dm": "device-mapper", "mpath": "multipath",
}

var holderFsTypes = map[string]string{
	"LVM2_member":       "LVM",
	"crypto_LUKS":       "LUKS",
	"linux_raid_member": "software RAID (md)",
	"zfs_member":        "ZFS",
	"bcache":            "bcache",
	"ceph_bluestore":    "Ceph",
	"drbd":              "DRBD",
}

func walkLSBLK(m model.LSBLKModel, fn func(model.LSBLKModel)) {
	fn(m)
	for _, c := range m.Children {
		walkLSBLK(c, fn)
	}
}

func isUnderData(mp string) bool {
	return mp == "/DATA" || strings.HasPrefix(mp, "/DATA/")
}

// CheckDiskOperation decides whether op may run on target (a node of disk).
// It returns nil, or a *GuardError explaining why not.
func CheckDiskOperation(disk, target model.LSBLKModel, op DiskOp, env GuardEnv) error {
	what := map[DiskOp]string{DiskOpCreate: "formatted", DiskOpMount: "mounted", DiskOpFormat: "formatted", DiskOpUmount: "removed"}[op]

	// 1. the running system
	systemDisk := diskHoldsSystem(disk)
	walkLSBLK(disk, func(n model.LSBLKModel) {
		if env.SystemSources[n.Path] {
			systemDisk = true
		}
	})
	if systemDisk {
		return refuse("%s is the system disk (it holds /, /boot, /boot/efi or swap) - it can't be %s", disk.Path, what)
	}

	var err error
	walkLSBLK(disk, func(n model.LSBLKModel) {
		if err != nil {
			return
		}
		mps := NodeMountPoints(n)
		// 2. stacked storage: LVM / md / LUKS / ZFS / …
		if t, ok := holderTypes[n.Type]; ok {
			err = refuse("%s is in use by %s (%s) - remove it from there first", disk.Path, t, n.Path)
			return
		}
		if strings.HasPrefix(n.Type, "raid") || n.Type == "md" {
			err = refuse("%s is part of a software RAID array (%s) - remove it from the array first", disk.Path, n.Path)
			return
		}
		if t, ok := holderFsTypes[n.FsType]; ok {
			err = refuse("%s is in use by %s (%s) - remove it from there first", disk.Path, t, n.Path)
			return
		}
		// 3. fstab (Persistent Mounts / admin-managed)
		for _, mp := range mps {
			if env.FstabMountPoints[mp] {
				err = refuse("%s is managed in Persistent Mounts (%s) - manage it there", disk.Path, mp)
				return
			}
		}
		for _, src := range []string{"UUID=" + n.UUID, "PARTUUID=" + n.PartUUID, "LABEL=" + n.Label, n.Path} {
			if strings.HasSuffix(src, "=") || src == "" {
				continue
			}
			if env.FstabSources[src] {
				err = refuse("%s has an /etc/fstab entry (%s) - manage it in Persistent Mounts", disk.Path, src)
				return
			}
		}
		for _, mp := range mps {
			// 4. /DATA - the apps' data root
			if isUnderData(mp) {
				err = refuse("%s is mounted at %s, which apps use - it can't be %s here", n.Path, mp, what)
				return
			}
			// 5. storage pool (mergerfs) member
			if mp == "/" || strings.HasPrefix(mp, "[") {
				continue
			}
			for _, b := range env.PoolBranches {
				if b == mp || strings.HasPrefix(b, mp+"/") {
					err = refuse("%s (%s) is part of the storage pool - remove it from the pool first", n.Path, mp)
					return
				}
			}
		}
		// 6. VM disk passthrough
		names := append([]string{n.Path}, env.Aliases[n.Path]...)
		for _, def := range env.VMDefinitions {
			for _, name := range names {
				if strings.Contains(def, "'"+name+"'") || strings.Contains(def, "\""+name+"\"") {
					err = refuse("%s is attached to a virtual machine - detach it first", n.Path)
					return
				}
			}
		}
	})
	if err != nil {
		return err
	}

	if env.FstabLookupFailed && op != DiskOpUmount {
		return refuse("couldn't read /etc/fstab to check %s is unused - refusing", disk.Path)
	}

	// 7. per-operation
	switch op {
	case DiskOpCreate:
		if target.Path != disk.Path {
			return refuse("%s is a partition - a whole disk is required", target.Path)
		}
		if !IsAvailableDisk(disk, false) {
			return refuse("%s is not in the list of available disks (it has partitions or filesystems, is mounted, or isn't a supported internal disk)", disk.Path)
		}
	case DiskOpMount:
		if !IsDiskSupported(disk) {
			return refuse("%s isn't a supported internal disk", disk.Path)
		}
		mounted := ""
		walkLSBLK(disk, func(n model.LSBLKModel) {
			if mps := NodeMountPoints(n); len(mps) > 0 && mounted == "" {
				mounted = mps[0]
			}
		})
		if mounted != "" {
			return refuse("%s is already mounted (%s)", disk.Path, mounted)
		}
	case DiskOpFormat:
		if !IsDiskSupported(disk) {
			return refuse("%s isn't a supported internal disk", disk.Path)
		}
		if target.Path != disk.Path && target.Type != "part" {
			return refuse("%s is a %q device, not a partition", target.Path, target.Type)
		}
		if len(target.Children) > 0 && target.Path != disk.Path {
			return refuse("%s has devices stacked on it - it can't be formatted", target.Path)
		}
	case DiskOpUmount:
		// nothing extra: the checks above already exclude system, fstab,
		// /DATA, pool members and stacked storage.
	default:
		return refuse("unknown operation")
	}
	return nil
}

// --- host state collection (impure) ---

func collectGuardEnv() GuardEnv {
	env := GuardEnv{
		FstabMountPoints: map[string]bool{},
		FstabSources:     map[string]bool{},
		Aliases:          map[string][]string{},
		SystemSources:    map[string]bool{},
	}

	if mounts, err := mountinfo.GetMounts(nil); err == nil {
		for _, m := range mounts {
			if isSystemMountPoint(m.Mountpoint) && strings.HasPrefix(m.Source, "/dev/") {
				env.SystemSources[m.Source] = true
				if r, err := filepath.EvalSymlinks(m.Source); err == nil {
					env.SystemSources[r] = true
				}
			}
		}
	}
	if raw, err := os.ReadFile("/proc/swaps"); err == nil {
		for src := range ParseSwapDevices(string(raw)) {
			env.SystemSources[src] = true
		}
	}

	if entries, err := fstab.Get().GetAllEntries(); err != nil {
		if !os.IsNotExist(err) {
			env.FstabLookupFailed = true
		}
	} else {
		for _, e := range entries {
			if e == nil {
				continue
			}
			env.FstabMountPoints[e.MountPoint] = true
			env.FstabSources[e.Source] = true
		}
	}

	env.PoolBranches = collectPoolBranches()
	env.VMDefinitions = readVMDefinitions()

	for _, dir := range []string{"/dev/disk/by-id", "/dev/disk/by-path"} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			full := filepath.Join(dir, e.Name())
			if r, err := filepath.EvalSymlinks(full); err == nil {
				env.Aliases[r] = append(env.Aliases[r], full)
			}
		}
	}
	return env
}

// ParseSwapDevices: device paths listed in /proc/swaps.
func ParseSwapDevices(procSwaps string) map[string]bool {
	out := map[string]bool{}
	for i, line := range strings.Split(procSwaps, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 2 || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		out[f[0]] = true
	}
	return out
}

// ParseMergerFSBranches normalises a mergerfs srcmounts value list (entries
// may carry "=RW"/"=NC" modes).
func ParseMergerFSBranches(values []string) []string {
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if i := strings.LastIndex(v, "="); i > 0 {
			v = v[:i]
		}
		if v != "" {
			out = append(out, filepath.Clean(v))
		}
	}
	return out
}

func collectPoolBranches() []string {
	var branches []string
	if mounts, err := mountinfo.GetMounts(mountinfo.FSTypeFilter("fuse.mergerfs")); err == nil {
		for _, m := range mounts {
			if src, err := mergerfs.GetSource(m.Mountpoint); err == nil {
				branches = append(branches, ParseMergerFSBranches(src)...)
			}
		}
	}
	if MyService != nil {
		if merges, err := MyService.LocalStorage().GetMergeAllFromDB(nil); err == nil {
			for _, m := range merges {
				for _, v := range m.SourceVolumes {
					if v != nil && v.MountPoint != "" {
						branches = append(branches, filepath.Clean(v.MountPoint))
					}
				}
			}
		}
	}
	return branches
}

func readVMDefinitions() []string {
	var defs []string
	for _, pattern := range []string{"/etc/libvirt/qemu/*.xml", "/run/libvirt/qemu/*.xml"} {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil {
				defs = append(defs, string(b))
			}
		}
	}
	return defs
}

// GuardDiskOperation validates path (syntax, lsblk-listed, real block device)
// and runs CheckDiskOperation against the live host state. It returns the
// disk and the node for path.
func (d *diskService) GuardDiskOperation(path string, op DiskOp) (model.LSBLKModel, model.LSBLKModel, error) {
	if err := ValidateBlockDevicePath(path); err != nil {
		return model.LSBLKModel{}, model.LSBLKModel{}, err
	}
	disk, node, ok := FindBlockDevice(d.LSBLK(false), path)
	if !ok {
		return model.LSBLKModel{}, model.LSBLKModel{}, &InvalidDeviceError{Msg: path + " is not a block device listed by lsblk"}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode()&os.ModeDevice == 0 {
		return model.LSBLKModel{}, model.LSBLKModel{}, &InvalidDeviceError{Msg: path + " is not a block device"}
	}
	return disk, node, CheckDiskOperation(disk, node, op, collectGuardEnv())
}

// ResolveListedBlockDevice: path is syntactically valid and listed by lsblk
// (any level). Returns the node. Used by the smaller endpoints (SMART,
// standby, mount) that don't need the full guard.
func (d *diskService) ResolveListedBlockDevice(path string) (model.LSBLKModel, model.LSBLKModel, error) {
	if err := ValidateBlockDevicePath(path); err != nil {
		return model.LSBLKModel{}, model.LSBLKModel{}, err
	}
	disk, node, ok := FindBlockDevice(d.LSBLK(true), path)
	if !ok {
		// the cache may predate a hotplug
		disk, node, ok = FindBlockDevice(d.LSBLK(false), path)
	}
	if !ok {
		return model.LSBLKModel{}, model.LSBLKModel{}, &InvalidDeviceError{Msg: path + " is not a block device listed by lsblk"}
	}
	return disk, node, nil
}

// --- per-disk busy lock ---

// BusySet is an atomic check-and-set of "an operation is running on this
// disk" (the old package-level map was read and written from concurrent
// requests without a lock, so two formats of one disk could both start).
type BusySet struct {
	mu sync.Mutex
	m  map[string]string
}

func NewBusySet() *BusySet { return &BusySet{m: map[string]string{}} }

// TryAcquire marks key busy with what; false if it already was.
func (b *BusySet) TryAcquire(key, what string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, busy := b.m[key]; busy {
		return false
	}
	b.m[key] = what
	return true
}

func (b *BusySet) Release(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.m, key)
}

func (b *BusySet) IsBusy(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, busy := b.m[key]
	return busy
}

// DiskBusy is keyed by the top-level disk path, so a format of /dev/sdb1 and
// a create on /dev/sdb exclude each other.
var DiskBusy = NewBusySet()
