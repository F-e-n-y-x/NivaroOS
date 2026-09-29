import { describe, test, expect, vi } from 'vitest'
import { terminalShortcut, shortcutLabel, isMacPlatform, guardTerminalKeydown } from '../termKeys'

const key = (k, mods = {}, code) => ({ type: 'keydown', key: k, code: code || '', ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...mods })
const pc = { mac: false }
const mac = { mac: true }

describe('terminal shortcuts (PC)', () => {
	test('Ctrl+Shift+C copies and blocks the browser default (DevTools inspect)', () => {
		expect(terminalShortcut(key('C', { ctrlKey: true, shiftKey: true }, 'KeyC'), pc)).toEqual({ action: 'copy', preventDefault: true })
	})

	test("Ctrl+Shift+V pastes but keeps the browser's paste (it fires the paste event)", () => {
		expect(terminalShortcut(key('V', { ctrlKey: true, shiftKey: true }, 'KeyV'), pc)).toEqual({ action: 'paste', preventDefault: false })
	})

	test('Ctrl+Shift+F finds, Ctrl+Shift+A selects all', () => {
		expect(terminalShortcut(key('F', { ctrlKey: true, shiftKey: true }), pc).action).toBe('find')
		expect(terminalShortcut(key('A', { ctrlKey: true, shiftKey: true }), pc).action).toBe('selectAll')
	})

	test('plain Ctrl+C / Ctrl+V / Ctrl+F / Ctrl+A stay with the shell (SIGINT, quoted-insert...)', () => {
		for (const k of ['c', 'v', 'f', 'a', 'd', 'r', 'l', 'w']) {
			expect(terminalShortcut(key(k, { ctrlKey: true }), pc)).toBeNull()
		}
	})

	test('Ctrl+Alt+Shift+C is not a copy (AltGr layouts type characters with Ctrl+Alt)', () => {
		expect(terminalShortcut(key('C', { ctrlKey: true, shiftKey: true, altKey: true }), pc)).toBeNull()
	})

	test('non-Latin layout: falls back to the physical key', () => {
		expect(terminalShortcut(key('С', { ctrlKey: true, shiftKey: true }, 'KeyC'), pc).action).toBe('copy')
	})

	test('Ctrl+Insert / Shift+Insert', () => {
		expect(terminalShortcut(key('Insert', { ctrlKey: true }), pc).action).toBe('copy')
		expect(terminalShortcut(key('Insert', { shiftKey: true }), pc)).toEqual({ action: 'paste', preventDefault: false })
	})

	test('text size', () => {
		expect(terminalShortcut(key('=', { ctrlKey: true }), pc).action).toBe('zoomIn')
		expect(terminalShortcut(key('+', { ctrlKey: true, shiftKey: true }), pc).action).toBe('zoomIn')
		expect(terminalShortcut(key('-', { ctrlKey: true }), pc).action).toBe('zoomOut')
		expect(terminalShortcut(key('0', { ctrlKey: true }), pc).action).toBe('zoomReset')
	})

	test('only keydown counts, and ordinary typing is ignored', () => {
		expect(terminalShortcut({ ...key('C', { ctrlKey: true, shiftKey: true }), type: 'keyup' }, pc)).toBeNull()
		expect(terminalShortcut(key('c'), pc)).toBeNull()
		expect(terminalShortcut(key('C', { shiftKey: true }), pc)).toBeNull()
		expect(terminalShortcut(null, pc)).toBeNull()
	})

	test('Cmd chords mean nothing on a PC', () => {
		expect(terminalShortcut(key('c', { metaKey: true }), pc)).toBeNull()
	})
})

describe('terminal shortcuts (Mac)', () => {
	test('Cmd+C / Cmd+V / Cmd+F / Cmd+K', () => {
		expect(terminalShortcut(key('c', { metaKey: true }), mac).action).toBe('copy')
		expect(terminalShortcut(key('v', { metaKey: true }), mac)).toEqual({ action: 'paste', preventDefault: false })
		expect(terminalShortcut(key('f', { metaKey: true }), mac).action).toBe('find')
		expect(terminalShortcut(key('k', { metaKey: true }), mac).action).toBe('clear')
	})

	test('Ctrl+C is still SIGINT on a Mac', () => {
		expect(terminalShortcut(key('c', { ctrlKey: true }), mac)).toBeNull()
	})

	test('Ctrl+Shift+C still copies with a PC keyboard', () => {
		expect(terminalShortcut(key('C', { ctrlKey: true, shiftKey: true }), mac).action).toBe('copy')
	})

	test('Cmd+= zooms, Ctrl+= does not', () => {
		expect(terminalShortcut(key('=', { metaKey: true }), mac).action).toBe('zoomIn')
		expect(terminalShortcut(key('=', { ctrlKey: true }), mac)).toBeNull()
	})
})

describe('labels and platform', () => {
	test('labels', () => {
		expect(shortcutLabel('copy', pc)).toBe('Ctrl+Shift+C')
		expect(shortcutLabel('paste', mac)).toBe('⌘V')
		expect(shortcutLabel('clear', pc)).toBe('')
	})

	test('platform detection', () => {
		expect(isMacPlatform({ platform: 'MacIntel' })).toBe(true)
		expect(isMacPlatform({ userAgentData: { platform: 'Linux' }, platform: 'Linux x86_64' })).toBe(false)
		expect(isMacPlatform({ platform: 'Win32' })).toBe(false)
	})
})

describe('capture-phase guard', () => {
	const ev = (k, mods, target = 'term') => ({ ...key(k, mods), target, preventDefault: vi.fn(), stopPropagation: vi.fn() })
	const inside = (t) => t === 'term'

	test('Ctrl+Shift+C in the terminal: handled, default prevented, not propagated', () => {
		const run = vi.fn()
		const e = ev('C', { ctrlKey: true, shiftKey: true })
		expect(guardTerminalKeydown(e, { inside, run, mac: false })).toBe('copy')
		expect(e.preventDefault).toHaveBeenCalled()
		expect(e.stopPropagation).toHaveBeenCalled()
		expect(run).toHaveBeenCalledWith('copy')
	})

	test("Ctrl+Shift+V: kept from xterm, but the browser's paste still happens", () => {
		const e = ev('V', { ctrlKey: true, shiftKey: true })
		expect(guardTerminalKeydown(e, { inside, run: () => {}, mac: false })).toBe('paste')
		expect(e.stopPropagation).toHaveBeenCalled()
		expect(e.preventDefault).not.toHaveBeenCalled()
	})

	test('outside the terminal, or a shell key: untouched', () => {
		const outside = ev('C', { ctrlKey: true, shiftKey: true }, 'devtools-input')
		expect(guardTerminalKeydown(outside, { inside, run: () => {}, mac: false })).toBeNull()
		expect(outside.preventDefault).not.toHaveBeenCalled()
		const sigint = ev('c', { ctrlKey: true })
		expect(guardTerminalKeydown(sigint, { inside, run: () => {}, mac: false })).toBeNull()
		expect(sigint.preventDefault).not.toHaveBeenCalled()
		expect(sigint.stopPropagation).not.toHaveBeenCalled()
	})
})
