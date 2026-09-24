package engine

import (
	"os"
	"sort"
	"strings"
	"time"
)

const fsTypeMergerFS = "fuse.mergerfs"

// mountVolume is one mount the engine treats as a volume: a block device
// with a filesystem UUID, or a mergerfs pool.
type mountVolume struct {
	m     mountEntry
	dev   *blockDev // nil for a merge pool
	merge bool
	vol   Volume
}

// snapshot is the mount table plus the block devices, taken together so
// a resolution sees one consistent picture.
type snapshot struct {
	table *mountTable
	devs  *blockDevs
	vols  []mountVolume
	// outside are UUIDs of filesystems mounted only where backups may not
	// go (outside the allowed roots): resolving one is path_not_allowed,
	// not "offline".
	outside map[string]string // lower-case UUID -> mount point
	rootM   mountEntry        // the "/" mount
	sysDisk string            // the disk holding the root filesystem ("sda")
	at      time.Time
}

// ignoredFSTypes never back up or receive backups (snap loops, CD images).
var ignoredFSTypes = map[string]bool{"squashfs": true, "iso9660": true, "udf": true}

func (e *Engine) takeSnapshot() (*snapshot, error) {
	table, err := readMountTable(e.cfg.MountInfoPath)
	if err != nil {
		return nil, Errorf(CodeInternal, "reading the mount table: %w", err)
	}
	devs := scanBlockDevs(e.cfg.SysBlockDir, e.cfg.UdevDataDir, e.cfg.DevDir)
	s := &snapshot{table: table, devs: devs, outside: map[string]string{}, at: e.now()}
	if m, ok := table.containing("/"); ok {
		s.rootM = m
		if d := devs.deviceFor(m, e.cfg.DevDir); d != nil {
			s.sysDisk = d.Disk
		}
	}
	for i, m := range table.entries {
		if ignoredFSTypes[m.FSType] || table.shadowed(i) {
			continue
		}
		if !e.listable(m.MountPoint) {
			if d := devs.deviceFor(m, e.cfg.DevDir); d != nil && d.UUID != "" {
				s.outside[strings.ToLower(d.UUID)] = m.MountPoint
			}
			continue
		}
		if m.FSType == fsTypeMergerFS {
			s.vols = append(s.vols, mountVolume{m: m, merge: true, vol: Volume{
				MountID: m.ID, FSType: m.FSType, MountPoint: m.MountPoint, Label: baseLabel(m.MountPoint),
			}})
			continue
		}
		d := devs.deviceFor(m, e.cfg.DevDir)
		if d == nil || d.UUID == "" {
			continue
		}
		fstype := m.FSType
		s.vols = append(s.vols, mountVolume{m: m, dev: d, vol: Volume{
			MountID: m.ID, UUID: d.UUID, Serial: d.Serial, Size: d.Size, FSType: fstype,
			MountPoint: m.MountPoint, Label: d.Label, Tran: d.Tran,
		}})
	}
	return s, nil
}

// listable reports whether a mount point is somewhere backups may use:
// the root filesystem, anything holding an allowed root (a separate
// /DATA), or anything under one.
func (e *Engine) listable(mp string) bool {
	if mp == "/" || e.policy.allowedAncestor(mp) {
		return true
	}
	return e.policy.check(mp) == nil
}

// volumes lists every volume mount, in table order.
func (s *snapshot) volumes() []Volume {
	out := make([]Volume, 0, len(s.vols))
	for _, v := range s.vols {
		out = append(out, v.vol)
	}
	return out
}

// mountsOfUUID groups the volume mounts of every device whose filesystem
// UUID is uuid (case-insensitively: FAT UUIDs are shown in either case),
// by device. More than one device means a cloned filesystem.
func (s *snapshot) mountsOfUUID(uuid string) map[string][]mountVolume {
	out := map[string][]mountVolume{}
	for _, v := range s.vols {
		if v.dev != nil && strings.EqualFold(v.dev.UUID, uuid) {
			out[v.dev.Name] = append(out[v.dev.Name], v)
		}
	}
	return out
}

// devicesOfUUID lists every known block device with that filesystem
// UUID, mounted or not.
func (s *snapshot) devicesOfUUID(uuid string) []*blockDev {
	var out []*blockDev
	for _, d := range s.devs.byName {
		if strings.EqualFold(d.UUID, uuid) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// pickMount chooses which mount of one device serves the filesystem path
// fsPath ("/" + sub-path): one whose mountinfo root contains it,
// preferring the most specific root (a subvolume mounted on its own over
// the whole filesystem), then the oldest mount (lowest ID) so bind
// mounts don't change the choice.
func pickMount(mounts []mountVolume, fsPath string) (mountVolume, bool) {
	var best mountVolume
	found := false
	for _, v := range mounts {
		if !pathWithin(fsPath, v.m.Root) {
			continue
		}
		if !found || len(v.m.Root) > len(best.m.Root) || (len(v.m.Root) == len(best.m.Root) && v.m.ID < best.m.ID) {
			best, found = v, true
		}
	}
	return best, found
}

// volumeByMountPoint finds a volume mount by its mount point (merge
// pools are identified that way).
func (s *snapshot) volumeByMountPoint(mp string) (mountVolume, bool) {
	var best mountVolume
	found := false
	for _, v := range s.vols {
		if v.m.MountPoint == mp {
			best, found = v, true // the last one is the visible one
		}
	}
	return best, found
}

func (s *snapshot) volumeByID(id int) (mountVolume, bool) {
	for _, v := range s.vols {
		if v.m.ID == id {
			return v, true
		}
	}
	return mountVolume{}, false
}

func baseLabel(mp string) string {
	if mp == "/" {
		return "System"
	}
	i := strings.LastIndex(mp, "/")
	return mp[i+1:]
}

// mountDev returns st_dev of a mount point, which changes when the mount
// goes away (the path then shows the parent filesystem, or is gone).
func mountDev(mp string) (uint64, bool) {
	fi, err := os.Stat(mp)
	if err != nil {
		return 0, false
	}
	return statDev(fi)
}
