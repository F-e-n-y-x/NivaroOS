package jobs

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

// The restart loop. A backup that stopped apps or VMs starts them again
// in its post phase, and a crash is replayed at the next start - but
// either can happen while app-management or vm-sidecar isn't answering
// (at shutdown they may stop first; at boot they may not be up yet; an
// upgrade restarts them). Such a restart stays pending on its run
// (HookDone.PostDone false) and this loop tries it again with backoff,
// until the app or VM runs again or restartGiveUp has passed since the
// run ended, when the user is told to start it themselves.

const (
	defaultRestartRetry = 30 * time.Second
	maxRestartRetry     = 10 * time.Minute
	// restartIdle is the backstop tick when nothing is known to be
	// pending (a kick is lost only if the process dies right after).
	restartIdle = 10 * time.Minute
	// restartGiveUp: after this long an app that still doesn't start is
	// most likely broken or was removed; keep the box quiet.
	restartGiveUp = 24 * time.Hour
)

func (s *Service) restartRetry() time.Duration {
	if d := s.cfg.Timings.RestartRetry; d > 0 {
		return d
	}
	return defaultRestartRetry
}

// kickRestarts wakes the restart loop (never blocks).
func (s *Service) kickRestarts() {
	select {
	case s.restartKick <- struct{}{}:
	default:
	}
}

func (s *Service) restartLoop(ctx context.Context) {
	base := s.restartRetry()
	wait := restartIdle
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.restartKick:
			t.Stop()
			// A restart just failed: give its cause (a service still
			// starting or stopping) a moment before trying again.
			sleepCtx(ctx, base)
		case <-t.C:
		}
		if ctx.Err() != nil {
			return
		}
		if !s.retryRestarts() {
			wait = restartIdle
			continue
		}
		if wait >= restartIdle {
			wait = base
		} else {
			wait = min(wait*2, maxRestartRetry)
		}
	}
}

// retryRestarts makes one pass over every run with a pending restart and
// reports whether any is still pending afterwards.
func (s *Service) retryRestarts() bool {
	if s.store == nil {
		return false
	}
	rows, err := s.store.RunsWithPendingRestarts()
	if err != nil {
		log.Printf("backup: restarts: %v", err)
		return true
	}
	pending := false
	now := s.now()
	for _, r := range rows {
		job, err := s.loadRunJob(r)
		if err != nil {
			// The job was deleted since: restart with the default timeout.
			job = Job{ID: r.JobID}
		}
		x := &runExec{s: s, run: r, job: job, log: restartLog(s, r), hooks: decodeHooksDone(r.HooksDone), hooksOnly: true}
		ended := r.QueuedAt
		if r.EndedAt != nil {
			ended = *r.EndedAt
		}
		if now.Sub(ended) > restartGiveUp {
			x.giveUpRestarts()
			x.log.Close()
			continue
		}
		code := x.undoPreHooks()
		x.log.Close()
		for _, hd := range x.hooks {
			if !hd.PostDone {
				pending = true
			}
		}
		if code == "" {
			log.Printf("backup: run %s: started again what it stopped", r.ID)
		}
	}
	return pending
}

// restartLog is where a retry logs: the run's log while it is still a
// plain file, else nowhere (a compressed log is final; the service log
// and the notification carry the outcome).
func restartLog(s *Service, r RunRow) *RunLog {
	if r.LogPath != "" && !strings.HasSuffix(r.LogPath, ".gz") {
		if _, err := os.Stat(r.LogPath); err == nil {
			if lg, err := OpenRunLog(r.LogPath); err == nil {
				return lg
			}
		}
	}
	return &RunLog{}
}

// giveUpRestarts stops retrying a run's restarts and tells the user
// which apps and VMs are still off.
func (x *runExec) giveUpRestarts() {
	var names []string
	for i := range x.hooks {
		hd := &x.hooks[i]
		if hd.PostDone {
			continue
		}
		apps, vms := toRestart(*hd)
		names = append(names, apps...)
		names = append(names, vms...)
		hd.PostDone = true
	}
	if err := x.saveHooks(); err != nil {
		log.Printf("backup: run %s: %v", x.run.ID, err)
		return
	}
	if len(names) == 0 {
		return
	}
	log.Printf("backup: run %s: gave up starting %s again after %s", x.run.ID, strings.Join(names, ", "), restartGiveUp)
	x.s.notify(Notification{
		Message: Message{Key: "backup.notify.restart_gave_up", Args: map[string]interface{}{
			"job": x.job.Name, "targets": strings.Join(names, ", "),
		}},
		Level: NotifyLevelWarning, JobID: x.run.JobID, RunID: x.run.ID, WindowKind: "run",
		WindowProps: map[string]interface{}{"runId": x.run.ID, "jobId": x.run.JobID},
	})
}
