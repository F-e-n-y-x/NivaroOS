package service

// Settings > Online storage > Cache: one cache setting set for every cloud
// mount, per-account usage and clearing, and uploads that can never finish.
// The on-disk rules (what is pending, what is safe to drop, moving a cache)
// live in pkg/cloudcache; this file ties them to the live mounts.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/cloudcache"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
	rlog "github.com/rclone/rclone/fs/log"
	"github.com/rclone/rclone/vfs"
	"github.com/rclone/rclone/vfs/vfscache"
	"github.com/rclone/rclone/vfs/vfscache/writeback"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"gopkg.in/ini.v1"
)

var (
	cacheSetMu  sync.RWMutex
	cacheSet    = cloudcache.Defaults()
	cacheOpMu   sync.Mutex // one settings change / clear / upload action at a time
	stuckStore  = cloudcache.OpenStuckStore(filepath.Join(os.TempDir(), "nivaroos-cloud-stuck-unset.json"))
	uploadFails = newFailLog()
	// Parked uploads are pushed this far into the future: rclone never
	// gets to them, and nothing else about the file changes.
	parkFor = 50 * 365 * 24 * time.Hour
	// cacheFallback is shown when the chosen folder was missing at start.
	cacheFallback string
)

// CloudCacheSettings is the setting set mounts use now.
func CloudCacheSettings() cloudcache.Settings {
	cacheSetMu.RLock()
	defer cacheSetMu.RUnlock()
	return cacheSet
}

func setCloudCacheSettings(s cloudcache.Settings) {
	cacheSetMu.Lock()
	cacheSet = s
	cacheSetMu.Unlock()
}

// InitCloudCache loads the settings (before any mount), opens the list of
// stuck uploads and starts watching upload failures. It returns the
// settings so the caller can put the cache directory in place.
func InitCloudCache(ctx context.Context) cloudcache.Settings {
	s := cloudcache.Load(config.Cfg)
	if s.Dir != cloudcache.DefaultDir {
		// A chosen folder on a drive that isn't there (not plugged in, not
		// mounted yet): don't create it on the system disk underneath.
		if st, err := os.Stat(s.Dir); err != nil || !st.IsDir() {
			cacheFallback = fmt.Sprintf("The cache folder %s isn't available, so the cache is using %s until it is back (restart NivaroOS, or save the settings again).", s.Dir, cloudcache.DefaultDir)
			logger.Error("cloud cache: chosen folder missing, using the default", zap.String("dir", s.Dir))
			s.Dir = cloudcache.DefaultDir
		}
	}
	setCloudCacheSettings(s)
	stuckStore = cloudcache.OpenStuckStore(cloudcache.StuckFile)
	fs.SetLogger(&uploadLogHook{Handler: rlog.Handler})
	go watchUploads(ctx)
	return s
}

// ---- reaching rclone's VFS cache ----

// vfsCacheOf returns the VFS's disk cache (nil when it has none). rclone
// keeps it in an unexported field; the test in cloud_cache_test.go fails
// if an rclone update renames it.
func vfsCacheOf(v *vfs.VFS) *vfscache.Cache {
	if v == nil {
		return nil
	}
	f := reflect.ValueOf(v).Elem().FieldByName("cache")
	if !f.IsValid() || f.Type() != reflect.TypeOf((*vfscache.Cache)(nil)) {
		return nil
	}
	c, _ := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(*vfscache.Cache)
	return c
}

func queueOf(c *vfscache.Cache) []writeback.QueueInfo {
	if c == nil {
		return nil
	}
	q, _ := c.Queue()["queue"].([]writeback.QueueInfo)
	return q
}

func isParked(q writeback.QueueInfo) bool { return q.Expiry > (365 * 24 * time.Hour).Seconds() }

// cloudMount is one live mount: account (rclone remote) name, mount point
// and its VFS.
type cloudMount struct {
	Name       string
	MountPoint string
	VFS        *vfs.VFS
}

func liveMounts() []cloudMount {
	mountMu.Lock()
	defer mountMu.Unlock()
	out := make([]cloudMount, 0, len(MountLists))
	for mp, m := range MountLists {
		if m == nil || m.Fs == nil {
			continue
		}
		out = append(out, cloudMount{Name: m.Fs.Name(), MountPoint: mp, VFS: m.VFS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func liveMount(name string) (cloudMount, bool) {
	for _, m := range liveMounts() {
		if m.Name == name {
			return m, true
		}
	}
	return cloudMount{}, false
}

// ---- upload failures ----

const uploadFailPrefix = "vfs cache: failed to upload try #"

// failLog keeps the last upload error per file name, fed from rclone's log.
type failLog struct {
	mu   sync.Mutex
	last map[string]string
	kick chan struct{}
}

func newFailLog() *failLog {
	return &failLog{last: map[string]string{}, kick: make(chan struct{}, 1)}
}

func (l *failLog) record(name, msg string) {
	l.mu.Lock()
	l.last[name] = msg
	l.mu.Unlock()
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

func (l *failLog) get(name string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.last[name]
	return m, ok
}

func (l *failLog) forget(name string) {
	l.mu.Lock()
	delete(l.last, name)
	l.mu.Unlock()
}

// uploadLogHook sits in front of rclone's log handler and notes upload
// failures. rclone logs them with its writeback lock held, so this only
// records and signals - watchUploads acts on it.
type uploadLogHook struct{ slog.Handler }

func (h *uploadLogHook) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError && strings.Contains(r.Message, uploadFailPrefix) {
		var name string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "object" {
				name = a.Value.String()
				return false
			}
			return true
		})
		if name != "" {
			uploadFails.record(name, r.Message)
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h *uploadLogHook) WithAttrs(a []slog.Attr) slog.Handler {
	return &uploadLogHook{Handler: h.Handler.WithAttrs(a)}
}

func (h *uploadLogHook) WithGroup(n string) slog.Handler {
	return &uploadLogHook{Handler: h.Handler.WithGroup(n)}
}

// retryBase remembers the try count at a user's Retry, so "N failed
// tries" counts from there.
var (
	retryBaseMu sync.Mutex
	retryBase   = map[string]int{}
)

func watchUploads(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-uploadFails.kick:
			time.Sleep(time.Second) // let rclone put the item back on its queue
		}
		checkUploads(liveMounts(), time.Now())
	}
}

// checkUploads parks uploads that failed for good (and ones already known
// to be stuck, after a remount), and forgets stuck entries whose file is no
// longer waiting.
func checkUploads(mounts []cloudMount, now time.Time) {
	dir := CloudCacheSettings().Dir
	for _, m := range mounts {
		c := vfsCacheOf(m.VFS)
		if c == nil {
			continue
		}
		for _, q := range queueOf(c) {
			if q.Uploading || isParked(q) {
				continue
			}
			if _, known := stuckStore.Get(m.Name, q.Name); known {
				_ = c.QueueSetExpiry(q.ID, now.Add(parkFor), 0)
				continue
			}
			msg, failed := uploadFails.get(q.Name)
			if !failed {
				continue
			}
			key := m.Name + ":" + q.Name
			retryBaseMu.Lock()
			tries := max(q.Tries-retryBase[key], 1) // it failed, so it was tried
			retryBaseMu.Unlock()
			if !cloudcache.ShouldStop(msg, tries) {
				continue
			}
			if err := c.QueueSetExpiry(q.ID, now.Add(parkFor), 0); err != nil {
				continue
			}
			it := cloudcache.Stuck{Remote: m.Name, Name: q.Name, Size: q.Size, Reason: cloudcache.Reason(msg, tries), Tries: q.Tries, Since: now}
			if err := stuckStore.Put(it); err != nil {
				logger.Error("cloud cache: couldn't save the stuck-upload list", zap.Error(err))
			}
			logger.Info("cloud upload can't finish; stopped retrying", zap.String("account", m.Name), zap.String("file", q.Name), zap.String("reason", it.Reason))
		}
	}
	// A stuck file that isn't waiting any more (deleted, or rewritten and
	// uploaded) drops off the list.
	for _, it := range stuckStore.List("") {
		if dirty, exists := readItemMeta(dir, it.Remote, it.Name); !exists || !dirty {
			_ = stuckStore.Delete(it.Remote, it.Name)
		}
	}
}

// readItemMeta: exists, dirty.
func readItemMeta(dir, remote, name string) (dirty bool, exists bool) {
	for _, p := range cloudcache.PendingItems(dir, remote) {
		if p.Name == name {
			return true, true
		}
	}
	_, err := os.Lstat(filepath.Join(cloudcache.MetaRoot(dir, remote), filepath.FromSlash(name)))
	return false, err == nil
}

// parkKnownStuck parks this mount's known-stuck uploads right after it
// mounts (rclone requeues every pending file when its cache loads).
func parkKnownStuck(m cloudMount) {
	c := vfsCacheOf(m.VFS)
	for _, q := range queueOf(c) {
		if _, ok := stuckStore.Get(m.Name, q.Name); ok && !q.Uploading {
			_ = c.QueueSetExpiry(q.ID, time.Now().Add(parkFor), 0)
		}
	}
}

// ---- status ----

type CacheSettingsWire struct {
	Mode    string `json:"mode"`
	MaxSize int64  `json:"max_size"`
	MaxAge  int64  `json:"max_age"` // seconds
	Dir     string `json:"dir"`
}

func toWire(s cloudcache.Settings) CacheSettingsWire {
	return CacheSettingsWire{Mode: s.Mode, MaxSize: s.MaxSize, MaxAge: int64(s.MaxAge / time.Second), Dir: s.Dir}
}

func (w CacheSettingsWire) Settings() cloudcache.Settings {
	return cloudcache.Settings{Mode: w.Mode, MaxSize: w.MaxSize, MaxAge: time.Duration(w.MaxAge) * time.Second, Dir: w.Dir}
}

type CacheAccount struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Type       string `json:"type"`
	MountPoint string `json:"mount_point"`
	Mounted    bool   `json:"mounted"`
	cloudcache.Usage
	Uploading int                `json:"uploading"`  // being uploaded right now
	Waiting   int                `json:"waiting"`    // pending, not stuck
	WaitBytes int64              `json:"wait_bytes"` // size of those
	Stuck     []cloudcache.Stuck `json:"stuck"`
}

type CacheStatus struct {
	Settings   CacheSettingsWire `json:"settings"`
	Defaults   CacheSettingsWire `json:"defaults"`
	Free       uint64            `json:"free"`  // bytes free where the cache is
	Total      uint64            `json:"total"` // size of that disk
	SystemDisk bool              `json:"system_disk"`
	Accounts   []CacheAccount    `json:"accounts"`
	Warnings   []string          `json:"warnings"`
}

type cloudAccount struct{ Name, MountPoint string }

func configuredAccounts() []cloudAccount {
	var out []cloudAccount
	for _, name := range rconfig.LoadedData().GetSectionList() {
		mp, _ := rconfig.LoadedData().GetValue(name, "mount_point")
		if mp != "" {
			out = append(out, cloudAccount{Name: name, MountPoint: mp})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func GetCloudCacheStatus() CacheStatus {
	s := CloudCacheSettings()
	st := CacheStatus{Settings: toWire(s), Defaults: toWire(cloudcache.Defaults()), Warnings: []string{}, Accounts: []CacheAccount{}}
	if cacheFallback != "" {
		st.Warnings = append(st.Warnings, cacheFallback)
	}
	var sfs unix.Statfs_t
	if unix.Statfs(s.Dir, &sfs) == nil {
		st.Free = sfs.Bavail * uint64(sfs.Bsize)
		st.Total = sfs.Blocks * uint64(sfs.Bsize)
	}
	if mounts, err := readMounts(); err == nil {
		if m, ok := cloudcache.MountFor(s.Dir, mounts); ok && m.Point == "/" {
			st.SystemDisk = true
		}
	}
	live := map[string]cloudMount{}
	for _, m := range liveMounts() {
		live[m.Name] = m
	}
	for _, a := range configuredAccounts() {
		acc := CacheAccount{Name: a.Name, MountPoint: a.MountPoint, Stuck: stuckStore.List(a.Name)}
		acc.Type, _ = rconfig.LoadedData().GetValue(a.Name, "type")
		acc.Label, _ = rconfig.LoadedData().GetValue(a.Name, "username")
		acc.Usage, _ = cloudcache.Scan(s.Dir, a.Name)
		if m, ok := live[a.Name]; ok {
			acc.Mounted = true
			for _, q := range queueOf(vfsCacheOf(m.VFS)) {
				if q.Uploading {
					acc.Uploading++
				}
			}
		}
		stuckNames := map[string]bool{}
		for _, it := range acc.Stuck {
			stuckNames[it.Name] = true
		}
		for _, p := range cloudcache.PendingItems(s.Dir, a.Name) {
			if !stuckNames[p.Name] {
				acc.Waiting++
				acc.WaitBytes += p.Size
			}
		}
		st.Accounts = append(st.Accounts, acc)
	}
	return st
}

// ---- changing the settings ----

// ErrCacheBusy is a refusal the user can fix by waiting (uploads running,
// files open).
type ErrCacheBusy struct{ Msg string }

func (e *ErrCacheBusy) Error() string { return e.Msg }

func readMounts() ([]cloudcache.Mount, error) {
	raw, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil, err
	}
	var out []cloudcache.Mount
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, cloudcache.Mount{Source: unescapeMount(f[0]), Point: unescapeMount(f[1]), FSType: f[2]})
	}
	return out, nil
}

// unescapeMount undoes /proc/mounts' octal escapes (\040 = space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func realEnv(currentDir string) cloudcache.Env {
	var mps []string
	for _, a := range configuredAccounts() {
		mps = append(mps, a.MountPoint)
	}
	return cloudcache.Env{
		Mounts: readMounts,
		Free: func(p string) (uint64, error) {
			// the folder may not exist yet: measure its nearest parent
			for {
				var st unix.Statfs_t
				err := unix.Statfs(p, &st)
				if err == nil {
					return st.Bavail * uint64(st.Bsize), nil
				}
				if !errors.Is(err, unix.ENOENT) || p == "/" {
					return 0, err
				}
				p = filepath.Dir(p)
			}
		},
		CacheUsed: func(dir string) int64 {
			if filepath.Clean(dir) != filepath.Clean(currentDir) {
				return 0
			}
			var n int64
			for _, r := range cloudcache.Remotes(dir) {
				u, _ := cloudcache.Scan(dir, r)
				n += u.UsedBytes - u.PendingBytes
			}
			return n
		},
		CloudMountPoints: mps,
		ProbeWritable: func(dir string) error {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			f, err := os.CreateTemp(dir, ".nivaroos-write-test-")
			if err != nil {
				return err
			}
			name := f.Name()
			_, werr := f.Write([]byte("ok"))
			cerr := f.Close()
			_ = os.Remove(name)
			if werr != nil {
				return werr
			}
			return cerr
		},
	}
}

// activeUploads counts uploads waiting or running on m that aren't parked.
func activeUploads(m cloudMount) (n int, bytes int64) {
	for _, q := range queueOf(vfsCacheOf(m.VFS)) {
		if q.Uploading || !isParked(q) {
			n++
			bytes += q.Size
		}
	}
	return n, bytes
}

// openUnder lists processes that have files open (or their working
// folder) inside mountPoint.
var openUnder = func(mountPoint string) []string {
	prefix := filepath.Clean(mountPoint) + "/"
	procs, _ := os.ReadDir("/proc")
	self := strconv.Itoa(os.Getpid())
	var out []string
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil || p.Name() == self {
			continue
		}
		base := filepath.Join("/proc", p.Name())
		hit := false
		if cwd, err := os.Readlink(filepath.Join(base, "cwd")); err == nil && (cwd == mountPoint || strings.HasPrefix(cwd, prefix)) {
			hit = true
		}
		if !hit {
			fds, _ := os.ReadDir(filepath.Join(base, "fd"))
			for _, fd := range fds {
				if t, err := os.Readlink(filepath.Join(base, "fd", fd.Name())); err == nil && (t == mountPoint || strings.HasPrefix(t, prefix)) {
					hit = true
					break
				}
			}
		}
		if hit {
			comm, _ := os.ReadFile(filepath.Join(base, "comm"))
			out = append(out, strings.TrimSpace(string(comm))+" ("+p.Name()+")")
		}
	}
	return out
}

// checkIdle refuses when a mount has uploads going or files open.
func checkIdle(mounts []cloudMount) error {
	for _, m := range mounts {
		if n, b := activeUploads(m); n > 0 {
			return &ErrCacheBusy{Msg: fmt.Sprintf("%s is still uploading %d %s (%s). Wait until the uploads finish, then save again - changing the cache now would interrupt them.", accountLabel(m.Name), n, plural(n, "file", "files"), cloudcache.HumanBytes(b))}
		}
		if who := openUnder(m.MountPoint); len(who) > 0 {
			return &ErrCacheBusy{Msg: fmt.Sprintf("Files on %s are open (%s). Close them, then save again.", accountLabel(m.Name), strings.Join(who, ", "))}
		}
	}
	return nil
}

func accountLabel(name string) string {
	if l, _ := rconfig.LoadedData().GetValue(name, "username"); l != "" {
		return l
	}
	return name
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ApplyCloudCache validates s and, unless dryRun, saves it and remounts
// every cloud drive with it (moving the cache when the location changes).
func ApplyCloudCache(w CacheSettingsWire, dryRun bool) (warnings []string, err error) {
	cacheOpMu.Lock()
	defer cacheOpMu.Unlock()
	old := CloudCacheSettings()
	s := w.Settings()
	warnings, err = cloudcache.Validate(&s, realEnv(old.Dir))
	if err != nil || dryRun {
		return warnings, err
	}
	if s == old {
		return warnings, nil
	}
	mounts := liveMounts()
	if err := checkIdle(mounts); err != nil {
		return warnings, err
	}
	if err := unmountForChange(mounts); err != nil {
		remountAll(mounts)
		return warnings, err
	}
	if s.Dir != old.Dir {
		moved, merr := moveCache(old.Dir, s.Dir)
		if merr != nil {
			// Everything went back to the old place: keep the old settings.
			_ = rconfig.SetCacheDir(old.Dir)
			remountAll(mounts)
			return warnings, merr
		}
		if moved > 0 {
			warnings = append(warnings, fmt.Sprintf("Moved %d %s waiting to upload to the new location; they carry on uploading from there.", moved, plural(moved, "file", "files")))
		}
	}
	setCloudCacheSettings(s)
	cacheFallback = ""
	cloudcache.Store(config.Cfg, s)
	if err := saveLocalStorageConf(config.Cfg); err != nil {
		warnings = append(warnings, "The settings are in use but couldn't be saved, so they'll reset on restart: "+err.Error())
	}
	if err := cloudcache.WriteEnvFile(cloudcache.EnvFile, s.Dir); err != nil {
		logger.Error("cloud cache: couldn't write the rclone daemon's cache env file", zap.Error(err))
	}
	if errs := remountAll(mounts); len(errs) > 0 {
		warnings = append(warnings, errs...)
	}
	logger.Info("cloud cache settings changed", zap.Any("settings", toWire(s)))
	return warnings, nil
}

var saveLocalStorageConf = func(cfg *ini.File) error { return cfg.SaveTo(config.ConfigFilePath) }

// moveCache points rclone at newDir and moves every pending upload there.
// If any pending file can't move, all moved ones go back and it fails:
// uploads are never stranded in a folder no mount reads.
func moveCache(oldDir, newDir string) (int, error) {
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		return 0, err
	}
	free := func(p string) (uint64, error) {
		var st unix.Statfs_t
		if err := unix.Statfs(p, &st); err != nil {
			return 0, err
		}
		return st.Bavail * uint64(st.Bsize), nil
	}
	res, err := cloudcache.Migrate(oldDir, newDir, cloudcache.MigrateOptions{Free: free})
	if err == nil && len(res.Left) == 0 {
		if serr := rconfig.SetCacheDir(newDir); serr != nil {
			err = serr
		} else {
			return res.MovedFiles, nil
		}
	}
	// Put back what moved.
	back, berr := cloudcache.Migrate(newDir, oldDir, cloudcache.MigrateOptions{Free: free})
	if berr != nil || len(back.Left) > 0 {
		logger.Error("cloud cache: couldn't move pending uploads back", zap.Error(berr), zap.Strings("left", back.Left))
	}
	if err == nil {
		err = &ErrCacheBusy{Msg: "Some files waiting to upload couldn't be moved (" + strings.Join(res.Left, "; ") + "), so the cache stays where it is."}
	}
	return 0, err
}

// unmountForChange unmounts every live cloud mount and waits until each
// is gone.
func unmountForChange(mounts []cloudMount) error {
	for _, m := range mounts {
		if err := MyService.Storage().UnmountStorage(m.MountPoint); err != nil {
			return fmt.Errorf("couldn't disconnect %s to apply the change: %w", accountLabel(m.Name), err)
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		left := 0
		mountMu.Lock()
		for _, m := range mounts {
			if _, ok := MountLists[m.MountPoint]; ok {
				left++
			}
		}
		mountMu.Unlock()
		if left == 0 {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the online drives didn't disconnect in time; nothing was changed")
}

func remountAll(mounts []cloudMount) (errs []string) {
	for _, m := range mounts {
		mountMu.Lock()
		_, already := MountLists[m.MountPoint]
		mountMu.Unlock()
		if already {
			continue
		}
		if err := MyService.Storage().MountStorage(m.MountPoint, m.Name); err != nil {
			// The mount watcher retries it (cloud_mount_watch.go).
			errs = append(errs, fmt.Sprintf("%s didn't reconnect (%v); it retries in the background.", accountLabel(m.Name), err))
		}
	}
	return errs
}

// ---- clearing ----

// ClearCloudCache drops cached data that is already uploaded for one
// account ("" = all, plus leftovers of removed accounts). Files waiting to
// upload, and files open right now, are kept.
func ClearCloudCache(name string) (cloudcache.ClearResult, error) {
	cacheOpMu.Lock()
	defer cacheOpMu.Unlock()
	dir := CloudCacheSettings().Dir
	var targets []string
	if name != "" {
		found := false
		for _, a := range configuredAccounts() {
			if a.Name == name {
				found = true
			}
		}
		if !found {
			return cloudcache.ClearResult{}, fmt.Errorf("no online storage account %q", name)
		}
		targets = []string{name}
	} else {
		targets = cloudcache.Remotes(dir)
	}
	var total cloudcache.ClearResult
	for _, r := range targets {
		var res cloudcache.ClearResult
		var err error
		if m, ok := liveMount(r); ok {
			res, err = clearMounted(dir, m)
		} else {
			res, err = cloudcache.Clear(dir, r)
		}
		total.RemovedFiles += res.RemovedFiles
		total.FreedBytes += res.FreedBytes
		total.KeptPending += res.KeptPending
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// clearMounted clears a mounted account through rclone's own cache, so its
// in-memory view stays right: only items that are uploaded, not dirty and
// not open are removed.
func clearMounted(dir string, m cloudMount) (cloudcache.ClearResult, error) {
	var res cloudcache.ClearResult
	c := vfsCacheOf(m.VFS)
	if c == nil {
		// Cache off: rclone isn't using what's left on disk.
		return cloudcache.Clear(dir, m.Name)
	}
	res.KeptPending = len(cloudcache.PendingItems(dir, m.Name))
	for _, name := range cloudcache.CleanItems(dir, m.Name) {
		if c.InUse(name) || c.DirtyItem(name) != nil {
			res.KeptPending++
			continue
		}
		data := filepath.Join(cloudcache.DataRoot(dir, m.Name), filepath.FromSlash(name))
		meta := filepath.Join(cloudcache.MetaRoot(dir, m.Name), filepath.FromSlash(name))
		var size int64
		if fi, err := os.Lstat(data); err == nil {
			size = diskUsage(fi)
		}
		c.Remove(name)
		// rclone only removes what it has loaded; anything else it
		// doesn't know about is safe to delete (still clean).
		if dirty, exists := readItemMeta(dir, m.Name, name); exists && !dirty {
			_ = os.Remove(meta)
			_ = os.Remove(data)
		}
		if _, err := os.Lstat(data); err != nil {
			res.RemovedFiles++
			res.FreedBytes += size
		}
	}
	return res, nil
}

func diskUsage(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*unix.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}

// ---- stuck upload actions ----

func findStuck(remote, name string) (cloudcache.Stuck, error) {
	it, ok := stuckStore.Get(remote, name)
	if !ok {
		return it, fmt.Errorf("%s isn't in the list of uploads that can't finish", filepath.Base(name))
	}
	return it, nil
}

// RetryStuckUpload puts a stuck upload back in the queue now.
func RetryStuckUpload(remote, name string) error {
	cacheOpMu.Lock()
	defer cacheOpMu.Unlock()
	if _, err := findStuck(remote, name); err != nil {
		return err
	}
	m, ok := liveMount(remote)
	if !ok {
		return fmt.Errorf("%s isn't connected - reconnect it first", accountLabel(remote))
	}
	c := vfsCacheOf(m.VFS)
	for _, q := range queueOf(c) {
		if q.Name != name {
			continue
		}
		retryBaseMu.Lock()
		retryBase[remote+":"+name] = q.Tries
		retryBaseMu.Unlock()
		uploadFails.forget(name)
		if err := stuckStore.Delete(remote, name); err != nil {
			return err
		}
		return c.QueueSetExpiry(q.ID, time.Now(), 0)
	}
	return fmt.Errorf("%s isn't waiting to upload any more", filepath.Base(name))
}

// DiscardStuckUpload throws away the file waiting to upload. What's
// already in the cloud (an older version of the file) is left alone.
func DiscardStuckUpload(remote, name string) error {
	cacheOpMu.Lock()
	defer cacheOpMu.Unlock()
	if _, err := findStuck(remote, name); err != nil {
		return err
	}
	return discardLocked(remote, name)
}

func discardLocked(remote, name string) error {
	dir := CloudCacheSettings().Dir
	if m, ok := liveMount(remote); ok {
		if err := discardInVFS(m.VFS, name); err != nil {
			return err
		}
	} else {
		_ = os.Remove(filepath.Join(cloudcache.DataRoot(dir, remote), filepath.FromSlash(name)))
		_ = os.Remove(filepath.Join(cloudcache.MetaRoot(dir, remote), filepath.FromSlash(name)))
	}
	uploadFails.forget(name)
	return stuckStore.Delete(remote, name)
}

// discardInVFS removes the pending local copy of name. A file that never
// reached the cloud is removed from the drive completely; for a changed
// file only the local change goes (File.Remove would also delete the
// version in the cloud).
func discardInVFS(v *vfs.VFS, name string) error {
	c := vfsCacheOf(v)
	if c == nil {
		return errors.New("this drive has no cache")
	}
	if node, err := v.Stat(name); err == nil {
		if f, ok := node.(*vfs.File); ok && f.DirEntry() == nil {
			return f.Remove()
		}
	}
	c.Remove(name)
	return nil
}

// SaveStuckUpload copies a stuck upload's file into destDir (a folder on
// this server), checks the copy, then drops the upload.
func SaveStuckUpload(remote, name, destDir string) (cloudcache.SaveResult, error) {
	cacheOpMu.Lock()
	defer cacheOpMu.Unlock()
	it, err := findStuck(remote, name)
	if err != nil {
		return cloudcache.SaveResult{}, err
	}
	destDir = filepath.Clean(strings.TrimSpace(destDir))
	if !filepath.IsAbs(destDir) {
		return cloudcache.SaveResult{}, errors.New("choose a folder on this server")
	}
	for _, a := range configuredAccounts() {
		if mp := filepath.Clean(a.MountPoint); destDir == mp || strings.HasPrefix(destDir, mp+"/") {
			return cloudcache.SaveResult{}, errors.New("choose a folder on a disk in this server, not on an online drive")
		}
	}
	dir := CloudCacheSettings().Dir
	if destDir == dir || strings.HasPrefix(destDir, dir+"/") {
		return cloudcache.SaveResult{}, errors.New("choose a folder outside the cache")
	}
	size := it.Size
	for _, p := range cloudcache.PendingItems(dir, remote) {
		if p.Name == name {
			size = p.Size
		}
	}
	res, err := cloudcache.SaveCopy(dir, remote, name, destDir, size)
	if err != nil {
		return res, err
	}
	if err := discardLocked(remote, name); err != nil {
		return res, fmt.Errorf("saved to %s, but couldn't remove it from the upload queue: %w", res.Path, err)
	}
	logger.Info("stuck cloud upload saved to a folder", zap.String("account", remote), zap.String("file", name), zap.String("to", res.Path))
	return res, nil
}
