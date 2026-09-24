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
	"github.com/rclone/rclone/fs/rc"
)

// liveStats builds a job's Stats from rclone's accounting group
// "bk_<run_id>" (spec §10.2) and the job's own progress counters.
func (e *Engine) liveStats(j *job) Stats {
	j.mu.Lock()
	p := j.progress
	step := j.step
	state := j.state
	j.mu.Unlock()
	st := Stats{Step: step, TotalBytes: p.totalBytes, TotalFiles: p.totalFiles, CurrentFile: p.current}
	if state == JobQueued {
		return st
	}
	sg := accounting.StatsGroup(context.Background(), j.group)
	out, err := sg.RemoteStats(false)
	if err == nil {
		st.Bytes = asInt64(out["bytes"])
		st.Files = asInt64(out["transfers"])
		st.Errors = asInt64(out["errors"])
		st.SpeedBps = asInt64(out["speed"])
		if tb := asInt64(out["totalBytes"]); tb > st.TotalBytes {
			st.TotalBytes = tb
		}
		if tf := asInt64(out["totalTransfers"]); tf > st.TotalFiles {
			st.TotalFiles = tf
		}
		if eta, ok := out["eta"].(float64); ok {
			v := int64(eta)
			st.ETASec = &v
		}
		if tr, ok := out["transferring"].([]rc.Params); ok && len(tr) > 0 {
			if name, ok := tr[0]["name"].(string); ok {
				st.CurrentFile = name
			}
		}
	}
	if p.own {
		// Archive and extract stream through the engine itself.
		st.Bytes, st.Files = p.bytes, p.files
		st.CurrentFile = p.current
		if el := time.Since(p.ownStart).Seconds(); el > 0 {
			st.SpeedBps = int64(float64(p.bytes) / el)
		}
		st.ETASec = nil
		if st.SpeedBps > 0 && st.TotalBytes > st.Bytes {
			v := (st.TotalBytes - st.Bytes) / st.SpeedBps
			st.ETASec = &v
		}
	}
	return st
}

func asInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case int32:
		return int64(x)
	}
	return 0
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
