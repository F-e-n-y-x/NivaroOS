// Package cloudcache holds the disk-cache settings shared by every cloud
// (rclone) mount on this server, and the on-disk logic around rclone's VFS
// cache directory: usage, pending uploads, clearing and migrating it.
//
// There is one setting set, stored in local-storage.conf ([cloud_cache]).
// nivaroos-local-storage mounts in-process with it; nivaroos-core reads the
// same section for the mounts it asks the rclone daemon (rclone.service)
// for, and the daemon's --cache-dir comes from EnvFile, which this package
// writes.
//
// rclone's cache layout, for a cache dir D and a remote R:
//
//	D/vfs/R/<path>      cached file data (sparse: only the parts read)
//	D/vfsMeta/R/<path>  JSON metadata; "Dirty": true = not uploaded yet
//
// A dirty item is a file the user wrote that is still waiting to upload.
// Nothing in this package ever deletes one.
package cloudcache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const (
	ModeOff    = "off"    // no disk cache: streaming only
	ModeWrites = "writes" // cache files opened for writing (uploads)
	ModeFull   = "full"   // cache everything read or written

	DefaultDir     = "/var/cache/nivaroos/rclone"
	DefaultMaxSize = int64(20) << 30 // 20 GiB
	DefaultMaxAge  = time.Hour

	// Section is the local-storage.conf section holding the settings.
	Section = "cloud_cache"
	// EnvFile feeds rclone.service's --cache-dir (EnvironmentFile=).
	EnvFile = "/etc/nivaroos/rclone-cache.env"

	MinMaxAge = time.Minute
	MaxMaxAge = 365 * 24 * time.Hour
)

// Settings is the one cache setting set applied to every cloud mount.
type Settings struct {
	Mode    string        `json:"mode"`
	MaxSize int64         `json:"max_size"` // bytes
	MaxAge  time.Duration `json:"-"`
	Dir     string        `json:"dir"`
}

func Defaults() Settings {
	return Settings{Mode: ModeFull, MaxSize: DefaultMaxSize, MaxAge: DefaultMaxAge, Dir: DefaultDir}
}

// VFSCacheMode is rclone's vfscommon.CacheMode number for the mode
// (off=0, minimal=1, writes=2, full=3).
func (s Settings) VFSCacheMode() int {
	switch s.Mode {
	case ModeOff:
		return 0
	case ModeWrites:
		return 2
	default:
		return 3
	}
}

// Load reads the settings from cfg's [cloud_cache] section; missing or
// unparsable values fall back to the defaults.
func Load(cfg *ini.File) Settings {
	s := Defaults()
	if cfg == nil || !cfg.HasSection(Section) {
		return s
	}
	sec := cfg.Section(Section)
	if m := strings.ToLower(strings.TrimSpace(sec.Key("Mode").String())); validMode(m) {
		s.Mode = m
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(sec.Key("MaxSize").String()), 10, 64); err == nil && n > 0 {
		s.MaxSize = n
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(sec.Key("MaxAge").String()), 10, 64); err == nil && n > 0 {
		s.MaxAge = time.Duration(n) * time.Second
	}
	if d := strings.TrimSpace(sec.Key("Dir").String()); d != "" && filepath.IsAbs(d) {
		s.Dir = filepath.Clean(d)
	}
	return s
}

// Store writes s into cfg's [cloud_cache] section (the caller saves cfg).
func Store(cfg *ini.File, s Settings) {
	sec := cfg.Section(Section)
	sec.Key("Mode").SetValue(s.Mode)
	sec.Key("MaxSize").SetValue(strconv.FormatInt(s.MaxSize, 10))
	sec.Key("MaxAge").SetValue(strconv.FormatInt(int64(s.MaxAge/time.Second), 10))
	sec.Key("Dir").SetValue(s.Dir)
}

// WriteEnvFile writes rclone.service's environment file (RCLONE_CACHE_DIR)
// atomically.
func WriteEnvFile(path, dir string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := "# Written by nivaroos-local-storage (Settings > Online storage > Cache).\n" +
		"RCLONE_CACHE_DIR=" + dir + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validMode(m string) bool { return m == ModeOff || m == ModeWrites || m == ModeFull }

// Mount is one line of the mount table.
type Mount struct {
	Point  string
	FSType string
	Source string
}

// Env is what validation needs to know about the machine; tests fake it.
type Env struct {
	// Mounts returns the current mount table.
	Mounts func() ([]Mount, error)
	// Free returns the bytes available to root on the filesystem holding path.
	Free func(path string) (uint64, error)
	// CacheUsed returns what the cloud cache already uses under dir (it is
	// counted as available when sizing the cache there).
	CacheUsed func(dir string) int64
	// CloudMountPoints are the configured mount points of cloud accounts
	// (even ones not mounted right now).
	CloudMountPoints []string
	// ProbeWritable creates dir if needed and checks a file can be written.
	ProbeWritable func(dir string) error
}

// ValidationError is a user-facing reason the settings were refused.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// IsValidation reports whether err is a ValidationError.
func IsValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

var ramFS = map[string]bool{"tmpfs": true, "ramfs": true, "devtmpfs": true}
var pseudoFS = map[string]bool{"proc": true, "sysfs": true, "devpts": true, "cgroup": true, "cgroup2": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "configfs": true, "fusectl": true, "mqueue": true, "hugetlbfs": true,
	"pstore": true, "bpf": true, "efivarfs": true, "autofs": true, "binfmt_misc": true}

// isCloudFS: rclone FUSE mounts, and any other network filesystem the
// cache would only make slower.
func isCloudFS(t string) bool {
	return t == "fuse.rclone" || strings.HasPrefix(t, "fuse.rclone") || t == "cifs" || t == "smb3" || t == "nfs" || t == "nfs4" || t == "fuse.sshfs"
}

// MountFor returns the mount holding path (longest mount point prefix).
func MountFor(path string, mounts []Mount) (Mount, bool) {
	best := -1
	for i, m := range mounts {
		if within(path, m.Point) && (best < 0 || len(m.Point) > len(mounts[best].Point)) {
			best = i
		}
	}
	if best < 0 {
		return Mount{}, false
	}
	return mounts[best], true
}

// within reports whether p is dir or inside it (both clean absolute paths).
func within(p, dir string) bool {
	if dir == "/" {
		return true
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// Validate normalises s and checks it can be used. It returns warnings the
// user should see (not errors), or a *ValidationError.
func Validate(s *Settings, env Env) (warnings []string, err error) {
	s.Mode = strings.ToLower(strings.TrimSpace(s.Mode))
	if !validMode(s.Mode) {
		return nil, invalid("Choose a cache mode: off, uploads only or full.")
	}
	if s.MaxSize <= 0 {
		return nil, invalid("The maximum cache size must be more than 0.")
	}
	if s.MaxAge < MinMaxAge || s.MaxAge > MaxMaxAge {
		return nil, invalid("Keep cached data for at least 1 minute and at most 365 days.")
	}
	dir := strings.TrimSpace(s.Dir)
	if dir == "" {
		dir = DefaultDir
	}
	if !filepath.IsAbs(dir) {
		return nil, invalid("The cache location must be a full folder path, like %s.", DefaultDir)
	}
	dir = filepath.Clean(dir)
	s.Dir = dir
	if dir == "/" {
		return nil, invalid("Choose a folder for the cache, not the whole disk.")
	}
	for _, bad := range []string{"/proc", "/sys", "/dev", "/run", "/var/run", "/boot", "/etc", "/usr", "/bin", "/sbin", "/lib"} {
		if within(dir, bad) {
			return nil, invalid("%s is a system folder - choose another location for the cache.", bad)
		}
	}
	for _, mp := range env.CloudMountPoints {
		if mp = filepath.Clean(mp); mp != "" && mp != "/" && (within(dir, mp) || within(mp, dir)) {
			return nil, invalid("The cache can't be inside an online storage drive (%s) - choose a folder on a disk in this server.", mp)
		}
	}
	var mounts []Mount
	if env.Mounts != nil {
		if mounts, err = env.Mounts(); err != nil {
			return nil, fmt.Errorf("reading the mount table: %w", err)
		}
	}
	for _, m := range mounts {
		if isCloudFS(m.FSType) && m.Point != "/" && within(m.Point, dir) {
			return nil, invalid("The cache folder can't contain the network or online drive mounted at %s.", m.Point)
		}
	}
	m, ok := MountFor(dir, mounts)
	if ok {
		switch {
		case isCloudFS(m.FSType):
			return nil, invalid("The cache can't be on an online or network drive (%s) - choose a folder on a disk in this server.", m.Point)
		case ramFS[m.FSType]:
			return nil, invalid("%s is in memory (%s), so the cache would use RAM and vanish on restart - choose a folder on a disk.", dir, m.FSType)
		case pseudoFS[m.FSType]:
			return nil, invalid("%s is not on a disk - choose another location for the cache.", dir)
		}
	}
	if env.ProbeWritable != nil {
		if err := env.ProbeWritable(dir); err != nil {
			return nil, invalid("NivaroOS can't write to %s: %v", dir, err)
		}
	}
	if env.Free != nil {
		free, ferr := env.Free(dir)
		if ferr != nil {
			return nil, invalid("Can't check the free space at %s: %v", dir, ferr)
		}
		avail := int64(free)
		if env.CacheUsed != nil {
			avail += env.CacheUsed(dir)
		}
		if s.MaxSize >= avail {
			return nil, invalid("The maximum cache size (%s) must be less than the free space at %s (%s).", HumanBytes(s.MaxSize), dir, HumanBytes(avail))
		}
		if s.MaxSize > avail*9/10 {
			warnings = append(warnings, fmt.Sprintf("The cache may use almost all the free space at %s (%s free).", dir, HumanBytes(avail)))
		}
	}
	if ok && m.Point == "/" {
		warnings = append(warnings, "This folder is on the system disk. A full cache leaves less room for NivaroOS and your apps - keep the maximum size well below the free space, or choose a folder on a data drive.")
	}
	if s.Mode == ModeOff {
		warnings = append(warnings, "With the cache off, files stream straight from the cloud: videos may take longer to seek, and some apps can't save to online storage (they need to write parts of a file out of order).")
	}
	return warnings, nil
}

// HumanBytes formats n like "20 GB" (binary units, as the UI shows them).
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	if val >= 10 || val == float64(int64(val)) {
		return fmt.Sprintf("%.0f %cB", val, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", val, "KMGTPE"[exp])
}
