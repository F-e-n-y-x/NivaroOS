package engine

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"golang.org/x/sys/unix"
)

// resolveTimeout bounds how long Resolve waits for a network endpoint to
// answer before calling it offline.
const resolveTimeout = 20 * time.Second

// target is an endpoint resolved at the moment of use (spec §6.2): where
// it is, how to reach it with rclone, and what to watch for.
type target struct {
	ep     Endpoint
	sub    string // cleaned ep.SubPath
	online bool
	q      fsQuirks

	// local kinds (volume, usb, merge)
	local      bool
	mv         mountVolume
	mountPoint string
	mountDev   uint64 // st_dev of the mount point when resolved
	path       string // absolute, symlink-free path of the endpoint
	fstype     string

	// network kinds (cloud, smb)
	backend  string // rclone backend type
	fsName   string // rclone.conf section, or ":smb{hash}"
	fsRoot   string // root inside the remote
	opts     configmap.Simple
	smb      *SMBCreds
	provider string

	display string // Resolved.Root
}

// fsOpts says how a local endpoint is opened for one use.
type fsOpts struct {
	links         bool // translate symlinks to .rclonelink (both sides of a local-to-local copy)
	oneFileSystem bool // sources: don't descend into other mounts
}

// fsAt opens the endpoint (or rel below it) as an rclone filesystem.
func (t *target) fsAt(ctx context.Context, rel string, o fsOpts) (fs.Fs, error) {
	if t.local {
		return newBackendFs(ctx, "local", ":local", filepath.Join(t.path, filepath.FromSlash(rel)), localFsOptions(t.q, o.links, o.oneFileSystem))
	}
	root := t.fsRoot
	if rel != "" {
		root = path.Join(root, rel)
	}
	return newBackendFs(ctx, t.backend, t.fsName, root, t.opts)
}

// isCloud reports whether the endpoint is an rclone cloud remote (the
// engine runs at most one such job at a time).
func (t *target) isCloud() bool { return t.ep.Kind == EPCloud }

// resolve maps an endpoint to a target. An endpoint that is merely absent
// (drive unplugged, network down) resolves with online=false; errors are
// for endpoints that can never work as given.
func (e *Engine) resolve(ctx context.Context, ep Endpoint, creds *SMBCreds) (*target, error) {
	if !ep.Kind.Valid() {
		return nil, Errorf(CodeEndpointUnknown, "unknown endpoint kind %q", ep.Kind)
	}
	sub, err := cleanSubPath(ep.SubPath)
	if err != nil {
		return nil, err
	}
	t := &target{ep: ep, sub: sub}
	switch ep.Kind {
	case EPVolume, EPUSB, EPMerge:
		return t, e.resolveLocal(t)
	case EPCloud:
		return t, e.resolveCloud(ctx, t)
	case EPSMB:
		return t, e.resolveSMB(ctx, t, creds)
	}
	return nil, Errorf(CodeEndpointUnknown, "unknown endpoint kind %q", ep.Kind)
}

func (e *Engine) resolveLocal(t *target) error {
	snap, err := e.fresh()
	if err != nil {
		return err
	}
	t.local = true
	fsPath := "/" + t.sub
	var mv mountVolume
	switch t.ep.Kind {
	case EPMerge:
		v, ok := snap.volumeByMountPoint(filepath.Clean(t.ep.RefID))
		if !ok || !v.merge {
			t.display = t.ep.RefID
			return nil // pool not mounted
		}
		mv = v
		fsPath = filepath.Join(v.m.Root, t.sub)
	default:
		if t.ep.RefID == "" {
			return Errorf(CodeEndpointUnknown, "volume endpoint without a filesystem UUID")
		}
		var candidates [][]mountVolume
		for _, mounts := range snap.mountsOfUUID(t.ep.RefID) {
			if matchesDevice(mounts[0].dev, t.ep.Match) {
				candidates = append(candidates, mounts)
			}
		}
		if len(candidates) > 1 {
			var names []string
			for _, c := range candidates {
				names = append(names, "/dev/"+c[0].dev.Name)
			}
			return Errorf(CodeAmbiguousDevice, "filesystem %s is mounted from %d devices (%s): a cloned drive", t.ep.RefID, len(candidates), strings.Join(names, ", "))
		}
		if len(candidates) == 0 {
			t.display = t.ep.RefID
			if mp, ok := snap.outside[strings.ToLower(t.ep.RefID)]; ok && len(snap.mountsOfUUID(t.ep.RefID)) == 0 {
				return Errorf(CodePathNotAllowed, "filesystem %s is mounted at %s, outside the folders backups may use", t.ep.RefID, mp)
			}
			return nil // not plugged in, not mounted, or a different drive with a cloned UUID
		}
		v, ok := pickMount(candidates[0], fsPath)
		if !ok {
			t.display = t.ep.RefID
			return nil // the subvolume holding this path isn't mounted
		}
		mv = v
	}
	t.mv = mv
	t.mountPoint = mv.m.MountPoint
	t.fstype = mv.m.FSType
	p := filepath.Join(mv.m.MountPoint, strings.TrimPrefix(fsPath, mv.m.Root))
	t.display = p
	real, err := evalExisting(p)
	if err != nil {
		return Errorf(CodeIOError, "resolving %s: %w", p, err)
	}
	if !pathWithin(real, mv.m.MountPoint) {
		return Errorf(CodePathNotAllowed, "%s leads outside its drive (to %s) through a symbolic link", p, real)
	}
	if err := e.policy.check(real); err != nil {
		return err
	}
	if inner, ok := snap.table.containing(real); ok && inner.ID != mv.m.ID {
		return Errorf(CodePathNotAllowed, "%s is on another drive mounted at %s", real, inner.MountPoint)
	}
	dev, ok := mountDev(mv.m.MountPoint)
	if !ok {
		return nil // mount point vanished between reading the table and now
	}
	t.path = real
	t.mountDev = dev
	t.q = localQuirks(mv.m.FSType)
	t.provider = "local"
	t.online = true
	return nil
}

// matchesDevice applies a USB endpoint's pinned serial and size.
func matchesDevice(d *blockDev, m *DevMatch) bool {
	if m == nil || d == nil {
		return true
	}
	if m.Serial != "" && !strings.EqualFold(m.Serial, d.Serial) {
		return false
	}
	if m.SizeBytes != 0 && m.SizeBytes != d.Size {
		return false
	}
	return true
}

func (e *Engine) resolveCloud(ctx context.Context, t *target) error {
	r, ok := findRemote(t.ep.RefID)
	if !ok {
		return Errorf(CodeEndpointUnknown, "no cloud account %q in the rclone config", t.ep.RefID)
	}
	t.backend, t.provider, t.fsName, t.fsRoot = r.Type, r.Type, r.Name, t.sub
	t.q = remoteQuirks(r.Type, nil)
	t.opts = cloudFsOptions(t.q)
	t.display = cloudRoot(r.Name, t.sub)
	return e.probeRemote(ctx, t)
}

func (e *Engine) resolveSMB(ctx context.Context, t *target, creds *SMBCreds) error {
	if creds == nil || creds.Host == "" || creds.Share == "" {
		return Errorf(CodeEndpointUnknown, "share %q: no connection details were given", t.ep.RefID)
	}
	opts, err := smbFsOptions(*creds)
	if err != nil {
		return err
	}
	c := *creds
	t.smb = &c
	t.backend, t.provider, t.fsName = "smb", "smb", smbFsName(c)
	t.fsRoot = path.Join(c.Share, t.sub)
	t.opts = opts
	t.q = remoteQuirks("smb", nil)
	t.display = smbDisplay(c, t.sub)
	return e.probeRemote(ctx, t)
}

// probeRemote connects to a network endpoint once: sign-in problems are
// errors (cloud_auth), anything else just means offline for now.
func (e *Engine) probeRemote(ctx context.Context, t *target) error {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err == nil {
		// Listing the root proves the credentials and the network; a
		// destination folder that doesn't exist yet is fine.
		_, err = f.List(ctx, "")
		if errors.Is(err, fs.ErrorDirNotFound) {
			err = nil
		}
	}
	if err != nil {
		code := classifyError(err)
		if code == CodeCloudAuth || code == CodePathNotAllowed {
			return &Error{Code: code, Detail: redact(err.Error(), t.smb), Err: err}
		}
		e.cfg.Logf("engine: %s is offline: %s", t.display, redact(err.Error(), t.smb))
		return nil
	}
	t.q = remoteQuirks(t.provider, f)
	t.online = true
	return nil
}

// redact removes a share password from an error text, in case a backend
// ever echoes its connection settings.
func redact(s string, c *SMBCreds) string {
	if c != nil && c.Password != "" {
		s = strings.ReplaceAll(s, c.Password, "***")
	}
	return s
}

// Resolve maps an endpoint to where it is right now (spec §6.2).
func (e *Engine) Resolve(ctx context.Context, req ResolveRequest) (Resolved, error) {
	t, err := e.resolve(ctx, req.Endpoint, req.SMBCreds)
	if err != nil {
		return Resolved{}, err
	}
	out := Resolved{Root: t.display, Online: t.online, Quirks: sortedQuirks(t.q.Quirks)}
	if !t.online {
		out.Quirks = []Quirk{}
		return out, nil
	}
	if t.local {
		out.MountID = t.mv.m.ID
	}
	out.Free = e.freeSpace(ctx, t)
	m, err := e.readMarker(ctx, t)
	if err != nil {
		return Resolved{}, err
	}
	out.Marker = m
	return out, nil
}

// freeSpace is the space available at a target: statfs for local
// folders (the fullest-yet-best branch for merge pools, where a file
// lands on one branch), About for remotes; nil when unknown.
func (e *Engine) freeSpace(ctx context.Context, t *target) *int64 {
	if t.local {
		if t.mv.merge {
			return mergeBranchFree(t.mv.m)
		}
		return statfsFree(t.path)
	}
	free, _ := e.about.get(t.fsName+"|"+t.fsRoot, e.now(), func(ctx context.Context) (*int64, *int64, error) {
		f, err := t.fsAt(ctx, "", fsOpts{})
		if err != nil {
			return nil, nil, err
		}
		return usageOf(ctx, f)
	}, e.cfg.Logf)
	if free != nil {
		return free
	}
	// First ask: wait briefly for the answer instead of reporting unknown.
	ctx, cancel := context.WithTimeout(ctx, aboutTimeout)
	defer cancel()
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil
	}
	free, _, err = usageOf(ctx, f)
	if err != nil {
		return nil
	}
	return free
}

// statfsFree is the free space for unprivileged writers at p (or its
// nearest existing parent).
func statfsFree(p string) *int64 {
	for {
		var st unix.Statfs_t
		if err := unix.Statfs(p, &st); err == nil {
			v := int64(st.Bavail) * int64(st.Bsize)
			return &v
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

func statfsTotal(p string) *int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil {
		return nil
	}
	v := int64(st.Blocks) * int64(st.Bsize)
	return &v
}

// mergeBranches lists a mergerfs pool's branch folders from its mount
// source ("/mnt/a:/mnt/b=RW"), falling back to the pool's control file.
func mergeBranches(m mountEntry) []string {
	src := m.Source
	if v, ok := mountOptionValue(m.SuperOpts+","+m.Options, "fsname"); ok && strings.Contains(v, "/") {
		src = v
	}
	if !strings.Contains(src, "/") {
		if b, err := getxattr(filepath.Join(m.MountPoint, ".mergerfs"), "user.mergerfs.srcmounts"); err == nil {
			src = b
		}
	}
	var out []string
	for _, b := range strings.Split(src, ":") {
		b, _, _ = strings.Cut(b, "=") // "=RW" / "=NC" branch modes
		if strings.HasPrefix(b, "/") {
			out = append(out, filepath.Clean(b))
		}
	}
	return out
}

// mergeBranchFree is the most any one branch can take: mergerfs writes a
// file to a single branch, so the pool's summed free space overstates it.
func mergeBranchFree(m mountEntry) *int64 {
	var best *int64
	for _, b := range mergeBranches(m) {
		if f := statfsFree(b); f != nil && (best == nil || *f > *best) {
			best = f
		}
	}
	if best == nil {
		return statfsFree(m.MountPoint)
	}
	return best
}

func getxattr(p, name string) (string, error) {
	buf := make([]byte, 4096)
	n, err := unix.Getxattr(p, name, buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// ResolvePath turns an absolute local path into an endpoint: a cloud
// remote when it is under that remote's FUSE mount, a merge pool, or the
// volume (by filesystem UUID) that holds it.
func (e *Engine) ResolvePath(ctx context.Context, req ResolvePathRequest) (ResolvePathResult, error) {
	p := req.Path
	if strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return ResolvePathResult{Reason: CodePathNotAllowed}, nil
	}
	p = filepath.Clean(p)
	if r, rel, ok := remoteForMountPath(p); ok {
		ep := Endpoint{Kind: EPCloud, RefID: r.Name, SubPath: rel, Label: r.Label}
		return ResolvePathResult{Endpoint: &ep, OK: true}, nil
	}
	real, err := evalExisting(p)
	if err != nil {
		return ResolvePathResult{Reason: CodeEndpointUnknown}, nil
	}
	if r, rel, ok := remoteForMountPath(real); ok {
		ep := Endpoint{Kind: EPCloud, RefID: r.Name, SubPath: rel, Label: r.Label}
		return ResolvePathResult{Endpoint: &ep, OK: true}, nil
	}
	if err := e.policy.check(real); err != nil {
		return ResolvePathResult{Reason: CodePathNotAllowed}, nil
	}
	snap, err := e.fresh()
	if err != nil {
		return ResolvePathResult{}, err
	}
	m, ok := snap.table.containing(real)
	if !ok {
		return ResolvePathResult{Reason: CodeEndpointUnknown}, nil
	}
	v, ok := snap.volumeByID(m.ID)
	if !ok {
		// A share mounted for Files (cifs), a tmpfs, ...: not a volume the
		// engine can identify. SMB shares are mapped by the job side.
		return ResolvePathResult{Reason: CodeEndpointUnknown}, nil
	}
	if v.merge {
		rel, _ := filepath.Rel(m.MountPoint, real)
		ep := Endpoint{Kind: EPMerge, RefID: m.MountPoint, SubPath: relSlash(rel), Label: v.vol.Label}
		return ResolvePathResult{Endpoint: &ep, OK: true}, nil
	}
	sub := strings.TrimPrefix(fsPathIn(m, real), "/")
	ep := Endpoint{Kind: EPVolume, RefID: v.dev.UUID, SubPath: filepath.ToSlash(sub), Label: volumeLabel(v)}
	if isRemovable(v, snap) {
		ep.Kind = EPUSB
		ep.Match = &DevMatch{Serial: v.dev.Serial, SizeBytes: v.dev.Size}
	}
	return ResolvePathResult{Endpoint: &ep, OK: true}, nil
}

func relSlash(rel string) string {
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// isRemovable reports a USB or otherwise removable drive, never the disk
// the system runs from.
func isRemovable(v mountVolume, s *snapshot) bool {
	if v.dev == nil || v.dev.Disk == s.sysDisk {
		return false
	}
	return v.dev.Removable || v.dev.Tran == "usb"
}

func volumeLabel(v mountVolume) string {
	if v.dev != nil && v.dev.Label != "" {
		return v.dev.Label
	}
	return baseLabel(v.m.MountPoint)
}
