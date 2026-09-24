// Package jobs is the job side of Backup & Sync: job definitions and
// their store, validation, triggers, conditions, the queue and its locks,
// hooks, run records and logs, notifications, the schedules.json
// migration and the /v1/backup REST API. It moves no data itself; it
// drives package engine through EngineClient.
//
// model.go, errors.go, events.go, clock.go, locks.go, engine.go,
// apitypes.go and i18nkeys.go are the FROZEN contracts (spec §4, §5,
// §10.1, §11, §17). Change them only additively.
package jobs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Types shared with the engine, defined once in engine/api.go.
type (
	Endpoint     = engine.Endpoint
	EndpointKind = engine.EndpointKind
	DevMatch     = engine.DevMatch
	Filters      = engine.Filters
	Options      = engine.Options
	Guards       = engine.Guards
	Retention    = engine.Retention
)

// Endpoint kinds, re-exported for readability on the job side.
const (
	EPVolume = engine.EPVolume
	EPUSB    = engine.EPUSB
	EPMerge  = engine.EPMerge
	EPSMB    = engine.EPSMB
	EPCloud  = engine.EPCloud
)

// JobType is what a job does. v1.1 adds "backup" (restic), later "twoway".
type JobType string

const (
	TypeCopy    JobType = "copy"    // copy new/changed files, never delete at the destination
	TypeMirror  JobType = "mirror"  // exact copy, deleted/replaced files kept in the recycle folder
	TypeArchive JobType = "archive" // one .tar.zst per run, keep last N
)

// Valid reports whether t is a v1 job type.
func (t JobType) Valid() bool { return t == TypeCopy || t == TypeMirror || t == TypeArchive }

// Preset prefixes of Endpoint.Preset.
const (
	PresetData    = "data:"    // + /DATA folder name
	PresetAppData = "appdata:" // + app name
	PresetVM      = "vm:"      // + VM name
	PresetShare   = "share:"   // + share id
)

// TriggerKind is what starts a job.
type TriggerKind string

const (
	TriggerSchedule      TriggerKind = "schedule"
	TriggerVolumeMounted TriggerKind = "volume_mounted"
	TriggerManual        TriggerKind = "manual"
)

// Trigger is one way a job starts (spec §7.2).
type Trigger struct {
	Kind TriggerKind `json:"kind"`
	// Cron is a robfig 5-field spec or descriptor (@daily), server local
	// time, validated server-side and kept byte-for-byte.
	Cron string `json:"cron,omitempty"`
	// VolumeRef is the volume whose attach fires a volume_mounted
	// trigger; nil = the job's destination (or its source when the
	// destination is not a volume/usb).
	VolumeRef *Endpoint `json:"volume,omitempty"`
	// MinGapHours: at most one volume_mounted run per N hours (0 = once
	// per attach).
	MinGapHours int `json:"min_gap_hours,omitempty"`
	// CatchUp runs a missed occurrence once after boot (§7.2).
	CatchUp bool `json:"catch_up"`
}

// WhenUnmet is what a run does when a condition doesn't hold.
type WhenUnmet string

const (
	UnmetSkip WhenUnmet = "skip" // record skipped (grey, not a failure)
	UnmetWait WhenUnmet = "wait" // re-check every 5 min up to WaitMaxMin, then skipped
	UnmetFail WhenUnmet = "fail" // failed + notification
)

// Conditions gate a run at precheck (§7.4).
type Conditions struct {
	DestAvailable bool        `json:"dest_available"` // forced true for mirror
	Window        *TimeWindow `json:"window,omitempty"`
	// WhenUnmet defaults to "wait" for jobs with a schedule trigger and
	// "skip" otherwise.
	WhenUnmet  WhenUnmet `json:"when_unmet"`
	WaitMaxMin int       `json:"wait_max_min,omitempty"` // default 360
}

// TimeWindow is "HH:MM" server local time; End < Start wraps midnight.
type TimeWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Hook phases, actions, app modes and failure policies (§8.2).
const (
	HookPre  = "pre"
	HookPost = "post" // runs if the matching pre ran, even after failure/cancel

	HookStopApps   = "stop_apps"
	HookStartApps  = "start_apps"
	HookShutdownVM = "shutdown_vm"
	HookStartVM    = "start_vm"

	AppModeTogether   = "together"
	AppModeOneAtATime = "one_at_a_time"

	FailAbort    = "abort" // default
	FailContinue = "continue"
)

// Hook stops/starts apps or a VM around the transfer.
type Hook struct {
	Phase      string   `json:"phase"`
	Action     string   `json:"action"`
	Apps       []string `json:"apps,omitempty"`
	AppMode    string   `json:"app_mode,omitempty"`
	VM         string   `json:"vm,omitempty"`
	TimeoutSec int      `json:"timeout_sec"` // default 300 (VM: 600)
	FailPolicy string   `json:"fail_policy"`
}

// Retry is the backoff policy for transient errors.
type Retry struct {
	Max        int   `json:"max"`         // default 3
	BackoffSec []int `json:"backoff_sec"` // default [60, 600, 3600]; the last repeats
}

// NotifyPrefs are per-job notification choices. Failure notifications
// can't be turned off in v1 (the UI shows the switch locked on).
type NotifyPrefs struct {
	OnSuccess       bool `json:"on_success"`
	OnFailure       bool `json:"on_failure"`
	StaleAfterHours int  `json:"stale_after_hours"` // default max(48, 2 x schedule interval)
}

// NeedsAttention values on a job.
const (
	AttentionNone               = ""
	AttentionMigratedUnresolved = "migrated_unresolved" // imported, a side couldn't be resolved; disabled
	AttentionDestChanged        = "dest_changed"        // marker mismatch; "Reconnect to this folder"
	AttentionImported           = "imported"            // imported from Scheduled Tasks, not edited yet (badge)
)

// Job is a job definition as the REST API sends and receives it (spec §11
// "Job JSON"). JobRow is its stored form.
type Job struct {
	ID         string      `json:"id,omitempty"` // "bk_" + 12 hex; ignored on create
	Name       string      `json:"name"`
	Type       JobType     `json:"type"`
	Enabled    bool        `json:"enabled"`
	Revision   int         `json:"revision,omitempty"` // PUT must send the current one (409 otherwise); starts at 1
	Sources    []Endpoint  `json:"sources"`            // copy/mirror: exactly 1; archive: 1..16
	Dest       Endpoint    `json:"dest"`
	Triggers   []Trigger   `json:"triggers"` // [] = manual only
	Conditions Conditions  `json:"conditions"`
	Filters    Filters     `json:"filters"`
	Options    Options     `json:"options"`
	Guards     Guards      `json:"guards"`
	Retention  Retention   `json:"retention"`
	Hooks      []Hook      `json:"hooks"`
	Retry      Retry       `json:"retry"`
	Notify     NotifyPrefs `json:"notify"`
	// Server-owned: ignored on create/update.
	DestFolderID   string    `json:"-"`
	NeedsAttention string    `json:"needs_attention"`
	MigratedFrom   *string   `json:"migrated_from"`
	CreatedAt      time.Time `json:"created_at,omitzero"`
	UpdatedAt      time.Time `json:"updated_at,omitzero"`
}

// JobRow is the persisted job (gorm, table job_rows). JSON columns are
// strings with typed accessors via ToJob/JobToRow, so no gorm datatypes
// dependency is needed.
type JobRow struct {
	ID             string `gorm:"primaryKey"`
	Name           string `gorm:"not null"`
	Type           string `gorm:"not null"`
	Enabled        bool
	Sources        string // JSON []Endpoint
	Dest           string // JSON Endpoint
	Triggers       string // JSON []Trigger
	Conditions     string // JSON Conditions
	Filters        string // JSON Filters
	Options        string // JSON Options
	Guards         string // JSON Guards
	Retention      string // JSON Retention
	Hooks          string // JSON []Hook
	RetryPolicy    string // JSON Retry
	Notify         string // JSON NotifyPrefs
	DestFolderID   string // uuid in the destination marker (§6.2)
	NeedsAttention string
	MigratedFrom   *string `gorm:"uniqueIndex"` // schedule task id; UNIQUE keeps migration idempotent
	Revision       int     `gorm:"not null;default:1"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobToRow encodes a job for storage.
func JobToRow(j Job) (JobRow, error) {
	r := JobRow{
		ID: j.ID, Name: j.Name, Type: string(j.Type), Enabled: j.Enabled,
		DestFolderID: j.DestFolderID, NeedsAttention: j.NeedsAttention, MigratedFrom: j.MigratedFrom,
		Revision: j.Revision, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	fields := []struct {
		dst *string
		v   interface{}
	}{
		{&r.Sources, nonNil(j.Sources)}, {&r.Dest, j.Dest}, {&r.Triggers, nonNil(j.Triggers)},
		{&r.Conditions, j.Conditions}, {&r.Filters, j.Filters}, {&r.Options, j.Options},
		{&r.Guards, j.Guards}, {&r.Retention, j.Retention}, {&r.Hooks, nonNil(j.Hooks)},
		{&r.RetryPolicy, j.Retry}, {&r.Notify, j.Notify},
	}
	for _, f := range fields {
		raw, err := json.Marshal(f.v)
		if err != nil {
			return JobRow{}, fmt.Errorf("encode job %s: %w", j.ID, err)
		}
		*f.dst = string(raw)
	}
	return r, nil
}

// ToJob decodes a stored job. An empty column decodes to the zero value,
// so rows written by an older schema still load.
func (r JobRow) ToJob() (Job, error) {
	j := Job{
		ID: r.ID, Name: r.Name, Type: JobType(r.Type), Enabled: r.Enabled,
		DestFolderID: r.DestFolderID, NeedsAttention: r.NeedsAttention, MigratedFrom: r.MigratedFrom,
		Revision: r.Revision, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	fields := []struct {
		name string
		src  string
		v    interface{}
	}{
		{"sources", r.Sources, &j.Sources}, {"dest", r.Dest, &j.Dest}, {"triggers", r.Triggers, &j.Triggers},
		{"conditions", r.Conditions, &j.Conditions}, {"filters", r.Filters, &j.Filters}, {"options", r.Options, &j.Options},
		{"guards", r.Guards, &j.Guards}, {"retention", r.Retention, &j.Retention}, {"hooks", r.Hooks, &j.Hooks},
		{"retry", r.RetryPolicy, &j.Retry}, {"notify", r.Notify, &j.Notify},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.src) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(f.src), f.v); err != nil {
			return Job{}, fmt.Errorf("decode job %s %s: %w", r.ID, f.name, err)
		}
	}
	if j.Sources == nil {
		j.Sources = []Endpoint{}
	}
	if j.Triggers == nil {
		j.Triggers = []Trigger{}
	}
	if j.Hooks == nil {
		j.Hooks = []Hook{}
	}
	return j, nil
}

// nonNil makes nil slices encode as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// RunKind is what a run does.
type RunKind string

const (
	KindBackup  RunKind = "backup"  // the job's own copy/mirror/archive
	KindPreview RunKind = "preview" // dry run (plan), ends waiting_user or success
	KindRestore RunKind = "restore"
	KindVerify  RunKind = "verify"
	KindPrune   RunKind = "prune"
)

// RunTrigger is why a run was queued. Also the queue priority, highest
// first: manual, retry, schedule/volume_mounted, catch_up.
type RunTrigger string

const (
	RunBySchedule      RunTrigger = "schedule"
	RunByVolumeMounted RunTrigger = "volume_mounted"
	RunByCatchUp       RunTrigger = "catch_up"
	RunByManual        RunTrigger = "manual"
	RunByRetry         RunTrigger = "retry"
)

// RunStatus is where a run is in the §5 state machine.
type RunStatus string

const (
	// resting
	StatusQueued      RunStatus = "queued"
	StatusRunning     RunStatus = "running" // see Phase
	StatusWaitingUser RunStatus = "waiting_user"
	// final
	StatusSuccess     RunStatus = "success"
	StatusPartial     RunStatus = "partial" // finished with file errors
	StatusFailed      RunStatus = "failed"
	StatusCancelled   RunStatus = "cancelled"
	StatusSkipped     RunStatus = "skipped"
	StatusInterrupted RunStatus = "interrupted"
)

// Final reports whether s is a final state.
func (s RunStatus) Final() bool {
	switch s {
	case StatusSuccess, StatusPartial, StatusFailed, StatusCancelled, StatusSkipped, StatusInterrupted:
		return true
	}
	return false
}

// RunPhase is the step of a running run.
type RunPhase string

const (
	PhasePrecheck  RunPhase = "precheck"
	PhasePreHooks  RunPhase = "pre_hooks"
	PhaseTransfer  RunPhase = "transfer"
	PhaseVerify    RunPhase = "verify"
	PhasePrune     RunPhase = "prune"
	PhasePostHooks RunPhase = "post_hooks"
)

// RunRow is one persisted run (gorm, table run_rows). Only state
// transitions are written; live progress stays in memory.
type RunRow struct {
	ID           string `gorm:"primaryKey"` // "run_" + ULID (sortable: newest = greatest)
	JobID        string `gorm:"index"`
	Kind         string
	Trigger      string
	Status       string `gorm:"index"`
	Phase        string
	Attempt      int
	ErrorCode    string
	Summary      string // EncodeMessage(key, args)
	QueuedAt     time.Time
	StartedAt    *time.Time
	EndedAt      *time.Time
	FilesAdded   int64
	FilesChanged int64
	FilesDeleted int64
	FilesSkipped int64
	FilesErrored int64
	// BytesTransferred / BytesTotal are the transfer's byte counts.
	BytesTransferred int64
	BytesTotal       int64
	EngineJobID      int64
	HooksDone        string // JSON []HookDone - post-hook replay source
	GuardInfo        string // JSON engine.GuardInfo, for waiting_user
	LogPath          string // <data>/logs/<job>/<run>.jsonl[.gz]
	PreviewPath      string // <data>/previews/<run>.jsonl, plan output
	Coalesced        int
	RestoreSpec      string // JSON engine.RestoreSpec, kind=restore
	// Decision records a waiting_user answer: "" | "proceed" | "copy_once" | "decline".
	Decision string
}

// HookDone records a pre hook's progress before it acts, so a crash can
// be undone at start (post-hook replay, §8.2).
type HookDone struct {
	HookIdx    int               `json:"hook_idx"`
	PreDone    bool              `json:"pre_done"`
	PostDone   bool              `json:"post_done"`
	PriorState map[string]string `json:"prior_state"` // app or "vm:<name>" -> "running" | "stopped" | "shut off"
}

// MetaRow is a key/value row (gorm, table meta_rows).
type MetaRow struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// MetaRow keys.
const (
	MetaSchemaVersion   = "schema_version"
	MetaMigrationV1     = "migration.schedules_v1" // JSON MigrationReport
	MetaSettings        = "settings"               // JSON AppSettings
	MetaRememberedDrive = "usb."                   // + fs UUID -> JSON RememberedDrive
)

// SchemaVersion is the store schema this build writes.
const SchemaVersion = 1

// RememberedDrive is a USB drive seen by a job, listed offline when absent.
type RememberedDrive struct {
	Endpoint Endpoint  `json:"endpoint"`
	Label    string    `json:"label"` // user rename, display only
	LastSeen time.Time `json:"last_seen"`
}

// AppSettings are app-wide defaults (GET/PUT /settings).
type AppSettings struct {
	MaxConcurrent       int  `json:"max_concurrent"` // 1..4; the engine still caps cloud at 1
	CatchUpDefault      bool `json:"catch_up_default"`
	LogRetentionDays    int  `json:"log_retention_days"`
	DefaultDeletePct    int  `json:"default_delete_pct"`
	DefaultChangePct    int  `json:"default_change_pct"`
	DefaultVersionsDays int  `json:"default_versions_days"`
}

// Defaults (spec §4 comments), in one place for validate and the UI.
const (
	DefaultMaxConcurrent    = 2
	DefaultLogRetentionDays = 180
	DefaultEmptySourcePct   = 50
	DefaultDeletePct        = 10
	DefaultChangePct        = 30
	DefaultVersionsDays     = 30
	DefaultKeepLast         = 8
	DefaultWaitMaxMin       = 360
	DefaultMaxDurationSec   = 86400
	DefaultHookTimeoutSec   = 300
	DefaultVMHookTimeoutSec = 600
	DefaultRetryMax         = 3
	DefaultStaleMinHours    = 48
	MaxArchiveSources       = 16
	WaitingUserTimeout      = 72 * time.Hour
)

// DefaultRetryBackoffSec is the default Retry.BackoffSec.
var DefaultRetryBackoffSec = []int{60, 600, 3600}

// DefaultAppSettings returns the settings of a fresh install.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		MaxConcurrent:       DefaultMaxConcurrent,
		CatchUpDefault:      true,
		LogRetentionDays:    DefaultLogRetentionDays,
		DefaultDeletePct:    DefaultDeletePct,
		DefaultChangePct:    DefaultChangePct,
		DefaultVersionsDays: DefaultVersionsDays,
	}
}

// Message is a translatable sentence: an i18n key plus named args (spec
// §12.12 - the backend never sends English). Arg conventions the UI
// applies before interpolating:
//   - a key ending in "_key" holds another i18n key; it is translated
//     first (e.g. "reason_key": "backup.err.cloud_auth.title")
//   - "bytes" or a key ending in "_bytes" is a byte count to format
//   - "at" or a key ending in "_at" is an RFC 3339 time to format in the
//     server time zone
//
// Everything else is inserted as is.
type Message struct {
	Key  string                 `json:"key"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// EncodeMessage is the RunRow.Summary form: "key" or "key|{json args}".
func EncodeMessage(m Message) string {
	if len(m.Args) == 0 {
		return m.Key
	}
	raw, err := json.Marshal(m.Args)
	if err != nil {
		return m.Key
	}
	return m.Key + "|" + string(raw)
}

// DecodeMessage parses EncodeMessage's form. "" decodes to nil.
func DecodeMessage(s string) *Message {
	if s == "" {
		return nil
	}
	key, rawArgs, found := strings.Cut(s, "|")
	m := &Message{Key: key}
	if found {
		var args map[string]interface{}
		if json.Unmarshal([]byte(rawArgs), &args) == nil {
			m.Args = args
		}
	}
	return m
}
