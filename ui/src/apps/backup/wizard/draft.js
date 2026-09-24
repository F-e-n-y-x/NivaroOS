// The job wizard's model (spec §12.4). A draft is the REST Job JSON plus
// the few choices the wizard shows differently from how the job stores
// them (one schedule switch instead of a trigger list, per-app "stop
// during the backup" boxes instead of hook pairs). newDraft() and
// draftFromJob() build one, draftToJob() turns it back into the Job the
// API takes, and validateDraft() runs the same rules the server's
// validate.go does (spec §11 "Server-side validation") so errors show
// while typing; the server stays authoritative (POST /validate, 400
// field_errors, both mapped onto the same field ids by serverFieldToUi).
import { validateCron, nextRuns } from '../../../shared/scheduling/cronPatterns'
import { cleanSubPath, isSameOrInside, validFolderName } from '../../../shared/storage/locations'
import { isAppOrVmSource, safeTypes } from '../presets'
import { sourcesLabel } from '../summaries'

export const STEPS = Object.freeze(['start', 'what', 'where', 'when', 'keep', 'review'])
export const JOB_TYPES = Object.freeze(['mirror', 'copy', 'archive'])
export const MAX_ARCHIVE_SOURCES = 16

// Defaults (services/backup/jobs/model.go).
export const DEFAULTS = Object.freeze({
	emptySourcePct: 50,
	deletePct: 10,
	changePct: 30,
	versionsDays: 30,
	keepLast: 8,
	waitMaxMin: 360,
	maxDurationSec: 86400,
	hookTimeoutSec: 300,
	vmHookTimeoutSec: 600,
	retryMax: 3,
	retryBackoffSec: Object.freeze([60, 600, 3600]),
	staleMinHours: 48,
	plugStaleHours: 336
})

export const BACKUPS_FOLDER = 'NivaroOS Backups'

function clone(v) {
	return v === undefined ? undefined : JSON.parse(JSON.stringify(v))
}

// A folder name from a job name: one path segment, no slashes.
export function folderNameFor(name) {
	const n = String(name || '').replace(/[/\\\0]+/g, '-').replace(/\s+/g, ' ').trim().slice(0, 120)
	return validFolderName(n) ? n : 'Backup'
}

export function defaultDestSubPath(name) {
	return `${BACKUPS_FOLDER}/${folderNameFor(name)}`
}

// The job's own folder at the destination: the name the user typed, else
// the source folder's name ("NivaroOS Backups/photos").
export function destFolderSubPath(d) {
	return defaultDestSubPath(d.name.trim() || epShortLabel(d.sources[0]) || '')
}

function epShortLabel(ep) {
	if (!ep) return ''
	const tail = ep.sub_path ? ep.sub_path.split('/').filter(Boolean).pop() : ''
	return tail || ep.label || ep.ref_id || ''
}

// defaultName is "<source> → <destination>".
export function defaultName(draft) {
	const src = epShortLabel(draft.sources[0])
	const extra = draft.sources.length > 1 ? ` +${draft.sources.length - 1}` : ''
	const dst = draft.dest ? draft.dest.label || draft.dest.ref_id : ''
	if (!src && !dst) return ''
	if (!dst) return src + extra
	if (!src) return dst
	return `${src}${extra} → ${dst}`
}

function appOf(ep) {
	const p = (ep && ep.preset) || ''
	return p.startsWith('appdata:') ? p.slice(8) : ''
}

function vmOf(ep) {
	const p = (ep && ep.preset) || ''
	return p.startsWith('vm:') ? p.slice(3) : ''
}

// The apps and VMs the draft's sources hold (from their presets).
export function sourceApps(draft) {
	return [...new Set(draft.sources.map(appOf).filter(Boolean))]
}

export function sourceVms(draft) {
	return [...new Set(draft.sources.map(vmOf).filter(Boolean))]
}

// newDraft for a new job. settings is GET /settings (AppSettings) or null.
export function newDraft({ settings = null, preset = null, locations = [], sourceEndpoint = null, destEndpoint = null } = {}) {
	const s = settings || {}
	const type = (preset && preset.type) || 'copy'
	const sources = sourceEndpoint ? [clone(sourceEndpoint)] : preset ? clone(preset.source(locations)) : []
	const presetTriggers = preset ? preset.triggers() : [{ kind: 'schedule', cron: '0 3 * * *', catch_up: true }]
	const sched = presetTriggers.find(t => t.kind === 'schedule')
	const plug = presetTriggers.find(t => t.kind === 'volume_mounted')
	const catchUp = s.catch_up_default !== undefined ? !!s.catch_up_default : true
	const d = {
		id: '',
		revision: 0,
		presetId: preset ? preset.id : '',
		name: '',
		nameTouched: false,
		type,
		typeTouched: false,
		enabled: true,
		sources,
		dest: destEndpoint ? clone(destEndpoint) : null,
		destPathTouched: false,
		scheduleOn: !!sched,
		cron: sched ? sched.cron : '0 3 * * *',
		plugOn: !!plug,
		plugGapHours: plug ? plug.min_gap_hours || 0 : 24,
		plugVolume: null,
		catchUp,
		plugFirst: !!plug,
		extraTriggers: [],
		destAvailable: true,
		whenUnmet: '',
		waitMaxMin: DEFAULTS.waitMaxMin,
		windowOn: false,
		windowStart: '22:00',
		windowEnd: '07:00',
		retryMax: DEFAULTS.retryMax,
		retryBackoffSec: DEFAULTS.retryBackoffSec.slice(),
		versionsDays: s.default_versions_days !== undefined ? s.default_versions_days : DEFAULTS.versionsDays,
		keepLast: DEFAULTS.keepLast,
		deletePct: s.default_delete_pct || DEFAULTS.deletePct,
		changePct: s.default_change_pct || DEFAULTS.changePct,
		emptySourcePct: DEFAULTS.emptySourcePct,
		allowEmptySource: false,
		previewFirst: type === 'mirror',
		previewTouched: false,
		verify: false,
		lowPriority: true,
		maxDurationHours: DEFAULTS.maxDurationSec / 3600,
		copyEmptyDirs: false,
		preserveMeta: undefined,
		excludePresets: ['caches', 'trash', 'temp'],
		exclude: [],
		include: [],
		maxSizeOn: false,
		maxSizeGb: 4,
		stopApps: {},
		appMode: 'together',
		shutdownVms: {},
		extraHooks: [],
		notifyOnSuccess: false,
		staleAfterHours: 0,
		runNow: true,
		needsAttention: '',
		migratedFrom: null
	}
	syncHookChoices(d)
	if (d.dest && !d.destPathTouched && d.dest.sub_path === undefined) d.dest.sub_path = ''
	return d
}

// syncHookChoices adds a "stop" box (on by default: app data copied while
// the app runs can be corrupt) for every app or VM the sources hold and
// drops boxes of apps no longer backed up, unless an existing hook still
// names them.
export function syncHookChoices(d) {
	const apps = sourceApps(d)
	const vms = sourceVms(d)
	const keepApps = new Set(d.extraHooks.flatMap(h => h.apps || []))
	const stop = {}
	for (const a of apps) stop[a] = d.stopApps[a] !== undefined ? d.stopApps[a] : true
	for (const a of Object.keys(d.stopApps)) if (!(a in stop) && d.stopAppsFromJob && d.stopAppsFromJob.includes(a)) stop[a] = d.stopApps[a]
	for (const a of keepApps) if (!(a in stop)) stop[a] = true
	d.stopApps = stop
	const vm = {}
	for (const v of vms) vm[v] = d.shutdownVms[v] !== undefined ? d.shutdownVms[v] : true
	for (const v of Object.keys(d.shutdownVms)) if (!(v in vm) && d.shutdownVmsFromJob && d.shutdownVmsFromJob.includes(v)) vm[v] = d.shutdownVms[v]
	d.shutdownVms = vm
	return d
}

// draftFromJob reads a stored job (GET /jobs/:id) into a draft.
export function draftFromJob(job, { settings = null } = {}) {
	const d = newDraft({ settings })
	const triggers = job.triggers || []
	const sched = triggers.find(t => t.kind === 'schedule')
	const plug = triggers.find(t => t.kind === 'volume_mounted')
	const c = job.conditions || {}
	const o = job.options || {}
	const g = job.guards || {}
	const r = job.retention || {}
	const f = job.filters || {}
	Object.assign(d, {
		id: job.id || '',
		revision: job.revision || 0,
		presetId: '',
		name: job.name || '',
		nameTouched: true,
		type: JOB_TYPES.includes(job.type) ? job.type : 'copy',
		typeTouched: true,
		enabled: job.enabled !== false,
		sources: clone(job.sources || []),
		dest: job.dest ? clone(job.dest) : null,
		destPathTouched: true,
		scheduleOn: !!sched,
		cron: sched ? sched.cron : '0 3 * * *',
		plugOn: !!plug,
		plugGapHours: plug ? plug.min_gap_hours || 0 : 24,
		plugVolume: plug && plug.volume ? clone(plug.volume) : null,
		catchUp: triggers.length ? triggers.some(t => t.catch_up) : d.catchUp,
		plugFirst: !!(plug && sched && triggers.indexOf(plug) < triggers.indexOf(sched)),
		extraTriggers: clone(triggers.filter(t => t !== sched && t !== plug)),
		destAvailable: job.type === 'mirror' ? true : c.dest_available !== false,
		whenUnmet: c.when_unmet || '',
		waitMaxMin: c.wait_max_min || DEFAULTS.waitMaxMin,
		windowOn: !!(c.window && c.window.start && c.window.end),
		windowStart: (c.window && c.window.start) || '22:00',
		windowEnd: (c.window && c.window.end) || '07:00',
		retryMax: job.retry && Number.isInteger(job.retry.max) ? job.retry.max : DEFAULTS.retryMax,
		retryBackoffSec: job.retry && Array.isArray(job.retry.backoff_sec) && job.retry.backoff_sec.length ? job.retry.backoff_sec.slice() : DEFAULTS.retryBackoffSec.slice(),
		versionsDays: r.versions_days !== undefined ? r.versions_days : job.type === 'mirror' ? 0 : d.versionsDays,
		keepLast: r.keep_last || DEFAULTS.keepLast,
		deletePct: g.delete_pct || d.deletePct,
		changePct: g.change_pct || d.changePct,
		emptySourcePct: g.empty_source_pct || DEFAULTS.emptySourcePct,
		allowEmptySource: !!g.allow_empty_source,
		previewFirst: !!o.preview_first,
		previewTouched: true,
		verify: !!o.verify,
		lowPriority: o.low_priority !== false,
		maxDurationHours: o.max_duration_sec ? Math.max(1, Math.round(o.max_duration_sec / 3600)) : DEFAULTS.maxDurationSec / 3600,
		copyEmptyDirs: !!o.copy_empty_dirs,
		preserveMeta: o.preserve_meta,
		excludePresets: (f.exclude_presets || []).slice(),
		exclude: (f.exclude || []).slice(),
		include: (f.include || []).slice(),
		maxSizeOn: !!f.max_size_bytes,
		maxSizeGb: f.max_size_bytes ? Math.round((f.max_size_bytes / 1e9) * 10) / 10 : 4,
		notifyOnSuccess: !!(job.notify && job.notify.on_success),
		staleAfterHours: (job.notify && job.notify.stale_after_hours) || 0,
		runNow: false,
		needsAttention: job.needs_attention || '',
		migratedFrom: job.migrated_from === undefined ? null : job.migrated_from
	})
	// A mirror stored with versions_days 0 keeps versions forever; any
	// other type falls back to the default if switched to Mirror.
	if (job.type !== 'mirror' && r.versions_days === undefined) d.versionsDays = settings && settings.default_versions_days !== undefined ? settings.default_versions_days : DEFAULTS.versionsDays

	// Hooks: the stop/start pairs the wizard shows as boxes; anything else
	// is kept as it is.
	const stopApps = {}
	const vms = {}
	const extra = []
	for (const h of job.hooks || []) {
		if (h.phase === 'pre' && h.action === 'stop_apps') {
			for (const a of h.apps || []) stopApps[a] = true
			if (h.app_mode) d.appMode = h.app_mode
		} else if (h.phase === 'pre' && h.action === 'shutdown_vm' && h.vm) {
			vms[h.vm] = true
		} else if (h.phase === 'post' && (h.action === 'start_apps' || h.action === 'start_vm')) {
			// Rebuilt from the pre hooks.
		} else {
			extra.push(clone(h))
		}
	}
	d.hookTimeouts = {}
	for (const h of job.hooks || []) {
		if (h.phase === 'pre' && h.action === 'stop_apps') d.hookTimeouts.apps = { timeout_sec: h.timeout_sec, fail_policy: h.fail_policy }
		if (h.phase === 'pre' && h.action === 'shutdown_vm') d.hookTimeouts['vm:' + h.vm] = { timeout_sec: h.timeout_sec, fail_policy: h.fail_policy }
	}
	for (const a of sourceApps(d)) if (!(a in stopApps)) stopApps[a] = false
	for (const v of sourceVms(d)) if (!(v in vms)) vms[v] = false
	d.stopApps = stopApps
	d.stopAppsFromJob = Object.keys(stopApps)
	d.shutdownVms = vms
	d.shutdownVmsFromJob = Object.keys(vms)
	d.extraHooks = extra
	return d
}

// effectiveWhenUnmet: "wait" for scheduled jobs, "skip" otherwise, unless
// the user chose.
export function effectiveWhenUnmet(d) {
	if (d.whenUnmet) return d.whenUnmet
	return d.scheduleOn ? 'wait' : 'skip'
}

// staleHours: max(48, 2 x schedule interval); two weeks for plug-in
// jobs; 0 (no staleness warning) for jobs that only run by hand.
export function staleHours(d, now = new Date()) {
	if (d.scheduleOn && !validateCron(d.cron)) {
		const runs = nextRuns(d.cron, now, 2)
		if (runs.length === 2) {
			const interval = (runs[1] - runs[0]) / 3600000
			return Math.max(DEFAULTS.staleMinHours, Math.ceil(interval * 2))
		}
		return DEFAULTS.staleMinHours
	}
	return d.plugOn ? DEFAULTS.plugStaleHours : 0
}

function hooksOf(d) {
	const hooks = []
	const apps = Object.keys(d.stopApps).filter(a => d.stopApps[a])
	const t = d.hookTimeouts || {}
	if (apps.length) {
		const ta = t.apps || {}
		hooks.push({ phase: 'pre', action: 'stop_apps', apps, app_mode: d.appMode === 'one_at_a_time' ? 'one_at_a_time' : 'together', timeout_sec: ta.timeout_sec || DEFAULTS.hookTimeoutSec, fail_policy: ta.fail_policy || 'abort' })
		hooks.push({ phase: 'post', action: 'start_apps', apps: apps.slice(), app_mode: d.appMode === 'one_at_a_time' ? 'one_at_a_time' : 'together', timeout_sec: ta.timeout_sec || DEFAULTS.hookTimeoutSec, fail_policy: 'continue' })
	}
	for (const vm of Object.keys(d.shutdownVms).filter(v => d.shutdownVms[v])) {
		const tv = t['vm:' + vm] || {}
		hooks.push({ phase: 'pre', action: 'shutdown_vm', vm, timeout_sec: tv.timeout_sec || DEFAULTS.vmHookTimeoutSec, fail_policy: tv.fail_policy || 'abort' })
		hooks.push({ phase: 'post', action: 'start_vm', vm, timeout_sec: tv.timeout_sec || DEFAULTS.vmHookTimeoutSec, fail_policy: 'continue' })
	}
	return hooks.concat(clone(d.extraHooks))
}

// draftToJob is the Job JSON for POST /jobs, PUT /jobs/:id and /validate.
export function draftToJob(d, { now = new Date() } = {}) {
	// Stored order is kept (server field errors name triggers by index).
	const triggers = []
	const schedule = d.scheduleOn ? { kind: 'schedule', cron: d.cron, catch_up: !!d.catchUp } : null
	let plug = null
	if (d.plugOn) {
		plug = { kind: 'volume_mounted', catch_up: !!d.catchUp }
		if (d.plugVolume) plug.volume = clone(d.plugVolume)
		if (Number(d.plugGapHours) > 0) plug.min_gap_hours = Math.round(Number(d.plugGapHours))
	}
	for (const t of d.plugFirst ? [plug, schedule] : [schedule, plug]) if (t) triggers.push(t)
	for (const t of d.extraTriggers) triggers.push({ ...clone(t), catch_up: !!d.catchUp })

	const conditions = { dest_available: d.type === 'mirror' ? true : !!d.destAvailable, when_unmet: effectiveWhenUnmet(d) }
	if (conditions.when_unmet === 'wait') conditions.wait_max_min = Math.round(Number(d.waitMaxMin) || DEFAULTS.waitMaxMin)
	if (d.windowOn) conditions.window = { start: d.windowStart, end: d.windowEnd }

	const filters = { exclude_presets: d.excludePresets.slice(), exclude: d.exclude.map(s => s.trim()).filter(Boolean) }
	const include = d.include.map(s => s.trim()).filter(Boolean)
	if (include.length) filters.include = include
	if (d.maxSizeOn && Number(d.maxSizeGb) > 0) filters.max_size_bytes = Math.round(Number(d.maxSizeGb) * 1e9)

	const options = {
		verify: !!d.verify,
		preview_first: d.type === 'archive' ? false : !!d.previewFirst,
		low_priority: !!d.lowPriority,
		max_duration_sec: Math.round(Number(d.maxDurationHours) * 3600) || DEFAULTS.maxDurationSec,
		copy_empty_dirs: !!d.copyEmptyDirs
	}
	if (d.preserveMeta !== undefined && d.preserveMeta !== null) options.preserve_meta = !!d.preserveMeta

	const retention = {}
	if (d.type === 'mirror') retention.versions_days = Math.round(Number(d.versionsDays) || 0)
	if (d.type === 'archive') retention.keep_last = Math.round(Number(d.keepLast) || DEFAULTS.keepLast)

	const name = d.name.trim() || defaultName(d)
	const job = {
		name,
		type: d.type,
		enabled: !!d.enabled,
		sources: clone(d.sources),
		dest: d.dest ? clone(d.dest) : { kind: '', ref_id: '', sub_path: '', label: '' },
		triggers,
		conditions,
		filters,
		options,
		guards: {
			empty_source_pct: Math.round(Number(d.emptySourcePct)),
			delete_pct: Math.round(Number(d.deletePct)),
			change_pct: Math.round(Number(d.changePct)),
			allow_empty_source: !!d.allowEmptySource
		},
		retention,
		hooks: hooksOf(d),
		retry: { max: Math.round(Number(d.retryMax) || 0), backoff_sec: d.retryBackoffSec.slice() },
		notify: { on_success: !!d.notifyOnSuccess, on_failure: true, stale_after_hours: d.staleAfterHours || staleHours(d, now) },
		needs_attention: d.needsAttention || '',
		migrated_from: d.migratedFrom === undefined ? null : d.migratedFrom
	}
	if (d.id) {
		job.id = d.id
		job.revision = d.revision
	}
	return job
}

// ---------------------------------------------------------------------
// Validation. Field ids are the wizard's; each belongs to one step.

export const FIELD_STEP = Object.freeze({
	type: 'what',
	sources: 'what',
	hooks: 'what',
	'filters.exclude': 'what',
	'filters.include': 'review',
	'filters.max_size_bytes': 'what',
	dest: 'where',
	'dest.sub_path': 'where',
	'dest.type': 'where',
	cron: 'when',
	'plug.min_gap_hours': 'when',
	'plug.volume': 'when',
	'window.start': 'when',
	'window.end': 'when',
	wait_max_min: 'when',
	'retry.max': 'when',
	'retention.versions_days': 'keep',
	'retention.keep_last': 'keep',
	'guards.delete_pct': 'keep',
	'guards.change_pct': 'keep',
	'guards.empty_source_pct': 'keep',
	name: 'review',
	'options.max_duration_sec': 'review'
})

const TIME_RE = /^([01]\d|2[0-3]):[0-5]\d$/

function intIn(v, min, max) {
	const n = Number(v)
	return Number.isInteger(n) && n >= min && n <= max
}

function sameLocation(a, b) {
	return !!(a && b && a.kind === b.kind && a.ref_id === b.ref_id)
}

// validateDraft returns { field: code } (codes are FIELD_CODES or error
// codes, rendered with fieldErrorKey()). `plugDrive` is the endpoint a
// plug-in trigger would watch (plugTarget()).
export function validateDraft(d) {
	const e = {}
	if (!JOB_TYPES.includes(d.type)) e.type = 'invalid'

	const n = d.sources.length
	if (!n) e.sources = 'required'
	else if (d.type !== 'archive' && n > 1) e.sources = 'too_many'
	else if (n > MAX_ARCHIVE_SOURCES) e.sources = 'too_many'
	else if (d.sources.some(s => cleanSubPath(s.sub_path).error)) e.sources = 'path_not_allowed'

	for (const p of d.exclude) if (String(p).includes('\0')) e['filters.exclude'] = 'invalid_filter'
	for (const p of d.include) if (String(p).includes('\0')) e['filters.include'] = 'invalid_filter'
	if (d.maxSizeOn && !(Number(d.maxSizeGb) > 0)) e['filters.max_size_bytes'] = 'out_of_range'

	if (!d.dest || !d.dest.kind || !d.dest.ref_id) e.dest = 'required'
	else {
		const sp = cleanSubPath(d.dest.sub_path)
		if (sp.error) e['dest.sub_path'] = 'path_not_allowed'
		else if (d.sources.some(s => sameLocation(s, d.dest) && (isSameOrInside(s.sub_path, sp.path) || isSameOrInside(sp.path, s.sub_path)))) e.dest = 'dest_inside_source'
		if (!safeTypes(d.sources, d.dest.kind).includes(d.type)) e['dest.type'] = 'appdata_cloud_needs_archive'
	}

	if (d.scheduleOn && validateCron(d.cron)) e.cron = 'invalid_cron'
	if (d.plugOn) {
		if (!intIn(d.plugGapHours, 0, 720)) e['plug.min_gap_hours'] = 'out_of_range'
		const drive = d.plugVolume || (d.dest && ['usb', 'volume'].includes(d.dest.kind) ? d.dest : null) || d.sources.find(s => ['usb', 'volume'].includes(s.kind))
		if (!drive) e['plug.volume'] = 'not_resolvable'
	}
	if (d.windowOn) {
		if (!TIME_RE.test(d.windowStart)) e['window.start'] = 'invalid_time'
		if (!TIME_RE.test(d.windowEnd)) e['window.end'] = 'invalid_time'
		else if (d.windowStart === d.windowEnd) e['window.end'] = 'invalid'
	}
	if (effectiveWhenUnmet(d) === 'wait' && !intIn(d.waitMaxMin, 5, 10080)) e.wait_max_min = 'out_of_range'
	if (!intIn(d.retryMax, 0, 10)) e['retry.max'] = 'out_of_range'

	if (d.type === 'mirror' && !intIn(d.versionsDays, 0, 3650)) e['retention.versions_days'] = 'out_of_range'
	if (d.type === 'archive' && !intIn(d.keepLast, 1, 1000)) e['retention.keep_last'] = 'out_of_range'
	if (!intIn(d.deletePct, 1, 100)) e['guards.delete_pct'] = 'out_of_range'
	if (!intIn(d.changePct, 1, 100)) e['guards.change_pct'] = 'out_of_range'
	if (!intIn(d.emptySourcePct, 1, 100)) e['guards.empty_source_pct'] = 'out_of_range'

	if (!(d.name.trim() || defaultName(d))) e.name = 'required'
	else if (d.name.trim().length > 200) e.name = 'out_of_range'
	if (!intIn(d.maxDurationHours, 1, 720)) e['options.max_duration_sec'] = 'out_of_range'
	return e
}

// errorsForStep keeps the errors of one step, in FIELD_STEP order.
export function errorsForStep(errors, step) {
	const out = {}
	for (const f of Object.keys(FIELD_STEP)) if (FIELD_STEP[f] === step && errors[f]) out[f] = errors[f]
	for (const f of Object.keys(errors)) if (!FIELD_STEP[f] && step === 'review') out[f] = errors[f]
	return out
}

// firstStepWithErrors is where Review sends the user to fix things.
export function firstStepWithErrors(errors) {
	for (const s of STEPS) if (Object.keys(errorsForStep(errors, s)).length) return s
	return null
}

// serverFieldToUi maps a server field path ("triggers[1].cron",
// "dest.sub_path", "sources[0].sub_path", "filters.exclude[2]") onto the
// wizard's field ids, using the job the draft produced to tell which
// trigger is which.
export function serverFieldToUi(path, job) {
	const p = String(path || '')
	const trig = /^triggers\[(\d+)\](?:\.(\w+))?/.exec(p)
	if (trig) {
		const t = job && job.triggers ? job.triggers[+trig[1]] : null
		if (t && t.kind === 'volume_mounted') return trig[2] === 'min_gap_hours' ? 'plug.min_gap_hours' : 'plug.volume'
		return 'cron'
	}
	if (/^sources/.test(p)) return 'sources'
	if (/^hooks/.test(p)) return 'hooks'
	if (/^filters\.exclude/.test(p)) return 'filters.exclude'
	if (/^filters\.include/.test(p)) return 'filters.include'
	if (p === 'type') return 'dest.type'
	if (p === 'dest.sub_path') return 'dest.sub_path'
	if (/^dest/.test(p)) return 'dest'
	if (/^conditions\.window\.start/.test(p)) return 'window.start'
	if (/^conditions\.window/.test(p)) return 'window.end'
	if (/^conditions\.wait_max_min/.test(p)) return 'wait_max_min'
	if (/^conditions/.test(p)) return 'dest'
	if (/^retry/.test(p)) return 'retry.max'
	if (/^options\.max_duration_sec/.test(p)) return 'options.max_duration_sec'
	if (FIELD_STEP[p]) return p
	return p
}

export function mapServerFieldErrors(fieldErrors, job) {
	const out = {}
	for (const [path, code] of Object.entries(fieldErrors || {})) {
		const f = serverFieldToUi(path, job)
		if (!out[f]) out[f] = code
	}
	return out
}

// Checks from POST /validate that block the wizard (spec §12.4 "Blocking
// checks"); the rest are shown as information.
export const BLOCKING_CHECKS = Object.freeze(['not_inside', 'allowed_roots', 'type_for_dest'])

export function blockingCheckErrors(result) {
	const out = {}
	for (const c of (result && result.checks) || []) {
		if (c.status !== 'fail' || !BLOCKING_CHECKS.includes(c.id)) continue
		if (c.id === 'type_for_dest') out['dest.type'] = c.code || 'appdata_cloud_needs_archive'
		else if (c.id === 'not_inside') out.dest = c.code || 'dest_inside_source'
		else out.dest = c.code || 'path_not_allowed'
	}
	return out
}

// describeSources for the name default and summaries.
export { sourcesLabel }

// hasAppOrVmSource: shows the "Keep apps consistent" block.
export function hasAppOrVmSource(d) {
	return d.sources.some(isAppOrVmSource) || Object.keys(d.stopApps).length > 0 || Object.keys(d.shutdownVms).length > 0
}
