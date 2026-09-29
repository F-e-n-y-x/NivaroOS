// Keyboard shortcuts of the web terminal, as a desktop terminal has them
// (GNOME Terminal, Konsole, Windows Terminal, VS Code):
//
//   Ctrl+Shift+C   copy the selection      (Cmd+C on a Mac)
//   Ctrl+Shift+V   paste                    (Cmd+V on a Mac)
//   Ctrl+Shift+F   find in the scrollback   (Cmd+F)
//   Ctrl+Shift+A   select all               (Cmd+A)
//   Ctrl+Insert / Shift+Insert   copy / paste (the X11 pair)
//   Ctrl+= / Ctrl+- / Ctrl+0     text size
//
// Plain Ctrl+C, Ctrl+V, Ctrl+F, Ctrl+A... are left alone: they belong to
// the shell (SIGINT, readline's quoted-insert, forward-char, line start).
//
// These chords are caught on window in the CAPTURE phase, before xterm and
// before anything else on the page sees them, and their default is
// prevented - so Chrome's own Ctrl+Shift+C (DevTools "inspect element"),
// Ctrl+Shift+F and friends do not fire while the terminal has focus.
// Chrome gives pages the first look at every shortcut except the few it
// reserves for itself (Ctrl+T/N/W, Ctrl+Shift+T/N/W, Ctrl+Tab...), which
// no page can block; the DevTools shortcuts are not among them.
//
// Paste is the exception to "prevent the default": the browser's own
// paste (Ctrl+Shift+V is "paste as plain text" in Chrome and Firefox) is
// what fires a `paste` event with the clipboard text, and that works over
// plain http on a LAN IP, where navigator.clipboard does not exist and
// without the "allow this site to read the clipboard?" prompt. So for
// paste the key is only kept away from xterm (stopPropagation) and the
// resulting paste event is picked up by the terminal.

export const ACTIONS = ['copy', 'paste', 'find', 'selectAll', 'clear', 'zoomIn', 'zoomOut', 'zoomReset']

export function isMacPlatform(nav) {
	const n = nav || (typeof navigator !== 'undefined' ? navigator : null)
	if (!n) return false
	const p = (n.userAgentData && n.userAgentData.platform) || n.platform || ''
	return /mac|iphone|ipad|ipod/i.test(p)
}

// The letter a chord was typed with. e.key is layout-aware ("c" on QWERTY
// and on Dvorak's c), but on a non-Latin layout it is e.g. Cyrillic "с";
// the physical key (e.code) is the fallback then, like desktop terminals do.
function letterOf(e) {
	const k = (e.key || '').toLowerCase()
	if (/^[a-z]$/.test(k)) return k
	const code = e.code || ''
	if (/^Key[A-Z]$/.test(code)) return code.slice(3).toLowerCase()
	return k
}

// terminalShortcut(keyboardEvent, { mac }) -> { action, preventDefault } or
// null when the key belongs to the shell.
export function terminalShortcut(e, opts = {}) {
	if (!e || (e.type && e.type !== 'keydown')) return null
	const mac = opts.mac === undefined ? isMacPlatform() : !!opts.mac
	const ctrl = !!e.ctrlKey
	const meta = !!e.metaKey
	const shift = !!e.shiftKey
	const alt = !!e.altKey
	const key = e.key || ''
	const letter = letterOf(e)
	const hit = (action) => ({ action, preventDefault: action !== 'paste' })

	// Ctrl+Shift+<letter>: the Linux/Windows terminal chords. Also honoured
	// on a Mac (some people use a PC keyboard there).
	if (ctrl && shift && !alt && !meta) {
		if (letter === 'c') return hit('copy')
		if (letter === 'v') return hit('paste')
		if (letter === 'f') return hit('find')
		if (letter === 'a') return hit('selectAll')
	}
	// Cmd on a Mac never reaches the shell anyway (Ctrl is its Ctrl).
	if (mac && meta && !ctrl && !alt && !shift) {
		if (letter === 'c') return hit('copy')
		if (letter === 'v') return hit('paste')
		if (letter === 'f') return hit('find')
		if (letter === 'a') return hit('selectAll')
		if (letter === 'k') return hit('clear')
	}
	if (key === 'Insert' && !alt && !meta) {
		if (ctrl && !shift) return hit('copy')
		if (shift && !ctrl) return hit('paste')
	}
	// Text size. Shift is allowed for "+" (Shift+= on US keyboards).
	const zoomMod = mac ? (meta && !ctrl) : (ctrl && !meta)
	if (zoomMod && !alt) {
		if (key === '=' || key === '+' || e.code === 'NumpadAdd') return hit('zoomIn')
		if (!shift && (key === '-' || e.code === 'NumpadSubtract')) return hit('zoomOut')
		if (!shift && (key === '0' || e.code === 'Numpad0')) return hit('zoomReset')
	}
	return null
}

// How a shortcut is shown in menus and tooltips on this platform.
export function shortcutLabel(action, opts = {}) {
	const mac = opts.mac === undefined ? isMacPlatform() : !!opts.mac
	const pc = { copy: 'Ctrl+Shift+C', paste: 'Ctrl+Shift+V', find: 'Ctrl+Shift+F', selectAll: 'Ctrl+Shift+A', zoomIn: 'Ctrl+=', zoomOut: 'Ctrl+-', zoomReset: 'Ctrl+0' }
	const macs = { copy: '⌘C', paste: '⌘V', find: '⌘F', selectAll: '⌘A', clear: '⌘K', zoomIn: '⌘=', zoomOut: '⌘-', zoomReset: '⌘0' }
	return (mac ? macs : pc)[action] || ''
}

// The window capture-phase keydown handler of one terminal. `inside(target)`
// says whether the key was typed into this terminal; `run(action)` performs
// it. Returns the action taken, or null when the key was left alone.
export function guardTerminalKeydown(e, { inside, run, mac } = {}) {
	if (!e || !inside || !inside(e.target)) return null
	const hit = terminalShortcut(e, { mac })
	if (!hit) return null
	// Nothing else on the page (xterm included) sees the chord...
	e.stopPropagation()
	// ...and the browser doesn't act on it - except paste, whose default is
	// the paste event that carries the clipboard text.
	if (hit.preventDefault) e.preventDefault()
	if (run) run(hit.action)
	return hit.action
}
