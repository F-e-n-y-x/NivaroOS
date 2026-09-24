// Thin REST client for nivaroos-download-sidecar (port 28642), used by the
// Download Station windowed app. Same auth convention as vmSidecar.js - the
// JWT is read straight from localStorage and sent as Authorization.
//
// The API always goes through the NivaroOS gateway's same-origin route
// /v1/download-station/*, so it works wherever the dashboard works: on the
// LAN, over https (the sidecar itself speaks plain http) and behind a
// reverse proxy or tunnel that only publishes the dashboard's port.
//
// The lite browser is different: its proxied pages must run on the
// sidecar's own origin (never the UI's, where page scripts could read the
// user's token), so it is only available when that origin is loadable -
// i.e. not from an https page. See browserAvailable().
export const SIDECAR_PORT = 28642
export const GATEWAY_PREFIX = '/v1/download-station'

function loc() {
	return typeof window !== 'undefined' && window.location ? window.location : { protocol: 'http:', hostname: 'localhost', origin: 'http://localhost' }
}

function isHttpsPage() {
	return loc().protocol === 'https:'
}

function hostForUrl() {
	const h = loc().hostname || 'localhost'
	// A bare IPv6 literal needs brackets in a URL.
	return h.includes(':') && !h.startsWith('[') ? `[${h}]` : h
}

// The sidecar's own origin (lite browser pages run here).
export function sidecarOrigin() {
	return `http://${hostForUrl()}:${SIDECAR_PORT}`
}

export function apiBase() {
	return `${loc().origin}${GATEWAY_PREFIX}`
}

function authToken() {
	try {
		return localStorage.getItem('access_token') || ''
	} catch (e) {
		return ''
	}
}

function makeError(message, code, status) {
	const err = new Error(message)
	err.code = code
	if (status) err.status = status
	return err
}

async function request(path, options = {}) {
	const headers = { ...(options.headers || {}) }
	const token = authToken()
	if (token) headers.Authorization = token
	let res
	try {
		res = await fetch(`${apiBase()}${path}`, { ...options, headers })
	} catch (e) {
		throw makeError(e.message || 'network error', 'unreachable')
	}
	if (!res.ok) {
		let text = ''
		let body = {}
		try {
			text = await res.text()
			body = JSON.parse(text)
		} catch (e) {}
		// A stopped sidecar comes back as the gateway's own 502, not as our
		// JSON: that's "not running", not a request error.
		if ([502, 503, 504].includes(res.status) && !body.error && !body.message) {
			throw makeError(`${options.method || 'GET'} ${path} failed: ${res.status}`, 'unreachable', res.status)
		}
		const code = res.status === 401 ? 'auth' : res.status === 403 ? 'forbidden' : 'http'
		throw makeError(body.error || body.message || `${options.method || 'GET'} ${path} failed: ${res.status}`, code, res.status)
	}
	if (res.status === 204) return null
	const text = await res.text()
	if (!text || !text.trim()) return null
	try {
		return JSON.parse(text)
	} catch (e) {
		return null
	}
}

const jsonBody = (payload, method = 'POST') => ({
	method,
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify(payload)
})

const enc = encodeURIComponent

export const downloadSidecar = {
	get baseUrl() {
		return apiBase()
	},
	// The origin proxied lite-browser pages run on - postMessage events from
	// the browser iframe are only trusted when they come from here.
	get origin() {
		return sidecarOrigin()
	},
	// Whether the lite browser can be shown in this page at all.
	browserAvailable() {
		return !isHttpsPage()
	},
	isHttpsPage,

	health: () => request('/health'),
	status: () => request('/status'),
	getSettings: () => request('/settings'),
	updateSettings: patch => request('/settings', jsonBody(patch, 'PUT')),

	listDownloads: () => request('/downloads'),
	getDownload: id => request(`/downloads/${enc(id)}`),
	addDownload: payload => request('/downloads', jsonBody(payload)),
	updateDownload: (id, patch) => request(`/downloads/${enc(id)}`, jsonBody(patch, 'PATCH')),
	deleteDownload: (id, deleteFile) => request(`/downloads/${enc(id)}${deleteFile ? '?delete_file=true' : ''}`, { method: 'DELETE' }),
	pause: id => request(`/downloads/${enc(id)}/pause`, { method: 'POST' }),
	resume: id => request(`/downloads/${enc(id)}/resume`, { method: 'POST' }),
	// Starts over from byte zero (pausing first if needed); a completed
	// file is replaced atomically once the new copy is complete.
	redownload: id => request(`/downloads/${enc(id)}/redownload`, { method: 'POST' }),
	pauseAll: () => request('/downloads/pause-all', { method: 'POST' }),
	resumeAll: () => request('/downloads/resume-all', { method: 'POST' }),
	clearCompleted: () => request('/downloads/clear-completed', { method: 'POST' }),
	probe: payload => request('/probe', jsonBody(payload)),
	events: after => request(`/events?after=${after || 0}`),

	storageRoots: () => request('/storage/roots'),
	createFolder: (parent, name) => request('/storage/folders', jsonBody({ parent, name })),

	adblockStats: () => request('/adblock'),
	updateFilterLists: () => request('/adblock/update', { method: 'POST' }),

	listHistory: (q, limit) => request(`/browser/history?q=${enc(q || '')}&limit=${limit || 500}`),
	addHistory: (url, title) => request('/browser/history', jsonBody({ url, title })),
	deleteHistory: id => request(`/browser/history/${enc(id)}`, { method: 'DELETE' }),
	clearHistory: () => request('/browser/history', { method: 'DELETE' }),

	createBrowserSession: () => request('/browser/sessions', { method: 'POST' }),
	getBrowserSession: sid => request(`/browser/sessions/${enc(sid)}`),
	deleteBrowserSession: sid => request(`/browser/sessions/${enc(sid)}`, { method: 'DELETE' }),
	clearBrowserCookies: sid => request(`/browser/sessions/${enc(sid)}/clear-cookies`, { method: 'POST' }),
	// The user typed/picked this address: lets the proxy reach its host
	// even when it's on the local network.
	markTyped: (sid, url) => request(`/browser/sessions/${enc(sid)}/typed`, jsonBody({ url })),
	// The URL the proxy really served for a page's nav ID.
	getNav: (sid, nav) => request(`/browser/sessions/${enc(sid)}/navs/${enc(nav)}`),
	captureUrl: (sid, url, referer) => request(`/browser/sessions/${enc(sid)}/captures`, jsonBody({ url, referer })),
	getCapture: (sid, cid) => request(`/browser/sessions/${enc(sid)}/captures/${enc(cid)}`),

	// Absolute URL of a page as served through the lite-browser proxy. Kept
	// in sync with BrowserSession.proxyPath on the Go side.
	proxyUrl(prefix, rawUrl) {
		const u = new URL(rawUrl)
		return `${sidecarOrigin()}${prefix}${u.protocol.slice(0, -1)}/${u.host}${u.pathname}${u.search}${u.hash}`
	}
}

// Shared formatting helpers for the app's components. Binary (1024-based)
// units, labelled as such (KiB, MiB...) - the speed limit setting uses the
// same MiB so the numbers you type and the numbers you see agree.
export const MIB = 1024 * 1024

export function formatBytes(n) {
	if (n == null || n < 0) return '—'
	if (n < 1024) return `${n} B`
	const units = ['KiB', 'MiB', 'GiB', 'TiB']
	let v = n / 1024
	let i = 0
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024
		i++
	}
	return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

export function formatSpeed(bps) {
	if (!bps || bps < 1) return ''
	return `${formatBytes(Math.round(bps))}/s`
}

export function formatEta(sec) {
	if (sec == null || sec < 0) return ''
	if (sec < 60) return `${sec}s`
	const m = Math.floor(sec / 60)
	if (m < 60) return `${m}m ${sec % 60}s`
	const h = Math.floor(m / 60)
	if (h < 48) return `${h}h ${m % 60}m`
	return `${Math.floor(h / 24)}d ${h % 24}h`
}

// IDM-style categories, by extension.
export const CATEGORIES = [
	{ id: 'compressed', label: 'Compressed', icon: 'folder-zip-outline', ext: ['zip', 'rar', '7z', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst', 'lz', 'lzma', 'cab', 'arj'] },
	{ id: 'programs', label: 'Programs', icon: 'application-cog-outline', ext: ['exe', 'msi', 'apk', 'deb', 'rpm', 'dmg', 'pkg', 'appimage', 'iso', 'img', 'run', 'sh', 'bin', 'jar', 'flatpak', 'snap'] },
	{ id: 'video', label: 'Video', icon: 'filmstrip', ext: ['mp4', 'mkv', 'avi', 'mov', 'webm', 'wmv', 'flv', 'm4v', 'mpg', 'mpeg', 'ts', '3gp'] },
	{ id: 'music', label: 'Music', icon: 'music-note-outline', ext: ['mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma', 'alac'] },
	{ id: 'documents', label: 'Documents', icon: 'file-document-outline', ext: ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'txt', 'epub', 'csv', 'rtf', 'md'] },
	{ id: 'images', label: 'Images', icon: 'image-outline', ext: ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'tiff', 'heic', 'avif', 'psd'] }
]

export function categoryOf(filename) {
	const m = /\.([a-z0-9]+)$/i.exec(filename || '')
	const ext = m ? m[1].toLowerCase() : ''
	const cat = CATEGORIES.find(c => c.ext.includes(ext))
	return cat ? cat.id : 'other'
}

export function fileIcon(filename) {
	const id = categoryOf(filename)
	const cat = CATEGORIES.find(c => c.id === id)
	return cat ? cat.icon : 'file-outline'
}

// Where a path sits relative to the storage roots: the root it's under,
// or null. Used by the folder picker to keep navigation inside them.
export function rootOf(path, roots) {
	const p = (path || '').replace(/\/+$/, '') || '/'
	let best = null
	for (const r of roots || []) {
		if (p === r || p.startsWith(r + '/')) {
			if (!best || r.length > best.length) best = r
		}
	}
	return best
}
