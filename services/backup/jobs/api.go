package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The REST API (spec §11, contract in apitypes.go and
// docs/specs/backup-api.json). Every route answers under APIBase and
// without it; GET /health and GET /downloads/<token> need no JWT.

// maxBody caps a request body (a job is a few kB).
const maxBody = 1 << 20

// Handler is the service's HTTP handler.
func (s *Service) Handler() http.Handler {
	api := http.NewServeMux()
	s.routes(api)
	authed := s.requireAuth(api)
	devMux := http.NewServeMux()
	s.deviceRoutes(devMux)
	return withCORS(stripPrefix(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
			s.handleHealth(w, r)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/downloads/"):
			s.handleDownload(w, r, strings.TrimPrefix(r.URL.Path, "/downloads/"))
			return
		}
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, ErrorBody{ErrorCode: ErrStoreUnavailable, Detail: describeErr(s.storeErr)})
			return
		}
		// /devices/{id}/* take the device token only (devices.go).
		if id, scoped, bad := deviceScope(r.URL.Path); bad {
			writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: r.Method + " " + r.URL.Path})
			return
		} else if scoped {
			s.serveDevice(devMux, w, r, id)
			return
		}
		authed.ServeHTTP(w, r)
	})))
}

func (s *Service) routes(m *http.ServeMux) {
	m.HandleFunc("GET /capabilities", s.handleCapabilities)
	m.HandleFunc("GET /locations", s.handleLocations)
	m.HandleFunc("GET /locations/browse", s.handleLocationsBrowse)
	m.HandleFunc("POST /locations/resolve-path", s.handleResolvePath)
	m.HandleFunc("GET /jobs", s.handleJobsList)
	m.HandleFunc("POST /jobs", s.handleJobCreate)
	m.HandleFunc("GET /jobs/{id}", s.handleJobGet)
	m.HandleFunc("PUT /jobs/{id}", s.handleJobUpdate)
	m.HandleFunc("DELETE /jobs/{id}", s.handleJobDelete)
	m.HandleFunc("POST /jobs/{id}/toggle", s.handleJobToggle)
	m.HandleFunc("POST /jobs/{id}/run", s.handleJobRun)
	m.HandleFunc("POST /jobs/{id}/reconnect-dest", s.handleReconnectDest)
	m.HandleFunc("GET /jobs/{id}/versions", s.handleVersions)
	m.HandleFunc("GET /jobs/{id}/versions/{vid}/browse", s.handleVersionBrowse)
	m.HandleFunc("POST /jobs/{id}/restore", s.handleRestore)
	m.HandleFunc("POST /validate", s.handleValidate)
	m.HandleFunc("POST /cron/preview", s.handleCronPreview)
	m.HandleFunc("GET /runs", s.handleRunsList)
	m.HandleFunc("GET /runs/{id}", s.handleRunGet)
	m.HandleFunc("GET /runs/{id}/log", s.handleRunLog)
	m.HandleFunc("POST /runs/{id}/cancel", s.handleRunCancel)
	m.HandleFunc("POST /runs/{id}/decide", s.handleRunDecide)
	m.HandleFunc("GET /runs/{id}/preview", s.handleRunPreview)
	m.HandleFunc("POST /downloads", s.handleDownloadCreate)
	m.HandleFunc("GET /settings", s.handleSettingsGet)
	m.HandleFunc("PUT /settings", s.handleSettingsPut)
	m.HandleFunc("GET /migration", s.handleMigrationGet)
	m.HandleFunc("POST /migration/rerun", s.handleMigrationRerun)
	m.HandleFunc("GET /drives", s.handleDrivesList)
	m.HandleFunc("PUT /drives/{uuid}", s.handleDriveUpdate)
	m.HandleFunc("DELETE /drives/{uuid}", s.handleDriveForget)
	m.HandleFunc("GET /busy", s.handleBusy)
	m.HandleFunc("GET /devices", s.handleDevicesList)
	m.HandleFunc("POST /devices", s.handleDeviceEnroll)
	m.HandleFunc("DELETE /devices/{id}", s.handleDeviceRevoke)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: r.Method + " " + r.URL.Path})
	})
}

// ---------------------------------------------------------------------
// Responses

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("backup: write response: %v", err)
	}
}

func writeOK(w http.ResponseWriter, status int, data interface{}) {
	writeJSON(w, status, Envelope{Success: status, Message: "ok", Data: data})
}

func writeError(w http.ResponseWriter, status int, body ErrorBody) {
	writeJSON(w, status, Envelope{Success: status, Message: string(body.ErrorCode), Data: body})
}

func writeValidation(w http.ResponseWriter, fe map[string]string) {
	writeError(w, http.StatusBadRequest, ErrorBody{ErrorCode: ErrValidation, FieldErrors: fe})
}

// engineStatus is the HTTP status for an engine error code.
func engineStatus(c ErrorCode) int {
	switch c {
	case ErrNotFound:
		return http.StatusNotFound
	case ErrEngineUnavailable:
		return http.StatusServiceUnavailable
	case ErrPathNotAllowed, ErrEndpointUnknown, ErrInvalidFilter, ErrAmbiguousDevice, ErrDestInsideSource:
		return http.StatusBadRequest
	case ErrDestOffline, ErrSourceOffline, ErrCloudAuth, ErrDestMarkerMismatch:
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// fail answers an error with the right status and body.
func (s *Service) fail(w http.ResponseWriter, err error) {
	var rc *RevisionConflict
	var ee *engine.Error
	switch {
	case errors.As(err, &rc):
		cur := rc.Current
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrRevisionConflict, Current: &cur})
	case isNoRecord(err):
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: err.Error()})
	case errors.Is(err, errInvalidState):
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: err.Error()})
	case errors.As(err, &ee):
		writeError(w, engineStatus(ee.Code), ErrorBody{ErrorCode: ee.Code, Detail: ee.Detail})
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, ErrorBody{ErrorCode: ErrEngineUnavailable, Detail: err.Error()})
	default:
		log.Printf("backup: api: %v", err)
		writeError(w, http.StatusInternalServerError, ErrorBody{ErrorCode: ErrInternal, Detail: err.Error()})
	}
}

// decode reads a JSON body into v.
func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	body := http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, ErrorBody{ErrorCode: ErrValidation, Detail: "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

func (s *Service) audit(r *http.Request, action, jobID, detail string) {
	c := callerOf(r)
	log.Printf("backup: audit user=%q action=%s job=%s %s", c.User, action, jobID, detail)
}

func (s *Service) rateLimited(w http.ResponseWriter, r *http.Request) bool {
	if s.limiter.allow(callerOf(r).key(), s.now()) {
		return false
	}
	writeError(w, http.StatusTooManyRequests, ErrorBody{ErrorCode: ErrRateLimited})
	return true
}

// ---------------------------------------------------------------------
// Health, capabilities

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ServiceHealth{Installed: true, Running: true, Service: "backup", Version: s.cfg.Version})
}

func (s *Service) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	now := s.now()
	tz, off := ServerTimezone(now)
	settings, _ := s.store.Settings()
	caps := Capabilities{
		Version: s.cfg.Version, Timezone: tz, UTCOffset: off, ClockArmed: isArmed(s.clock),
		MaxConcurrent: settings.MaxConcurrent,
		Restic:        ResticCap{Path: "/usr/lib/nivaroos/bin/restic", Min: "0.14.0"},
	}
	if cs, ok := s.clock.(ClockStatus); ok {
		caps.ClockSynced = cs.Synced()
	}
	var info unix.Sysinfo_t
	if unix.Sysinfo(&info) == nil {
		caps.RAMBytes = int64(info.Totalram) * int64(info.Unit)
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if h, err := s.engine.Health(ctx); err == nil {
			caps.Engine = EngineCap{Available: h.API == engine.APIVersion, API: h.API, Rclone: h.Rclone}
		}
	}()
	go func() {
		defer wg.Done()
		_, err := s.apps.List(ctx)
		caps.Installed.AppManagement = err == nil
	}()
	go func() {
		defer wg.Done()
		_, err := s.vms.List(ctx)
		caps.Installed.VMManager = err == nil
	}()
	wg.Wait()
	writeOK(w, http.StatusOK, caps)
}

// ---------------------------------------------------------------------
// Locations

func (s *Service) handleLocations(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	if role != "" && role != RoleSource && role != RoleDest {
		writeValidation(w, map[string]string{"role": string(FieldInvalid)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	locs, err := s.locations(ctx, role)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, locs)
}

func (s *Service) handleLocationsBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ep := Endpoint{Kind: EndpointKind(q.Get("kind")), RefID: q.Get("ref_id"), SubPath: q.Get("sub_path")}
	fe := fieldErrors{}
	normalizeEndpoint(&ep, "endpoint", fe)
	delete(fe, "endpoint.sub_path") // an smb server root is browsable (it lists shares)
	if sub, ok := CleanSubPath(q.Get("sub_path")); ok {
		ep.SubPath = sub
	} else {
		fe.add("sub_path", ErrPathNotAllowed)
	}
	p, ok := CleanSubPath(q.Get("path"))
	if !ok {
		fe.add("path", ErrPathNotAllowed)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	creds, err := s.smbCredsFor(ctx, []Endpoint{ep})
	if err != nil {
		s.fail(w, err)
		return
	}
	req := engine.BrowseRequest{Endpoint: ep, Path: p, DirsOnly: q.Get("dirs_only") == "1" || q.Get("dirs_only") == "true"}
	if c, ok := creds[ep.RefID]; ok {
		req.SMBCreds = &c
	}
	res, err := s.engine.Browse(ctx, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	if res.Entries == nil {
		res.Entries = []engine.Entry{}
	}
	writeOK(w, http.StatusOK, res)
}

func (s *Service) handleResolvePath(w http.ResponseWriter, r *http.Request) {
	var req ResolvePathRequest
	if !decode(w, r, &req) {
		return
	}
	p := strings.TrimSpace(req.Path)
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || len(p) > maxSubPathLen {
		writeValidation(w, map[string]string{"path": string(ErrPathNotAllowed)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.engine.ResolvePath(ctx, engine.ResolvePathRequest{Path: path.Clean(p)})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, res)
}

// ---------------------------------------------------------------------
// Jobs

// jobItem builds a GET /jobs entry. online is the destination's state,
// resolved by the caller (nil = not known).
func (s *Service) jobItem(j Job, online *bool) (JobListItem, error) {
	item := JobListItem{Job: j, DestOnline: online != nil && *online}
	last, err := s.store.LastRun(j.ID, []RunKind{KindBackup, KindPreview}, finalStatuses)
	if err != nil {
		return item, err
	}
	if last != nil {
		item.LastRun = runBrief(*last)
	}
	active, err := s.store.LastRun(j.ID, nil, []RunStatus{StatusQueued, StatusRunning, StatusWaitingUser})
	if err != nil {
		return item, err
	}
	if active != nil {
		item.ActiveRun = runBrief(*active)
	}
	item.NextRun = s.trig.NextRun(j, s.now())
	st, _ := s.store.JobState(j.ID)
	item.Health = jobHealth(j, st, last, active, online, s.now())
	return item, nil
}

var finalStatuses = []RunStatus{StatusSuccess, StatusPartial, StatusFailed, StatusCancelled, StatusSkipped, StatusInterrupted}

func runBrief(r RunRow) *RunBrief {
	return &RunBrief{ID: r.ID, Kind: RunKind(r.Kind), Status: RunStatus(r.Status), EndedAt: r.EndedAt, Summary: DecodeMessage(r.Summary)}
}

// jobHealth is the list's status dot, worst first (apitypes.go).
func jobHealth(j Job, st JobState, last, active *RunRow, online *bool, now time.Time) string {
	if !j.Enabled {
		return HealthDisabled
	}
	if (active != nil && RunStatus(active.Status) == StatusWaitingUser) ||
		(last != nil && RunStatus(last.Status) == StatusFailed) || j.NeedsAttention == AttentionDestChanged {
		return HealthProblem
	}
	offlineDest := online != nil && !*online && (j.Dest.Kind == EPUSB || j.Dest.Kind == EPVolume || j.Dest.Kind == EPMerge)
	if offlineDest || (last != nil && RunStatus(last.Status) == StatusSkipped && ErrorCode(last.ErrorCode) == ErrDestOffline) {
		return HealthOffline
	}
	stale := false
	if len(j.Triggers) > 0 && j.Notify.StaleAfterHours > 0 {
		since := j.CreatedAt
		if st.LastSuccessAt != nil {
			since = *st.LastSuccessAt
		}
		stale = now.Sub(since) > time.Duration(j.Notify.StaleAfterHours)*time.Hour
	}
	if stale || j.NeedsAttention == AttentionMigratedUnresolved || (last != nil && RunStatus(last.Status) == StatusPartial) {
		return HealthWarning
	}
	return HealthOK
}

// destOnline resolves destinations concurrently, 3 s each.
func (s *Service) destOnline(ctx context.Context, jobs []Job) []*bool {
	out := make([]*bool, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j Job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			creds, err := s.smbCredsFor(cctx, []Endpoint{j.Dest})
			if err != nil {
				f := false
				out[i] = &f
				return
			}
			req := engine.ResolveRequest{Endpoint: j.Dest}
			if c, ok := creds[j.Dest.RefID]; ok {
				req.SMBCreds = &c
			}
			res, err := s.engine.Resolve(cctx, req)
			if err != nil && engine.CodeOf(err) == ErrEngineUnavailable {
				return // unknown
			}
			on := err == nil && res.Online
			out[i] = &on
		}(i, j)
	}
	wg.Wait()
	return out
}

func (s *Service) handleJobsList(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs()
	if err != nil {
		s.fail(w, err)
		return
	}
	online := s.destOnline(r.Context(), jobs)
	out := make([]JobListItem, 0, len(jobs))
	for i, j := range jobs {
		item, err := s.jobItem(j, online[i])
		if err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, item)
	}
	writeOK(w, http.StatusOK, out)
}

// jobDetail is GET /jobs/:id (and every job write's response).
func (s *Service) jobDetail(ctx context.Context, j Job) (JobDetail, error) {
	online := s.destOnline(ctx, []Job{j})[0]
	item, err := s.jobItem(j, online)
	if err != nil {
		return JobDetail{}, err
	}
	d := JobDetail{JobListItem: item}
	st, _ := s.store.JobState(j.ID)
	d.Stats.SizeBytes, d.Stats.LastSuccess = st.SizeBytes, st.LastSuccessAt
	if j.Type != TypeCopy && online != nil && *online {
		vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if vers, err := s.versions(vctx, j); err == nil {
			for _, v := range vers {
				if v.Kind != engine.VersionCurrent {
					d.Stats.VersionsCount++
				}
			}
		}
	}
	return d, nil
}

func (s *Service) respondJob(w http.ResponseWriter, r *http.Request, status int, j Job) {
	d, err := s.jobDetail(r.Context(), j)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, status, d)
}

func (s *Service) handleJobGet(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.respondJob(w, r, http.StatusOK, j)
}

func (s *Service) handleJobCreate(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	var in Job
	if !decode(w, r, &in) {
		return
	}
	norm, fe := s.normalizeJob(r.Context(), in, "")
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	j, err := s.store.CreateJob(norm, s.now())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "create", j.ID, "")
	s.rememberDrives(j, nil)
	s.jobChanged(j, JobChangeCreated)
	s.respondJob(w, r, http.StatusCreated, j)
}

func (s *Service) handleJobUpdate(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	id := r.PathValue("id")
	var in Job
	if !decode(w, r, &in) {
		return
	}
	if in.Revision <= 0 {
		writeValidation(w, map[string]string{"revision": string(FieldRequired)})
		return
	}
	norm, fe := s.normalizeJob(r.Context(), in, id)
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	destChanged, wasUnresolved := false, false
	j, err := s.store.UpdateJob(id, in.Revision, norm, s.now(), func(prev Job, stored *Job) {
		// An edit clears the "imported" badge, and a now valid imported
		// job no longer needs attention.
		wasUnresolved = stored.NeedsAttention == AttentionMigratedUnresolved
		if stored.NeedsAttention == AttentionImported || stored.NeedsAttention == AttentionMigratedUnresolved {
			stored.NeedsAttention = AttentionNone
		}
		// A new destination is a new backup folder with its own marker;
		// the old folder's "changed" warning no longer applies.
		if !sameEndpoint(prev.Dest, stored.Dest) {
			destChanged = true
			stored.DestFolderID = NewUUID4()
			if stored.NeedsAttention == AttentionDestChanged {
				stored.NeedsAttention = AttentionNone
			}
		}
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if destChanged {
		// The history belongs to the old folder: the next run is a first
		// run (no baseline, a new marker).
		if err := s.store.UpdateJobState(id, func(st *JobState) {
			st.LastSuccessAt, st.SizeBytes, st.SourceFiles, st.DestFiles, st.SourceFilesBy = nil, 0, 0, 0, nil
			st.GuardTripped = false
		}); err != nil {
			s.fail(w, err)
			return
		}
	}
	if wasUnresolved && j.MigratedFrom != nil {
		// The unresolved import's task still runs in core; now that the
		// user fixed the job, let it take the task over.
		ctx := s.ctx
		s.goRun(func() {
			if _, err := s.Migrate(ctx); err != nil {
				log.Printf("backup: migration pass after fixing job %s: %v", j.ID, err)
			}
		})
	}
	s.audit(r, "update", j.ID, fmt.Sprintf("revision=%d", j.Revision))
	s.rememberDrives(j, nil)
	s.jobChanged(j, JobChangeUpdated)
	s.respondJob(w, r, http.StatusOK, j)
}

func (s *Service) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	purge := r.URL.Query().Get("purge_data") == "true" || r.URL.Query().Get("purge_data") == "1"
	j, err := s.store.GetJob(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	active, err := s.store.LastRun(id, nil, []RunStatus{StatusQueued, StatusRunning, StatusWaitingUser})
	if err != nil {
		s.fail(w, err)
		return
	}
	if active != nil || s.queue.activeForJob(id) {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: "a run of this job is active; cancel it first"})
		return
	}
	var res DeleteJobResult
	if purge {
		spec, err := json.Marshal(purgeSpec{Job: j, DestFolderID: j.DestFolderID})
		if err != nil {
			s.fail(w, err)
			return
		}
		runID, _, err := s.queue.Enqueue(j, EnqueueOptions{Kind: KindPrune, Trigger: RunByManual, RestoreSpec: string(spec)})
		if err != nil {
			s.fail(w, err)
			return
		}
		res.PurgeRunID = runID
	}
	removed, err := s.store.DeleteJob(id, res.PurgeRunID)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, rr := range removed {
		removeRunFiles(rr)
		_ = s.clearRunDecision(rr.ID)
	}
	s.audit(r, "delete", id, fmt.Sprintf("purge_data=%v", purge))
	s.jobChanged(j, JobChangeDeleted)
	writeOK(w, http.StatusOK, res)
}

func (s *Service) handleJobToggle(w http.ResponseWriter, r *http.Request) {
	var req ToggleRequest
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if req.Enabled {
		// Turning on a job that still can't run (an unresolved import)
		// would only produce failures.
		cur, err := s.store.GetJob(id)
		if err != nil {
			s.fail(w, err)
			return
		}
		if _, fe := s.normalizeJob(r.Context(), cur, cur.ID); len(fe) > 0 {
			writeValidation(w, fe)
			return
		}
	}
	j, err := s.store.MutateJob(id, s.now(), true, func(j *Job) error {
		j.Enabled = req.Enabled
		return nil
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "toggle", id, fmt.Sprintf("enabled=%v", req.Enabled))
	s.jobChanged(j, JobChangeToggled)
	s.respondJob(w, r, http.StatusOK, j)
}

func (s *Service) handleJobRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if !decode(w, r, &req) {
		return
	}
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if _, fe := s.normalizeJob(r.Context(), j, j.ID); len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	kind := KindBackup
	if req.Preview {
		kind = KindPreview
	}
	runID, _, err := s.queue.Enqueue(j, EnqueueOptions{Kind: kind, Trigger: RunByManual})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "run", j.ID, "run="+runID)
	writeOK(w, http.StatusAccepted, RunStarted{RunID: runID})
}

// handleReconnectDest re-adopts the folder at the destination: the job
// takes over the folder's marker id (so the next run accepts it), or,
// when the folder has no marker any more, starts over as a first run
// that writes a new one. The UI confirms first. A marker that names
// another job is only taken over with adopt_other_job, and never while
// that job still exists here: a mirror would then delete that job's
// backup, with only the guards to stop it.
func (s *Service) handleReconnectDest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in ReconnectRequest
	if !decode(w, r, &in) {
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	creds, err := s.smbCredsFor(ctx, []Endpoint{j.Dest})
	if err != nil {
		s.fail(w, err)
		return
	}
	req := engine.ResolveRequest{Endpoint: j.Dest}
	if c, ok := creds[j.Dest.RefID]; ok {
		req.SMBCreds = &c
	}
	res, err := s.engine.Resolve(ctx, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !res.Online {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrDestOffline})
		return
	}
	if res.Marker != nil && res.Marker.JobID != "" && res.Marker.JobID != j.ID {
		if _, err := s.store.GetJob(res.Marker.JobID); err == nil {
			writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrDestMarkerMismatch,
				Detail: "the folder belongs to job " + res.Marker.JobID + ", which still exists"})
			return
		} else if !isNoRecord(err) {
			s.fail(w, err)
			return
		}
		if !in.AdoptOtherJob {
			writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrDestMarkerMismatch,
				Detail: "the folder belongs to another job (" + res.Marker.JobID + "); confirm with adopt_other_job"})
			return
		}
	}
	j, err = s.store.MutateJob(id, s.now(), true, func(j *Job) error {
		if res.Marker != nil && res.Marker.DestFolderID != "" {
			j.DestFolderID = res.Marker.DestFolderID
		} else {
			j.DestFolderID = NewUUID4()
		}
		if j.NeedsAttention == AttentionDestChanged {
			j.NeedsAttention = AttentionNone
		}
		return nil
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if res.Marker == nil {
		if err := s.store.UpdateJobState(id, func(st *JobState) { st.LastSuccessAt = nil }); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.audit(r, "reconnect_dest", id, fmt.Sprintf("adopt_other_job=%v", in.AdoptOtherJob))
	s.jobChanged(j, JobChangeUpdated)
	s.respondJob(w, r, http.StatusOK, j)
}

// ---------------------------------------------------------------------
// Validate, cron preview

func (s *Service) handleValidate(w http.ResponseWriter, r *http.Request) {
	var in Job
	if !decode(w, r, &in) {
		return
	}
	res := s.validate(r.Context(), in)
	writeOK(w, http.StatusOK, res)
}

// validate is POST /validate: field rules, job-side checks and the
// engine's prechecks. It never saves.
func (s *Service) validate(ctx context.Context, in Job) ValidateResult {
	norm, fe := s.normalizeJob(ctx, in, in.ID)
	res := ValidateResult{OK: len(fe) == 0, Checks: []Check{}, FieldErrors: fe}
	add := func(id string, st engine.CheckStatus, code ErrorCode) {
		c := Check{ID: id, Status: st, Code: code, MsgKey: "backup.check." + id}
		res.Checks = append(res.Checks, c)
		if st == engine.CheckFail {
			res.OK = false
		}
	}
	hasField := func(prefix string) (string, bool) {
		for k, v := range fe {
			if strings.HasPrefix(k, prefix) {
				return v, true
			}
		}
		return "", false
	}
	if _, bad := hasField("triggers"); bad {
		add(CheckCron, engine.CheckFail, "")
	} else {
		add(CheckCron, engine.CheckPass, "")
	}
	if _, bad := hasField("hooks"); bad {
		add(CheckHooks, engine.CheckFail, "")
	} else if len(norm.Hooks) > 0 {
		add(CheckHooks, engine.CheckPass, "")
	}
	if needsArchiveForDest(norm) {
		add(CheckTypeForDest, engine.CheckFail, ErrAppdataCloudNeedsArchive)
	} else {
		add(CheckTypeForDest, engine.CheckPass, "")
	}
	_, badSrc := hasField("sources")
	_, badDst := hasField("dest")
	if badSrc || badDst {
		return res
	}

	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	creds, err := s.smbCredsFor(pctx, append([]Endpoint{norm.Dest}, norm.Sources...))
	if err != nil {
		add(engine.CheckDestResolves, engine.CheckFail, engine.CodeOf(err))
		return res
	}
	req := engine.PrecheckRequest{
		JobType: string(norm.Type), Sources: norm.Sources, Dest: norm.Dest, SMBCreds: creds,
		Filters: norm.Filters, Guards: norm.Guards, FirstRun: true,
	}
	if in.ID != "" {
		// Editing: check against the stored job's folder and history.
		if stored, err := s.store.GetJob(in.ID); err == nil {
			req.DestFolderID = stored.DestFolderID
			if st, err := s.store.JobState(in.ID); err == nil && st.LastSuccessAt != nil && sameEndpoint(stored.Dest, norm.Dest) {
				req.FirstRun = false
				req.Baseline = &engine.Baseline{SourceFiles: st.SourceFiles, DestFiles: st.DestFiles}
			}
		}
	}
	pre, err := s.engine.Precheck(pctx, req)
	if err != nil {
		add(engine.CheckDestResolves, engine.CheckFail, engine.CodeOf(err))
		return res
	}
	res.Checks = append(res.Checks, pre.Checks...)
	res.Estimate = pre.Estimate
	if !pre.OK {
		res.OK = false
	}
	for _, c := range pre.Checks {
		if c.Status == engine.CheckFail {
			res.OK = false
		}
	}
	if sameDisk := s.sameDisk(pctx, norm); sameDisk {
		add(CheckSameDisk, engine.CheckWarn, "")
	}
	if others, err := s.store.ListJobs(); err == nil {
		res.Checks = append(res.Checks, overlapWarnings(norm, in.ID, others)...)
	}
	return res
}

// sameDisk: the destination is on the physical disk of a source, so one
// failing disk loses both.
func (s *Service) sameDisk(ctx context.Context, j Job) bool {
	locs, err := s.engine.Locations(ctx)
	if err != nil {
		return false
	}
	disk := func(ep Endpoint) string {
		for _, l := range locs {
			if l.Kind == ep.Kind && l.RefID == ep.RefID {
				return l.PhysicalDisk
			}
		}
		return ""
	}
	d := disk(j.Dest)
	if d == "" {
		return false
	}
	for _, src := range j.Sources {
		if disk(src) == d {
			return true
		}
	}
	return false
}

func (s *Service) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	var req CronPreviewRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Cron) > 256 {
		writeValidation(w, map[string]string{"cron": string(FieldInvalidCron)})
		return
	}
	writeOK(w, http.StatusOK, PreviewCron(req.Cron, s.now()))
}

// ---------------------------------------------------------------------
// Versions, restore, downloads

func (s *Service) versions(ctx context.Context, j Job) ([]Version, error) {
	creds, err := s.smbCredsFor(ctx, []Endpoint{j.Dest})
	if err != nil {
		return nil, err
	}
	req := engine.VersionsRequest{JobID: j.ID, JobType: string(j.Type), Dest: j.Dest}
	if c, ok := creds[j.Dest.RefID]; ok {
		req.SMBCreds = &c
	}
	vers, err := s.engine.ListVersions(ctx, req)
	if vers == nil {
		vers = []Version{}
	}
	return vers, err
}

func (s *Service) handleVersions(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	vers, err := s.versions(ctx, j)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, vers)
}

// versionIDRe: "current", "v_<ts>" or "a_<archive file name>".
var versionIDRe = regexp.MustCompile(`^(current|v_[0-9]{8}T[0-9]{6}Z|a_[^/\\\x00]{1,255})$`)

func (s *Service) handleVersionBrowse(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	vid := r.PathValue("vid")
	p, ok := CleanSubPath(r.URL.Query().Get("path"))
	fe := fieldErrors{}
	if !versionIDRe.MatchString(vid) {
		fe.add("version_id", FieldInvalid)
	}
	if !ok {
		fe.add("path", ErrPathNotAllowed)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	creds, err := s.smbCredsFor(ctx, []Endpoint{j.Dest})
	if err != nil {
		s.fail(w, err)
		return
	}
	req := engine.BrowseRequest{Endpoint: j.Dest, VersionID: vid, Path: p}
	if c, ok := creds[j.Dest.RefID]; ok {
		req.SMBCreds = &c
	}
	res, err := s.engine.Browse(ctx, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	if res.Entries == nil {
		res.Entries = []engine.Entry{}
	}
	writeOK(w, http.StatusOK, res)
}

// cleanPaths validates restore/download paths (relative to the version
// root; [] = everything when allowEmpty).
func cleanPaths(paths []string, field string, allowEmpty bool, fe fieldErrors) []string {
	if len(paths) == 0 && !allowEmpty {
		fe.add(field, FieldRequired)
	}
	if len(paths) > maxRestorePaths {
		fe.add(field, FieldTooMany)
	}
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for i, p := range paths {
		c, ok := CleanSubPath(p)
		if !ok || c == "" {
			fe.add(fmt.Sprintf("%s[%d]", field, i), ErrPathNotAllowed)
			continue
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) handleRestore(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	var req RestoreRequest
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	if !versionIDRe.MatchString(req.VersionID) {
		fe.add("version_id", FieldInvalid)
	}
	paths := cleanPaths(req.Paths, "paths", true, fe)
	spec := engine.RestoreSpec{VersionID: req.VersionID, Paths: paths, Conflict: req.Conflict, DryRun: req.DryRun}
	switch spec.Conflict {
	case "":
		spec.Conflict = engine.ConflictKeepBoth
	case engine.ConflictKeepBoth, engine.ConflictOverwrite, engine.ConflictSkip:
	default:
		fe.add("conflict", FieldInvalid)
	}
	switch req.Target.Mode {
	case RestoreOriginal, "":
		if len(j.Sources) != 1 {
			// An archive of several folders has no single original place.
			fe.add("target.mode", FieldInvalid)
		} else {
			spec.Target = j.Sources[0]
		}
	case RestoreOther:
		if req.Target.Endpoint == nil {
			fe.add("target.endpoint", FieldRequired)
			break
		}
		ep := *req.Target.Endpoint
		normalizeEndpoint(&ep, "target.endpoint", fe)
		ep.Preset = ""
		spec.Target = ep
	default:
		fe.add("target.mode", FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		s.fail(w, err)
		return
	}
	runID, _, err := s.queue.Enqueue(j, EnqueueOptions{Kind: KindRestore, Trigger: RunByManual, RestoreSpec: string(raw)})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "restore", j.ID, fmt.Sprintf("run=%s version=%s paths=%d target=%s|%s|%s dry_run=%v",
		runID, spec.VersionID, len(spec.Paths), spec.Target.Kind, spec.Target.RefID, spec.Target.SubPath, spec.DryRun))
	writeOK(w, http.StatusAccepted, RunStarted{RunID: runID})
}

func (s *Service) handleDownloadCreate(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if !decode(w, r, &req) {
		return
	}
	if _, err := s.store.GetJob(req.JobID); err != nil {
		s.fail(w, err)
		return
	}
	fe := fieldErrors{}
	if !versionIDRe.MatchString(req.VersionID) {
		fe.add("version_id", FieldInvalid)
	}
	req.Paths = cleanPaths(req.Paths, "paths", false, fe)
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	writeOK(w, http.StatusOK, s.downloads.issue(req, clientIP(r), s.now()))
}

// handleDownload streams GET /downloads/<token>: the token is the
// credential (single use, 60 s, the requester's IP).
func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request, token string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, ErrorBody{ErrorCode: ErrStoreUnavailable})
		return
	}
	req, ok := s.downloads.redeem(token, clientIP(r), s.now())
	if !ok {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrTokenInvalid})
		return
	}
	j, err := s.store.GetJob(req.JobID)
	if err != nil {
		s.fail(w, err)
		return
	}
	creds, err := s.smbCredsFor(r.Context(), []Endpoint{j.Dest})
	if err != nil {
		s.fail(w, err)
		return
	}
	dreq := engine.DownloadRequest{Dest: j.Dest, VersionID: req.VersionID, Paths: req.Paths}
	if c, ok := creds[j.Dest.RefID]; ok {
		dreq.SMBCreds = &c
	}
	dl, err := s.engine.OpenDownload(r.Context(), dreq)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer dl.Body.Close()
	ct := dl.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	name := dl.Name
	if name == "" || !utf8.ValidString(name) {
		name = "download"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if dl.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(dl.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, dl.Body); err != nil {
		log.Printf("backup: download for job %s: %v", j.ID, err)
	}
	s.audit(r, "download", j.ID, fmt.Sprintf("version=%s paths=%d", req.VersionID, len(req.Paths)))
}

// ---------------------------------------------------------------------
// Settings, migration, drives, busy

func (s *Service) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, st)
}

// ValidateSettings checks AppSettings ranges.
func ValidateSettings(st AppSettings) map[string]string {
	fe := fieldErrors{}
	if st.MaxConcurrent < 1 || st.MaxConcurrent > 4 {
		fe.add("max_concurrent", FieldOutOfRange)
	}
	if st.LogRetentionDays < 1 || st.LogRetentionDays > maxVersionsDays {
		fe.add("log_retention_days", FieldOutOfRange)
	}
	if st.DefaultDeletePct < 1 || st.DefaultDeletePct > 100 {
		fe.add("default_delete_pct", FieldOutOfRange)
	}
	if st.DefaultChangePct < 1 || st.DefaultChangePct > 100 {
		fe.add("default_change_pct", FieldOutOfRange)
	}
	if st.DefaultVersionsDays < 0 || st.DefaultVersionsDays > maxVersionsDays {
		fe.add("default_versions_days", FieldOutOfRange)
	}
	if len(fe) == 0 {
		return nil
	}
	return fe
}

func (s *Service) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var st AppSettings
	if !decode(w, r, &st) {
		return
	}
	if fe := ValidateSettings(st); fe != nil {
		writeValidation(w, fe)
		return
	}
	if err := s.store.SaveSettings(st); err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "settings", "", fmt.Sprintf("%+v", st))
	s.queue.wake() // the worker count may have grown
	writeOK(w, http.StatusOK, st)
}

func (s *Service) handleMigrationGet(w http.ResponseWriter, r *http.Request) {
	rep, err := s.MigrationReport()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, rep)
}

func (s *Service) handleMigrationRerun(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	rep, err := s.Migrate(ctx)
	if err != nil {
		log.Printf("backup: migration rerun: %v", err)
	}
	s.audit(r, "migration_rerun", "", rep.State)
	writeOK(w, http.StatusOK, rep)
}

func (s *Service) handleDrivesList(w http.ResponseWriter, r *http.Request) {
	drives, err := s.store.RememberedDrives()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]RememberedDrive, 0, len(drives))
	for _, d := range drives {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint.RefID < out[j].Endpoint.RefID })
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDriveUpdate(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	var req RememberedDriveUpdate
	if !decode(w, r, &req) {
		return
	}
	label := strings.TrimSpace(req.Label)
	if utf8.RuneCountInString(label) > maxLabelLen || strings.ContainsAny(label, "\x00\r\n") {
		writeValidation(w, map[string]string{"label": string(FieldInvalid)})
		return
	}
	key := MetaRememberedDrive + uuid
	var d RememberedDrive
	ok, err := s.store.GetMetaJSON(key, &d)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "drive " + uuid})
		return
	}
	d.Label = label
	if err := s.store.SetMetaJSON(key, d); err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, d)
}

func (s *Service) handleDriveForget(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	key := MetaRememberedDrive + uuid
	var d RememberedDrive
	ok, err := s.store.GetMetaJSON(key, &d)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "drive " + uuid})
		return
	}
	jobs, err := s.store.ListJobs()
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, j := range jobs {
		eps := append([]Endpoint{j.Dest}, j.Sources...)
		for _, t := range j.Triggers {
			if t.VolumeRef != nil {
				eps = append(eps, *t.VolumeRef)
			}
		}
		for _, ep := range eps {
			if ep.Kind == EPUSB && strings.EqualFold(ep.RefID, uuid) {
				writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: "job " + j.ID + " uses this drive"})
				return
			}
		}
	}
	if err := s.store.DeleteMeta(key); err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, struct{}{})
}

func (s *Service) handleBusy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, target := q.Get("kind"), q.Get("target")
	fe := fieldErrors{}
	if kind != LockApp && kind != LockVM {
		fe.add("kind", FieldInvalid)
	}
	if target == "" {
		fe.add("target", FieldRequired)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	runID, busy := s.queue.holder(kind, target)
	writeOK(w, http.StatusOK, BusyResult{Busy: busy, RunID: runID})
}
