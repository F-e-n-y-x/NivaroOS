package engine

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Volumes is the current mount table as the engine sees it.
func (e *Engine) Volumes(ctx context.Context) ([]Volume, error) {
	s, err := e.fresh()
	if err != nil {
		return nil, err
	}
	return s.volumes(), nil
}

// Locations lists what is present right now (spec §6.1): one entry per
// filesystem (by UUID; bind mounts and subvolumes of it are the same
// location), every merge pool, and every remote in the shared rclone
// config. The /DATA folders are presets of the volume that holds them.
func (e *Engine) Locations(ctx context.Context) ([]Location, error) {
	s, err := e.fresh()
	if err != nil {
		return nil, err
	}
	out := []Location{}
	seen := map[string]bool{}
	for _, v := range s.vols {
		if v.merge {
			out = append(out, e.mergeLocation(v))
			continue
		}
		key := strings.ToLower(v.dev.UUID)
		if seen[key] {
			continue
		}
		seen[key] = true
		groups := s.mountsOfUUID(v.dev.UUID)
		mounts := groups[v.dev.Name]
		best, ok := pickMount(mounts, "/")
		if !ok {
			best = mounts[0]
			for _, m := range mounts[1:] {
				if len(m.m.Root) < len(best.m.Root) {
					best = m
				}
			}
		}
		out = append(out, e.volumeLocation(best, s))
	}
	for _, r := range remotes() {
		out = append(out, e.cloudLocation(r))
	}
	e.attachPresets(s, out)
	return out, nil
}

func (e *Engine) volumeLocation(v mountVolume, s *snapshot) Location {
	q := localQuirks(v.m.FSType)
	loc := Location{
		Kind: EPVolume, RefID: v.dev.UUID, Label: volumeLabel(v), FSType: v.m.FSType, MountPoint: v.m.MountPoint,
		Online: true, Free: statfsFree(v.m.MountPoint), Total: statfsTotal(v.m.MountPoint),
		PhysicalDisk: v.dev.Disk, SystemDisk: v.dev.Disk != "" && v.dev.Disk == s.sysDisk,
		Quirks: sortedQuirks(q.Quirks), Warnings: warningsFor(q.Quirks),
	}
	if isRemovable(v, s) {
		loc.Kind = EPUSB
		loc.Removable = true
		loc.Match = &DevMatch{Serial: v.dev.Serial, SizeBytes: v.dev.Size}
	}
	if loc.SystemDisk {
		loc.Warnings = append(loc.Warnings, WarnSystemDisk)
	}
	if q.WinEncoding && worldWritable(v.m) {
		loc.Warnings = append(loc.Warnings, WarnWorldWritable)
	}
	if loc.Warnings == nil {
		loc.Warnings = []Warning{}
	}
	return loc
}

func (e *Engine) mergeLocation(v mountVolume) Location {
	q := localQuirks(v.m.FSType)
	total := statfsTotal(v.m.MountPoint)
	return Location{
		Kind: EPMerge, RefID: v.m.MountPoint, Label: baseLabel(v.m.MountPoint), FSType: v.m.FSType, MountPoint: v.m.MountPoint,
		Online: true, Free: mergeBranchFree(v.m), Total: total,
		Quirks: sortedQuirks(q.Quirks), Warnings: []Warning{},
	}
}

func (e *Engine) cloudLocation(r remote) Location {
	q := remoteQuirks(r.Type, nil)
	loc := Location{
		Kind: EPCloud, RefID: r.Name, Label: r.Label, Provider: r.Type, MountPoint: r.MountPoint,
		Online: true, Quirks: sortedQuirks(q.Quirks), Warnings: warningsFor(q.Quirks),
	}
	if loc.MountPoint == "." {
		loc.MountPoint = ""
	}
	t := &target{backend: r.Type, fsName: r.Name, provider: r.Type, opts: cloudFsOptions(q)}
	loc.Free, loc.Total = e.about.get(r.Name+"|", e.now(), func(ctx context.Context) (*int64, *int64, error) {
		f, err := t.fsAt(ctx, "", fsOpts{})
		if err != nil {
			return nil, nil, err
		}
		return usageOf(ctx, f)
	}, e.cfg.Logf)
	if loc.Warnings == nil {
		loc.Warnings = []Warning{}
	}
	return loc
}

// attachPresets offers the /DATA folders, each app's data and each VM's
// folder as shortcuts inside the location that holds them.
func (e *Engine) attachPresets(s *snapshot, locs []Location) {
	root, err := filepath.EvalSymlinks(e.cfg.DataRoot)
	if err != nil {
		return
	}
	add := func(id, p, label string) {
		m, ok := s.table.containing(p)
		if !ok || m.MountPoint == p {
			return // a drive mounted there is a location of its own
		}
		v, ok := s.volumeByID(m.ID)
		if !ok || v.merge {
			return
		}
		for i := range locs {
			if locs[i].Kind == EPCloud || locs[i].Kind == EPMerge || !strings.EqualFold(locs[i].RefID, v.dev.UUID) {
				continue
			}
			sub := strings.TrimPrefix(fsPathIn(m, p), "/")
			locs[i].Presets = append(locs[i].Presets, FolderPreset{ID: id, SubPath: filepath.ToSlash(sub), Label: label})
		}
	}
	for _, d := range listDirs(root) {
		p := filepath.Join(root, d)
		switch d {
		case "AppData":
			for _, app := range listDirs(p) {
				add("appdata:"+app, filepath.Join(p, app), app)
			}
		case "VMs":
			for _, vm := range listDirs(p) {
				add("vm:"+vm, filepath.Join(p, vm), vm)
			}
		}
		add("data:"+d, p, d)
	}
}

// listDirs lists the real (not symlinked, not hidden) folders in dir,
// sorted.
func listDirs(dir string) []string {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range des {
		if d.IsDir() && !strings.HasPrefix(d.Name(), ".") && d.Name() != "lost+found" {
			out = append(out, d.Name())
		}
	}
	sort.Strings(out)
	return out
}
