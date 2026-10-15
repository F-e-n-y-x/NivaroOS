// Profile pictures. The server keeps one 512 px PNG per user and an
// avatar_version (hash) in the user info: '' means none (draw initials).

export const AVATAR_MAX_SOURCE = 20 << 20 // the picked file; we upload our own 512 px crop
export const AVATAR_TYPES = ['image/png', 'image/jpeg', 'image/webp']
export const AVATAR_OUT = 512

// The signed-in user: Vuex is empty again after a reload, Login keeps a copy.
export function currentUser(store) {
	const u = store && store.state.user
	if (u && u.username) return u
	try {
		return JSON.parse(localStorage.getItem('user') || 'null') || {}
	} catch (e) {
		return {}
	}
}

export function saveCurrentUser(store, user) {
	store.commit('SET_USER', user)
	try {
		localStorage.setItem('user', JSON.stringify(user))
	} catch (e) { /* storage full or blocked */ }
}

// The URL a media ticket for avatars names (service/mediaTicket.js).
export const AVATAR_PATH = '/v1/users/avatar'

// version: the user's avatar_version; '' = none, undefined = unknown (try it).
// mt: a media ticket for AVATAR_PATH - never the access token (audit L5).
export function avatarUrl(username, version, mt) {
	if (!username || version === '' || version === null || !mt) return ''
	const q = new URLSearchParams({ username, mt })
	if (version) q.set('v', version)
	return `${AVATAR_PATH}?${q}`
}

export function initials(name) {
	const parts = String(name || '').trim().split(/[\s._-]+/).filter(Boolean)
	if (!parts.length) return '?'
	const first = [...parts[0]][0]
	const second = parts.length > 1 ? [...parts[1]][0] : ''
	return (first + second).toUpperCase()
}

export function checkAvatarFile(file) {
	if (!file) return 'No file'
	if (!AVATAR_TYPES.includes(file.type)) return 'Choose a PNG, JPEG or WebP picture'
	if (file.size > AVATAR_MAX_SOURCE) return 'That picture is too large (max 20 MB)'
	return ''
}

// Crop geometry for a square view of `view` px over a w x h image at
// `zoom` (1 = image just covers the view). Offsets are the image centre's
// shift from the view centre, in view px.
export function coverScale(w, h, view, zoom) {
	return Math.max(view / w, view / h) * zoom
}

export function clampOffset(x, y, w, h, view, zoom) {
	const s = coverScale(w, h, view, zoom)
	const mx = Math.max(0, (w * s - view) / 2)
	const my = Math.max(0, (h * s - view) / 2)
	return { x: Math.min(mx, Math.max(-mx, x)), y: Math.min(my, Math.max(-my, y)) }
}

// The source square (image px) the view shows.
export function cropRect(x, y, w, h, view, zoom) {
	const s = coverScale(w, h, view, zoom)
	const side = view / s
	return { sx: w / 2 - x / s - side / 2, sy: h / 2 - y / s - side / 2, side }
}

// The login screen asks the server for the typed name's picture (public,
// rate-limited; 404 = unknown name or none). The last account signed in on
// this browser also keeps a small copy here, so its picture shows at once.
export const LOGIN_AVATAR_PATH = '/v1/users/login-avatar'

export function loginAvatarUrl(username) {
	const name = String(username || '').trim()
	return name ? `${LOGIN_AVATAR_PATH}?${new URLSearchParams({ username: name })}` : ''
}

// Resolves to url once the browser has the picture, rejects on 404/error.
export function preloadImage(url) {
	return new Promise((resolve, reject) => {
		const img = new Image()
		img.onload = () => resolve(url)
		img.onerror = reject
		img.src = url
	})
}

// lookup(name): `wait` ms after the last call, show(picture URL or '').
// A slow answer for an earlier name never replaces a later one.
export function createAvatarLookup(show, { wait = 300, load = preloadImage } = {}) {
	let timer
	let seq = 0
	return (name) => {
		clearTimeout(timer)
		const mine = ++seq
		const url = loginAvatarUrl(name)
		if (!url) return show('')
		timer = setTimeout(() => {
			load(url).then(() => url, () => '').then(src => {
				if (mine === seq) show(src)
			})
		}, wait)
	}
}

const REMEMBER_KEY = 'avatar_thumb'

export function rememberedAvatar(username) {
	try {
		const r = JSON.parse(localStorage.getItem(REMEMBER_KEY) || 'null')
		return r && username && r.username === username ? r.data : ''
	} catch (e) {
		return ''
	}
}

export function forgetAvatar() {
	try {
		localStorage.removeItem(REMEMBER_KEY)
	} catch (e) { /* blocked */ }
}

// src: an image URL or a canvas. Kept at 128 px (a few KB).
export function rememberAvatar(username, src) {
	const store = (img) => {
		const c = document.createElement('canvas')
		c.width = c.height = 128
		c.getContext('2d').drawImage(img, 0, 0, 128, 128)
		try {
			localStorage.setItem(REMEMBER_KEY, JSON.stringify({ username, data: c.toDataURL('image/png') }))
		} catch (e) { /* storage full or blocked */ }
	}
	if (typeof src !== 'string') return store(src)
	const img = new Image()
	img.onload = () => store(img)
	img.src = src
}

// After sign-in / on desktop load: keep the login screen's copy current.
export async function syncRememberedAvatar(user) {
	if (!user || !user.avatar_version) return forgetAvatar()
	const { ticket } = await import('@/service/mediaTicket')
	const mt = await ticket('url', AVATAR_PATH).catch(() => '')
	if (mt) rememberAvatar(user.username, avatarUrl(user.username, user.avatar_version, mt))
}
