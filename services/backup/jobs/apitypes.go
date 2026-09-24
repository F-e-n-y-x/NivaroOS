package jobs

import (
	"time"

	commonmodel "github.com/F-e-n-y-x/NivaroOS/services/common/model"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// REST contract (spec §11, frozen). Every route is under APIBase; request
// and response examples for each are in docs/specs/backup-api.json, and
// TestAPIFixtures decodes every one strictly into the types below.
//
// Auth: GET /health and GET /downloads/:token need no JWT. Everything else
// needs the ES256 JWT (header or ?token=), validated the way Download
// Station does; every non-GET also needs the admin role. Loopback
// automation (no Origin, no Sec-Fetch-Site, not via the gateway) skips the
// JWT, as in services/common/middleware/localauth.go.
//
// /devices/:id/* are the exception: they take only that device's own
// token (devices.go) - no JWT, no loopback shortcut - and a device token
// is rejected on every other route.

// APIBase is where the gateway mounts the service (127.0.0.1:28643). The
// service accepts paths with or without it.
const APIBase = "/v1/backup"

// ListenAddr is the service's only listen address.
const ListenAddr = "127.0.0.1:28643"

// Envelope is the standard NivaroOS response, {success, message, data}.
// Success is the HTTP status (200, 202, 400, 401, 403, 404, 409, 429,
// 500, 503); Message is "ok" or the error code; Data is the typed
// response below, or an ErrorBody when Success >= 400.
type Envelope = commonmodel.Result

// ErrorBody is Data of every error response.
type ErrorBody struct {
	ErrorCode ErrorCode `json:"error_code"`
	// Detail is technical English for "Show technical output"; the UI
	// never shows it as the message.
	Detail string `json:"detail,omitempty"`
	// FieldErrors maps a JSON field path ("dest.sub_path",
	// "triggers[1].cron", "guards.delete_pct") to a FieldCode or an
	// ErrorCode (validation only).
	FieldErrors map[string]string `json:"field_errors,omitempty"`
	// Current is the stored job (revision_conflict only).
	Current *Job `json:"current,omitempty"`
}

// ServiceHealth is GET /health (no auth, no envelope - like Download
// Station's, so the UI's install probe reads it directly).
type ServiceHealth struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Service   string `json:"service"` // "backup"
	Version   string `json:"version"`
}

// Capabilities is GET /capabilities (spec §3.4).
type Capabilities struct {
	Version       string       `json:"version"` // the module's version
	Engine        EngineCap    `json:"engine"`
	Restic        ResticCap    `json:"restic"`
	Timezone      string       `json:"timezone"`   // IANA, e.g. "Europe/Berlin"
	UTCOffset     string       `json:"utc_offset"` // "+02:00"
	ClockSynced   bool         `json:"clock_synced"`
	ClockArmed    bool         `json:"clock_armed"` // the gate opened (synced or timed out)
	RAMBytes      int64        `json:"ram_bytes"`
	MaxConcurrent int          `json:"max_concurrent"`
	Installed     InstalledCap `json:"installed"`
}

// EngineCap is the engine block of Capabilities.
type EngineCap struct {
	Available bool   `json:"available"`
	API       int    `json:"api"`
	Rclone    string `json:"rclone"`
}

// ResticCap is reserved for v1.1; v1 always reports available=false,
// installable=false.
type ResticCap struct {
	Available   bool   `json:"available"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	Min         string `json:"min"`
	Installable bool   `json:"installable"`
}

// InstalledCap says which optional hook targets exist, so the UI hides
// presets that can't apply.
type InstalledCap struct {
	AppManagement bool `json:"app_management"`
	VMManager     bool `json:"vm_manager"`
}

// LocationRole filters GET /locations?role=.
const (
	RoleSource = "source"
	RoleDest   = "dest"
)

// Location is one entry of GET /locations: the engine's Location plus
// what the job side merges in (SMB connections, remembered USB drives,
// app and VM folder presets).
type Location = engine.Location

// BrowseResult is GET /locations/browse and GET /jobs/:id/versions/:vid/browse.
type BrowseResult = engine.BrowseResult

// ResolvePathRequest is POST /locations/resolve-path.
type ResolvePathRequest = engine.ResolvePathRequest

// ResolvePathResult is its response.
type ResolvePathResult = engine.ResolvePathResult

// Job health, worst first (GET /jobs).
const (
	HealthProblem  = "problem" // last run failed or needs a decision
	HealthOffline  = "offline" // destination not connected
	HealthWarning  = "warning" // partial, stale, needs attention
	HealthOK       = "ok"
	HealthDisabled = "disabled"
)

// RunBrief is a job's last run in lists.
type RunBrief struct {
	ID      string     `json:"id"`
	Kind    RunKind    `json:"kind"`
	Status  RunStatus  `json:"status"`
	EndedAt *time.Time `json:"ended_at"`
	Summary *Message   `json:"summary"`
}

// JobListItem is one entry of GET /jobs. It never carries logs.
type JobListItem struct {
	Job
	Health     string     `json:"health"`
	LastRun    *RunBrief  `json:"last_run"`
	ActiveRun  *RunBrief  `json:"active_run"` // queued, running or waiting_user
	NextRun    *time.Time `json:"next_run"`   // earliest schedule trigger; null = none
	DestOnline bool       `json:"dest_online"`
}

// JobStats is the extra block of GET /jobs/:id.
type JobStats struct {
	SizeBytes     int64      `json:"size_bytes"` // at the last success
	VersionsCount int        `json:"versions_count"`
	LastSuccess   *time.Time `json:"last_success"`
}

// JobDetail is GET /jobs/:id.
type JobDetail struct {
	JobListItem
	Stats JobStats `json:"stats"`
}

// DeleteJobResult is DELETE /jobs/:id; it names the purge run, if any.
type DeleteJobResult struct {
	PurgeRunID string `json:"purge_run_id,omitempty"`
}

// ReconnectRequest is POST /jobs/:id/reconnect-dest. AdoptOtherJob
// confirms taking over a folder whose marker names another job (one this
// box no longer has, e.g. from before a reinstall); without it that is
// refused with dest_marker_mismatch.
type ReconnectRequest struct {
	AdoptOtherJob bool `json:"adopt_other_job"`
}

// ToggleRequest is POST /jobs/:id/toggle.
type ToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// RunRequest is POST /jobs/:id/run.
type RunRequest struct {
	Preview bool `json:"preview"`
}

// RunStarted is the 202 response of run and restore.
type RunStarted struct {
	RunID string `json:"run_id"`
}

// Check is one validation or precheck result. The job side adds its own
// check IDs (below) to the engine's.
type Check = engine.Check

// Job-side check IDs.
const (
	CheckCron        = "cron"
	CheckHooks       = "hooks"
	CheckTypeForDest = "type_for_dest" // appdata/VM -> cloud needs archive
	CheckSameDisk    = "same_disk"     // warn: destination on the source's physical disk
	CheckJobOverlap  = "job_overlap"   // warn: shares folders with a mirror (msg_key says how)
)

// ValidateResult is POST /validate.
type ValidateResult struct {
	OK       bool            `json:"ok"`
	Checks   []Check         `json:"checks"`
	Estimate engine.Estimate `json:"estimate"`
	// FieldErrors are the same as a 400's, so the wizard can show them
	// inline without saving.
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

// CronPreviewRequest is POST /cron/preview.
type CronPreviewRequest struct {
	Cron string `json:"cron"`
}

// CronPreview is its response. HumanKey is one of CronHumanKeys; Next
// holds the next 5 activations in server time.
type CronPreview struct {
	Valid     bool                   `json:"valid"`
	HumanKey  string                 `json:"human_key"`
	Args      map[string]interface{} `json:"args"`
	Next      []time.Time            `json:"next"`
	Timezone  string                 `json:"timezone"`
	UTCOffset string                 `json:"utc_offset"`
	Error     string                 `json:"error"` // parser message when !Valid
}

// Run is a run as the API shows it (RunRow minus file paths).
type Run struct {
	ID         string              `json:"id"`
	JobID      string              `json:"job_id"`
	JobName    string              `json:"job_name"`
	Kind       RunKind             `json:"kind"`
	Trigger    RunTrigger          `json:"trigger"`
	Status     RunStatus           `json:"status"`
	Phase      RunPhase            `json:"phase"`
	Attempt    int                 `json:"attempt"`
	ErrorCode  ErrorCode           `json:"error_code"`
	Summary    *Message            `json:"summary"`
	QueuedAt   time.Time           `json:"queued_at"`
	StartedAt  *time.Time          `json:"started_at"`
	EndedAt    *time.Time          `json:"ended_at"`
	Counts     RunCounts           `json:"counts"`
	Coalesced  int                 `json:"coalesced"`
	Guard      *engine.GuardInfo   `json:"guard"`       // waiting_user
	Steps      []RunStep           `json:"steps"`       // for the run window's step list
	Restore    *engine.RestoreSpec `json:"restore"`     // kind=restore
	FileErrors []engine.FileError  `json:"file_errors"` // partial
	HasLog     bool                `json:"has_log"`
}

// RunCounts are a run's file and byte counts.
type RunCounts struct {
	Added            int64 `json:"added"`
	Changed          int64 `json:"changed"`
	Deleted          int64 `json:"deleted"`
	Skipped          int64 `json:"skipped"`
	Errored          int64 `json:"errored"`
	BytesTransferred int64 `json:"bytes_transferred"`
	BytesTotal       int64 `json:"bytes_total"`
}

// Step states for RunStep.
const (
	StepPending = "pending"
	StepActive  = "active"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// RunStep is one line of the run window's step list, e.g.
// {"key":"backup.run.step.stop_app","args":{"app":"immich"},"state":"done"}.
type RunStep struct {
	Phase RunPhase               `json:"phase"`
	Key   string                 `json:"key"`
	Args  map[string]interface{} `json:"args,omitempty"`
	State string                 `json:"state"`
}

// LiveStats are in-memory progress (GET /runs/:id while running).
type LiveStats struct {
	Bytes       int64  `json:"bytes"`
	TotalBytes  int64  `json:"total_bytes"`
	Files       int64  `json:"files"`
	TotalFiles  int64  `json:"total_files"`
	SpeedBps    int64  `json:"speed_bps"`
	ETASec      *int64 `json:"eta_sec"`
	Errors      int64  `json:"errors"`
	CurrentFile string `json:"current_file"`
}

// RunDetail is GET /runs/:id.
type RunDetail struct {
	Run
	Live *LiveStats `json:"live"` // null unless running
}

// RunList is GET /runs; pass NextBefore as ?before= for the next page
// ("" = no more).
type RunList struct {
	Runs       []Run  `json:"runs"`
	NextBefore string `json:"next_before"`
}

// LogPage is GET /runs/:id/log?after=<offset>&limit=<n> (default 500,
// max 2000). Offsets are bytes into the uncompressed log; Done is true
// when the run is final and the page reached the end.
type LogPage struct {
	Lines      []engine.LogLine `json:"lines"`
	NextOffset int64            `json:"next_offset"`
	Done       bool             `json:"done"`
}

// DecideRequest is POST /runs/:id/decide on a waiting_user run.
type DecideRequest struct {
	Proceed bool `json:"proceed"`
	// Mode, when proceeding: "as_shown" (default) or "copy_once" (run
	// this once as Copy, no deletes).
	Mode string `json:"mode,omitempty"`
}

// Decide modes.
const (
	DecideAsShown  = "as_shown"
	DecideCopyOnce = "copy_once"
)

// PreviewCounts summarises a plan.
type PreviewCounts struct {
	Add      int64 `json:"add"`
	Update   int64 `json:"update"`
	Delete   int64 `json:"delete"`
	BytesAdd int64 `json:"bytes_add"`
}

// PreviewPage is GET /runs/:id/preview?op=add|update|delete&q=&offset=&limit=
// (limit default 200, max 1000).
type PreviewPage struct {
	Counts     PreviewCounts        `json:"counts"`
	Items      []engine.PreviewItem `json:"items"`
	Total      int64                `json:"total"`       // matching items for op+q
	NextOffset int64                `json:"next_offset"` // -1 = end
}

// Version is one entry of GET /jobs/:id/versions.
type Version = engine.Version

// RestoreTarget modes.
const (
	RestoreOriginal = "original"
	RestoreOther    = "other"
)

// RestoreTarget is where POST /jobs/:id/restore puts files.
type RestoreTarget struct {
	Mode     string    `json:"mode"`
	Endpoint *Endpoint `json:"endpoint,omitempty"` // mode=other
}

// RestoreRequest is POST /jobs/:id/restore.
type RestoreRequest struct {
	VersionID string        `json:"version_id"`
	Paths     []string      `json:"paths"`
	Target    RestoreTarget `json:"target"`
	Conflict  string        `json:"conflict"` // engine.Conflict*; default keep_both
	DryRun    bool          `json:"dry_run"`
}

// DownloadRequest is POST /downloads.
type DownloadRequest struct {
	JobID     string   `json:"job_id"`
	VersionID string   `json:"version_id"`
	Paths     []string `json:"paths"`
}

// DownloadToken is its response: GET /downloads/<token> within
// ExpiresIn seconds, once, from the same IP.
type DownloadToken struct {
	Token     string `json:"token"` // "dl_" + 32 hex
	ExpiresIn int    `json:"expires_in"`
}

// MigrationItem is one Scheduled Task the migration looked at.
type MigrationItem struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	// Result: "imported", "imported_unresolved", "skipped_exists".
	Result string `json:"result"`
	JobID  string `json:"job_id,omitempty"`
	// Notes are i18n keys (MigrationNoteKeys) with args.
	Notes []Message `json:"notes"`
	// DroppedArgs are extra_args flags that had no typed equivalent.
	DroppedArgs []string `json:"dropped_args"`
}

// MigrationReport is GET /migration (and MetaRow migration.schedules_v1).
// State is "pending" (core not reachable yet), "done" or "failed".
type MigrationReport struct {
	State     string          `json:"state"`
	RanAt     *time.Time      `json:"ran_at"`
	Imported  int             `json:"imported"`
	Items     []MigrationItem `json:"items"`
	ErrorCode ErrorCode       `json:"error_code,omitempty"`
	Detail    string          `json:"detail,omitempty"`
}

// Migration states and item results.
const (
	MigrationPending = "pending"
	MigrationDone    = "done"
	MigrationFailed  = "failed"

	MigratedImported           = "imported"
	MigratedImportedUnresolved = "imported_unresolved"
	MigratedSkippedExists      = "skipped_exists"
)

// ScheduleMigratedMarker is the field Backup sets on a Scheduled Task it
// imported (schedules.json "migrated_to"); core's executor skips such
// tasks, and `nivaroos-backup release-scheduled-tasks` clears it again.
const (
	ScheduleMigratedField  = "migrated_to"
	ScheduleMigratedMarker = "backup"
)

// ReleaseScheduledTasksCmd is the subcommand the uninstaller runs before
// removing the module: it re-enables every Scheduled Task Backup imported.
const ReleaseScheduledTasksCmd = "release-scheduled-tasks"

// BusyResult is GET /busy?kind=app|vm&target=<name>.
type BusyResult struct {
	Busy  bool   `json:"busy"`
	RunID string `json:"run_id,omitempty"` // the run holding it
}

// RememberedDriveUpdate is PUT /drives/:uuid (rename; display only).
type RememberedDriveUpdate struct {
	Label string `json:"label"`
}
