package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
)

// remote is one section of the shared rclone config (what local-storage
// calls a cloud account).
type remote struct {
	Name       string // section name, the endpoint RefID
	Type       string // backend: "drive", "terabox", ...
	Label      string // local-storage's "username" key, the name the user gave it
	MountPoint string // local-storage's FUSE mount of it, "/mnt/<type>_<name>"
}

// remotes lists the rclone config sections. rclone's config storage
// re-reads the file whenever its size or modtime changes, so this always
// reflects what local-storage last wrote.
func remotes() []remote {
	data := config.LoadedData()
	var out []remote
	for _, name := range data.GetSectionList() {
		typ, _ := data.GetValue(name, "type")
		if typ == "" {
			continue
		}
		label, _ := data.GetValue(name, "username")
		if label == "" {
			label = name
		}
		mp, _ := data.GetValue(name, "mount_point")
		out = append(out, remote{Name: name, Type: typ, Label: label, MountPoint: filepath.Clean(mp)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func findRemote(name string) (remote, bool) {
	for _, r := range remotes() {
		if r.Name == name {
			return r, true
		}
	}
	return remote{}, false
}

// remoteForMountPath maps a path under a cloud FUSE mount
// (/mnt/<type>_<name>/...) to the remote and the path inside it, so the
// engine never reads or writes through the mount (spec §3.6).
func remoteForMountPath(p string) (remote, string, bool) {
	for _, r := range remotes() {
		if r.MountPoint == "" || r.MountPoint == "." || r.MountPoint == "/" {
			continue
		}
		if pathWithin(p, r.MountPoint) {
			rel := strings.TrimPrefix(strings.TrimPrefix(p, r.MountPoint), "/")
			return r, rel, true
		}
	}
	return remote{}, "", false
}

// newBackendFs builds an rclone filesystem for backend at root with the
// given option overrides - the same thing fs.NewFs does for a
// "name,opt=val:root" string, without putting credentials or paths into
// a string rclone would parse (quotes and colons in names are safe).
// configName is the rclone.conf section for named remotes, or an
// on-the-fly name (":local", ":smb{hash}") for the rest.
func newBackendFs(ctx context.Context, backend, configName, root string, opts configmap.Simple) (fs.Fs, error) {
	info, err := fs.Find(backend)
	if err != nil {
		return nil, err
	}
	lookup := configName
	if strings.HasPrefix(lookup, ":") {
		lookup = "" // on-the-fly: nothing in rclone.conf, nothing to save back
	}
	m := fs.ConfigMap(info.Prefix, info.Options, lookup, opts)
	f, err := info.NewFs(ctx, configName, root, m)
	if err == fs.ErrorIsFile {
		// The root is a file: rclone returns its parent; callers want the
		// path itself to be a directory.
		return nil, Errorf(CodePathNotAllowed, "%s is a file, not a folder", root)
	}
	return f, err
}

// localFsOptions are the local backend settings for one side of a
// transfer.
func localFsOptions(q fsQuirks, links, oneFileSystem bool) configmap.Simple {
	o := configmap.Simple{
		"skip_specials":     "true", // sockets, FIFOs and devices are never copied
		"fatal_if_no_space": "true", // a full destination ends the run at once
	}
	if links {
		o["links"] = "true" // symlinks travel as .rclonelink files and are recreated
	}
	if oneFileSystem {
		o["one_file_system"] = "true" // a volume's backup never wanders into other mounts
	}
	if q.WinEncoding {
		o["encoding"] = windowsEncoding
	}
	if q.CaseInsensitive {
		o["case_insensitive"] = "true"
	}
	return o
}

// smbFsName is the on-the-fly config name of an SMB share: stable for one
// host, share and user, so a destination and its recycle folder count as
// the same remote, and never containing the password.
func smbFsName(c SMBCreds) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{c.Host, c.Share, c.User, c.Domain}, "\x00")))
	return ":smb{" + hex.EncodeToString(sum[:])[:10] + "}"
}

// smbFsOptions are the rclone smb backend settings for one share. The
// password is obscured the way rclone expects it and lives only in this
// map, for this call.
func smbFsOptions(c SMBCreds) (configmap.Simple, error) {
	o := configmap.Simple{"host": c.Host, "user": c.User}
	if c.Password != "" {
		p, err := obscure.Obscure(c.Password)
		if err != nil {
			return nil, Errorf(CodeInternal, "obscuring the share password: %w", err)
		}
		o["pass"] = p
	}
	if c.Domain != "" {
		o["domain"] = c.Domain
	}
	return o, nil
}

// cloudFsOptions are overrides for a named remote (spec §6.3 table).
func cloudFsOptions(q fsQuirks) configmap.Simple {
	o := configmap.Simple{}
	if q.Drive {
		o["skip_gdocs"] = "true"           // Google Docs can't be downloaded as files
		o["stop_on_upload_limit"] = "true" // 750 GB/day: stop cleanly, retry tomorrow
	}
	return o
}

// aboutCache remembers cloud free space for a minute (spec §6.1); About
// can take seconds and some providers rate-limit it.
type aboutCache struct {
	ctx     context.Context // the engine's: ends at Close
	mu      sync.Mutex
	entries map[string]*aboutEntry
	closed  bool
	loads   sync.WaitGroup // background refreshes, waited for by close
}

type aboutEntry struct {
	free, total *int64
	at          time.Time
	loading     bool
}

const (
	aboutTTL     = 60 * time.Second
	aboutTimeout = 5 * time.Second
)

func newAboutCache(ctx context.Context) *aboutCache {
	return &aboutCache{ctx: ctx, entries: map[string]*aboutEntry{}}
}

// close stops new refreshes and waits for running ones (they end with
// the engine's context).
func (c *aboutCache) close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.loads.Wait()
}

// get returns the cached usage of remote r and, when it is missing or
// stale, refreshes it in the background with load.
func (c *aboutCache) get(key string, now time.Time, load func(ctx context.Context) (free, total *int64, err error), logf func(string, ...interface{})) (free, total *int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil {
		e = &aboutEntry{}
		c.entries[key] = e
	}
	if !e.loading && !c.closed && now.Sub(e.at) > aboutTTL {
		e.loading = true
		c.loads.Add(1)
		go func() {
			defer c.loads.Done()
			ctx, cancel := context.WithTimeout(c.ctx, aboutTimeout)
			defer cancel()
			f, t, err := load(ctx)
			c.mu.Lock()
			defer c.mu.Unlock()
			e.loading = false
			e.at = time.Now()
			if err != nil {
				logf("engine: free space of %s: %v", key, err)
				return
			}
			e.free, e.total = f, t
		}()
	}
	return e.free, e.total
}

// usageOf asks a filesystem for its free and total space; nil when the
// backend can't tell.
func usageOf(ctx context.Context, f fs.Fs) (free, total *int64, err error) {
	about := f.Features().About
	if about == nil {
		return nil, nil, nil
	}
	u, err := about(ctx)
	if err != nil {
		return nil, nil, err
	}
	if u.Free != nil {
		v := *u.Free
		free = &v
	}
	if u.Total != nil {
		v := *u.Total
		total = &v
	}
	return free, total, nil
}

// cloudRoot joins a remote name and a path for display ("gdrive:photos").
func cloudRoot(name, sub string) string {
	return name + ":" + sub
}

func smbDisplay(c SMBCreds, sub string) string {
	return fmt.Sprintf("smb://%s/%s", c.Host, path.Join(c.Share, sub))
}
