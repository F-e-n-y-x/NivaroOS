// Terminal sessions: REST client, list helpers and what the terminal
// windows remember between openings. See
// docs/specs/2026-09-29-terminal-sessions.md.
//
// Kept free of Vue/axios imports so it can be unit tested; the app binds it
// to its axios wrapper in src/service/terminalSessions.js.

// Paths for the app's api wrapper, which adds the /v1 prefix.
export const FAMILY_PATHS = {
	host: '/sys/terminal-sessions',
	container: '/container/terminal-sessions',
}

export function familyOf(session) {
	return session && session.kind === 'container' ? 'container' : 'host'
}

function unwrap(res) {
	return res && res.data ? res.data.data : undefined
}

export function statusOf(err) {
	return (err && err.response && err.response.status) || 0
}

// http: { get(url, params), post(url, body), put(url, body), delete(url) }
// returning axios-style responses ({ data: { success, message, data } }).
export function createSessionApi(http) {
	const base = (kind) => FAMILY_PATHS[kind === 'container' ? 'container' : 'host']
	return {
		// Both families, merged. A family that fails (service down, older
		// server) is reported in `errors` instead of failing the whole list.
		async listAll() {
			const kinds = ['host', 'container']
			const results = await Promise.allSettled(kinds.map((k) => http.get(base(k))))
			const lists = []
			const errors = {}
			let limits = null
			results.forEach((r, i) => {
				if (r.status === 'fulfilled') {
					const d = unwrap(r.value) || {}
					lists.push(Array.isArray(d.sessions) ? d.sessions : [])
					if (!limits && d.limits) limits = d.limits
				} else {
					errors[kinds[i]] = r.reason
				}
			})
			return { sessions: mergeSessions(lists), errors, limits }
		},
		async listContainer(container) {
			const res = await http.get(base('container'), container ? { container } : undefined)
			const d = unwrap(res) || {}
			return { sessions: mergeSessions([d.sessions || []]), limits: d.limits || null }
		},
		async get(kind, id) {
			return unwrap(await http.get(`${base(kind)}/${encodeURIComponent(id)}`))
		},
		async create(kind, body) {
			return unwrap(await http.post(base(kind), body || {}))
		},
		async rename(session, title) {
			return unwrap(await http.put(`${base(familyOf(session))}/${encodeURIComponent(session.id)}`, { title }))
		},
		async kill(session) {
			return unwrap(await http.delete(`${base(familyOf(session))}/${encodeURIComponent(session.id)}`))
		},
		// 'gone' | 'alive' | 'unknown' - used after a failed WebSocket
		// handshake, whose HTTP status a browser can't see.
		async probe(kind, id) {
			try {
				const s = await this.get(kind, id)
				return s ? 'alive' : 'gone'
			} catch (err) {
				return statusOf(err) === 404 ? 'gone' : 'unknown'
			}
		},
	}
}

function ts(v) {
	const t = v ? Date.parse(v) : NaN
	return isFinite(t) ? t : 0
}

// Running sessions first, most recently used first; then the exited ones.
export function mergeSessions(lists) {
	const seen = new Set()
	const all = []
	for (const list of lists) {
		for (const s of list || []) {
			if (!s || !s.id) continue
			const key = familyOf(s) + ':' + s.id
			if (seen.has(key)) continue
			seen.add(key)
			all.push(s)
		}
	}
	return all.sort((a, b) => {
		const ra = a.state === 'exited' ? 1 : 0
		const rb = b.state === 'exited' ? 1 : 0
		if (ra !== rb) return ra - rb
		return (ts(b.last_activity_at) || ts(b.created_at)) - (ts(a.last_activity_at) || ts(a.created_at))
	})
}

export function sessionKey(s) {
	return s ? familyOf(s) + ':' + s.id : ''
}

function basename(p) {
	if (!p) return ''
	const parts = String(p).split('/')
	return parts[parts.length - 1] || p
}

// True when nothing but the shell itself is in the foreground (ending the
// session then loses nothing but the prompt).
export function isIdleShell(s) {
	if (!s || !s.command) return true
	return basename(s.command) === basename(s.shell) || /^-?(ba|z|fi|a|da|k|tc|c)?sh$/.test(basename(s.command))
}

export function shortCwd(s) {
	const cwd = s && s.cwd
	if (!cwd) return ''
	if (s.kind === 'host' && s.user) {
		const home = s.user === 'root' ? '/root' : `/home/${s.user}`
		if (cwd === home) return '~'
		if (cwd.startsWith(home + '/')) return '~' + cwd.slice(home.length)
	}
	return cwd
}

// "vim · ~/projects", "~/projects", "bash"
export function sessionSubtitle(s) {
	if (!s) return ''
	const cwd = shortCwd(s)
	const cmd = isIdleShell(s) ? '' : basename(s.command)
	if (cmd && cwd) return `${cmd} · ${cwd}`
	return cmd || cwd || basename(s.shell)
}

// Relative time with a caller-supplied translator for the units.
export function relativeTime(iso, now = Date.now()) {
	const t = ts(iso)
	if (!t) return ''
	const s = Math.max(0, Math.round((now - t) / 1000))
	if (s < 45) return 'just now'
	const m = Math.round(s / 60)
	if (m < 60) return `${m} min ago`
	const h = Math.round(m / 60)
	if (h < 24) return `${h} h ago`
	return `${Math.round(h / 24)} d ago`
}

// Why a session ended, as one line (a translation key; {code} is the
// exit code).
export function exitSummary(detail) {
	const code = detail && typeof detail.code === 'number' ? detail.code : -1
	switch (detail && detail.reason) {
		case 'killed':
			return 'This session was ended'
		case 'timeout':
			return 'Ended after being left idle with no one connected'
		case 'evicted':
			return 'Ended to make room for a new session'
		default:
			return code >= 0 ? 'Shell exited with code {code}' : 'Shell exited'
	}
}

export function attachUrl(session, { wsBase, token, cols, rows }) {
	const q = new URLSearchParams()
	if (token) q.set('token', token)
	if (cols) q.set('cols', String(cols))
	if (rows) q.set('rows', String(rows))
	const path = session.attach_path || `/v1${FAMILY_PATHS[familyOf(session)]}/${session.id}/attach`
	return `${wsBase}${path}?${q.toString()}`
}

// --- what a Terminal window remembers --------------------------------------
// Per window (survives a page reload, since the window itself is restored
// from localStorage by the desktop): the sessions its tabs show.
// Last closed: when a Terminal window is closed, its sessions keep running
// and the next Terminal window that opens picks them back up.

const WINDOW_PREFIX = 'nvos_term_window_'
const LAST_CLOSED_KEY = 'nvos_term_last_closed'
const CONTAINER_PREFIX = 'nvos_term_container_'
const LAST_CLOSED_MAX_AGE = 7 * 24 * 3600 * 1000

function store(storage) {
	if (storage) return storage
	try {
		return typeof localStorage !== 'undefined' ? localStorage : null
	} catch (e) {
		return null
	}
}

function readJSON(storage, key) {
	const st = store(storage)
	if (!st) return null
	try {
		const v = JSON.parse(st.getItem(key))
		return v && typeof v === 'object' ? v : null
	} catch (e) {
		return null
	}
}

function writeJSON(storage, key, value) {
	const st = store(storage)
	if (!st) return
	try {
		if (value === null) st.removeItem(key)
		else st.setItem(key, JSON.stringify(value))
	} catch (e) {
		/* full or blocked */
	}
}

function cleanLayout(layout) {
	if (!layout || !Array.isArray(layout.tabs)) return null
	const tabs = layout.tabs
		.filter((t) => t && typeof t.id === 'string' && t.id)
		.map((t) => ({ id: t.id, kind: t.kind === 'container' ? 'container' : 'host' }))
	if (!tabs.length) return null
	const active = typeof layout.active === 'string' && tabs.some((t) => t.id === layout.active) ? layout.active : tabs[0].id
	return { tabs, active }
}

export function saveWindowLayout(windowId, layout, storage) {
	writeJSON(storage, WINDOW_PREFIX + windowId, cleanLayout(layout))
}

export function loadWindowLayout(windowId, storage) {
	return cleanLayout(readJSON(storage, WINDOW_PREFIX + windowId))
}

export function forgetWindowLayout(windowId, storage) {
	writeJSON(storage, WINDOW_PREFIX + windowId, null)
}

export function saveLastClosed(layout, storage, now = Date.now()) {
	const clean = cleanLayout(layout)
	writeJSON(storage, LAST_CLOSED_KEY, clean ? Object.assign({ at: now }, clean) : null)
}

// Taken once: a second Terminal window must not claim the same sessions.
export function takeLastClosed(storage, now = Date.now()) {
	const v = readJSON(storage, LAST_CLOSED_KEY)
	writeJSON(storage, LAST_CLOSED_KEY, null)
	if (!v || !v.at || now - v.at > LAST_CLOSED_MAX_AGE) return null
	return cleanLayout(v)
}

export function saveContainerSession(container, id, storage) {
	writeJSON(storage, CONTAINER_PREFIX + container, id ? { id } : null)
}

export function loadContainerSession(container, storage) {
	const v = readJSON(storage, CONTAINER_PREFIX + container)
	return v && typeof v.id === 'string' ? v.id : ''
}

// Which of a container's sessions its console should show when it opens:
// the one it showed last if it still runs, else the most recent one no
// one is looking at, else none (-> start a new one).
export function pickContainerSession(sessions, lastId) {
	const running = (sessions || []).filter((s) => s.state === 'running')
	if (lastId) {
		const last = running.find((s) => s.id === lastId)
		if (last) return last
	}
	return running.find((s) => !s.clients) || null
}

// Which saved tabs can be restored from a fresh list.
export function restorableTabs(layout, sessions) {
	if (!layout) return { tabs: [], active: null }
	const byKey = new Map((sessions || []).map((s) => [sessionKey(s), s]))
	const tabs = layout.tabs
		.map((t) => byKey.get(t.kind + ':' + t.id))
		.filter((s) => s && s.state === 'running')
	const active = tabs.some((s) => s.id === layout.active) ? layout.active : (tabs[0] ? tabs[0].id : null)
	return { tabs, active }
}
