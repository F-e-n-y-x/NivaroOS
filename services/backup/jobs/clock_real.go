package jobs

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	"golang.org/x/sys/unix"
)

// Clock gate timing (spec §7.1).
const (
	clockGatePoll    = 10 * time.Second
	clockGateTimeout = 10 * time.Minute
)

// RealClock is the production Clock: one robfig cron.Cron in the server's
// local time zone behind the clock gate. Entries can be added at any time;
// none fires before the gate opens, which happens when the kernel reports
// the time as synchronised or after clockGateTimeout (then it logs
// clock_unsynced). This keeps boards without an RTC, which boot in 1970,
// from firing a night's schedule at the wrong time.
type RealClock struct {
	cron   *cron.Cron
	armed  chan struct{}
	once   sync.Once
	synced atomic.Bool

	// syncedFn and the timings are replaceable in tests.
	syncedFn    func() bool
	poll        time.Duration
	gateTimeout time.Duration
	// onUnsynced runs once when the gate opens on the timeout.
	onUnsynced func()
}

var (
	_ Clock       = (*RealClock)(nil)
	_ ClockStatus = (*RealClock)(nil)
)

// NewRealClock returns a clock whose gate is not open yet; call Start.
func NewRealClock(loc *time.Location) *RealClock {
	if loc == nil {
		loc = time.Local
	}
	return &RealClock{
		cron:        cron.New(cron.WithParser(CronParser), cron.WithLocation(loc)),
		armed:       make(chan struct{}),
		syncedFn:    kernelTimeSynced,
		poll:        clockGatePoll,
		gateTimeout: clockGateTimeout,
	}
}

// kernelTimeSynced reports whether the kernel considers the system clock
// synchronised: adjtimex(2) without modes (read only, no privilege
// needed) clears STA_UNSYNC once an NTP client (systemd-timesyncd,
// chrony, ntpd) disciplines the clock. This is what timedatectl's
// NTPSynchronized property reads, without depending on timedatectl being
// installed.
func kernelTimeSynced() bool {
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return false
	}
	return state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0
}

// Start runs the gate in the background until it opens or ctx ends, then
// starts the cron scheduler. Stop it with Stop.
func (c *RealClock) Start(ctx context.Context) {
	go func() {
		deadline := time.NewTimer(c.gateTimeout)
		defer deadline.Stop()
		tick := time.NewTicker(c.poll)
		defer tick.Stop()
		for {
			if c.syncedFn() {
				c.synced.Store(true)
				c.open()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-deadline.C:
				log.Printf("backup: clock_unsynced: the system time is not NTP-synchronised after %s; starting schedules anyway", c.gateTimeout)
				if c.onUnsynced != nil {
					c.onUnsynced()
				}
				c.open()
				// Keep watching, so capabilities stop showing the banner
				// once the time does sync.
				go c.watchSync(ctx)
				return
			case <-tick.C:
			}
		}
	}()
}

func (c *RealClock) watchSync(ctx context.Context) {
	tick := time.NewTicker(c.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if c.syncedFn() {
				c.synced.Store(true)
				return
			}
		}
	}
}

func (c *RealClock) open() {
	c.once.Do(func() {
		c.cron.Start()
		close(c.armed)
	})
}

// Stop stops the scheduler; running entries finish on their own.
func (c *RealClock) Stop() {
	select {
	case <-c.armed:
		c.cron.Stop()
	default:
	}
}

func (c *RealClock) Add(spec string, fn func()) (cron.EntryID, error) {
	sched, err := ParseSchedule(spec)
	if err != nil {
		return 0, err
	}
	return c.cron.Schedule(sched, cron.FuncJob(fn)), nil
}

func (c *RealClock) Remove(id cron.EntryID) { c.cron.Remove(id) }

func (c *RealClock) Next(spec string, from time.Time) (time.Time, error) {
	sched, err := ParseSchedule(spec)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

// ParseSchedule parses spec with CronParser for running it. A field spec
// ("30 2 * * *") names a wall-clock time, and when the clocks fall back
// that time happens twice; robfig would fire at both, so the second one
// is skipped - a nightly backup runs once that night (spec §7.1). A time
// that doesn't exist when the clocks spring forward is skipped by robfig
// itself. Constant-delay specs (@every 6h) count real time and are left
// as they are.
func ParseSchedule(spec string) (cron.Schedule, error) {
	sched, err := CronParser.Parse(spec)
	if err != nil {
		return nil, err
	}
	if ss, ok := sched.(*cron.SpecSchedule); ok {
		return wallOnce{ss}, nil
	}
	return sched, nil
}

// wallOnce is a SpecSchedule that never fires twice at the same local
// wall-clock minute.
type wallOnce struct{ *cron.SpecSchedule }

func (w wallOnce) Next(t time.Time) time.Time {
	n := w.SpecSchedule.Next(t)
	if n.IsZero() {
		return n
	}
	const minute = "2006-01-02 15:04"
	if loc := w.Location; loc != nil && n.In(loc).Format(minute) == t.In(loc).Format(minute) {
		return w.SpecSchedule.Next(n)
	}
	return n
}

func (c *RealClock) Armed() <-chan struct{} { return c.armed }

// Synced reports whether the kernel time is synchronised (the gate opened
// because of it, or it synced later).
func (c *RealClock) Synced() bool { return c.synced.Load() }

// isArmed reports whether a clock's gate has opened.
func isArmed(c Clock) bool {
	select {
	case <-c.Armed():
		return true
	default:
		return false
	}
}
