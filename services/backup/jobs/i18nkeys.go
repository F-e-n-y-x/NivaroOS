package jobs

import (
	"sort"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// i18n keys the backend sends (spec §12.12: keys and args, never English).
// Each key maps to the args it is sent with; the English text in
// ui/src/assets/lang/en_US.json may only use those as {placeholders}.
// TestI18nKeysInEnUS checks both. Arg naming follows Message's rules
// (_key / bytes / _at suffixes).

// CronHumanKeys are CronPreview.HumanKey values. "days" is a list of
// weekday numbers joined by "," (0 = Sunday) the UI turns into names;
// "time" is "HH:MM".
var CronHumanKeys = map[string][]string{
	"backup.cron.every_minute":    nil,
	"backup.cron.every_n_minutes": {"n"},
	"backup.cron.hourly_at":       {"minute"},
	"backup.cron.every_n_hours":   {"n", "minute"},
	"backup.cron.daily_at":        {"time"},
	"backup.cron.weekdays_at":     {"days", "time"},
	"backup.cron.weekly_at":       {"day", "time"},
	"backup.cron.monthly_at":      {"dom", "time"},
	"backup.cron.custom":          {"expr"},
}

// RunSummaryKeys are Run.Summary keys.
var RunSummaryKeys = map[string][]string{
	"backup.run.summary.ok":          {"added", "changed", "deleted", "bytes"},
	"backup.run.summary.ok_nothing":  nil, // nothing had changed
	"backup.run.summary.partial":     {"added", "changed", "errors"},
	"backup.run.summary.failed":      {"reason_key"},
	"backup.run.summary.skipped":     {"reason_key"},
	"backup.run.summary.deferred":    {"reason_key", "retry_at"},
	"backup.run.summary.waiting":     {"guard_key", "pct"},
	"backup.run.summary.cancelled":   {"reason_key"},
	"backup.run.summary.interrupted": nil,
	"backup.run.summary.preview":     {"add", "update", "delete"},
	"backup.run.summary.restored":    {"files", "bytes"},
	"backup.run.summary.verified":    {"files"},
	"backup.run.summary.pruned":      {"removed"},
}

// RunStepKeys are RunStep.Key values.
var RunStepKeys = map[string][]string{
	"backup.run.step.precheck":    {"dest"},
	"backup.run.step.stop_apps":   {"apps"}, // comma-separated names
	"backup.run.step.shutdown_vm": {"vm"},
	"backup.run.step.transfer":    nil,
	"backup.run.step.archive":     nil,
	"backup.run.step.restore":     nil,
	"backup.run.step.verify":      nil,
	"backup.run.step.prune":       {"days"},
	"backup.run.step.prune_keep":  {"keep"},
	"backup.run.step.start_apps":  {"apps"},
	"backup.run.step.start_vm":    {"vm"},
}

// LogKeys are LogLine.MsgKey values; the map key's last segment is also
// the LogLine.Code ("backup.log.copied" -> "copied"). Engine rclone lines
// use Code "raw" with no key.
var LogKeys = map[string][]string{
	"backup.log.copied":         {"path"},
	"backup.log.updated":        {"path"},
	"backup.log.recycled":       {"path"},
	"backup.log.skipped":        {"path", "reason_key"},
	"backup.log.file_error":     {"path", "reason_key"},
	"backup.log.phase":          {"phase_key"},
	"backup.log.check":          {"check_key", "status"},
	"backup.log.app_stopped":    {"app"},
	"backup.log.app_started":    {"app"},
	"backup.log.app_was_off":    {"app"},
	"backup.log.vm_shutdown":    {"vm"},
	"backup.log.vm_started":     {"vm"},
	"backup.log.vm_was_off":     {"vm"},
	"backup.log.guard_tripped":  {"guard_key", "pct", "limit"},
	"backup.log.decision":       {"decision"},
	"backup.log.retry":          {"attempt", "max", "retry_at"},
	"backup.log.marker_written": nil,
	"backup.log.archive_done":   {"name", "bytes"},
	"backup.log.pruned":         {"name"},
	"backup.log.replayed":       nil, // post hooks replayed after a crash
	"backup.log.finished":       {"status_key"},
	"backup.log.lines_omitted":  {"count"}, // per-file lines past the engine's cap
}

// NotifyKeys are notification texts (spec §10.4). The service renders the
// English itself (it has no i18n) and stores {key, args} with the
// notification so the UI can re-render it in the user's language.
var NotifyKeys = map[string][]string{
	"backup.notify.failed":           {"job", "reason_key"},
	"backup.notify.waiting":          {"job", "guard_key", "pct"},
	"backup.notify.stale":            {"job", "days"},
	"backup.notify.offline":          {"job", "dest"},
	"backup.notify.success":          {"job"},
	"backup.notify.usb_running":      {"job", "drive"},
	"backup.notify.usb_done":         {"job", "drive"},
	"backup.notify.migrated":         {"count"},
	"backup.notify.decision_timeout": {"job"},
	"backup.notify.clock_unsynced":   nil,
	"backup.notify.partial":          {"job", "errors"},
	"backup.notify.preview_ready":    {"job"},            // a scheduled first run waits for its preview to be approved
	"backup.notify.restart_gave_up":  {"job", "targets"}, // targets: comma-separated apps / VMs still off
}

// MigrationNoteKeys are MigrationItem.Notes keys.
var MigrationNoteKeys = map[string][]string{
	"backup.migrate.note.move_to_copy":      nil,
	"backup.migrate.note.dest_excluded":     {"path"},
	"backup.migrate.note.unresolved_source": {"path"},
	"backup.migrate.note.unresolved_dest":   {"path"},
	"backup.migrate.note.dropped_args":      {"args"},
	"backup.migrate.note.dest_overlap":      nil, // shares its destination with another job
}

// GuardKeys name guards in guard_key args.
var GuardKeys = map[string][]string{
	"backup.guard.delete":       nil,
	"backup.guard.change":       nil,
	"backup.guard.empty_source": nil,
}

// VersionLabelKeys are Version.LabelKey values.
var VersionLabelKeys = map[string][]string{
	"backup.ver.current": nil,
	"backup.ver.recycle": {"at"},
	"backup.ver.archive": {"at"},
}

// CheckIDs are every engine and job-side check id; each is named with
// "backup.check.<id>".
var CheckIDs = []string{
	engine.CheckSourceResolves, engine.CheckDestResolves, engine.CheckNotInside, engine.CheckAllowedRoots,
	engine.CheckFreeSpace, engine.CheckFSQuirks, engine.CheckMetadata, engine.CheckSourceSentinel,
	engine.CheckDestMarker, CheckCron, CheckHooks, CheckTypeForDest, CheckSameDisk, CheckJobOverlap,
}

// CheckMessageKeys are Check.MsgKey values that aren't "backup.check.<id>"
// (the job_overlap warnings say which job and how).
var CheckMessageKeys = map[string][]string{
	"backup.check.overlap_mirror_dest":     {"job"}, // a mirror writes where this job reads
	"backup.check.overlap_own_mirror_dest": {"job"}, // this mirror writes where that job reads
	"backup.check.overlap_mirror_source":   {"job"}, // a mirror reads where this job writes
}

// Enumerations the UI names with "<prefix><value>".
var (
	RunStatuses   = []RunStatus{StatusQueued, StatusRunning, StatusWaitingUser, StatusSuccess, StatusPartial, StatusFailed, StatusCancelled, StatusSkipped, StatusInterrupted}
	RunPhases     = []RunPhase{PhasePrecheck, PhasePreHooks, PhaseTransfer, PhaseVerify, PhasePrune, PhasePostHooks}
	RunKinds      = []RunKind{KindBackup, KindPreview, KindRestore, KindVerify, KindPrune}
	RunTriggers   = []RunTrigger{RunBySchedule, RunByVolumeMounted, RunByCatchUp, RunByManual, RunByRetry}
	JobTypes      = []JobType{TypeCopy, TypeMirror, TypeArchive}
	JobHealths    = []string{HealthProblem, HealthOffline, HealthWarning, HealthOK, HealthDisabled}
	Quirks        = []engine.Quirk{engine.QuirkCaseInsensitive, engine.QuirkNTFSChars, engine.QuirkMtime2s, engine.QuirkMaxFile4G, engine.QuirkNoModTime, engine.QuirkNoHash, engine.QuirkNoMetadata, engine.QuirkPerBranchFree, engine.QuirkDailyQuota}
	Warnings      = []engine.Warning{engine.WarnLimitedChangeDetection, engine.WarnSystemDisk, engine.WarnExportedShare, engine.WarnWorldWritable}
	EndpointKinds = []EndpointKind{EPVolume, EPUSB, EPMerge, EPSMB, EPCloud}
)

// AllI18nKeys returns every key the backend relies on, with its args:
// the maps above, plus the enumerations and error codes expanded:
//
//	backup.err.<code>.{title,cause,fix}  backup.action.<action>
//	backup.field.<field code>            backup.check.<id>
//	backup.status.<status>               backup.phase.<phase>
//	backup.kind.<kind>                   backup.trigger.<run trigger>
//	backup.type.<type>.{label,promise,deleted}
//	backup.health.<health>               backup.quirk.<quirk>
//	backup.warn.<warning>                backup.ep.<endpoint kind>
//	backup.exclude.<preset>
func AllI18nKeys() map[string][]string {
	out := map[string][]string{}
	for _, m := range []map[string][]string{CronHumanKeys, RunSummaryKeys, RunStepKeys, LogKeys, NotifyKeys, MigrationNoteKeys, GuardKeys, VersionLabelKeys, CheckMessageKeys} {
		for k, v := range m {
			out[k] = v
		}
	}
	add := func(k string) { out[k] = nil }
	for _, c := range AllErrorCodes() {
		for _, part := range []string{"title", "cause", "fix"} {
			add("backup.err." + string(c) + "." + part)
		}
	}
	for _, a := range AllActions() {
		add("backup.action." + string(a))
	}
	for _, f := range AllFieldCodes {
		add("backup.field." + string(f))
	}
	for _, id := range CheckIDs {
		add("backup.check." + id)
	}
	for _, s := range RunStatuses {
		add("backup.status." + string(s))
	}
	for _, p := range RunPhases {
		add("backup.phase." + string(p))
	}
	for _, k := range RunKinds {
		add("backup.kind." + string(k))
	}
	for _, t := range RunTriggers {
		add("backup.trigger." + string(t))
	}
	for _, t := range JobTypes {
		for _, part := range []string{"label", "promise", "deleted"} {
			add("backup.type." + string(t) + "." + part)
		}
	}
	for _, h := range JobHealths {
		add("backup.health." + h)
	}
	for _, q := range Quirks {
		add("backup.quirk." + string(q))
	}
	for _, w := range Warnings {
		add("backup.warn." + string(w))
	}
	for _, k := range EndpointKinds {
		add("backup.ep." + string(k))
	}
	for _, p := range engine.ExcludePresets {
		add("backup.exclude." + p)
	}
	return out
}

// SortedI18nKeys is AllI18nKeys' keys, sorted.
func SortedI18nKeys() []string {
	all := AllI18nKeys()
	out := make([]string, 0, len(all))
	for k := range all {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
