package jobs

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rclone/rclone/fs/filter"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Limits of a job definition. They keep one request from storing
// something the engine or the UI can't handle.
const (
	maxNameLen        = 120
	maxLabelLen       = 200
	maxRefIDLen       = 512
	maxSubPathLen     = 4096
	maxTriggers       = 8
	maxHooks          = 16
	maxHookApps       = 32
	maxFilterRules    = 200
	maxFilterRuleLen  = 1024
	maxRetryMax       = 10
	maxWaitMaxMin     = 7 * 24 * 60
	minWaitMaxMin     = 5
	maxMinGapHours    = 24 * 365
	maxDurationSec    = 7 * 86400
	minDurationSec    = 60
	maxVersionsDays   = 3650
	maxKeepLast       = 1000
	maxStaleHours     = 24 * 365
	minHookTimeoutSec = 10
	maxHookTimeoutSec = 3600
	minBackoffSec     = 10
	maxBackoffSec     = 86400
	maxRestorePaths   = 1000
)

// nameRe is what app and VM names may look like (app-management compose
// names, libvirt domain names); it keeps them safe in URL paths.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// hhmmRe is a time-window bound.
var hhmmRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidateEnv is what validation needs beyond the job itself. Apps / VMs
// list what is installed (the value - running or not - doesn't matter
// here); they may be nil or return an error when app-management or
// vm-sidecar isn't reachable, and hook targets are then not checked here
// (the hook fails at run time instead, with app_stop_failed /
// vm_shutdown_timeout).
type ValidateEnv struct {
	Settings AppSettings
	Apps     func() (map[string]bool, error)
	VMs      func() (map[string]bool, error)
}

// fieldErrors collects field path -> code.
type fieldErrors map[string]string

func (fe fieldErrors) add(field string, code interface{}) {
	if _, dup := fe[field]; dup {
		return // the first problem of a field is the one to fix first
	}
	fe[field] = fmt.Sprint(code)
}

// NormalizeJob validates a job as a client sent it and fills in every
// default (spec §11 "Server-side validation"). It returns the normalised
// job and the field errors; the job may only be stored when there are
// none. Server-owned fields are cleared - the store keeps its own.
func NormalizeJob(in Job, env ValidateEnv) (Job, map[string]string) {
	j := in
	fe := fieldErrors{}
	j.ID, j.Revision, j.DestFolderID, j.NeedsAttention, j.MigratedFrom = "", 0, "", "", nil

	j.Name = strings.TrimSpace(j.Name)
	switch {
	case j.Name == "":
		fe.add("name", FieldRequired)
	case utf8.RuneCountInString(j.Name) > maxNameLen || strings.ContainsAny(j.Name, "\x00\r\n"):
		fe.add("name", FieldInvalid)
	}
	if !j.Type.Valid() {
		fe.add("type", FieldInvalid)
	}

	// Sources and destination.
	if j.Sources == nil {
		j.Sources = []Endpoint{}
	}
	switch {
	case len(j.Sources) == 0:
		fe.add("sources", FieldRequired)
	case j.Type == TypeArchive && len(j.Sources) > MaxArchiveSources:
		fe.add("sources", FieldTooMany)
	case j.Type != TypeArchive && j.Type.Valid() && len(j.Sources) != 1:
		fe.add("sources", FieldTooMany)
	}
	for i := range j.Sources {
		normalizeEndpoint(&j.Sources[i], fmt.Sprintf("sources[%d]", i), fe)
	}
	normalizeEndpoint(&j.Dest, "dest", fe)
	for i, src := range j.Sources {
		checkNotInside(src, j.Dest, j.Filters, fmt.Sprintf("sources[%d]", i), fe)
	}
	for i := 0; i < len(j.Sources); i++ {
		for k := i + 1; k < len(j.Sources); k++ {
			if sameEndpoint(j.Sources[i], j.Sources[k]) {
				fe.add(fmt.Sprintf("sources[%d]", k), FieldInvalid)
			}
		}
	}
	if needsArchiveForDest(j) {
		fe.add("type", ErrAppdataCloudNeedsArchive)
	}

	// Triggers.
	if j.Triggers == nil {
		j.Triggers = []Trigger{}
	}
	if len(j.Triggers) > maxTriggers {
		fe.add("triggers", FieldTooMany)
	}
	hasSchedule := false
	for i := range j.Triggers {
		t := &j.Triggers[i]
		p := fmt.Sprintf("triggers[%d]", i)
		switch t.Kind {
		case TriggerSchedule:
			hasSchedule = true
			t.VolumeRef, t.MinGapHours = nil, 0
			if strings.TrimSpace(t.Cron) == "" {
				fe.add(p+".cron", FieldRequired)
			} else if _, err := CronParser.Parse(t.Cron); err != nil {
				fe.add(p+".cron", FieldInvalidCron)
			}
		case TriggerVolumeMounted:
			t.Cron = ""
			if t.MinGapHours < 0 || t.MinGapHours > maxMinGapHours {
				fe.add(p+".min_gap_hours", FieldOutOfRange)
			}
			if t.VolumeRef != nil {
				normalizeEndpoint(t.VolumeRef, p+".volume", fe)
				if t.VolumeRef.Kind != EPVolume && t.VolumeRef.Kind != EPUSB {
					fe.add(p+".volume", FieldNotResolvable)
				}
			} else if triggerVolume(j, *t) == nil {
				fe.add(p+".volume", FieldNotResolvable)
			}
		case TriggerManual:
			t.Cron, t.VolumeRef, t.MinGapHours = "", nil, 0
		default:
			fe.add(p+".kind", FieldInvalid)
		}
	}

	// Conditions.
	c := &j.Conditions
	if j.Type == TypeMirror && !c.DestAvailable {
		fe.add("conditions.dest_available", FieldMirrorNeedsDest)
	}
	if c.Window != nil {
		c.Window.Start, c.Window.End = strings.TrimSpace(c.Window.Start), strings.TrimSpace(c.Window.End)
		if !hhmmRe.MatchString(c.Window.Start) {
			fe.add("conditions.window.start", FieldInvalidTime)
		}
		if !hhmmRe.MatchString(c.Window.End) {
			fe.add("conditions.window.end", FieldInvalidTime)
		}
		if c.Window.Start == c.Window.End {
			fe.add("conditions.window.end", FieldInvalid)
		}
	}
	switch c.WhenUnmet {
	case "":
		c.WhenUnmet = UnmetSkip
		if hasSchedule {
			c.WhenUnmet = UnmetWait
		}
	case UnmetSkip, UnmetWait, UnmetFail:
	default:
		fe.add("conditions.when_unmet", FieldInvalid)
	}
	if c.WaitMaxMin == 0 {
		c.WaitMaxMin = DefaultWaitMaxMin
	} else if c.WaitMaxMin < minWaitMaxMin || c.WaitMaxMin > maxWaitMaxMin {
		fe.add("conditions.wait_max_min", FieldOutOfRange)
	}

	normalizeFilters(&j.Filters, fe)

	// Options.
	o := &j.Options
	if o.MaxDurationSec == 0 {
		o.MaxDurationSec = DefaultMaxDurationSec
	} else if o.MaxDurationSec < minDurationSec || o.MaxDurationSec > maxDurationSec {
		fe.add("options.max_duration_sec", FieldOutOfRange)
	}

	// Guards: 0 means "the app default"; otherwise 1..100.
	g := &j.Guards
	pct := func(v *int, def int, field string) {
		if *v == 0 {
			*v = def
		}
		if *v < 1 || *v > 100 {
			fe.add(field, FieldOutOfRange)
		}
	}
	pct(&g.EmptySourcePct, DefaultEmptySourcePct, "guards.empty_source_pct")
	pct(&g.DeletePct, orDefault(env.Settings.DefaultDeletePct, DefaultDeletePct), "guards.delete_pct")
	pct(&g.ChangePct, orDefault(env.Settings.DefaultChangePct, DefaultChangePct), "guards.change_pct")

	// Retention applies to one type each.
	r := &j.Retention
	switch j.Type {
	case TypeMirror:
		r.KeepLast = 0
		if r.VersionsDays < 0 || r.VersionsDays > maxVersionsDays {
			fe.add("retention.versions_days", FieldOutOfRange)
		}
	case TypeArchive:
		r.VersionsDays = 0
		if r.KeepLast == 0 {
			r.KeepLast = DefaultKeepLast
		} else if r.KeepLast < 1 || r.KeepLast > maxKeepLast {
			fe.add("retention.keep_last", FieldOutOfRange)
		}
	default:
		*r = Retention{}
	}

	normalizeHooks(&j, env, fe)

	// Retry.
	rt := &j.Retry
	if rt.Max < 0 || rt.Max > maxRetryMax {
		fe.add("retry.max", FieldOutOfRange)
	}
	if len(rt.BackoffSec) == 0 {
		rt.BackoffSec = append([]int(nil), DefaultRetryBackoffSec...)
	}
	if len(rt.BackoffSec) > maxRetryMax {
		fe.add("retry.backoff_sec", FieldTooMany)
	}
	for i, b := range rt.BackoffSec {
		if b < minBackoffSec || b > maxBackoffSec {
			fe.add(fmt.Sprintf("retry.backoff_sec[%d]", i), FieldOutOfRange)
		}
	}

	// Notifications: failures always notify in v1.
	j.Notify.OnFailure = true
	if j.Notify.StaleAfterHours == 0 {
		j.Notify.StaleAfterHours = defaultStaleHours(j)
	} else if j.Notify.StaleAfterHours < 1 || j.Notify.StaleAfterHours > maxStaleHours {
		fe.add("notify.stale_after_hours", FieldOutOfRange)
	}

	if len(fe) == 0 {
		return j, nil
	}
	return j, fe
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// CleanSubPath normalises an endpoint sub-path: slash separated,
// relative, no "..", no NUL, no leading "/"; "." and "" are the root.
func CleanSubPath(p string) (string, bool) {
	if strings.ContainsRune(p, 0) || len(p) > maxSubPathLen || !utf8.ValidString(p) {
		return "", false
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	c := path.Clean("/" + p)
	c = strings.TrimPrefix(c, "/")
	return c, true
}

func normalizeEndpoint(ep *Endpoint, field string, fe fieldErrors) {
	if !ep.Kind.Valid() {
		fe.add(field+".kind", FieldInvalid)
	}
	ep.RefID = strings.TrimSpace(ep.RefID)
	switch {
	case ep.RefID == "":
		fe.add(field+".ref_id", FieldRequired)
	case len(ep.RefID) > maxRefIDLen || strings.ContainsAny(ep.RefID, "\x00\r\n"):
		fe.add(field+".ref_id", FieldInvalid)
	}
	sub, ok := CleanSubPath(ep.SubPath)
	if !ok {
		fe.add(field+".sub_path", ErrPathNotAllowed)
	}
	ep.SubPath = sub
	if ep.Kind == EPSMB && ok && sub == "" {
		// The first segment is the share; the root of a server is not a folder.
		fe.add(field+".sub_path", FieldRequired)
	}
	if ep.Kind != EPUSB {
		ep.Match = nil
	} else if ep.Match != nil && ep.Match.SizeBytes < 0 {
		fe.add(field+".match.size_bytes", FieldInvalid)
	}
	ep.Label = strings.TrimSpace(ep.Label)
	if utf8.RuneCountInString(ep.Label) > maxLabelLen || strings.ContainsRune(ep.Label, 0) {
		fe.add(field+".label", FieldInvalid)
	}
	if ep.Preset != "" && !validPreset(ep.Preset) {
		fe.add(field+".preset", FieldInvalid)
	}
}

func validPreset(p string) bool {
	for _, prefix := range []string{PresetData, PresetAppData, PresetVM, PresetShare} {
		if name, ok := strings.CutPrefix(p, prefix); ok {
			return name != "" && len(name) <= 255 && !strings.ContainsAny(name, "/\x00")
		}
	}
	return false
}

// sameEndpoint reports whether a and b name the same location by identity.
func sameEndpoint(a, b Endpoint) bool {
	return a.Kind == b.Kind && a.RefID == b.RefID && a.SubPath == b.SubPath
}

// subPathWithin reports whether inner is outer or below it (segment-wise).
func subPathWithin(inner, outer string) bool {
	if outer == "" || inner == outer {
		return true
	}
	return strings.HasPrefix(inner, outer+"/")
}

// endpointsOverlap reports whether one of a, b lies inside the other on
// the same endpoint.
func endpointsOverlap(a, b Endpoint) bool {
	if a.Kind != b.Kind || a.RefID != b.RefID {
		return false
	}
	return subPathWithin(a.SubPath, b.SubPath) || subPathWithin(b.SubPath, a.SubPath)
}

// checkDestOverlap refuses a destination that is, holds or lies inside
// another job's destination. Each destination folder belongs to one job
// (one identity marker), and a mirror recycles everything in its
// destination its source doesn't have - another job's backup below it
// included, marker and all. Endpoints are compared by identity, like
// checkNotInside; others is every stored job, selfID the job being
// edited ("" for a new one).
func checkDestOverlap(j Job, selfID string, others []Job, fe fieldErrors) {
	if j.Dest.RefID == "" {
		return
	}
	for _, o := range others {
		if o.ID == selfID || o.Dest.RefID == "" {
			continue
		}
		if endpointsOverlap(j.Dest, o.Dest) {
			fe.add("dest.sub_path", FieldDestOverlapsJob)
			return
		}
	}
}

// overlapWarnings are the job_overlap warnings of POST /validate: folders
// shared with a mirror that don't break anything by themselves but that
// the user should know about.
func overlapWarnings(j Job, selfID string, others []Job) []Check {
	var out []Check
	warn := func(key string, other Job) {
		out = append(out, Check{ID: CheckJobOverlap, Status: engine.CheckWarn, MsgKey: key,
			Args: map[string]interface{}{"job": other.Name}})
	}
	anyOverlap := func(eps []Endpoint, ep Endpoint) bool {
		for _, e := range eps {
			if e.RefID != "" && endpointsOverlap(e, ep) {
				return true
			}
		}
		return false
	}
	for _, o := range others {
		if o.ID == selfID {
			continue
		}
		switch {
		case o.Type == TypeMirror && anyOverlap(j.Sources, o.Dest):
			warn("backup.check.overlap_mirror_dest", o)
		case j.Type == TypeMirror && anyOverlap(o.Sources, j.Dest):
			warn("backup.check.overlap_own_mirror_dest", o)
		case o.Type == TypeMirror && j.Dest.RefID != "" && anyOverlap(o.Sources, j.Dest):
			warn("backup.check.overlap_mirror_source", o)
		}
	}
	return out
}

// DestExcludeRule is the filter rule that keeps a destination below its
// source out of the transfer: the destination's path relative to the
// source, anchored.
func DestExcludeRule(src, dst Endpoint) (string, bool) {
	if src.Kind != dst.Kind || src.RefID != dst.RefID || dst.SubPath == src.SubPath || !subPathWithin(dst.SubPath, src.SubPath) {
		return "", false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(dst.SubPath, src.SubPath), "/")
	return "/" + rel + "/**", true
}

// checkNotInside rejects a source inside (or equal to) the destination,
// and a destination inside the source unless its exclude rule is in the
// filters (spec §6.3 check 2; the migration adds that rule itself).
// Different endpoints can still overlap on disk (a volume mounted inside
// another); the engine's not_inside precheck catches those after
// resolving.
func checkNotInside(src, dst Endpoint, f Filters, field string, fe fieldErrors) {
	if !endpointsOverlap(src, dst) {
		return
	}
	if rule, ok := DestExcludeRule(src, dst); ok {
		for _, x := range f.Exclude {
			if x == rule {
				return
			}
		}
		fe.add("dest.sub_path", ErrDestInsideSource)
		return
	}
	fe.add(field+".sub_path", ErrDestInsideSource)
}

// needsArchiveForDest: app data and VM folders keep owners, modes and
// links that cloud storage can't, so toward a cloud they may only be
// archived (spec §6.3 check 6).
func needsArchiveForDest(j Job) bool {
	if j.Type == TypeArchive || j.Dest.Kind != EPCloud {
		return false
	}
	for _, s := range j.Sources {
		if strings.HasPrefix(s.Preset, PresetAppData) || strings.HasPrefix(s.Preset, PresetVM) {
			return true
		}
	}
	return false
}

// triggerVolume is the volume a volume_mounted trigger watches: its own
// ref, else the job's destination, else its (first) source - whichever is
// a volume or USB drive. nil when none is.
func triggerVolume(j Job, t Trigger) *Endpoint {
	if t.VolumeRef != nil {
		return t.VolumeRef
	}
	isVol := func(e Endpoint) bool { return (e.Kind == EPVolume || e.Kind == EPUSB) && e.RefID != "" }
	if isVol(j.Dest) {
		ep := j.Dest
		return &ep
	}
	for _, s := range j.Sources {
		if isVol(s) {
			ep := s
			return &ep
		}
	}
	return nil
}

func normalizeFilters(f *Filters, fe fieldErrors) {
	if f.ExcludePresets == nil {
		f.ExcludePresets = []string{}
	}
	if f.Exclude == nil {
		f.Exclude = []string{}
	}
	known := map[string]bool{}
	for _, p := range engine.ExcludePresets {
		known[p] = true
	}
	seen := map[string]bool{}
	presets := f.ExcludePresets[:0]
	for i, p := range f.ExcludePresets {
		if !known[p] {
			fe.add(fmt.Sprintf("filters.exclude_presets[%d]", i), FieldInvalid)
			continue
		}
		if !seen[p] {
			seen[p] = true
			presets = append(presets, p)
		}
	}
	f.ExcludePresets = presets
	check := func(rules []string, field string) []string {
		if len(rules) > maxFilterRules {
			fe.add(field, FieldTooMany)
		}
		out := make([]string, 0, len(rules))
		for i, r := range rules {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			if len(r) > maxFilterRuleLen || strings.ContainsAny(r, "\x00\r\n") {
				fe.add(fmt.Sprintf("%s[%d]", field, i), ErrInvalidFilter)
				continue
			}
			if _, err := filter.GlobPathToRegexp(r, false); err != nil {
				fe.add(fmt.Sprintf("%s[%d]", field, i), ErrInvalidFilter)
				continue
			}
			out = append(out, r)
		}
		return out
	}
	f.Exclude = check(f.Exclude, "filters.exclude")
	if len(f.Include) > 0 {
		f.Include = check(f.Include, "filters.include")
	}
	if len(f.Include) == 0 {
		f.Include = nil
	}
	if f.MaxSizeBytes < 0 {
		fe.add("filters.max_size_bytes", FieldOutOfRange)
	}
}

func normalizeHooks(j *Job, env ValidateEnv, fe fieldErrors) {
	if j.Hooks == nil {
		j.Hooks = []Hook{}
	}
	if len(j.Hooks) > maxHooks {
		fe.add("hooks", FieldTooMany)
	}
	var apps, vms map[string]bool
	if env.Apps != nil {
		if a, err := env.Apps(); err == nil {
			apps = a
		}
	}
	if env.VMs != nil {
		if v, err := env.VMs(); err == nil {
			vms = v
		}
	}
	for i := range j.Hooks {
		h := &j.Hooks[i]
		p := fmt.Sprintf("hooks[%d]", i)
		if h.Phase != HookPre && h.Phase != HookPost {
			fe.add(p+".phase", FieldInvalid)
		}
		if h.FailPolicy == "" {
			h.FailPolicy = FailAbort
		} else if h.FailPolicy != FailAbort && h.FailPolicy != FailContinue {
			fe.add(p+".fail_policy", FieldInvalid)
		}
		switch h.Action {
		case HookStopApps, HookStartApps:
			h.VM = ""
			if h.AppMode == "" {
				h.AppMode = AppModeTogether
			}
			if h.AppMode != AppModeTogether && h.AppMode != AppModeOneAtATime {
				fe.add(p+".app_mode", FieldInvalid)
			}
			if h.AppMode == AppModeOneAtATime && (h.Action != HookStopApps || h.Phase != HookPre || !oneAtATimeApplies(*j, *h)) {
				// One at a time splits an archive's sources, one app each.
				fe.add(p+".app_mode", FieldInvalid)
			}
			if len(h.Apps) == 0 {
				fe.add(p+".apps", FieldRequired)
			}
			if len(h.Apps) > maxHookApps {
				fe.add(p+".apps", FieldTooMany)
			}
			h.Apps = dedupe(h.Apps)
			for k, a := range h.Apps {
				if !nameRe.MatchString(a) {
					fe.add(fmt.Sprintf("%s.apps[%d]", p, k), FieldInvalid)
				} else if _, installed := apps[a]; apps != nil && !installed {
					fe.add(fmt.Sprintf("%s.apps[%d]", p, k), FieldUnknownApp)
				}
			}
			if h.TimeoutSec == 0 {
				h.TimeoutSec = DefaultHookTimeoutSec
			}
		case HookShutdownVM, HookStartVM:
			h.Apps, h.AppMode = nil, ""
			switch {
			case h.VM == "":
				fe.add(p+".vm", FieldRequired)
			case !nameRe.MatchString(h.VM):
				fe.add(p+".vm", FieldInvalid)
			case vms != nil && !hasKey(vms, h.VM):
				fe.add(p+".vm", FieldUnknownVM)
			}
			if h.TimeoutSec == 0 {
				h.TimeoutSec = DefaultVMHookTimeoutSec
			}
		default:
			fe.add(p+".action", FieldInvalid)
		}
		if h.TimeoutSec < minHookTimeoutSec || h.TimeoutSec > maxHookTimeoutSec {
			fe.add(p+".timeout_sec", FieldOutOfRange)
		}
	}
}

// oneAtATimeApplies: an archive with several sources, each an app's data
// folder (appdata:<app> preset) whose app the hook stops.
func oneAtATimeApplies(j Job, h Hook) bool {
	if j.Type != TypeArchive || len(j.Sources) < 2 {
		return false
	}
	listed := map[string]bool{}
	for _, a := range h.Apps {
		listed[a] = true
	}
	for _, s := range j.Sources {
		app, ok := strings.CutPrefix(s.Preset, PresetAppData)
		if !ok || !listed[app] {
			return false
		}
	}
	return true
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// defaultStaleHours is max(48, 2 x the longest gap between two runs of
// the most frequent schedule); a job without a schedule uses two weeks.
// The longest gap, not the first: "weekdays at 03:00" normally goes 72 h
// without a run over the weekend, and that must not count as stale.
func defaultStaleHours(j Job) int {
	best, found := 0, false
	for _, t := range j.Triggers {
		if t.Kind != TriggerSchedule {
			continue
		}
		sched, err := CronParser.Parse(t.Cron)
		if err != nil {
			continue
		}
		// Measure from a fixed point, so the default doesn't depend on
		// when the job was saved.
		var longest time.Duration
		prev := sched.Next(staleRef)
		for i := 0; i < staleSamples && !prev.IsZero(); i++ {
			next := sched.Next(prev)
			if next.IsZero() {
				break
			}
			if d := next.Sub(prev); d > longest {
				longest = d
			}
			prev = next
		}
		if longest == 0 {
			continue
		}
		h := int(math.Ceil(2 * longest.Hours()))
		if !found || h < best {
			best, found = h, true
		}
	}
	if !found {
		return 14 * 24
	}
	if best < DefaultStaleMinHours {
		return DefaultStaleMinHours
	}
	// A yearly schedule would default to two years, which the range
	// check refuses: the job saved but no run could start.
	if best > maxStaleHours {
		return maxStaleHours
	}
	return best
}

// staleSamples is how many gaps defaultStaleHours looks at: enough to
// cover a week of a daily-ish schedule and a year of a monthly one.
const staleSamples = 14

func hasKey(m map[string]bool, k string) bool {
	_, ok := m[k]
	return ok
}

// staleRef is a Monday 00:00 UTC.
var staleRef = mustTime("2026-01-05T00:00:00Z")

// sortedKeys returns m's keys sorted (stable field_errors in logs/tests).
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
