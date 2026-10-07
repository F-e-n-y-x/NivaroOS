// Pure helpers for the Torrents section (kept out of the components so
// vitest can cover them). The sidecar reports the same states whichever
// engine runs (torrent.go): downloading, stalled, metadata, seeding,
// queued, checking, moving, paused, completed, error.

export const ACTIVE_STATES = ['downloading', 'stalled', 'metadata', 'seeding', 'queued', 'checking', 'moving']

export const FILTERS = [
	{ id: 'all', label: 'All' },
	{ id: 'downloading', label: 'Downloading' },
	{ id: 'seeding', label: 'Seeding' },
	{ id: 'paused', label: 'Paused' },
	{ id: 'completed', label: 'Completed' }
]

export function matchesFilter(t, filter) {
	switch (filter) {
		case 'downloading':
			return ['downloading', 'stalled', 'metadata', 'queued', 'checking'].includes(t.state) && t.progress < 1
		case 'seeding':
			return t.state === 'seeding'
		case 'paused':
			return t.state === 'paused' || t.state === 'error'
		case 'completed':
			return t.progress >= 1 || t.state === 'completed'
	}
	return true
}

export function filterTorrents(list, filter, query) {
	const q = (query || '').trim().toLowerCase()
	return list.filter(t => matchesFilter(t, filter) && (!q || (t.name || '').toLowerCase().includes(q) || (t.category || '').toLowerCase().includes(q)))
}

export function countByFilter(list) {
	const out = {}
	for (const f of FILTERS) out[f.id] = list.filter(t => matchesFilter(t, f.id)).length
	return out
}

export function isPaused(t) {
	return t.state === 'paused' || t.state === 'completed' || t.state === 'error'
}

export function percent(t) {
	return Math.min(100, Math.floor((t.progress || 0) * 1000) / 10)
}

export function formatRatio(r) {
	if (r == null || r < 0) return '—'
	return r >= 100 ? r.toFixed(0) : r.toFixed(2)
}

const STATE_LABELS = {
	downloading: 'Downloading',
	stalled: 'Stalled',
	metadata: 'Getting metadata',
	seeding: 'Seeding',
	queued: 'Queued',
	checking: 'Checking',
	moving: 'Moving',
	paused: 'Paused',
	completed: 'Completed',
	error: 'Error'
}

export function stateLabel(state) {
	return STATE_LABELS[state] || state
}

// The badge colours reuse the download list's (ds-common.scss).
export function badgeClass(state) {
	if (state === 'seeding' || state === 'completed') return 'is-completed'
	if (state === 'error') return 'is-failed'
	if (state === 'paused' || state === 'queued') return 'is-paused'
	return 'is-downloading'
}

// What the Add box was given: magnet links and http(s) links, one per
// line (or several pasted together).
export function parseSources(text) {
	const out = []
	for (const raw of (text || '').split(/\s+/)) {
		const s = raw.trim()
		if (/^magnet:\?/i.test(s) || /^https?:\/\/\S+$/i.test(s)) {
			if (!out.includes(s)) out.push(s)
		}
	}
	return out
}

export function isTorrentFile(file) {
	return !!file && (/\.torrent$/i.test(file.name || '') || file.type === 'application/x-bittorrent')
}

// Name shown for a magnet before its metadata arrives (dn=, else the hash).
export function magnetName(m) {
	const dn = /[?&]dn=([^&]+)/i.exec(m || '')
	if (dn) {
		try {
			return decodeURIComponent(dn[1].replace(/\+/g, ' '))
		} catch (e) {
			return dn[1]
		}
	}
	const h = /xt=urn:bt(?:ih|mh):([^&]+)/i.exec(m || '')
	return h ? h[1] : m
}

// Speed limits are kept in bytes/s; the settings show KiB/s.
export const toKiB = b => Math.round((b || 0) / 1024)
export const fromKiB = k => Math.max(0, Math.round(Number(k) || 0)) * 1024

// Whether a settings section works on the running engine ('all' = none do:
// the user's own qBittorrent is configured in its own UI).
export function supported(unsupported, key) {
	const u = unsupported || []
	return !u.includes('all') && !u.includes(key)
}

export const PRIORITIES = [
	{ value: 1, label: 'Normal' },
	{ value: 6, label: 'High' },
	{ value: 7, label: 'Maximum' }
]

// A .torrent file as the base64 the sidecar's JSON API takes.
export function fileToBase64(file) {
	return new Promise((resolve, reject) => {
		const r = new FileReader()
		r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ''))
		r.onerror = () => reject(r.error)
		r.readAsDataURL(file)
	})
}
