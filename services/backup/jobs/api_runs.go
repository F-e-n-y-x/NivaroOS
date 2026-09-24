package jobs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Runs: list, detail, log, cancel, decide, preview (spec §11).

const (
	runsPageDefault    = 50
	runsPageMax        = 200
	previewPageDefault = 200
	previewPageMax     = 1000
)

// toRun is the API view of a run (RunRow minus file paths).
func (s *Service) toRun(r RunRow, job *Job) Run {
	out := Run{
		ID: r.ID, JobID: r.JobID, Kind: RunKind(r.Kind), Trigger: RunTrigger(r.Trigger), Status: RunStatus(r.Status),
		Phase: RunPhase(r.Phase), Attempt: r.Attempt, ErrorCode: ErrorCode(r.ErrorCode), Summary: DecodeMessage(r.Summary),
		QueuedAt: r.QueuedAt, StartedAt: r.StartedAt, EndedAt: r.EndedAt, Coalesced: r.Coalesced,
		Counts: RunCounts{
			Added: r.FilesAdded, Changed: r.FilesChanged, Deleted: r.FilesDeleted, Skipped: r.FilesSkipped,
			Errored: r.FilesErrored, BytesTransferred: r.BytesTransferred, BytesTotal: r.BytesTotal,
		},
		Steps: []RunStep{}, FileErrors: loadFileErrors(r.LogPath), HasLog: logExists(r.LogPath),
	}
	if job != nil {
		out.JobName = job.Name
		out.Steps = runSteps(*job, r)
	} else if RunKind(r.Kind) == KindPrune {
		if pj, err := s.loadRunJob(r); err == nil {
			out.JobName = pj.Name
		}
	}
	if r.GuardInfo != "" {
		var g engine.GuardInfo
		if json.Unmarshal([]byte(r.GuardInfo), &g) == nil {
			out.Guard = &g
		}
	}
	if RunKind(r.Kind) == KindRestore {
		if spec, err := decodeRestoreSpec(r.RestoreSpec); err == nil {
			out.Restore = &spec
		}
	}
	return out
}

// runSteps is the run window's step list, derived from the job's
// definition and how far the run got.
func runSteps(j Job, r RunRow) []RunStep {
	type step struct {
		phase RunPhase
		key   string
		args  map[string]interface{}
		hook  int // -1 when not a hook
	}
	var steps []step
	add := func(p RunPhase, key string, args map[string]interface{}, hook int) {
		steps = append(steps, step{p, key, args, hook})
	}
	kind := RunKind(r.Kind)
	add(PhasePrecheck, "backup.run.step.precheck", map[string]interface{}{"dest": j.Dest.Label}, -1)
	withHooks := kind == KindBackup || kind == KindPreview
	if kind == KindRestore {
		if spec, err := decodeRestoreSpec(r.RestoreSpec); err == nil && restoreToOriginal(j, spec) {
			withHooks = true
		}
	}
	if withHooks {
		for i, h := range j.Hooks {
			if h.Phase != HookPre {
				continue
			}
			switch h.Action {
			case HookStopApps:
				add(PhasePreHooks, "backup.run.step.stop_apps", map[string]interface{}{"apps": strings.Join(h.Apps, ", ")}, i)
			case HookStartApps:
				add(PhasePreHooks, "backup.run.step.start_apps", map[string]interface{}{"apps": strings.Join(h.Apps, ", ")}, i)
			case HookShutdownVM:
				add(PhasePreHooks, "backup.run.step.shutdown_vm", map[string]interface{}{"vm": h.VM}, i)
			case HookStartVM:
				add(PhasePreHooks, "backup.run.step.start_vm", map[string]interface{}{"vm": h.VM}, i)
			}
		}
	}
	switch {
	case kind == KindRestore:
		add(PhaseTransfer, "backup.run.step.restore", nil, -1)
	case j.Type == TypeArchive:
		add(PhaseTransfer, "backup.run.step.archive", nil, -1)
	default:
		add(PhaseTransfer, "backup.run.step.transfer", nil, -1)
	}
	if kind == KindBackup {
		if j.Options.Verify {
			add(PhaseVerify, "backup.run.step.verify", nil, -1)
		}
		switch {
		case j.Type == TypeMirror && j.Retention.VersionsDays > 0:
			add(PhasePrune, "backup.run.step.prune", map[string]interface{}{"days": j.Retention.VersionsDays}, -1)
		case j.Type == TypeArchive && j.Retention.KeepLast > 0:
			add(PhasePrune, "backup.run.step.prune_keep", map[string]interface{}{"keep": j.Retention.KeepLast}, -1)
		}
	}
	if withHooks {
		// Undo steps for what the pre hooks stop, then explicit post hooks.
		for i := len(j.Hooks) - 1; i >= 0; i-- {
			h := j.Hooks[i]
			if h.Phase != HookPre {
				continue
			}
			switch h.Action {
			case HookStopApps:
				add(PhasePostHooks, "backup.run.step.start_apps", map[string]interface{}{"apps": strings.Join(h.Apps, ", ")}, i)
			case HookShutdownVM:
				add(PhasePostHooks, "backup.run.step.start_vm", map[string]interface{}{"vm": h.VM}, i)
			}
		}
		for i, h := range j.Hooks {
			if h.Phase != HookPost {
				continue
			}
			switch h.Action {
			case HookStopApps:
				add(PhasePostHooks, "backup.run.step.stop_apps", map[string]interface{}{"apps": strings.Join(h.Apps, ", ")}, i)
			case HookStartApps:
				add(PhasePostHooks, "backup.run.step.start_apps", map[string]interface{}{"apps": strings.Join(h.Apps, ", ")}, i)
			case HookShutdownVM:
				add(PhasePostHooks, "backup.run.step.shutdown_vm", map[string]interface{}{"vm": h.VM}, i)
			case HookStartVM:
				add(PhasePostHooks, "backup.run.step.start_vm", map[string]interface{}{"vm": h.VM}, i)
			}
		}
	}

	order := map[RunPhase]int{PhasePrecheck: 0, PhasePreHooks: 1, PhaseTransfer: 2, PhaseVerify: 3, PhasePrune: 4, PhasePostHooks: 5}
	cur, reached := order[RunPhase(r.Phase)]
	status := RunStatus(r.Status)
	done := decodeHooksDone(r.HooksDone)
	hookDone := func(idx int, post bool) bool {
		for _, hd := range done {
			if hd.HookIdx == idx {
				if post {
					return hd.PostDone
				}
				return hd.PreDone
			}
		}
		return false
	}
	out := make([]RunStep, 0, len(steps))
	for _, st := range steps {
		p := order[st.phase]
		state := StepPending
		switch {
		case status == StatusQueued && !reached:
			state = StepPending
		case status == StatusSuccess:
			state = StepDone
		case !reached:
			state = StepPending
		case p < cur:
			state = StepDone
		case p == cur:
			switch {
			case st.hook >= 0 && hookDone(st.hook, st.phase == PhasePostHooks):
				state = StepDone
			case status == StatusRunning:
				state = StepActive
			case status == StatusWaitingUser:
				state = StepDone
			case status == StatusFailed || status == StatusCancelled || status == StatusInterrupted:
				state = StepFailed
			case status == StatusPartial:
				state = StepDone
			default:
				state = StepSkipped
			}
		default: // later phase
			switch {
			case status == StatusRunning || status == StatusQueued:
				state = StepPending
			case st.phase == PhasePostHooks && st.hook >= 0 && hookDone(st.hook, true):
				state = StepDone
			case status == StatusPartial:
				state = StepDone
			default:
				state = StepSkipped
			}
		}
		out = append(out, RunStep{Phase: st.phase, Key: st.key, Args: st.args, State: state})
	}
	return out
}

// jobsByID loads the jobs referenced by runs.
func (s *Service) jobsByID() map[string]*Job {
	jobs, err := s.store.ListJobs()
	out := map[string]*Job{}
	if err != nil {
		return out
	}
	for i := range jobs {
		out[jobs[i].ID] = &jobs[i]
	}
	return out
}

func (s *Service) handleRunsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := RunFilter{JobID: q.Get("job_id"), Before: q.Get("before"), Limit: runsPageDefault}
	fe := fieldErrors{}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > runsPageMax {
			fe.add("limit", FieldOutOfRange)
		}
		f.Limit = n
	}
	if v := q.Get("status"); v != "" {
		for _, st := range strings.Split(v, ",") {
			rs := RunStatus(strings.TrimSpace(st))
			if !validStatus(rs) {
				fe.add("status", FieldInvalid)
				continue
			}
			f.Statuses = append(f.Statuses, rs)
		}
	}
	if v := q.Get("kind"); v != "" {
		k := RunKind(v)
		if !validKind(k) {
			fe.add("kind", FieldInvalid)
		}
		f.Kinds = []RunKind{k}
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	rows, err := s.store.ListRuns(f)
	if err != nil {
		s.fail(w, err)
		return
	}
	jobs := s.jobsByID()
	out := RunList{Runs: make([]Run, 0, len(rows))}
	for _, row := range rows {
		out.Runs = append(out.Runs, s.toRun(row, jobs[row.JobID]))
	}
	if len(rows) == f.Limit && len(rows) > 0 {
		out.NextBefore = rows[len(rows)-1].ID
	}
	writeOK(w, http.StatusOK, out)
}

func validStatus(s RunStatus) bool {
	for _, x := range RunStatuses {
		if x == s {
			return true
		}
	}
	return false
}

func validKind(k RunKind) bool {
	for _, x := range RunKinds {
		if x == k {
			return true
		}
	}
	return false
}

func (s *Service) runDetail(r RunRow) RunDetail {
	var job *Job
	if j, err := s.store.GetJob(r.JobID); err == nil {
		job = &j
	}
	d := RunDetail{Run: s.toRun(r, job)}
	if RunStatus(r.Status) == StatusRunning {
		d.Live = s.liveStats(r.ID)
		if d.Live == nil {
			d.Live = &LiveStats{}
		}
	}
	return d
}

func (s *Service) handleRunGet(w http.ResponseWriter, r *http.Request) {
	row, err := s.store.GetRun(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, s.runDetail(row))
}

func (s *Service) handleRunLog(w http.ResponseWriter, r *http.Request) {
	row, err := s.store.GetRun(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	final := RunStatus(row.Status).Final()
	page := LogPage{Lines: []engine.LogLine{}, NextOffset: after}
	if row.LogPath == "" || !logExists(row.LogPath) {
		page.Done = final
		writeOK(w, http.StatusOK, page)
		return
	}
	lines, next, eof, err := ReadLogPage(row.LogPath, after, limit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.fail(w, err)
		return
	}
	page.Lines, page.NextOffset, page.Done = lines, next, final && eof
	writeOK(w, http.StatusOK, page)
}

func (s *Service) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.store.GetRun(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if RunStatus(row.Status).Final() {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: "the run already ended"})
		return
	}
	row, err = s.queue.Cancel(id, ErrCancelledByUser)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "cancel", row.JobID, "run="+id)
	writeOK(w, http.StatusOK, s.runDetail(row))
}

// decideMu serialises decisions, so a double click can't requeue twice.
var decideMu sync.Mutex

func (s *Service) handleRunDecide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req DecideRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Mode != "" && req.Mode != DecideAsShown && req.Mode != DecideCopyOnce {
		writeValidation(w, map[string]string{"mode": string(FieldInvalid)})
		return
	}
	decideMu.Lock()
	defer decideMu.Unlock()
	row, err := s.store.GetRun(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if RunStatus(row.Status) != StatusWaitingUser {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: "the run is not waiting for a decision"})
		return
	}
	if !req.Proceed {
		row.Decision = "decline"
		row, err = s.endWithoutRunning(row, StatusCancelled, ErrCancelledByUser, Message{
			Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(ErrCancelledByUser)},
		})
		if err != nil {
			s.fail(w, err)
			return
		}
		s.audit(r, "decide", row.JobID, "run="+id+" proceed=false")
		writeOK(w, http.StatusOK, s.runDetail(row))
		return
	}
	// Proceed: accept what was shown, once, for this run only.
	accepted := s.acceptedGuards(row)
	if err := s.store.SetMetaJSON(runOverrideKey(id), accepted); err != nil {
		s.fail(w, err)
		return
	}
	// ... bound to what was reviewed: more deletions or changes by the
	// time the run goes on stop it again.
	if rev := s.reviewedCounts(row); rev != nil {
		if err := s.store.SetMetaJSON(runReviewedKey(id), rev); err != nil {
			s.fail(w, err)
			return
		}
	} else if err := s.store.DeleteMeta(runReviewedKey(id)); err != nil {
		s.fail(w, err)
		return
	}
	row.Decision = "proceed"
	if req.Mode == DecideCopyOnce {
		row.Decision = DecideCopyOnce
	}
	if RunKind(row.Kind) == KindPreview {
		row.Kind = string(KindBackup) // the approved preview becomes the run
	}
	row.Status, row.EndedAt, row.ErrorCode, row.Phase, row.QueuedAt = string(StatusQueued), nil, "", "", s.now()
	if err := s.store.SaveRun(row); err != nil {
		s.fail(w, err)
		return
	}
	if row.LogPath != "" {
		if lg, err := OpenRunLog(orLogPath(s, row)); err == nil {
			lg.Info("backup.log.decision", map[string]interface{}{"decision": row.Decision})
			lg.Close()
		}
	}
	s.queue.requeue(row, true)
	s.audit(r, "decide", row.JobID, fmt.Sprintf("run=%s proceed=true mode=%s guards=%v", id, row.Decision, accepted))
	writeOK(w, http.StatusOK, s.runDetail(row))
}

// acceptedGuards are the guards a "proceed" accepts: the one that
// tripped, and after a preview everything the preview showed (deletes
// and changes), plus what earlier decisions on this run accepted.
func (s *Service) acceptedGuards(row RunRow) []string {
	set := map[string]bool{}
	var prev []string
	if _, err := s.store.GetMetaJSON(runOverrideKey(row.ID), &prev); err == nil {
		for _, g := range prev {
			set[g] = true
		}
	}
	if row.GuardInfo != "" {
		var g engine.GuardInfo
		if json.Unmarshal([]byte(row.GuardInfo), &g) == nil && g.Guard != "" {
			set[g.Guard] = true
		}
	}
	if row.PreviewPath != "" {
		set["delete"], set["change"] = true, true
	}
	out := make([]string, 0, len(set))
	for _, g := range []string{"delete", "change", "empty_source"} {
		if set[g] {
			out = append(out, g)
		}
	}
	return out
}

// reviewedCounts is what the user saw before a "proceed": the counts of
// the plan list they could review, else the tripped guard's count (an
// upper bound of both deletions and changes). nil when neither is known.
func (s *Service) reviewedCounts(row RunRow) *engine.Reviewed {
	if row.PreviewPath != "" {
		if page, err := readPreviewPage(row.PreviewPath, "", "", 0, 1); err == nil {
			return &engine.Reviewed{Deleted: page.Counts.Delete, Updated: page.Counts.Update}
		}
	}
	if row.GuardInfo != "" {
		var g engine.GuardInfo
		if json.Unmarshal([]byte(row.GuardInfo), &g) == nil && (g.Guard == "delete" || g.Guard == "change") {
			return &engine.Reviewed{Deleted: g.Count, Updated: g.Count}
		}
	}
	return nil
}

func (s *Service) handleRunPreview(w http.ResponseWriter, r *http.Request) {
	row, err := s.store.GetRun(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if row.PreviewPath == "" {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "no plan for this run"})
		return
	}
	q := r.URL.Query()
	op := q.Get("op")
	if op != "" && op != "add" && op != "update" && op != "delete" {
		writeValidation(w, map[string]string{"op": string(FieldInvalid)})
		return
	}
	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	limit := previewPageDefault
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > previewPageMax {
			writeValidation(w, map[string]string{"limit": string(FieldOutOfRange)})
			return
		}
		limit = n
	}
	page, err := readPreviewPage(row.PreviewPath, op, strings.ToLower(q.Get("q")), offset, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "the preview was cleaned up"})
			return
		}
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, page)
}

// readPreviewPage scans a plan file for one page of matching items and
// their total. Counts cover the whole plan.
func readPreviewPage(file, op, q string, offset int64, limit int) (PreviewPage, error) {
	f, err := os.Open(file)
	if err != nil {
		return PreviewPage{}, err
	}
	defer f.Close()
	page := PreviewPage{Items: []engine.PreviewItem{}, NextOffset: -1}
	var counts PreviewCounts
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var match int64
	for sc.Scan() {
		var it engine.PreviewItem
		if json.Unmarshal(sc.Bytes(), &it) != nil {
			continue
		}
		switch it.Op {
		case "add":
			counts.Add++
			counts.BytesAdd += it.Size
		case "update":
			counts.Update++
		case "delete":
			counts.Delete++
		case engine.PreviewOmittedAdd:
			counts.Add += it.Count
			counts.BytesAdd += it.Size
			continue
		}
		if (op != "" && it.Op != op) || (q != "" && !strings.Contains(strings.ToLower(it.Path), q)) {
			continue
		}
		if match >= offset && len(page.Items) < limit {
			page.Items = append(page.Items, it)
		}
		match++
	}
	if err := sc.Err(); err != nil {
		return PreviewPage{}, err
	}
	page.Counts, page.Total = counts, match
	if next := offset + int64(len(page.Items)); next < match {
		page.NextOffset = next
	}
	return page, nil
}
