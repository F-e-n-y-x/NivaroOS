// Pure state logic of the Backup & Sync app shell (spec §12.2, §12.3,
// §12.8): what the Overview header says, which jobs need attention and
// why, what's running and coming up, job filters and sorting, activity
// grouping and live progress. No Vue and no I/O, so it's unit-tested in
// __tests__/state.spec.js against the WP-0 fixtures.
import { errorInfo } from './errorCodes'
import { errorCodeFromMessage } from './messages'

// Job health, worst first (jobs/apitypes.go Health*).
export const HEALTH_ORDER = Object.freeze(['problem', 'offline', 'warning', 'ok', 'disabled'])

// Status is always icon + text + colour (spec §13). Tones map to the
// --status-<tone>-{bg,fg} tokens.
export const HEALTH_VIEW = Object.freeze({
	problem: { tone: 'danger', icon: 'alert-octagon-outline' },
	offline: { tone: 'warn', icon: 'power-plug-off-outline' },
	warning: { tone: 'warn', icon: 'alert-outline' },
	ok: { tone: 'ok', icon: 'check-circle-outline' },
	disabled: { tone: 'muted', icon: 'pause-circle-outline' }
})

export const STATUS_VIEW = Object.freeze({
	queued: { tone: 'info', icon: 'clock-outline' },
	running: { tone: 'info', icon: 'progress-upload' },
	waiting_user: { tone: 'warn', icon: 'hand-back-right-outline' },
	success: { tone: 'ok', icon: 'check-circle-outline' },
	partial: { tone: 'warn', icon: 'alert-outline' },
	failed: { tone: 'danger', icon: 'close-circle-outline' },
	cancelled: { tone: 'muted', icon: 'cancel' },
	skipped: { tone: 'muted', icon: 'debug-step-over' },
	interrupted: { tone: 'warn', icon: 'restart-alert' }
})

export const TYPE_ICON = Object.freeze({ copy: 'content-copy', mirror: 'mirror', archive: 'archive-outline' })

const ACTIVE = ['queued', 'running', 'waiting_user']
const FINAL = ['success', 'partial', 'failed', 'cancelled', 'skipped', 'interrupted']

export function isActiveStatus(s) {
	return ACTIVE.includes(s)
}
export function isFinalStatus(s) {
	return FINAL.includes(s)
}

function time(v) {
	const n = v ? Date.parse(v) : NaN
	return isNaN(n) ? null : n
}

// Overview header (§12.2): one of empty | problem | attention | ok.
export function overallState(jobs) {
	const list = jobs || []
	if (!list.length) return { state: 'empty', jobs: 0, attention: 0, lastSuccess: null }
	const items = attentionItems(list)
	let lastSuccess = null
	for (const j of list) {
		const r = j.last_run
		if (r && (r.status === 'success' || r.status === 'partial') && r.kind !== 'restore') {
			const at = time(r.ended_at)
			if (at !== null && (lastSuccess === null || at > time(lastSuccess))) lastSuccess = r.ended_at
		}
	}
	const problem = items.some(i => i.severity === 'problem')
	return { state: problem ? 'problem' : items.length ? 'attention' : 'ok', jobs: list.length, attention: items.length, lastSuccess }
}

// attentionItems lists the jobs that need the user (§12.2), worst first.
// Each item is { job, reason, code, severity, actions, runId }:
//   reason  i18n suffix under backup.attention.* (or 'error' for a code)
//   code    an error_code whose backup.err.<code>.* texts explain it
//   actions one to three fixes (errorCodes.js actions)
export function attentionItems(jobs) {
	const out = []
	for (const job of jobs || []) {
		const item = attentionFor(job)
		if (item) out.push(item)
	}
	// A run waiting for a decision holds up that job until answered.
	const rank = i => (i.reason === 'waiting' ? 0 : i.severity === 'problem' ? 1 : 2)
	return out.sort((a, b) => rank(a) - rank(b) || String(a.job.name).localeCompare(String(b.job.name)))
}

export function attentionFor(job) {
	if (!job) return null
	const active = job.active_run
	const last = job.last_run
	if (active && active.status === 'waiting_user') {
		return { job, reason: 'waiting', code: '', severity: 'problem', actions: ['review'], runId: active.id }
	}
	if (job.needs_attention === 'dest_changed') {
		return { job, reason: 'error', code: 'dest_marker_mismatch', severity: 'problem', actions: errorInfo('dest_marker_mismatch').actions.slice(0, 3), runId: last ? last.id : '' }
	}
	if (job.needs_attention === 'migrated_unresolved') {
		return { job, reason: 'migrated_unresolved', code: '', severity: 'warning', actions: ['edit_job'], runId: '' }
	}
	if (job.health === 'problem') {
		const code = (last && errorCodeFromMessage(last.summary)) || 'internal'
		const actions = errorInfo(code).actions.slice(0, 2)
		if (last && !actions.includes('view_log')) actions.push('view_log')
		return { job, reason: 'error', code, severity: 'problem', actions: actions.slice(0, 3), runId: last ? last.id : '' }
	}
	if (job.health === 'offline') {
		return { job, reason: 'error', code: 'dest_offline', severity: 'warning', actions: errorInfo('dest_offline').actions.slice(0, 3), runId: last ? last.id : '' }
	}
	if (job.health === 'warning') {
		if (last && last.status === 'partial') return { job, reason: 'partial', code: '', severity: 'warning', actions: ['view_log', 'retry'], runId: last.id }
		return { job, reason: 'stale', code: '', severity: 'warning', actions: ['retry'], runId: last ? last.id : '' }
	}
	return null
}

// runningItems are the jobs with a run in progress, running ones first.
export function runningItems(jobs) {
	const rank = { running: 0, waiting_user: 1, queued: 2 }
	return (jobs || []).filter(j => j.active_run && isActiveStatus(j.active_run.status)).sort((a, b) => rank[a.active_run.status] - rank[b.active_run.status])
}

// upcomingItems (§12.2 "Coming up"): the next scheduled runs, soonest
// first, then the enabled jobs that run when their drive is plugged in.
export function upcomingItems(jobs, limit = 5) {
	const list = (jobs || []).filter(j => j.enabled)
	const scheduled = list
		.filter(j => time(j.next_run) !== null && !(j.active_run && isActiveStatus(j.active_run.status)))
		.sort((a, b) => time(a.next_run) - time(b.next_run))
		.slice(0, limit)
		.map(job => ({ job, kind: 'schedule', at: job.next_run }))
	const plug = list.filter(j => (j.triggers || []).some(t => t.kind === 'volume_mounted')).map(job => ({ job, kind: 'plug', at: null }))
	return scheduled.concat(plug.slice(0, 3))
}

// Filters of the Jobs list (§12.3).
export const JOB_FILTERS = Object.freeze(['all', 'problems', 'running', 'disabled', 'imported'])
export const JOB_SORTS = Object.freeze(['next_run', 'name', 'last_run', 'health'])

function jobText(job) {
	const parts = [job.name, job.dest && job.dest.label]
	for (const s of job.sources || []) parts.push(s.label, s.sub_path)
	return parts.filter(Boolean).join(' ').toLowerCase()
}

export function filterJobs(jobs, { filter = 'all', query = '' } = {}) {
	const q = String(query || '').trim().toLowerCase()
	return (jobs || []).filter(job => {
		if (q && !jobText(job).includes(q)) return false
		switch (filter) {
			case 'problems':
				return !!attentionFor(job)
			case 'running':
				return !!(job.active_run && isActiveStatus(job.active_run.status))
			case 'disabled':
				return !job.enabled
			case 'imported':
				return !!job.migrated_from && job.needs_attention === 'imported'
			default:
				return true
		}
	})
}

export function sortJobs(jobs, sort = 'next_run') {
	const byName = (a, b) => String(a.name).localeCompare(String(b.name))
	const or = (v, d) => (v === null ? d : v)
	const cmp = {
		name: byName,
		// No next run (manual, plug-in only, paused) sorts last.
		next_run: (a, b) => or(time(a.next_run), Infinity) - or(time(b.next_run), Infinity) || byName(a, b),
		// Most recent first; never run sorts last.
		last_run: (a, b) => or(time(b.last_run && b.last_run.ended_at), -Infinity) - or(time(a.last_run && a.last_run.ended_at), -Infinity) || byName(a, b),
		health: (a, b) => HEALTH_ORDER.indexOf(a.health) - HEALTH_ORDER.indexOf(b.health) || byName(a, b)
	}[sort] || byName
	return (jobs || []).slice().sort(cmp)
}

// jobTriggers returns what starts a job, for its card:
// [{ kind: 'schedule', cron } | { kind: 'volume_mounted' }], empty when the
// job only runs by hand.
export function jobTriggers(job) {
	return ((job && job.triggers) || []).filter(t => t.kind === 'schedule' || t.kind === 'volume_mounted')
}

// distinctCrons of a job list, for one /cron/preview per expression.
export function distinctCrons(jobs) {
	const set = new Set()
	for (const j of jobs || []) for (const t of jobTriggers(j)) if (t.kind === 'schedule' && t.cron) set.add(t.cron)
	return [...set]
}

// retentionMessage says what the job keeps, as {key, args}, or null.
export function retentionMessage(job) {
	const r = (job && job.retention) || {}
	if (job && job.type === 'mirror') return r.versions_days ? { key: 'backup.keep.recycle_days', args: { days: r.versions_days } } : { key: 'backup.keep.recycle_forever' }
	if (job && job.type === 'archive') return { key: 'backup.keep.last_archives', args: { keep: r.keep_last || 8 } }
	return null
}

// Live progress (§10.3). Socket properties are all strings.
const LIVE_FIELDS = ['bytes', 'total_bytes', 'files', 'total_files', 'speed_bps', 'eta_sec', 'errors']

export function liveFromEvent(props) {
	const p = props || {}
	const live = { current_file: p.current_file || '' }
	for (const f of LIVE_FIELDS) {
		const n = p[f] === undefined || p[f] === '' ? null : Number(p[f])
		live[f] = n === null || isNaN(n) ? null : n
	}
	if (live.eta_sec !== null && live.eta_sec < 0) live.eta_sec = null
	return live
}

// runProgress -> { ratio (0..1) | null when unknown, percent text-ready }.
export function runProgress(live) {
	if (!live) return { ratio: null, percent: null }
	let ratio = null
	if (live.total_bytes > 0) ratio = live.bytes / live.total_bytes
	else if (live.total_files > 0) ratio = live.files / live.total_files
	if (ratio === null || isNaN(ratio)) return { ratio: null, percent: null }
	ratio = Math.max(0, Math.min(1, ratio))
	return { ratio, percent: Math.floor(ratio * 100) }
}

// groupRunsByDay groups a newest-first run list into days (server time).
export function groupRunsByDay(runs, fmt) {
	const groups = []
	let cur = null
	for (const run of runs || []) {
		const at = run.ended_at || run.started_at || run.queued_at
		const day = fmt ? fmt.dayKey(at) : String(at || '').slice(0, 10)
		if (!cur || cur.day !== day) {
			cur = { day, at, runs: [] }
			groups.push(cur)
		}
		cur.runs.push(run)
	}
	return groups
}

// Activity filters (§12.8), persisted per browser.
export const ACTIVITY_RESULTS = Object.freeze(['all', 'ok', 'problems', 'skipped', 'active'])
export const ACTIVITY_KINDS = Object.freeze(['all', 'backup', 'restore', 'preview', 'verify', 'prune'])
export const ACTIVITY_RANGES = Object.freeze(['all', '1d', '7d', '30d'])
export const DEFAULT_ACTIVITY_FILTERS = Object.freeze({ jobId: '', result: 'all', kind: 'all', range: '7d' })

const RESULT_STATUSES = {
	ok: ['success'],
	problems: ['failed', 'partial', 'interrupted', 'waiting_user'],
	skipped: ['skipped', 'cancelled'],
	active: ['queued', 'running', 'waiting_user']
}

// normalizeActivityFilters accepts whatever localStorage held.
export function normalizeActivityFilters(raw) {
	const f = raw && typeof raw === 'object' ? raw : {}
	return {
		jobId: typeof f.jobId === 'string' ? f.jobId : '',
		result: ACTIVITY_RESULTS.includes(f.result) ? f.result : DEFAULT_ACTIVITY_FILTERS.result,
		kind: ACTIVITY_KINDS.includes(f.kind) ? f.kind : DEFAULT_ACTIVITY_FILTERS.kind,
		range: ACTIVITY_RANGES.includes(f.range) ? f.range : DEFAULT_ACTIVITY_FILTERS.range
	}
}

// activityQuery is the server side of the filters (GET /runs params).
export function activityQuery(filters) {
	const f = normalizeActivityFilters(filters)
	const q = {}
	if (f.jobId) q.jobId = f.jobId
	if (f.kind !== 'all') q.kind = f.kind
	if (f.result !== 'all') q.status = RESULT_STATUSES[f.result]
	return q
}

// runInRange is the date part, applied client-side (the API pages by id).
export function runInRange(run, range, now = Date.now()) {
	const days = { '1d': 1, '7d': 7, '30d': 30 }[range]
	if (!days) return true
	const at = time(run.ended_at || run.started_at || run.queued_at)
	return at === null || at >= now - days * 86400000
}

// localPath is where an endpoint lives on this server, when it's a local
// disk (for "Open folder in Files"); '' for cloud and network shares.
export function localPath(endpoint, locations) {
	if (!endpoint || !['volume', 'usb', 'merge'].includes(endpoint.kind)) return ''
	const loc = (locations || []).find(l => l.kind === endpoint.kind && l.ref_id === endpoint.ref_id)
	if (!loc || !loc.mount_point || loc.online === false) return ''
	const base = loc.mount_point.replace(/\/+$/, '')
	const sub = String(endpoint.sub_path || '').replace(/^\/+|\/+$/g, '')
	return sub ? `${base}/${sub}` : base || '/'
}

// joinPath joins relative path segments ('' is the root).
export function joinPath(...parts) {
	return parts
		.map(p => String(p || '').replace(/^\/+|\/+$/g, ''))
		.filter(Boolean)
		.join('/')
}

// parentPath of a relative path ('' at the root).
export function parentPath(p) {
	const parts = String(p || '').split('/').filter(Boolean)
	parts.pop()
	return parts.join('/')
}

// decisionPayload is POST /runs/:id/decide's body for "Continue" on a
// paused run. When the run could go on either deleting (as shown) or not
// (once as a copy), nothing is preselected (spec §12.6): the user has to
// pick one, and until then there is no payload (Continue is disabled).
export function decisionPayload(offerCopyOnce, mode) {
	if (!offerCopyOnce) return { proceed: true, mode: 'as_shown' }
	if (mode !== 'as_shown' && mode !== 'copy_once') return null
	return { proceed: true, mode }
}
