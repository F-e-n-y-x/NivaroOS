// Clipboard for the web terminal, working over plain http too.
//
// navigator.clipboard only exists in a secure context (https or
// localhost). NivaroOS is very often opened as http://192.168.x.y, where
// it is undefined, so:
//  - copy falls back to document.execCommand('copy'), feeding the text
//    through the resulting `copy` event (no hidden textarea, so the
//    terminal keeps its focus and cursor). It works in a user gesture
//    (a key press, a click), which is always the case here.
//  - paste from the keyboard never needs the API: the browser's own paste
//    fires a `paste` event (see termKeys.js). Paste from a menu click can
//    only use navigator.clipboard.readText(); without it the caller tells
//    the user to press Ctrl+Shift+V instead.

function envOf(env) {
	return env || (typeof window !== 'undefined' ? window : {})
}

export function execCopy(text, env) {
	const doc = envOf(env).document
	if (!doc || typeof doc.execCommand !== 'function') return false
	let fed = false
	const onCopy = (e) => {
		if (!e.clipboardData) return
		e.clipboardData.setData('text/plain', text)
		e.preventDefault()
		fed = true
	}
	doc.addEventListener('copy', onCopy, true)
	let ok = false
	try {
		ok = !!doc.execCommand('copy')
	} catch (err) {
		ok = false
	} finally {
		doc.removeEventListener('copy', onCopy, true)
	}
	return ok && fed
}

// Resolves true when the text reached the clipboard.
export function copyText(text, env) {
	const w = envOf(env)
	if (!text) return Promise.resolve(false)
	const clip = w.navigator && w.navigator.clipboard
	if (w.isSecureContext && clip && typeof clip.writeText === 'function') {
		return clip.writeText(text).then(() => true, () => execCopy(text, w))
	}
	return Promise.resolve(execCopy(text, w))
}

export function canReadClipboard(env) {
	const w = envOf(env)
	const clip = w.navigator && w.navigator.clipboard
	return !!(w.isSecureContext && clip && typeof clip.readText === 'function')
}

// Resolves to the clipboard text, or null when this page can't read it
// (insecure context, permission denied) - the caller then asks for the
// keyboard shortcut.
export function readClipboard(env) {
	const w = envOf(env)
	if (!canReadClipboard(w)) return Promise.resolve(null)
	return w.navigator.clipboard.readText().then((t) => (typeof t === 'string' ? t : ''), () => null)
}

// Number of lines a paste would enter. A single trailing newline doesn't
// start another line.
export function pasteLineCount(text) {
	if (!text) return 0
	const norm = String(text).replace(/\r\n?/g, '\n').replace(/\n$/, '')
	return norm.split('\n').length
}

// A multi-line paste into a shell runs each line as it arrives - unless the
// shell turned on bracketed paste (bash 5.1+, zsh, fish do), which makes it
// wait for Enter. Only the first case is worth a warning.
export function pasteNeedsConfirm(text, { bracketed = false, disabled = false } = {}) {
	if (disabled || bracketed) return false
	return pasteLineCount(text) > 1
}

// First lines of a paste, for the confirmation's preview.
export function pastePreview(text, maxLines = 6, maxCols = 120) {
	const lines = String(text || '').replace(/\r\n?/g, '\n').replace(/\n$/, '').split('\n')
	const shown = lines.slice(0, maxLines).map((l) => (l.length > maxCols ? l.slice(0, maxCols - 1) + '…' : l))
	return { lines: shown, more: Math.max(0, lines.length - shown.length) }
}
