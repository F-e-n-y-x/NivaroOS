package jobs

import (
	"context"
	"log"
	"math"
	"os"
	"sort"
	"time"
)

// Housekeeping, on a tick (spec §5, §9, §10.4): waiting runs past their
// 72 h decision window are cancelled, stale jobs are reported, and run
// logs are kept within the retention limits.

const (
	defaultMaintenance = time.Hour
	// Log retention (spec §9): the last maxRunsPerJob runs of each job,
	// nothing older than AppSettings.LogRetentionDays, and at most
	// maxLogBytes of logs in total (oldest go first).
	maxRunsPerJob = 100
	maxLogBytes   = 500 << 20
	// previewKeep: plan output of final runs is kept this long.
	previewKeep = 7 * 24 * time.Hour
	staleRepeat = 24 * time.Hour
)

func (s *Service) maintenance(ctx context.Context) {
	every := s.cfg.Timings.Maintenance
	if every <= 0 {
		every = defaultMaintenance
	}
	// First pass shortly after start (a box that was off for days).
	sleepCtx(ctx, every/60)
	lastRetention := time.Time{}
	for ctx.Err() == nil {
		s.expireDecisions()
		s.checkStale()
		if s.unresolvedImports() {
			// A drive that was unplugged at import time may be back.
			if _, err := s.Migrate(ctx); err != nil {
				log.Printf("backup: maintenance: migration pass: %v", err)
			}
		}
		s.downloads.gc(s.now())
		if s.now().Sub(lastRetention) >= 24*time.Hour || every < time.Hour {
			s.applyRetention()
			lastRetention = s.now()
		}
		sleepCtx(ctx, every)
	}
}

// expireDecisions cancels waiting_user runs nobody answered for 72 h.
// EndedAt of a waiting run is when it started waiting.
func (s *Service) expireDecisions() {
	rows, err := s.store.ListRuns(RunFilter{Statuses: []RunStatus{StatusWaitingUser}})
	if err != nil {
		log.Printf("backup: maintenance: %v", err)
		return
	}
	now := s.now()
	for _, r := range rows {
		since := r.QueuedAt
		if r.EndedAt != nil {
			since = *r.EndedAt
		}
		if now.Sub(since) < WaitingUserTimeout {
			continue
		}
		r.Decision = "decline"
		if _, err := s.endWithoutRunning(r, StatusCancelled, ErrDecisionTimeout, Message{
			Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(ErrDecisionTimeout)},
		}); err != nil {
			log.Printf("backup: run %s: %v", r.ID, err)
			continue
		}
		name := ""
		if j, err := s.store.GetJob(r.JobID); err == nil {
			name = j.Name
		}
		s.notify(Notification{
			Message: Message{Key: "backup.notify.decision_timeout", Args: map[string]interface{}{"job": name}},
			Level:   NotifyLevelWarning, JobID: r.JobID, RunID: r.ID, WindowKind: "run",
			WindowProps: map[string]interface{}{"runId": r.ID, "jobId": r.JobID},
		})
	}
}

// checkStale reports, at most once a day per job, an enabled job with a
// trigger that hasn't succeeded within its stale_after_hours.
func (s *Service) checkStale() {
	jobs, err := s.store.ListJobs()
	if err != nil {
		return
	}
	now := s.now()
	for _, j := range jobs {
		if !j.Enabled || len(j.Triggers) == 0 || j.Notify.StaleAfterHours <= 0 {
			continue
		}
		st, err := s.store.JobState(j.ID)
		if err != nil {
			continue
		}
		since := j.CreatedAt
		if st.LastSuccessAt != nil {
			since = *st.LastSuccessAt
		}
		if now.Sub(since) < time.Duration(j.Notify.StaleAfterHours)*time.Hour {
			continue
		}
		if st.StaleNotifiedAt != nil && now.Sub(*st.StaleNotifiedAt) < staleRepeat {
			continue
		}
		stamp := now
		if err := s.store.UpdateJobState(j.ID, func(st *JobState) { st.StaleNotifiedAt = &stamp }); err != nil {
			continue
		}
		days := int(math.Floor(now.Sub(since).Hours() / 24))
		s.notify(Notification{
			Message: Message{Key: "backup.notify.stale", Args: map[string]interface{}{"job": j.Name, "days": days}},
			Level:   NotifyLevelWarning, JobID: j.ID, WindowKind: "app",
			WindowProps: map[string]interface{}{"section": "jobs", "jobId": j.ID},
		})
	}
}

// applyRetention drops old final runs with their files, then trims logs
// to the total size cap. A job's newest successful run is always kept,
// so "last success" never disappears from its history.
func (s *Service) applyRetention() {
	settings, _ := s.store.Settings()
	days := settings.LogRetentionDays
	if days <= 0 {
		days = DefaultLogRetentionDays
	}
	cutoff := s.now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := s.store.ListRuns(RunFilter{})
	if err != nil {
		log.Printf("backup: retention: %v", err)
		return
	}
	perJob := map[string]int{}
	keptSuccess := map[string]bool{}
	var kept []RunRow
	for _, r := range rows { // newest first
		final := RunStatus(r.Status).Final()
		perJob[r.JobID]++
		keepSuccess := RunStatus(r.Status) == StatusSuccess && !keptSuccess[r.JobID]
		if keepSuccess {
			keptSuccess[r.JobID] = true
		}
		old := r.EndedAt != nil && r.EndedAt.Before(cutoff)
		if final && !keepSuccess && (perJob[r.JobID] > maxRunsPerJob || old) {
			removeRunFiles(r)
			if err := s.store.db.Delete(&RunRow{}, "id = ?", r.ID).Error; err != nil {
				log.Printf("backup: retention: delete run %s: %v", r.ID, err)
			}
			_ = s.clearRunDecision(r.ID)
			continue
		}
		if final && r.PreviewPath != "" && r.EndedAt != nil && s.now().Sub(*r.EndedAt) > previewKeep {
			_ = os.Remove(r.PreviewPath)
			r.PreviewPath = ""
			_ = s.store.SaveRun(r)
		}
		kept = append(kept, r)
	}
	// Size cap: oldest final logs first.
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	var total int64
	sizes := make([]int64, len(kept))
	for i, r := range kept {
		sizes[i] = fileSize(r.LogPath)
		total += sizes[i]
	}
	for i := 0; i < len(kept) && total > maxLogBytes; i++ {
		r := kept[i]
		if !RunStatus(r.Status).Final() || r.LogPath == "" {
			continue
		}
		removeRunFiles(RunRow{LogPath: r.LogPath})
		total -= sizes[i]
		r.LogPath = ""
		if err := s.store.SaveRun(r); err != nil {
			log.Printf("backup: retention: %v", err)
		}
	}
}
