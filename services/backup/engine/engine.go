package engine

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	rlog "github.com/rclone/rclone/fs/log"
	"golang.org/x/sys/unix"
)

// Config says where the engine finds the system. The zero value of every
// field means the real system path; tests point them into t.TempDir().
type Config struct {
	// MountInfoPath is the mount table (default /proc/self/mountinfo).
	MountInfoPath string
	// SysBlockDir, UdevDataDir and DevDir are /sys/class/block,
	// /run/udev/data and /dev.
	SysBlockDir, UdevDataDir, DevDir string
	// MachineIDPath identifies this machine in destination markers
	// (default /etc/machine-id).
	MachineIDPath string

	// DataRoot is the NivaroOS data folder whose sub-folders are offered
	// as presets (default /DATA).
	DataRoot string
	// AllowedRoots replaces DefaultAllowedRoots when set.
	AllowedRoots []string
	// StagingDir is also allowed (default /var/lib/nivaroos/backup/staging).
	StagingDir string
	// SpoolDir holds the engine's per-job log spools; it is emptied at
	// start (default /var/lib/nivaroos/backup/engine).
	SpoolDir string

	// RcloneConfigPath overrides rclone's default config file - the one
	// local-storage writes (/root/.config/rclone/rclone.conf for root).
	RcloneConfigPath string

	// MaxRunning and MaxCloud cap concurrent jobs (defaults 2 and 1).
	MaxRunning, MaxCloud int
	// KeepFinished is how long finished job results stay (default 1 h).
	KeepFinished time.Duration
	// WatchBackstop is how often the mount table is re-read even without
	// a change notification (default 60 s).
	WatchBackstop time.Duration

	// NoMemoryLimit skips debug.SetMemoryLimit (tests).
	NoMemoryLimit bool
	// Logf receives the engine's own diagnostics (default log.Printf).
	Logf func(format string, args ...interface{})
	// Now is the clock (default time.Now).
	Now func() time.Time
}

func (c *Config) setDefaults() {
	def := func(p *string, v string) {
		if *p == "" {
			*p = v
		}
	}
	def(&c.MountInfoPath, "/proc/self/mountinfo")
	def(&c.SysBlockDir, "/sys/class/block")
	def(&c.UdevDataDir, "/run/udev/data")
	def(&c.DevDir, "/dev")
	def(&c.MachineIDPath, "/etc/machine-id")
	def(&c.DataRoot, "/DATA")
	def(&c.StagingDir, "/var/lib/nivaroos/backup/staging")
	def(&c.SpoolDir, "/var/lib/nivaroos/backup/engine")
	if c.AllowedRoots == nil {
		c.AllowedRoots = DefaultAllowedRoots
	}
	if c.MaxRunning <= 0 {
		c.MaxRunning = 2
	}
	if c.MaxCloud <= 0 {
		c.MaxCloud = 1
	}
	if c.KeepFinished <= 0 {
		c.KeepFinished = time.Hour
	}
	if c.WatchBackstop <= 0 {
		c.WatchBackstop = 60 * time.Second
	}
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Engine implements API. Create it with New and stop it with Close.
type Engine struct {
	cfg     Config
	policy  rootPolicy
	started time.Time
	host    string // marker host id

	ctx    context.Context // ends at Close
	cancel context.CancelFunc
	wg     sync.WaitGroup

	snapMu sync.RWMutex
	snap   *snapshot

	about *aboutCache
	jobs  *jobManager
	watch *mountWatch
	subs  *eventHub
}

var _ API = (*Engine)(nil)

var rcloneInit sync.Once

// New starts an engine: it reads the mount table, starts the mount
// watcher and the job scheduler, and loads rclone's config file (the
// one local-storage maintains; rclone re-reads it whenever it changes on
// disk, so accounts added or refreshed there are picked up without a
// restart).
func New(cfg Config) (*Engine, error) {
	cfg.setDefaults()
	rcloneInit.Do(func() {
		configfile.Install()
		// rclone logs a NOTICE per file in a dry run and INFO per
		// transfer; the run log has those as structured lines, so the
		// journal only gets warnings and errors.
		rlog.Handler.SetLevel(slog.LevelWarn)
		installRcloneLogHook()
	})
	if cfg.RcloneConfigPath != "" {
		if err := config.SetConfigPath(cfg.RcloneConfigPath); err != nil {
			return nil, fmt.Errorf("rclone config path %s: %w", cfg.RcloneConfigPath, err)
		}
	}
	if !cfg.NoMemoryLimit {
		limit := SetMemoryLimit()
		cfg.Logf("engine: memory limit %d MiB", limit>>20)
	}
	if err := os.MkdirAll(cfg.SpoolDir, 0o700); err != nil {
		return nil, fmt.Errorf("engine spool dir: %w", err)
	}
	cleanSpool(cfg.SpoolDir, cfg.Logf)

	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		cfg:     cfg,
		policy:  newRootPolicy(cfg.AllowedRoots, cfg.StagingDir),
		started: cfg.Now(),
		host:    machineHost(cfg.MachineIDPath),
		ctx:     ctx,
		cancel:  cancel,
		about:   newAboutCache(ctx),
		subs:    newEventHub(),
	}
	e.jobs = newJobManager(e)
	snap, err := e.takeSnapshot()
	if err != nil {
		cancel()
		return nil, err
	}
	e.snap = snap
	e.watch = newMountWatch(e, snap)
	if err := e.watch.start(); err != nil {
		cancel()
		return nil, err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.jobs.janitor(ctx)
	}()
	registerLogEngine(e)
	return e, nil
}

// Close cancels every job, stops the watcher and waits for both.
func (e *Engine) Close() error {
	unregisterLogEngine(e)
	e.jobs.stopAll()
	e.cancel()
	e.watch.stop()
	e.jobs.wait()
	e.about.close()
	e.wg.Wait()
	e.subs.closeAll()
	return nil
}

func (e *Engine) now() time.Time { return e.cfg.Now() }

// Health reports the contract version, the linked rclone and this
// process.
func (e *Engine) Health(ctx context.Context) (Health, error) {
	if err := e.ctx.Err(); err != nil {
		return Health{}, Errorf(CodeEngineUnavailable, "engine is shutting down")
	}
	return Health{API: APIVersion, Rclone: RcloneVersion(), PID: os.Getpid(), StartedAt: e.started}, nil
}

// current returns the last mount snapshot.
func (e *Engine) current() *snapshot {
	e.snapMu.RLock()
	defer e.snapMu.RUnlock()
	return e.snap
}

// fresh re-reads the mount table (resolution happens at the moment of
// use, spec §6.2) and publishes the result.
func (e *Engine) fresh() (*snapshot, error) {
	s, err := e.takeSnapshot()
	if err != nil {
		return nil, err
	}
	e.publish(s)
	return s, nil
}

// publish makes s the current snapshot and cancels every running job
// whose mounts are gone (spec §6.2 cancel on unmount).
func (e *Engine) publish(s *snapshot) {
	e.snapMu.Lock()
	e.snap = s
	e.snapMu.Unlock()
	e.jobs.checkMounts(s)
}

// mountPresent is the cheap check wrapped filesystems run before every
// write: is the mount still in the current table?
func (e *Engine) mountPresent(id int) bool {
	s := e.current()
	_, ok := s.table.byMountID(id)
	return ok
}

// SetMemoryLimit applies the engine's soft memory limit (spec §3.5): 25 %
// of RAM, at least 512 MiB and at most 2 GiB, so a large job makes the
// garbage collector work harder instead of taking the box down. It
// returns the limit in bytes. New calls it unless Config.NoMemoryLimit.
func SetMemoryLimit() int64 {
	const minLimit, maxLimit = 512 << 20, 2 << 30
	limit := int64(maxLimit)
	var si unix.Sysinfo_t
	if err := unix.Sysinfo(&si); err == nil {
		total := int64(si.Totalram) * int64(si.Unit)
		limit = total / 4
	}
	if limit < minLimit {
		limit = minLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	debug.SetMemoryLimit(limit)
	return limit
}

// cleanSpool removes log spools left by a previous process: their jobs
// died with it.
func cleanSpool(dir string, logf func(string, ...interface{})) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			logf("engine: removing stale spool %s: %v", e.Name(), err)
		}
	}
}

func statDev(fi os.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

// rcloneGlobalDefaults is applied to the context of every engine
// operation (spec §3.5): bounded parallelism, no --fast-list (it holds
// the whole listing in memory), and a few low-level retries; the job
// side owns the high-level retries.
func rcloneContext(ctx context.Context) (context.Context, *fs.ConfigInfo) {
	ctx, ci := fs.AddConfig(ctx)
	ci.Transfers = 4
	ci.Checkers = 8
	ci.BufferSize = 16 * fs.Mebi
	ci.UseListR = false
	ci.LowLevelRetries = 10
	ci.Retries = 1
	ci.AskPassword = false
	ci.Interactive = false
	return ctx, ci
}
