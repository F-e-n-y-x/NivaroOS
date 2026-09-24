package jobs

import (
	"context"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// GET /locations (spec §6.1): what the engine sees right now, plus what
// only the job side knows - saved network shares, remembered USB drives
// that are unplugged, and folder presets for installed apps and VMs.

// Folders the app and VM presets live under (the NivaroOS layout; the
// engine resolves them to whichever volume holds them).
const (
	appDataRoot = "/DATA/AppData"
	vmsRoot     = "/DATA/VMs"
)

// locations builds the merged list for role ("" = both).
func (s *Service) locations(ctx context.Context, role string) ([]Location, error) {
	locs, err := s.engine.Locations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Location, 0, len(locs)+4)
	for _, l := range locs {
		if l.Quirks == nil {
			l.Quirks = []engine.Quirk{}
		}
		if l.Warnings == nil {
			l.Warnings = []engine.Warning{}
		}
		out = append(out, l)
	}

	var wg sync.WaitGroup
	var smbLocs []Location
	var appPresets, vmPresets []engine.FolderPreset
	var appEP, vmEP *Endpoint
	wg.Add(3)
	go func() {
		defer wg.Done()
		smbLocs = s.smbLocations(ctx)
	}()
	go func() {
		defer wg.Done()
		if role == RoleDest {
			return // app and VM folders are sources
		}
		appEP, appPresets = s.appPresets(ctx)
	}()
	go func() {
		defer wg.Done()
		if role == RoleDest {
			return
		}
		vmEP, vmPresets = s.vmPresets(ctx)
	}()
	wg.Wait()
	out = append(out, smbLocs...)

	// Remembered USB drives: rename labels of present ones, list absent
	// ones as offline.
	drives, err := s.store.RememberedDrives()
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for i := range out {
		if out[i].Kind != EPUSB {
			continue
		}
		present[strings.ToUpper(out[i].RefID)] = true
		if d, ok := drives[out[i].RefID]; ok && d.Label != "" {
			out[i].Label = d.Label
		}
	}
	uuids := make([]string, 0, len(drives))
	for u := range drives {
		uuids = append(uuids, u)
	}
	sort.Strings(uuids)
	for _, u := range uuids {
		if present[strings.ToUpper(u)] {
			continue
		}
		d := drives[u]
		label := d.Label
		if label == "" {
			label = d.Endpoint.Label
		}
		var seen *time.Time
		if !d.LastSeen.IsZero() {
			ls := d.LastSeen
			seen = &ls
		}
		out = append(out, Location{
			Kind: EPUSB, RefID: u, Match: d.Endpoint.Match, Label: label, Online: false, Removable: true,
			LastSeen: seen, Quirks: []engine.Quirk{}, Warnings: []engine.Warning{},
		})
	}

	// App and VM presets go on the location that holds them.
	addPresets := func(ep *Endpoint, presets []engine.FolderPreset) {
		if ep == nil || len(presets) == 0 {
			return
		}
		for i := range out {
			if out[i].Kind == ep.Kind && out[i].RefID == ep.RefID {
				out[i].Presets = append(out[i].Presets, presets...)
				return
			}
		}
	}
	addPresets(appEP, appPresets)
	addPresets(vmEP, vmPresets)
	if role == RoleDest {
		for i := range out {
			out[i].Presets = keepPresets(out[i].Presets, func(p engine.FolderPreset) bool {
				return !strings.HasPrefix(p.ID, PresetAppData) && !strings.HasPrefix(p.ID, PresetVM)
			})
		}
	}
	return out, nil
}

func keepPresets(in []engine.FolderPreset, keep func(engine.FolderPreset) bool) []engine.FolderPreset {
	if in == nil {
		return nil
	}
	out := in[:0]
	for _, p := range in {
		if keep(p) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// smbLocations lists saved network shares; one is online when core has
// at least one of its shares mounted.
func (s *Service) smbLocations(ctx context.Context) []Location {
	if s.smb == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conns, err := s.smb.Connections(cctx)
	if err != nil {
		return nil
	}
	out := make([]Location, 0, len(conns))
	for _, c := range conns {
		loc := Location{
			Kind: EPSMB, RefID: c.ID, Label: c.Label(), Quirks: []engine.Quirk{engine.QuirkNoMetadata},
			Warnings: []engine.Warning{},
		}
		for _, share := range c.Shares {
			mp := path.Join(c.MountPoint, share)
			if c.MountPoint == "" || !isMountPoint(mp) {
				continue
			}
			loc.Online = true
			var st unix.Statfs_t
			if unix.Statfs(mp, &st) == nil && loc.Free == nil {
				free := int64(st.Bavail) * int64(st.Bsize)
				total := int64(st.Blocks) * int64(st.Bsize)
				loc.Free, loc.Total = &free, &total
			}
		}
		out = append(out, loc)
	}
	return out
}

// isMountPoint reports whether p is a mount point (its device differs
// from its parent's).
func isMountPoint(p string) bool {
	var st, parent unix.Stat_t
	if unix.Stat(p, &st) != nil || unix.Stat(path.Dir(p), &parent) != nil {
		return false
	}
	return st.Dev != parent.Dev
}

// appPresets are appdata:<app> presets for installed apps whose folder
// exists, on the endpoint holding /DATA/AppData.
func (s *Service) appPresets(ctx context.Context) (*Endpoint, []engine.FolderPreset) {
	if s.apps == nil {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	apps, err := s.apps.List(cctx)
	if err != nil || len(apps) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(apps))
	for n := range apps {
		if _, err := os.Stat(path.Join(appDataRoot, n)); err == nil {
			names = append(names, n)
		}
	}
	return s.presetsUnder(cctx, appDataRoot, PresetAppData, names)
}

// vmPresets are vm:<name> presets for VMs whose folder exists.
func (s *Service) vmPresets(ctx context.Context) (*Endpoint, []engine.FolderPreset) {
	if s.vms == nil {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	vms, err := s.vms.List(cctx)
	if err != nil || len(vms) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(vms))
	for n := range vms {
		if _, err := os.Stat(path.Join(vmsRoot, n)); err == nil {
			names = append(names, n)
		}
	}
	return s.presetsUnder(cctx, vmsRoot, PresetVM, names)
}

func (s *Service) presetsUnder(ctx context.Context, root, prefix string, names []string) (*Endpoint, []engine.FolderPreset) {
	if len(names) == 0 {
		return nil, nil
	}
	res, err := s.engine.ResolvePath(ctx, engine.ResolvePathRequest{Path: root})
	if err != nil || !res.OK || res.Endpoint == nil {
		return nil, nil
	}
	sort.Strings(names)
	out := make([]engine.FolderPreset, 0, len(names))
	for _, n := range names {
		out = append(out, engine.FolderPreset{
			ID: prefix + n, SubPath: strings.TrimPrefix(path.Join(res.Endpoint.SubPath, n), "/"), Label: n,
		})
	}
	return res.Endpoint, out
}
