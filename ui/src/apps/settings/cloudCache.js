// Settings > Online storage > Cache: the logic behind CloudCachePanel.vue,
// kept out of the component so it can be tested. The service
// (local-storage, /v1/cloud/cache) validates everything again.

export const GB = 1024 * 1024 * 1024

export const MODES = [
	{
		value: 'full',
		label: 'Full',
		desc: 'Keeps recently used parts of files on this server, so videos seek instantly and uploads finish in the background.'
	},
	{
		value: 'writes',
		label: 'Uploads only',
		desc: 'Only files you save to online storage wait here while they upload. Opening files streams them from the cloud.'
	},
	{
		value: 'off',
		label: 'Off',
		desc: 'Nothing is kept on this server. Files stream from the cloud, so seeking is slower, and some apps can\'t save to online storage.'
	}
]

export const AGE_UNITS = [
	{ value: 60, label: 'minutes' },
	{ value: 3600, label: 'hours' },
	{ value: 86400, label: 'days' }
]

const MIN_AGE = 60
const MAX_AGE = 365 * 86400

// formatBytes: 1536 -> "1.5 KB", 20 GiB -> "20 GB" (binary units, as the
// service writes them).
export function formatBytes(n) {
	n = Number(n) || 0
	if (n < 1024) return `${n} B`
	const units = ['KB', 'MB', 'GB', 'TB', 'PB']
	let v = n
	let i = -1
	do {
		v /= 1024
		i++
	} while (v >= 1024 && i < units.length - 1)
	const s = v >= 10 || Number.isInteger(v) ? Math.round(v).toString() : v.toFixed(1)
	return `${s} ${units[i]}`
}

// ageToParts splits seconds into the biggest whole unit: 3600 -> 1 hours,
// 5400 -> 90 minutes.
export function ageToParts(seconds) {
	seconds = Math.max(0, Math.round(Number(seconds) || 0))
	for (let i = AGE_UNITS.length - 1; i >= 0; i--) {
		const u = AGE_UNITS[i].value
		if (seconds >= u && seconds % u === 0) return { n: seconds / u, unit: u }
	}
	return { n: Math.max(1, Math.round(seconds / 60)), unit: 60 }
}

// settingsToForm / formToSettings convert between the service's settings
// ({mode, max_size bytes, max_age seconds, dir}) and the form's fields.
export function settingsToForm(s) {
	const age = ageToParts(s.max_age)
	return {
		mode: s.mode || 'full',
		sizeGB: Math.round(((Number(s.max_size) || 0) / GB) * 10) / 10,
		ageN: age.n,
		ageUnit: age.unit,
		dir: s.dir || ''
	}
}

export function formToSettings(f) {
	return {
		mode: f.mode,
		max_size: Math.round(Number(f.sizeGB) * GB),
		max_age: Math.round(Number(f.ageN) * Number(f.ageUnit)),
		dir: (f.dir || '').trim()
	}
}

export function sameSettings(a, b) {
	return a.mode === b.mode && a.max_size === b.max_size && a.max_age === b.max_age && (a.dir || '') === (b.dir || '')
}

// validateForm returns what's wrong with the form ('' = fine to send).
// free is the free space where the cache is now (bytes, 0 = unknown); it
// only matters when the location isn't changing.
export function validateForm(f, { free = 0, currentDir = '', used = 0 } = {}) {
	if (!MODES.some(m => m.value === f.mode)) return 'Choose a cache mode.'
	const size = Number(f.sizeGB)
	if (!Number.isFinite(size) || size <= 0) return 'Enter a maximum size above 0 GB.'
	const n = Number(f.ageN)
	if (!Number.isFinite(n) || n <= 0) return 'Enter how long to keep cached data.'
	const age = n * Number(f.ageUnit)
	if (age < MIN_AGE || age > MAX_AGE) return 'Keep cached data for at least 1 minute and at most 365 days.'
	const dir = (f.dir || '').trim()
	if (!dir.startsWith('/')) return 'Choose a folder for the cache.'
	if (dir === '/') return 'Choose a folder for the cache, not the whole disk.'
	if (free > 0 && dir.replace(/\/+$/, '') === currentDir.replace(/\/+$/, '') && size * GB >= free + used) {
		return `The maximum size must be less than the free space there (${formatBytes(free + used)}).`
	}
	return ''
}

// accountLine: what the panel says about one account's cache.
export function accountLine(a) {
	const parts = [`${formatBytes(a.used_bytes)} cached`]
	if (a.uploading > 0) parts.push(`uploading ${a.uploading} ${a.uploading === 1 ? 'file' : 'files'} now`)
	if (a.waiting > 0) parts.push(`${a.waiting} ${a.waiting === 1 ? 'file' : 'files'} waiting to upload (${formatBytes(a.wait_bytes)})`)
	else if (!a.uploading) parts.push('nothing waiting to upload')
	const stuck = (a.stuck || []).length
	if (stuck > 0) parts.push(`${stuck} can't finish`)
	if (!a.mounted) parts.push('not connected')
	return parts.join(' · ')
}

// clearable: bytes Clear could free on an account (cached minus waiting).
export function clearable(a) {
	return Math.max(0, (Number(a.used_bytes) || 0) - (Number(a.pending_bytes) || 0))
}

export function totals(accounts) {
	return (accounts || []).reduce(
		(t, a) => ({
			used: t.used + (Number(a.used_bytes) || 0),
			waiting: t.waiting + (Number(a.waiting) || 0),
			waitBytes: t.waitBytes + (Number(a.wait_bytes) || 0),
			stuck: t.stuck + ((a.stuck || []).length)
		}),
		{ used: 0, waiting: 0, waitBytes: 0, stuck: 0 }
	)
}

// stuckList flattens every account's stuck uploads for display.
export function stuckList(accounts) {
	const out = []
	for (const a of accounts || []) {
		for (const s of a.stuck || []) {
			out.push({ ...s, label: a.label || a.name, key: `${s.remote}:${s.name}`, fileName: s.name.split('/').pop() })
		}
	}
	return out
}

// clearResultText: the toast after Clear.
export function clearResultText(r) {
	const freed = formatBytes(r.freed_bytes || 0)
	const kept = r.kept_pending || 0
	let s = r.removed_files > 0 ? `Freed ${freed}.` : 'Nothing to clear.'
	if (kept > 0) s += ` Kept ${kept} ${kept === 1 ? 'file' : 'files'} still waiting to upload or open.`
	return s
}
