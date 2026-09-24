package jobs

import (
	"time"

	"github.com/robfig/cron/v3"
)

// CronParser is the one cron dialect Backup accepts: 5 fields (minute,
// hour, day of month, month, day of week) or a descriptor (@daily,
// @every 6h), in the server's local time zone. It is the same parser
// core's Scheduled Tasks use, so a migrated cron means the same thing.
var CronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Clock is the scheduler clock (spec §17, frozen). In the module layout
// (spec §0) Backup owns its own robfig cron.Cron instead of sharing
// core's; the interface is unchanged so triggers can be tested with a
// fake clock.
//
// The real clock holds every entry until the clock gate opens (time is
// NTP-synced, or 10 minutes passed - then it logs clock_unsynced) and
// uses CronParser in time.Local.
type Clock interface {
	// Add registers fn to run on spec. It fails for a spec CronParser
	// rejects. Entries added before the gate opens start firing when it
	// opens.
	Add(spec string, fn func()) (cron.EntryID, error)
	// Remove unregisters an entry; unknown ids are ignored.
	Remove(id cron.EntryID)
	// Next is the first activation of spec strictly after from.
	Next(spec string, from time.Time) (time.Time, error)
	// Armed is closed once the clock gate opens. Catch-up waits on it.
	Armed() <-chan struct{}
}

// ClockStatus is what the real clock reports for capabilities and the
// "clock not synced" banner.
type ClockStatus interface {
	// Synced reports whether the gate opened because NTP synced (false
	// when it opened on the 10 minute timeout, or hasn't opened yet).
	Synced() bool
}
