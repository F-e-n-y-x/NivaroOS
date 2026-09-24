package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// This service runs as root, so "save to" must never be an arbitrary path
// (a download into /etc/cron.d or /root/.ssh is code execution). Downloads,
// the default folder and "New folder" are confined to storage roots: the
// NivaroOS data tree, the usual removable/extra-disk mount parents, and any
// other mounted data filesystem (a second disk mounted at /srv/disk1 or
// /volume1). Every check runs on the symlink-resolved path, so a symlink
// inside /DATA pointing at /etc is refused too.

var errPathNotAllowed = errors.New("save folder must be inside /DATA, /media, /mnt or a mounted storage drive")

type PathPolicy struct {
	mu    sync.Mutex
	base  []string // configured roots (-storage-roots)
	extra []string // added by tests
	// Cached mounted-storage roots from /proc/self/mounts.
	mounts     []string
	mountsRead time.Time
}

var pathPolicy = &PathPolicy{base: []string{"/DATA", "/media", "/mnt"}}

func (p *PathPolicy) SetBase(roots []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.base = nil
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if filepath.IsAbs(r) && filepath.Clean(r) != "/" {
			p.base = append(p.base, filepath.Clean(r))
		}
	}
}

// Filesystems that hold user data (a mount of one of these outside the
// system tree counts as a storage root).
var storageFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true,
	"ntfs": true, "ntfs3": true, "fuseblk": true, "vfat": true, "exfat": true, "hfsplus": true,
	"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "fuse.mergerfs": true, "fuse.rclone": true,
	"fuse.sshfs": true, "bcachefs": true, "jfs": true, "reiserfs": true,
}

// System locations a data mount must never make writable, whatever its
// filesystem.
var systemPrefixes = []string{"/boot", "/usr", "/var", "/etc", "/proc", "/sys", "/dev", "/run", "/root",
	"/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/opt", "/snap", "/tmp", "/home", "/nix", "/gnu"}

func underPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

func isSystemPath(p string) bool {
	if p == "/" {
		return true
	}
	for _, s := range systemPrefixes {
		if underPrefix(p, s) {
			return true
		}
	}
	return false
}

// unescapeMount decodes the octal escapes (\040 for a space...) that
// /proc/mounts uses in mount point paths.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (p *PathPolicy) mountRootsLocked() []string {
	if time.Since(p.mountsRead) < 30*time.Second {
		return p.mounts
	}
	p.mountsRead = time.Now()
	p.mounts = nil
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		mp, fstype := filepath.Clean(unescapeMount(fields[1])), fields[2]
		if !storageFSTypes[fstype] || isSystemPath(mp) {
			continue
		}
		// Read-only to this service (a read-only mount, or outside the
		// systemd unit's ReadWritePaths) - offering it would only fail.
		if syscall.Access(mp, 2 /* W_OK */) != nil {
			continue
		}
		p.mounts = append(p.mounts, mp)
	}
	return p.mounts
}

// Roots returns every storage root, symlink-resolved, deduplicated and
// sorted - also what the folder picker offers as its top level.
func (p *PathPolicy) Roots() []string {
	p.mu.Lock()
	raw := append(append(append([]string(nil), p.base...), p.extra...), p.mountRootsLocked()...)
	p.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range raw {
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			continue // not present on this install
		}
		if resolved == "/" || seen[resolved] {
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	sort.Strings(out)
	return out
}

// DisplayRoots is Roots as the user knows them (/DATA, not wherever a
// /DATA symlink happens to point) - what the folder picker shows.
func (p *PathPolicy) DisplayRoots() []string {
	p.mu.Lock()
	raw := append(append(append([]string(nil), p.base...), p.extra...), p.mountRootsLocked()...)
	p.mu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	for _, r := range raw {
		r = filepath.Clean(r)
		if seen[r] {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(r); err != nil || resolved == "/" {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// resolveExisting resolves symlinks in the longest existing ancestor of p
// and re-appends the not-yet-created remainder.
func resolveExisting(p string) (string, error) {
	p = filepath.Clean(p)
	rest := ""
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, rest), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		base := filepath.Base(cur)
		if base == ".." || base == "." {
			return "", errPathNotAllowed
		}
		if rest == "" {
			rest = base
		} else {
			rest = filepath.Join(base, rest)
		}
		cur = parent
	}
}

// Check returns the resolved form of dir if it lies inside a storage root.
func (p *PathPolicy) Check(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		return "", errors.New("save folder must be an absolute path")
	}
	resolved, err := resolveExisting(dir)
	if err != nil {
		return "", errPathNotAllowed
	}
	for _, r := range p.Roots() {
		if underPrefix(resolved, r) {
			return resolved, nil
		}
	}
	return "", errPathNotAllowed
}

// MkdirAll creates dir (inside a root) and re-checks the result, so a
// symlink swapped in while creating can't escape.
func (p *PathPolicy) MkdirAll(dir string) (string, error) {
	resolved, err := p.Check(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return "", err
	}
	return p.Check(resolved)
}

// Allowed reports whether an existing file path (a download to delete) is
// inside a storage root.
func (p *PathPolicy) Allowed(file string) bool {
	_, err := p.Check(filepath.Dir(file))
	return err == nil
}
