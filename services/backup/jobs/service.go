package jobs

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
)

// Config wires a Service. Only DataDir and Engine are required; every
// other collaborator defaults to the production one (loopback HTTP to
// the other NivaroOS services, the real clock).
type Config struct {
	DataDir     string
	RuntimePath string // /var/run/nivaroos
	Version     string
	Engine      EngineClient

	Clock     Clock // default NewRealClock(time.Local)
	Apps      AppController
	VMs       VMController
	Schedules ScheduleClient
	// ScheduleBusy is the Scheduled Tasks side of the hook locks
	// (default: ScheduleBusy over Schedules).
	ScheduleBusy BusyChecker
	Bus          Bus
	SMB          SMBSource
	// PublicKey verifies user JWTs (default: user-service's JWKS via
	// external.GetPublicKey).
	PublicKey func() (*ecdsa.PublicKey, error)
	// DeviceBackupRoot is where enrolled phones' backup folders go
	// (default DefaultDeviceBackupRoot, /DATA/Backup).
	DeviceBackupRoot string
	Now              func() time.Time
	// MinStateFree is the free space DataDir's disk needs for a run to
	// start (default 256 MiB; negative: no check).
	MinStateFree int64

	Timings Timings
}

// Timings shortens waits in tests; zero values are the production ones.
type Timings struct {
	EnginePoll     time.Duration // stats poll while a run transfers (1 s)
	WaitRecheck    time.Duration // when_unmet=wait re-check (5 min)
	EngineRetry    time.Duration // engine unavailable re-try (1 min)
	HookPoll       time.Duration // app / VM state poll (2 s)
	VolumeSettle   time.Duration // after a mount, before firing (30 s)
	Reconcile      time.Duration // mount table backstop (60 s)
	CatchUpDelay   time.Duration // after the clock gate opens (3 min)
	MigrationRetry time.Duration // while core is unreachable (30 s)
	Maintenance    time.Duration // stale / timeout / retention tick (1 h)
	RestartRetry   time.Duration // first retry of a failed app / VM restart (30 s)
}

// Service is the job side of Backup & Sync.
type Service struct {
	cfg       Config
	store     *Store
	storeErr  error
	engine    EngineClient
	clock     Clock
	apps      AppController
	vms       VMController
	sched     ScheduleClient
	schedBusy BusyChecker
	smb       SMBSource
	pub       *publisher
	queue     *Queue
	trig      *triggers
	downloads *downloadTokens
	limiter   *rateLimiter
	publicKey func() (*ecdsa.PublicKey, error)

	liveMu sync.Mutex
	lives  map[string]LiveStats

	migrateMu sync.Mutex

	// restartKick wakes the restart loop after a restart failed.
	restartKick chan struct{}

	ctx context.Context
	wg  sync.WaitGroup
}

// New opens the store and builds the service. When the store can't be
// opened the service still comes up - GET /health answers and every
// other route reports store_unavailable - and the error is returned for
// the caller to log.
func New(cfg Config) (*Service, error) {
	if cfg.Engine == nil {
		return nil, errors.New("jobs: no engine")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Service{
		cfg: cfg, engine: cfg.Engine, clock: cfg.Clock, apps: cfg.Apps, vms: cfg.VMs, sched: cfg.Schedules,
		schedBusy: cfg.ScheduleBusy, smb: cfg.SMB, publicKey: cfg.PublicKey,
		lives: map[string]LiveStats{}, downloads: newDownloadTokens(), limiter: newRateLimiter(30, time.Minute),
		ctx: context.Background(), restartKick: make(chan struct{}, 1),
	}
	if s.clock == nil {
		s.clock = NewRealClock(time.Local)
	}
	if s.apps == nil {
		s.apps = HTTPApps{RuntimePath: cfg.RuntimePath}
	}
	if s.vms == nil {
		s.vms = HTTPVMs{}
	}
	if s.sched == nil {
		s.sched = HTTPSchedules{RuntimePath: cfg.RuntimePath}
	}
	if s.schedBusy == nil {
		s.schedBusy = &ScheduleBusy{Client: s.sched}
	}
	if s.smb == nil {
		s.smb = &CoreDBSMB{}
	}
	if cfg.Bus == nil {
		cfg.Bus = HTTPBus{RuntimePath: cfg.RuntimePath}
	}
	if s.publicKey == nil {
		rp := cfg.RuntimePath
		s.publicKey = func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(rp) }
	}
	s.pub = newPublisher(cfg.Bus)
	s.queue = newQueue(s)
	s.trig = newTriggers(s)
	st, err := OpenStore(cfg.DataDir)
	if err != nil {
		s.storeErr = err
		return s, err
	}
	s.store = st
	return s, nil
}

func (s *Service) now() time.Time { return s.cfg.Now() }

const defaultMinStateFree = 256 << 20

func (s *Service) minStateFree() int64 {
	switch {
	case s.cfg.MinStateFree < 0:
		return math.MinInt64
	case s.cfg.MinStateFree > 0:
		return s.cfg.MinStateFree
	}
	return defaultMinStateFree
}

// freeBytes is the space an unprivileged write could still use on the
// filesystem holding dir.
func freeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// Store is the job store (nil when it couldn't be opened).
func (s *Service) Store() *Store { return s.store }

// Start recovers from the last shutdown and starts the background work:
// the event publisher, the clock gate, triggers, the queue, maintenance
// and the Scheduled Tasks migration. It returns at once.
func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	if s.store == nil {
		return fmt.Errorf("store unavailable: %w", s.storeErr)
	}
	s.goRun(func() { s.pub.run(ctx) })
	if rc, ok := s.clock.(*RealClock); ok {
		rc.onUnsynced = func() {
			s.notify(Notification{Message: Message{Key: "backup.notify.clock_unsynced"}, Level: NotifyLevelWarning})
		}
		rc.Start(ctx)
		s.goRun(func() {
			<-ctx.Done()
			rc.Stop()
		})
	}
	if err := s.recoverRuns(); err != nil {
		return err
	}
	if err := s.queue.load(); err != nil {
		return err
	}
	if err := s.trig.start(ctx); err != nil {
		return err
	}
	s.goRun(func() { s.queue.run(ctx) })
	s.goRun(func() { s.maintenance(ctx) })
	s.goRun(func() { s.restartLoop(ctx) })
	s.goRun(func() { s.migrationLoop(ctx) })
	return nil
}

func (s *Service) goRun(fn func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn()
	}()
}

// Wait blocks until every background goroutine and run has stopped
// (after the Start context ended), then closes the store.
func (s *Service) Wait() {
	s.wg.Wait()
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			log.Printf("backup: close store: %v", err)
		}
	}
}

// recoverRuns handles runs a crash or restart left running (spec §5):
// post-hook replay first, so no app stays stopped, then interrupted, then
// requeued once as a retry when the job allows retries. At boot
// app-management or vm-sidecar may not answer yet; a restart that fails
// stays pending and the restart loop retries it once they do.
func (s *Service) recoverRuns() error {
	rows, err := s.store.ListRuns(RunFilter{Statuses: []RunStatus{StatusRunning}})
	if err != nil {
		return err
	}
	for _, r := range rows {
		job, jerr := s.loadRunJob(r)
		if jerr == nil {
			if code := s.replayHooks(&r, job); code != "" {
				log.Printf("backup: run %s: post-hook replay: %s", r.ID, code)
			}
		}
		now := s.now()
		r.Status, r.ErrorCode = string(StatusInterrupted), string(ErrInterrupted)
		r.Summary, r.EndedAt = EncodeMessage(Message{Key: "backup.run.summary.interrupted"}), &now
		if r.LogPath != "" {
			if p, err := compressLog(orLogPath(s, r)); err == nil {
				r.LogPath = p
			}
		}
		if err := s.store.SaveRun(r); err != nil {
			return err
		}
		s.pub.publish(EventRunEnd, endProps(r, LiveStats{}))
		if jerr != nil {
			continue
		}
		switch RunKind(r.Kind) {
		case KindBackup, KindPreview, KindPrune:
		default:
			continue // a restore is not repeated behind the user's back
		}
		if job.Retry.Max > 0 && r.Attempt <= job.Retry.Max {
			if _, _, err := s.queue.Enqueue(job, EnqueueOptions{
				Kind: RunKind(r.Kind), Trigger: RunByRetry, Attempt: r.Attempt + 1, RestoreSpec: r.RestoreSpec,
			}); err != nil {
				log.Printf("backup: requeue interrupted run %s: %v", r.ID, err)
			}
		}
	}
	return nil
}

// notify sends a notification to the desktop.
func (s *Service) notify(n Notification) {
	s.pub.publish(EventNotify, n.props())
}

// jobChanged publishes job-changed and re-registers the job's triggers.
func (s *Service) jobChanged(j Job, change string) {
	s.pub.publish(EventJobChanged, map[string]string{
		PropJobID: j.ID, PropChange: change, PropRevision: strconv.Itoa(j.Revision),
	})
	if change == JobChangeDeleted {
		s.trig.removeJob(j.ID)
		return
	}
	s.trig.syncJob(j)
}

// rememberDrives records the USB drives a job uses (seen: when they were
// last seen, nil to leave that alone).
func (s *Service) rememberDrives(j Job, seen *time.Time) {
	eps := append([]Endpoint{j.Dest}, j.Sources...)
	for _, t := range j.Triggers {
		if t.VolumeRef != nil {
			eps = append(eps, *t.VolumeRef)
		}
	}
	for _, ep := range eps {
		if ep.Kind != EPUSB {
			continue
		}
		if err := s.store.RememberDrive(ep, seen); err != nil {
			log.Printf("backup: remember drive %s: %v", ep.RefID, err)
		}
	}
}

// normalizeJob is NormalizeJob plus the checks against the other stored
// jobs (checkDestOverlap). selfID is the job being edited, "" for a new
// one.
func (s *Service) normalizeJob(ctx context.Context, in Job, selfID string) (Job, map[string]string) {
	norm, fe := NormalizeJob(in, s.validateEnv(ctx))
	if _, bad := fe["dest.sub_path"]; bad {
		return norm, fe
	}
	others, err := s.store.ListJobs()
	if err != nil {
		// Saving without the check could let a mirror recycle another
		// job's backup; refusing for now is the safe side.
		log.Printf("backup: validate: list jobs: %v", err)
		if fe == nil {
			fe = map[string]string{}
		}
		fe["dest"] = string(ErrStoreUnavailable)
		return norm, fe
	}
	if selfID != "" && !hasJob(others, selfID) {
		return norm, fe // no such job: the caller answers 404
	}
	if fe == nil {
		fe = map[string]string{}
	}
	checkDestOverlap(norm, selfID, others, fe)
	return norm, fe
}

func hasJob(jobs []Job, id string) bool {
	for _, j := range jobs {
		if j.ID == id {
			return true
		}
	}
	return false
}

// validateEnv is the ValidateEnv for this service.
func (s *Service) validateEnv(ctx context.Context) ValidateEnv {
	settings, _ := s.store.Settings()
	return ValidateEnv{
		Settings: settings,
		Apps: func() (map[string]bool, error) {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return s.apps.List(cctx)
		},
		VMs: func() (map[string]bool, error) {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			vms, err := s.vms.List(cctx)
			if err != nil {
				return nil, err
			}
			out := make(map[string]bool, len(vms))
			for n := range vms {
				out[n] = true
			}
			return out, nil
		},
	}
}
