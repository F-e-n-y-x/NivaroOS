// Thin REST client for nivaroos-download-sidecar (port 28642), used by the
// Download Station windowed app. Same shape and auth convention as
// vmSidecar.js - the sidecar is reached on its own port, not through the
// gateway, with the JWT read straight from localStorage.
const hostname = typeof window !== 'undefined' ? window.location.hostname : 'localhost'
const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:'
const protocol = isHttps ? 'https:' : 'http:'
const PORT = 28642
const BASE_URL = `${protocol}//${hostname}:${PORT}`

function authToken() {
	return localStorage.getItem('access_token') || ''
}

async function request(path, options = {}) {
	const headers = { ...(options.headers || {}) }
	const token = authToken()
	if (token) headers.Authorization = token
	const res = await fetch(`${BASE_URL}${path}`, { ...options, headers })
	if (!res.ok) {
		let body = {}
		try {
			body = JSON.parse(await res.text())
		} catch (e) {}
		throw new Error(body.error || body.message || `${options.method || 'GET'} ${path} failed: ${res.status}`)
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
	baseUrl: BASE_URL,
	// The origin proxied lite-browser pages run on - postMessage events from
	// the browser iframe are only trusted when they come from here.
	origin: BASE_URL,

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
	redownload: id => request(`/downloads/${enc(id)}/redownload`, { method: 'POST' }),
	pauseAll: () => request('/downloads/pause-all', { method: 'POST' }),
	resumeAll: () => request('/downloads/resume-all', { method: 'POST' }),
	clearCompleted: () => request('/downloads/clear-completed', { method: 'POST' }),
	probe: payload => request('/probe', jsonBody(payload)),
	events: after => request(`/events?after=${after || 0}`),

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
	captureUrl: (sid, url, referer) => request(`/browser/sessions/${enc(sid)}/captures`, jsonBody({ url, referer })),
	getCapture: (sid, cid) => request(`/browser/sessions/${enc(sid)}/captures/${enc(cid)}`),

	// Absolute URL of a page as served through the lite-browser proxy. Kept
	// in sync with BrowserSession.proxyPath on the Go side.
	proxyUrl(prefix, rawUrl) {
		const u = new URL(rawUrl)
		return `${BASE_URL}${prefix}${u.protocol.slice(0, -1)}/${u.host}${u.pathname}${u.search}${u.hash}`
	}
}

// Shared formatting helpers for the app's components.
export function formatBytes(n) {
	if (n == null || n < 0) return '—'
	if (n < 1024) return `${n} B`
	const units = ['KB', 'MB', 'GB', 'TB']
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
