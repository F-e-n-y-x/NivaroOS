package jobs

import (
	"sort"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// ErrorCode is the one error-code enum of Backup & Sync (spec §5). It is
// the engine's type, so engine results need no conversion. Every code has
// a class here, and ui/src/apps/backup/errorCodes.js is generated from
// this table (go generate ./... ; checked by TestUIContractInSync).
type ErrorCode = engine.ErrorCode

//go:generate go run ../cmd/gen-uicontract

// ErrorClass decides what the lifecycle does with a failed run.
type ErrorClass string

const (
	// ClassTransient: retry with backoff while attempt < Retry.Max.
	ClassTransient ErrorClass = "transient"
	// ClassDeferred: no retry; status skipped, requeued once at the next
	// local midnight + 10 min.
	ClassDeferred ErrorClass = "deferred"
	// ClassGuard: no retry; waiting_user (or failed for marker mismatch).
	ClassGuard ErrorClass = "guard"
	// ClassConfig: no retry; failed + notification.
	ClassConfig ErrorClass = "config"
	// ClassHook: no retry; a failed post hook makes the run partial.
	ClassHook ErrorClass = "hook"
	// ClassLifecycle: interrupted is requeued once; the rest are final.
	ClassLifecycle ErrorClass = "lifecycle"
	// ClassAPI: REST errors, never a run's error_code.
	ClassAPI ErrorClass = "api"
)

// Every error code. Engine codes are re-declared from package engine so
// there is one spelling.
const (
	// transient
	ErrEngineUnavailable            = engine.CodeEngineUnavailable
	ErrNetworkUnreachable           = engine.CodeNetworkUnreachable
	ErrCloudRateLimited             = engine.CodeCloudRateLimited
	ErrDestOffline                  = engine.CodeDestOffline // transient only with when_unmet=wait
	ErrSourceOffline                = engine.CodeSourceOffline
	ErrIOError                      = engine.CodeIOError
	ErrBusy               ErrorCode = "busy" // a Scheduled Task held the app/VM for 30 min
	// ErrSystemDiskFull: the disk holding Backup's state (logs, plan
	// lists, the database) is nearly full; a run would fill it.
	ErrSystemDiskFull ErrorCode = "system_disk_full"
	// deferred
	ErrCloudQuotaDaily = engine.CodeCloudQuotaDaily
	// guard
	ErrDeleteGuard        = engine.CodeDeleteGuard
	ErrChangeGuard        = engine.CodeChangeGuard
	ErrEmptySource        = engine.CodeEmptySource
	ErrDestMarkerMismatch = engine.CodeDestMarkerMismatch
	// config
	ErrDestInsideSource                   = engine.CodeDestInsideSource
	ErrPathNotAllowed                     = engine.CodePathNotAllowed
	ErrFat32FileTooLarge                  = engine.CodeFat32FileTooLarge
	ErrCaseCollision                      = engine.CodeCaseCollision
	ErrEndpointUnknown                    = engine.CodeEndpointUnknown
	ErrAmbiguousDevice                    = engine.CodeAmbiguousDevice
	ErrCloudAuth                          = engine.CodeCloudAuth
	ErrNoSpace                            = engine.CodeNoSpace
	ErrInvalidFilter                      = engine.CodeInvalidFilter
	ErrAppdataCloudNeedsArchive ErrorCode = "appdata_cloud_needs_archive"
	ErrWindowClosed             ErrorCode = "window_closed" // outside the time window (skip/fail)
	// hook
	ErrAppStopFailed     ErrorCode = "app_stop_failed"
	ErrAppStartFailed    ErrorCode = "app_start_failed"
	ErrVMShutdownTimeout ErrorCode = "vm_shutdown_timeout"
	ErrVMStartFailed     ErrorCode = "vm_start_failed"
	// lifecycle
	ErrInterrupted        ErrorCode = "interrupted"
	ErrCancelledByUser              = engine.CodeCancelledByUser
	ErrCancelledUnmounted           = engine.CodeCancelledUnmounted
	ErrMaxDuration                  = engine.CodeMaxDuration
	ErrDecisionTimeout    ErrorCode = "decision_timeout" // waiting_user unanswered for 72 h
	ErrInternal                     = engine.CodeInternal
	// api
	ErrValidation       ErrorCode = "validation"
	ErrRevisionConflict ErrorCode = "revision_conflict"
	ErrNotFound                   = engine.CodeNotFound
	ErrUnauthorized     ErrorCode = "unauthorized"
	ErrForbidden        ErrorCode = "forbidden" // not an admin
	ErrRateLimited      ErrorCode = "rate_limited"
	ErrStoreUnavailable ErrorCode = "store_unavailable"
	ErrInvalidState     ErrorCode = "invalid_state" // e.g. decide on a run that isn't waiting_user
	ErrTokenInvalid     ErrorCode = "token_invalid" // download token unknown, used or expired
)

// Action is a one-click fix the UI offers next to an error (spec §12.2).
// The UI labels it with backup.action.<action> and implements each one.
type Action string

const (
	ActViewLog       Action = "view_log"       // open the run window on the log
	ActRetry         Action = "retry"          // POST /jobs/:id/run
	ActEditJob       Action = "edit_job"       // open the wizard on this job
	ActChangeDest    Action = "change_dest"    // wizard, Where step
	ActReconnectDest Action = "reconnect_dest" // POST /jobs/:id/reconnect-dest (after a confirm)
	ActFixSignIn     Action = "fix_sign_in"    // Storage > cloud accounts
	ActReview        Action = "review"         // open BackupPreviewWindow
	ActOpenStorage   Action = "open_storage"   // Storage app (free space, plug in drive)
	ActEditFilters   Action = "edit_filters"   // wizard, What step (excludes)
	ActSwitchArchive Action = "switch_archive" // wizard, set type archive
	ActHowToRun      Action = "how_to_run"     // explain how to connect the drive
	ActEditHooks     Action = "edit_hooks"     // wizard, What step (apps/VM)
	ActSignIn        Action = "sign_in"        // reload / log in again
)

// CodeInfo is a code's class and UI fixes.
type CodeInfo struct {
	Class   ErrorClass `json:"class"`
	Actions []Action   `json:"actions"`
}

var codeTable = map[ErrorCode]CodeInfo{
	ErrEngineUnavailable:  {ClassTransient, []Action{ActViewLog}},
	ErrNetworkUnreachable: {ClassTransient, []Action{ActRetry, ActViewLog}},
	ErrCloudRateLimited:   {ClassTransient, []Action{ActViewLog}},
	ErrDestOffline:        {ClassTransient, []Action{ActHowToRun, ActChangeDest}},
	ErrSourceOffline:      {ClassTransient, []Action{ActHowToRun, ActEditJob}},
	ErrIOError:            {ClassTransient, []Action{ActViewLog, ActRetry}},
	ErrBusy:               {ClassTransient, []Action{ActRetry, ActViewLog}},
	ErrSystemDiskFull:     {ClassTransient, []Action{ActOpenStorage, ActRetry}},

	ErrCloudQuotaDaily: {ClassDeferred, []Action{ActViewLog}},

	ErrDeleteGuard:        {ClassGuard, []Action{ActReview}},
	ErrChangeGuard:        {ClassGuard, []Action{ActReview}},
	ErrEmptySource:        {ClassGuard, []Action{ActReview, ActEditJob}},
	ErrDestMarkerMismatch: {ClassGuard, []Action{ActReconnectDest, ActChangeDest}},

	ErrDestInsideSource:         {ClassConfig, []Action{ActChangeDest, ActEditFilters}},
	ErrPathNotAllowed:           {ClassConfig, []Action{ActEditJob}},
	ErrFat32FileTooLarge:        {ClassConfig, []Action{ActEditFilters, ActChangeDest}},
	ErrCaseCollision:            {ClassConfig, []Action{ActViewLog, ActChangeDest}},
	ErrEndpointUnknown:          {ClassConfig, []Action{ActEditJob}},
	ErrAmbiguousDevice:          {ClassConfig, []Action{ActOpenStorage, ActChangeDest}},
	ErrCloudAuth:                {ClassConfig, []Action{ActFixSignIn}},
	ErrNoSpace:                  {ClassConfig, []Action{ActOpenStorage, ActChangeDest}},
	ErrInvalidFilter:            {ClassConfig, []Action{ActEditFilters}},
	ErrAppdataCloudNeedsArchive: {ClassConfig, []Action{ActSwitchArchive}},
	ErrWindowClosed:             {ClassConfig, []Action{ActEditJob}},

	ErrAppStopFailed:     {ClassHook, []Action{ActViewLog, ActEditHooks}},
	ErrAppStartFailed:    {ClassHook, []Action{ActViewLog}},
	ErrVMShutdownTimeout: {ClassHook, []Action{ActViewLog, ActEditHooks}},
	ErrVMStartFailed:     {ClassHook, []Action{ActViewLog}},

	ErrInterrupted:        {ClassLifecycle, []Action{ActViewLog}},
	ErrCancelledByUser:    {ClassLifecycle, []Action{ActRetry}},
	ErrCancelledUnmounted: {ClassLifecycle, []Action{ActHowToRun, ActRetry}},
	ErrMaxDuration:        {ClassLifecycle, []Action{ActEditJob, ActViewLog}},
	ErrDecisionTimeout:    {ClassLifecycle, []Action{ActRetry}},
	ErrInternal:           {ClassLifecycle, []Action{ActViewLog}},

	ErrValidation:       {ClassAPI, nil},
	ErrRevisionConflict: {ClassAPI, nil},
	ErrNotFound:         {ClassAPI, nil},
	ErrUnauthorized:     {ClassAPI, []Action{ActSignIn}},
	ErrForbidden:        {ClassAPI, nil},
	ErrRateLimited:      {ClassAPI, nil},
	ErrStoreUnavailable: {ClassAPI, nil},
	ErrInvalidState:     {ClassAPI, nil},
	ErrTokenInvalid:     {ClassAPI, nil},
}

// Info returns a code's class and actions. Unknown codes are treated as
// internal (ok=false).
func Info(c ErrorCode) (CodeInfo, bool) {
	info, ok := codeTable[c]
	if !ok {
		return codeTable[ErrInternal], false
	}
	return info, true
}

// ClassOf is Info(c).Class.
func ClassOf(c ErrorCode) ErrorClass {
	info, _ := Info(c)
	return info.Class
}

// Retryable reports whether a run failing with c is retried with backoff.
// dest_offline is transient only when the job waits for its destination
// (when_unmet=wait); otherwise it ends the run as skipped/failed.
func Retryable(c ErrorCode, unmet WhenUnmet) bool {
	if c == ErrDestOffline && unmet != UnmetWait {
		return false
	}
	return ClassOf(c) == ClassTransient
}

// AllErrorCodes lists every code, sorted.
func AllErrorCodes() []ErrorCode {
	out := make([]ErrorCode, 0, len(codeTable))
	for c := range codeTable {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AllActions lists every action, sorted.
func AllActions() []Action {
	seen := map[Action]bool{}
	for _, info := range codeTable {
		for _, a := range info.Actions {
			seen[a] = true
		}
	}
	out := make([]Action, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// FieldCode is a per-field validation failure in a 400 response's
// field_errors (field path -> code). Error codes may be used there too
// (e.g. "dest.sub_path": "path_not_allowed"); the UI looks a value up as
// backup.field.<code> first, then backup.err.<code>.title.
type FieldCode string

const (
	FieldRequired        FieldCode = "required"
	FieldInvalid         FieldCode = "invalid"
	FieldOutOfRange      FieldCode = "out_of_range" // args: min, max (in the key text)
	FieldTooMany         FieldCode = "too_many"     // archive: more than 16 sources
	FieldInvalidCron     FieldCode = "invalid_cron"
	FieldInvalidTime     FieldCode = "invalid_time" // window HH:MM
	FieldUnknownApp      FieldCode = "unknown_app"
	FieldUnknownVM       FieldCode = "unknown_vm"
	FieldMirrorNeedsDest FieldCode = "mirror_needs_dest" // mirror requires dest_available
	FieldNotResolvable   FieldCode = "not_resolvable"    // volume_mounted ref can't be resolved
	// FieldDestOverlapsJob: the destination is, holds or lies inside
	// another job's destination (a mirror would recycle the other backup).
	FieldDestOverlapsJob FieldCode = "dest_overlaps_job"
)

// AllFieldCodes lists every field code, in declaration order.
var AllFieldCodes = []FieldCode{
	FieldRequired, FieldInvalid, FieldOutOfRange, FieldTooMany, FieldInvalidCron, FieldInvalidTime,
	FieldUnknownApp, FieldUnknownVM, FieldMirrorNeedsDest, FieldNotResolvable, FieldDestOverlapsJob,
}
