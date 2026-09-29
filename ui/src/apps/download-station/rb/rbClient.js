// Client side of the Download Station browser (the real Chromium that runs
// on the server - see services/download-sidecar/rb_*.go). The page never
// runs any site's code: it gets pictures of the page over a WebSocket and
// sends the user's input back. Everything here is plain JS so it can be
// unit-tested without a DOM (rbClient.spec.js).

import { GATEWAY_PREFIX } from '../../../api/downloadSidecar'

export const FRAME_JPEG = 1
export const FRAME_CLIP_PNG = 2
export const FRAME_HEADER = 18

// Keep in sync with encodeFrameHeader (rb_proto.go):
// [u8 kind][u32 tab][u32 frame][u16 w][u16 h][u8 quality][u16 cssW][u16 cssH]
export function parseFrameHeader(buf) {
	const u8 = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
	if (u8.length < 1) return null
	if (u8[0] === FRAME_CLIP_PNG) return { kind: FRAME_CLIP_PNG, data: u8.subarray(1) }
	if (u8.length < FRAME_HEADER) return null
	const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength)
	return {
		kind: u8[0],
		tab: dv.getUint32(1),
		frame: dv.getUint32(5),
		w: dv.getUint16(9),
		h: dv.getUint16(11),
		quality: u8[13],
		cssW: dv.getUint16(14),
		cssH: dv.getUint16(16),
		data: u8.subarray(FRAME_HEADER)
	}
}

// ws(s)://<this page's host>/v1/download-station/rb/ws - same origin as the
// dashboard, so it works over https and through a tunnel.
export function wsUrl(loc, token) {
	const scheme = loc.protocol === 'https:' ? 'wss:' : 'ws:'
	const q = token ? `?token=${encodeURIComponent(token)}` : ''
	return `${scheme}//${loc.host}${GATEWAY_PREFIX}/rb/ws${q}`
}

// CDP modifier bits.
export function modsOf(e) {
	return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0)
}

// Keys the browser UI handles itself instead of sending to the page.
export function shortcutOf(e) {
	const ctrl = e.ctrlKey || e.metaKey
	const k = (e.key || '').toLowerCase()
	if (e.key === 'F6' || (ctrl && !e.shiftKey && k === 'l')) return 'focusUrl'
	if (e.key === 'F5' || (ctrl && k === 'r')) return e.shiftKey || (ctrl && e.key === 'F5') ? 'hardReload' : 'reload'
	if (e.altKey && !ctrl && e.key === 'ArrowLeft') return 'back'
	if (e.altKey && !ctrl && e.key === 'ArrowRight') return 'forward'
	if (e.key === 'BrowserBack') return 'back'
	if (e.key === 'BrowserForward') return 'forward'
	// Chrome keeps Ctrl+T/W/N for itself inside a normal tab, so Alt+T/W
	// work too (and are what the menu shows).
	if ((ctrl && !e.shiftKey && k === 't') || (e.altKey && !ctrl && k === 't')) return 'newTab'
	if ((ctrl && !e.shiftKey && k === 'w') || (e.altKey && !ctrl && k === 'w')) return 'closeTab'
	if (ctrl && e.shiftKey && k === 't') return 'reopenTab'
	if (ctrl && e.key === 'Tab') return e.shiftKey ? 'prevTab' : 'nextTab'
	if (ctrl && (e.key === 'PageDown' || e.key === 'PageUp')) return e.key === 'PageDown' ? 'nextTab' : 'prevTab'
	if (ctrl && !e.shiftKey && k === 'f') return 'find'
	// Ctrl+Shift+C as well, like the NivaroOS terminal.
	if (ctrl && !e.altKey && k === 'c') return 'copy'
	if (ctrl && !e.shiftKey && !e.altKey && k === 'x') return 'cut'
	if (ctrl && (e.key === '+' || e.key === '=')) return 'zoomIn'
	if (ctrl && (e.key === '-' || e.key === '_')) return 'zoomOut'
	if (ctrl && e.key === '0') return 'zoomReset'
	// The host browser's DevTools/view-source would inspect NivaroOS, not
	// the page: swallow them.
	if (e.key === 'F12' || (ctrl && e.shiftKey && (k === 'i' || k === 'j')) || (ctrl && k === 'u')) return 'swallow'
	if (e.shiftKey && e.key === 'F10') return 'contextMenu'
	if (e.key === 'ContextMenu') return 'contextMenu'
	return null
}

// A key message for the page, or null when the key must not be sent
// (composition in progress: the IME events carry the text instead).
export function keyMessage(e, type) {
	if (e.isComposing || e.keyCode === 229 || e.key === 'Process' || e.key === 'Dead' || e.key === 'Unidentified') return null
	const ctrl = e.ctrlKey || e.metaKey
	const text = type === 'down' && e.key && e.key.length === 1 && !ctrl ? e.key : type === 'down' && e.key === 'Enter' ? '\r' : ''
	return {
		t: 'key',
		type,
		key: e.key,
		code: e.code || '',
		keyCode: e.keyCode || 0,
		mods: modsOf(e),
		text,
		repeat: !!e.repeat,
		loc: e.location || 0
	}
}

// The remote Chromium runs on Linux, where the editing shortcuts are Ctrl
// chords: a Mac viewer's Cmd chords are turned into what they mean there
// (Cmd+A/Z/Shift+Z -> Ctrl+..., Cmd+Left/Right -> Home/End, Cmd+Up/Down ->
// Ctrl+Home/End, Cmd+Backspace/Delete -> delete to line start/end).
// Returns msg itself when nothing needs translating.
const CDP_ALT = 1
const CDP_CTRL = 2
const CDP_META = 4
const LINUX_KEYS = { Home: 36, End: 35 }
export function macChordToLinux(msg) {
	if (!msg || msg.t !== 'key' || !(msg.mods & CDP_META) || msg.mods & CDP_CTRL) return msg
	const base = msg.mods & ~CDP_META
	const as = (key, mods, keyCode = LINUX_KEYS[key]) => ({ ...msg, key, code: key in LINUX_KEYS ? key : msg.code, keyCode, mods, text: '' })
	switch (msg.key) {
		case 'ArrowLeft': return as('Home', base)
		case 'ArrowRight': return as('End', base)
		case 'ArrowUp': return as('Home', base | CDP_CTRL)
		case 'ArrowDown': return as('End', base | CDP_CTRL)
		case 'Backspace': return as('Backspace', (base & ~CDP_ALT) | CDP_CTRL | 8, msg.keyCode)
		case 'Delete': return as('Delete', (base & ~CDP_ALT) | CDP_CTRL | 8, msg.keyCode)
	}
	if (msg.key && msg.key.length === 1) return { ...msg, mods: base | CDP_CTRL, text: '' }
	return msg
}

// Wheel deltas in CSS pixels (line and page modes converted).
export function wheelDelta(e, pageH) {
	const mul = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? pageH || 800 : 1
	return { dx: e.deltaX * mul, dy: e.deltaY * mul }
}

// Accumulates pointer moves and wheel deltas and hands out at most one of
// each per animation frame, so a 1000 Hz mouse doesn't flood the socket.
export class InputCoalescer {
	constructor(send) {
		this.send = send
		this.move = null
		this.wheel = null
	}
	queueMove(msg) {
		this.move = msg
	}
	queueWheel(msg) {
		if (this.wheel) {
			this.wheel.dx += msg.dx
			this.wheel.dy += msg.dy
			this.wheel.x = msg.x
			this.wheel.y = msg.y
		} else {
			this.wheel = { ...msg }
		}
	}
	// Sends a pending move before a click, so the page sees the pointer
	// where the click lands.
	flushMove() {
		if (this.move) {
			this.send(this.move)
			this.move = null
		}
	}
	flush() {
		this.flushMove()
		if (this.wheel) {
			this.send(this.wheel)
			this.wheel = null
		}
	}
}

// ---- addresses ----

export const SEARCH_ENGINES = {
	duckduckgo: { label: 'DuckDuckGo', url: q => `https://duckduckgo.com/?q=${encodeURIComponent(q)}` },
	google: { label: 'Google', url: q => `https://www.google.com/search?q=${encodeURIComponent(q)}` }
}

export function looksLikeUrl(text) {
	const s = (text || '').trim()
	if (!s || /\s/.test(s)) return false
	if (/^https?:\/\//i.test(s)) return true
	if (/^localhost(:\d+)?(\/|$)/i.test(s)) return true
	if (/^\[[0-9a-f:]+\](:\d+)?(\/|$)/i.test(s)) return true
	if (/^\d{1,3}(\.\d{1,3}){3}(:\d+)?(\/|$)/.test(s)) return true
	return /^[\w-]+(\.[\w-]+)*\.[a-z][a-z0-9-]{1,62}(:\d+)?(\/.*)?$/i.test(s)
}

// What the address bar opens for what was typed: an address (https:// is
// assumed; http for LAN-looking hosts) or a search.
export function normalizeAddress(text, engine = 'duckduckgo') {
	const s = (text || '').trim()
	if (!s) return ''
	if (/^about:blank$/i.test(s)) return 'about:blank'
	if (/^https?:\/\//i.test(s)) return s
	if (looksLikeUrl(s)) {
		const host = s.split(/[/:?#]/)[0].toLowerCase()
		const lan = host === 'localhost' || /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.endsWith('.local') || host.endsWith('.lan') || !host.includes('.')
		return (lan ? 'http://' : 'https://') + s
	}
	const e = SEARCH_ENGINES[engine] || SEARCH_ENGINES.duckduckgo
	return e.url(s)
}

// Splits a URL for the address bar's "host highlighted" display.
export function splitForDisplay(raw) {
	try {
		const u = new URL(raw)
		if (u.protocol !== 'http:' && u.protocol !== 'https:') return { before: '', host: '', after: raw }
		const rest = raw.slice(raw.indexOf(u.host) + u.host.length)
		return { before: u.protocol === 'https:' ? '' : 'http://', host: u.host, after: rest === '/' ? '' : rest }
	} catch (e) {
		return { before: '', host: '', after: raw || '' }
	}
}

export function hostOf(raw) {
	try {
		return new URL(raw).hostname
	} catch (e) {
		return ''
	}
}

// ---- context menu ----

// The menu for a right-click, from the page's hit-test answer (hit) - only
// the sections that apply. Items are {id, label, icon?, hint?, disabled?};
// sections are separated in the UI.
export function buildContextMenu(hit, tab) {
	const h = hit || {}
	const t = tab || {}
	const sections = []
	if (h.link) {
		sections.push([
			{ id: 'openLinkNewTab', label: 'Open link in new tab', icon: 'tab-plus' },
			{ id: 'openLinkBackground', label: 'Open link in background tab', icon: 'tab' },
			{ id: 'copyLink', label: 'Copy link address', icon: 'link-variant' },
			...(h.linkText ? [{ id: 'copyLinkText', label: 'Copy link text', icon: 'format-text' }] : []),
			{ id: 'downloadLink', label: 'Download link with Download Station', icon: 'download', primary: true }
		])
	}
	if (h.image && /^https?:/i.test(h.image)) {
		sections.push([
			{ id: 'openImageNewTab', label: 'Open image in new tab', icon: 'image-outline' },
			{ id: 'copyImage', label: 'Copy image', icon: 'image-multiple-outline', disabled: !h.imageRect },
			{ id: 'copyImageAddress', label: 'Copy image address', icon: 'link-variant' },
			{ id: 'saveImage', label: 'Save image with Download Station', icon: 'download' }
		])
	}
	if (h.media && /^https?:/i.test(h.media)) {
		const what = h.mediaKind === 'audio' ? 'audio' : 'video'
		sections.push([
			{ id: 'copyMediaAddress', label: `Copy ${what} address`, icon: 'link-variant' },
			{ id: 'downloadMedia', label: `Download ${what} with Download Station`, icon: 'download' }
		])
	}
	if (h.editable) {
		sections.push([
			{ id: 'undo', label: 'Undo', icon: 'undo', hint: 'Ctrl+Z' },
			{ id: 'redo', label: 'Redo', icon: 'redo', hint: 'Ctrl+Shift+Z' },
			{ id: 'cut', label: 'Cut', icon: 'content-cut', hint: 'Ctrl+X', disabled: !h.selection },
			{ id: 'copy', label: 'Copy', icon: 'content-copy', hint: 'Ctrl+C', disabled: !h.selection },
			{ id: 'paste', label: 'Paste', icon: 'content-paste', hint: 'Ctrl+V' },
			{ id: 'selectAll', label: 'Select all', icon: 'select-all', hint: 'Ctrl+A' }
		])
	} else if (h.selection) {
		const short = h.selection.trim().replace(/\s+/g, ' ')
		const quoted = short.length > 24 ? short.slice(0, 24) + '…' : short
		const sel = [{ id: 'copy', label: 'Copy', icon: 'content-copy', hint: 'Ctrl+C' }]
		if (looksLikeUrl(short)) sel.push({ id: 'openSelection', label: 'Open as link', icon: 'open-in-app' })
		sel.push({ id: 'searchGoogle', label: `Search Google for "${quoted}"`, icon: 'magnify' })
		sel.push({ id: 'searchDuck', label: `Search DuckDuckGo for "${quoted}"`, icon: 'magnify' })
		sections.push(sel)
	}
	sections.push([
		{ id: 'back', label: 'Back', icon: 'arrow-left', hint: 'Alt+←', disabled: !t.canBack },
		{ id: 'forward', label: 'Forward', icon: 'arrow-right', hint: 'Alt+→', disabled: !t.canFwd },
		{ id: 'reload', label: 'Reload', icon: 'refresh', hint: 'F5' }
	])
	sections.push([
		{ id: 'copyPageAddress', label: 'Copy page address', icon: 'content-copy' },
		{ id: 'savePageLink', label: 'Save page link to Download Station', icon: 'tray-arrow-down' },
		{ id: 'openRealTab', label: 'Open page in a real browser tab', icon: 'open-in-new' },
		{ id: 'viewSource', label: 'View page source', icon: 'code-tags' }
	])
	return sections
}

// Moves the highlighted menu item with the keyboard, skipping disabled ones.
export function stepMenu(items, index, dir) {
	const n = items.length
	if (!n) return -1
	let i = index
	for (let k = 0; k < n; k++) {
		i = (i + dir + n) % n
		if (!items[i].disabled) return i
	}
	return -1
}

// ---- clipboard ----

// Writes text to the real clipboard. navigator.clipboard only exists in a
// secure context (https or localhost); on plain http inside the LAN the
// old execCommand route still works while the user's key press is recent.
export async function copyText(text, doc = typeof document !== 'undefined' ? document : null, nav = typeof navigator !== 'undefined' ? navigator : null) {
	if (nav && nav.clipboard && nav.clipboard.writeText) {
		try {
			await nav.clipboard.writeText(text)
			return true
		} catch (e) {}
	}
	if (!doc) return false
	const ta = doc.createElement('textarea')
	ta.value = text
	ta.setAttribute('readonly', '')
	ta.style.position = 'fixed'
	ta.style.top = '-1000px'
	ta.style.opacity = '0'
	doc.body.appendChild(ta)
	const prev = doc.activeElement
	ta.select()
	let ok = false
	try {
		ok = doc.execCommand('copy')
	} catch (e) {}
	doc.body.removeChild(ta)
	if (prev && prev.focus) prev.focus({ preventScroll: true })
	return ok
}

// ---- connection ----

const BACKOFF = [500, 1000, 2000, 4000, 8000]

// One WebSocket to the browser, with reconnects. Messages are JSON; binary
// messages are pictures (onFrame) or a copied image (onClipImage).
export class RbSession {
	constructor({ url, hello, onMessage, onFrame, onClipImage, onState, WebSocketImpl }) {
		this.url = url
		this.hello = hello
		this.onMessage = onMessage || (() => {})
		this.onFrame = onFrame || (() => {})
		this.onClipImage = onClipImage || (() => {})
		this.onState = onState || (() => {})
		this.WS = WebSocketImpl || (typeof WebSocket !== 'undefined' ? WebSocket : null)
		this.ws = null
		this.state = 'idle'
		this.attempt = 0
		this.closed = false
		this.fatal = null
		this.timer = null
	}
	setState(s, detail) {
		this.state = s
		this.onState(s, detail)
	}
	connect() {
		if (this.closed || !this.WS) return
		clearTimeout(this.timer)
		this.setState(this.attempt ? 'reconnecting' : 'connecting')
		const url = typeof this.url === 'function' ? this.url() : this.url
		let ws
		try {
			ws = new this.WS(url)
		} catch (e) {
			this.retry()
			return
		}
		ws.binaryType = 'arraybuffer'
		this.ws = ws
		ws.onopen = () => {
			this.attempt = 0
			this.setState('open')
			this.send({ t: 'hello', ...(typeof this.hello === 'function' ? this.hello() : this.hello) })
		}
		ws.onmessage = ev => {
			if (typeof ev.data === 'string') {
				let m
				try {
					m = JSON.parse(ev.data)
				} catch (e) {
					return
				}
				if (m.t === 'fatal') this.fatal = m
				this.onMessage(m)
				return
			}
			const f = parseFrameHeader(ev.data)
			if (!f) return
			if (f.kind === FRAME_CLIP_PNG) this.onClipImage(f.data)
			else if (f.kind === FRAME_JPEG) this.onFrame(f)
		}
		ws.onclose = ev => {
			if (this.ws !== ws) return
			this.ws = null
			if (this.closed) return
			if (this.fatal) {
				this.setState('fatal', this.fatal)
				return
			}
			this.retry(ev)
		}
		ws.onerror = () => {}
	}
	retry() {
		if (this.closed) return
		const delay = BACKOFF[Math.min(this.attempt, BACKOFF.length - 1)]
		this.attempt++
		this.setState('reconnecting', { delay, attempt: this.attempt })
		this.timer = setTimeout(() => this.connect(), delay)
	}
	send(msg) {
		const ws = this.ws
		if (!ws || ws.readyState !== 1) return false
		ws.send(JSON.stringify(msg))
		return true
	}
	close() {
		this.closed = true
		clearTimeout(this.timer)
		if (this.ws) {
			try {
				this.ws.close()
			} catch (e) {}
		}
		this.ws = null
		this.setState('closed')
	}
}
