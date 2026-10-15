package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs/accounting"
	rlog "github.com/rclone/rclone/fs/log"
)

// liveStats builds a job's Stats from rclone's accounting group
// "bk_<run_id>" (spec §10.2) and the job's own progress counters.
//
// Only rclone's locked getters are used: RemoteStats reads errors and
// lastError outside its lock, a data race with every failed transfer.
func (e *Engine) liveStats(j *job) Stats {
	j.mu.Lock()
	p := j.progress
	step := j.step
	state := j.state
	j.mu.Unlock()
	st := Stats{Step: step, TotalBytes: p.totalBytes, TotalFiles: p.totalFiles, CurrentFile: p.current}
	if state == JobQueued || state == JobDone || state == JobError {
		// Done jobs have their group deleted; StatsGroup would recreate it.
		return st
	}
	statsMu.Lock()
	sg := accounting.StatsGroup(context.Background(), j.group)
	st.Bytes, st.Files, st.Errors = sg.GetBytes(), sg.GetTransfers(), sg.GetErrors()
	if done := sg.Transferred(); len(done) > 0 {
		st.CurrentFile = done[len(done)-1].Name
	}
	statsMu.Unlock()
	if p.own {
		// Archive and extract stream through the engine itself.
		st.Bytes, st.Files = p.bytes, p.files
		st.CurrentFile = p.current
	}

	// Average speed since the first byte moved.
	// ponytail: whole-run average, not rclone's moving one; add a window if
	// a slow start skews the ETA.
	j.mu.Lock()
	if j.rateStart.IsZero() && st.Bytes > 0 {
		j.rateStart, j.rateBytes = time.Now(), st.Bytes
	}
	start, base := j.rateStart, j.rateBytes
	j.mu.Unlock()
	if p.own {
		start, base = p.ownStart, 0
	}
	if el := time.Since(start).Seconds(); !start.IsZero() && el >= 1 {
		st.SpeedBps = int64(float64(st.Bytes-base) / el)
	}
	st.TotalBytes = max(st.TotalBytes, st.Bytes)
	st.TotalFiles = max(st.TotalFiles, st.Files)
	if st.SpeedBps > 0 && st.TotalBytes > st.Bytes {
		v := (st.TotalBytes - st.Bytes) / st.SpeedBps
		st.ETASec = &v
	}
	return st
}

func fmtAny(v interface{}) string { return fmt.Sprint(v) }

// rclone's own log lines (errors and notices) can't carry a context, so
// they can't be tied to a job reliably. They always reach the journal
// through rclone's default output; when exactly one job is running they
// are also added to that job's log as "raw" lines, which is what the run
// window's "technical output" shows.
var (
	logHookMu     sync.Mutex
	logHookEngine []*Engine
)

func installRcloneLogHook() {
	rlog.Handler.AddOutput(false, func(level slog.Level, text string) {
		if level < slog.LevelWarn {
			return
		}
		logHookMu.Lock()
		engines := append([]*Engine(nil), logHookEngine...)
		logHookMu.Unlock()
		var running []*job
		for _, e := range engines {
			running = append(running, e.jobs.runningJobs()...)
		}
		if len(running) != 1 {
			return
		}
		lvl := LogWarn
		if level >= slog.LevelError {
			lvl = LogError
		}
		running[0].logRaw(lvl, strings.TrimRight(text, "\n"))
	})
}

func registerLogEngine(e *Engine) {
	logHookMu.Lock()
	logHookEngine = append(logHookEngine, e)
	logHookMu.Unlock()
}

func unregisterLogEngine(e *Engine) {
	logHookMu.Lock()
	defer logHookMu.Unlock()
	for i, x := range logHookEngine {
		if x == e {
			logHookEngine = append(logHookEngine[:i], logHookEngine[i+1:]...)
			return
		}
	}
}
