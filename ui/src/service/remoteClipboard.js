// Clipboard history shared by the VM console and Host Desktop: every text
// sent to a remote machine ("to") and every text it copied ("from"), newest
// first. One list for all consoles, so text copied in one VM can be sent
// to another or to the host.
//
// Kept in sessionStorage: it survives a page reload, but not closing the
// browser tab - clipboards hold passwords often enough that writing them
// to disk for good isn't a sane default.
import Vue from 'vue'

const STORAGE_KEY = 'nvos_remote_clipboard_v1'
export const MAX_ITEMS = 50
// Bigger texts are still sent; only the stored copy is cut.
export const MAX_STORED_CHARS = 100000
// Sending text to a remote clipboard makes it report the same text straight
// back as a copy; that echo isn't a new entry.
const ECHO_WINDOW_MS = 5000

function load() {
	try {
		const raw = sessionStorage.getItem(STORAGE_KEY)
		const items = raw ? JSON.parse(raw) : []
		return Array.isArray(items) ? items.filter((i) => i && typeof i.text === 'string') : []
	} catch (e) {
		return []
	}
}

const state = Vue.observable({ items: load() })

function save() {
	try {
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify(state.items))
	} catch (e) {
		// Storage full or blocked: the in-memory history still works.
	}
}

let seq = 0
const newId = (now) => `${now.toString(36)}-${(seq++).toString(36)}`

/**
 * Adds one entry.
 * @param {{text: string, direction: 'to'|'from', target: {kind: 'vm'|'host', name: string}}} entry
 * @param {number} [now] for tests
 * @returns {object|null} the stored entry, or null when nothing was added
 */
export function record({ text, direction, target }, now = Date.now()) {
	if (typeof text !== 'string' || !text || !target) return null
	const stored = text.length > MAX_STORED_CHARS ? text.slice(0, MAX_STORED_CHARS) : text
	const sameTarget = (i) => i.target.kind === target.kind && i.target.name === target.name

	if (direction === 'from') {
		const echo = state.items.find((i) => i.direction === 'to' && sameTarget(i) && i.text === stored && now - i.at < ECHO_WINDOW_MS)
		if (echo) return null
	}
	// The same text again from the same place moves to the top instead of
	// piling up duplicates.
	const dup = state.items.findIndex((i) => i.direction === direction && sameTarget(i) && i.text === stored)
	if (dup !== -1) state.items.splice(dup, 1)

	const item = {
		id: newId(now),
		text: stored,
		truncated: stored.length < text.length,
		direction,
		target: { kind: target.kind, name: target.name },
		at: now,
	}
	state.items.unshift(item)
	if (state.items.length > MAX_ITEMS) state.items.splice(MAX_ITEMS)
	save()
	return item
}

export function remove(id) {
	const i = state.items.findIndex((x) => x.id === id)
	if (i !== -1) {
		state.items.splice(i, 1)
		save()
	}
}

export function clear() {
	state.items.splice(0)
	save()
}

export function items() {
	return state.items
}

// Keysyms for typing text as key presses (for login screens, or machines
// without a clipboard agent). Latin-1 maps 1:1; anything else uses the
// X11 Unicode keysym range.
const ENTER = 0xff0d
const TAB = 0xff09

export function keysymFor(char) {
	if (char === '\n') return ENTER
	if (char === '\t') return TAB
	const cp = char.codePointAt(0)
	return cp <= 0xff ? cp : 0x01000000 + cp
}

// Longer texts take a while to type and are almost always meant for the
// clipboard instead.
export const MAX_TYPED_CHARS = 5000

/** Types text into an RFB connection; \r\n and \r count as one Enter. */
export function typeText(rfb, text) {
	if (!rfb || !text) return 0
	const normalized = text.replace(/\r\n?/g, '\n').slice(0, MAX_TYPED_CHARS)
	let n = 0
	for (const char of normalized) {
		const keysym = keysymFor(char)
		const code = char === '\n' ? 'Enter' : char === '\t' ? 'Tab' : null
		rfb.sendKey(keysym, code, true)
		rfb.sendKey(keysym, code, false)
		n++
	}
	return n
}
