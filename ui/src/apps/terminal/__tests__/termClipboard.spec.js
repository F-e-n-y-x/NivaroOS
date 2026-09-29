import { describe, test, expect, vi } from 'vitest'
import { copyText, execCopy, readClipboard, canReadClipboard, pasteLineCount, pasteNeedsConfirm, pastePreview } from '../termClipboard'

// A document whose execCommand('copy') dispatches a copy event the way a
// browser does.
function fakeDoc({ execResult = true, fireEvent = true } = {}) {
	const listeners = []
	const clipboard = {}
	return {
		clipboard,
		addEventListener: (t, fn) => { if (t === 'copy') listeners.push(fn) },
		removeEventListener: (t, fn) => { const i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1) },
		listeners,
		execCommand: vi.fn((cmd) => {
			if (cmd !== 'copy') return false
			if (fireEvent) {
				const ev = { clipboardData: { setData: (type, v) => { clipboard[type] = v } }, preventDefault: vi.fn() }
				listeners.slice().forEach((fn) => fn(ev))
			}
			return execResult
		}),
	}
}

describe('copy', () => {
	test('insecure context (http on a LAN IP): execCommand fallback feeds the copy event', async () => {
		const document = fakeDoc()
		const env = { isSecureContext: false, navigator: {}, document }
		await expect(copyText('ls -la', env)).resolves.toBe(true)
		expect(document.clipboard['text/plain']).toBe('ls -la')
		expect(document.execCommand).toHaveBeenCalledWith('copy')
		expect(document.listeners).toHaveLength(0) // listener removed again
	})

	test('secure context uses the async clipboard API', async () => {
		const writeText = vi.fn(() => Promise.resolve())
		const document = fakeDoc()
		const env = { isSecureContext: true, navigator: { clipboard: { writeText } }, document }
		await expect(copyText('hi', env)).resolves.toBe(true)
		expect(writeText).toHaveBeenCalledWith('hi')
		expect(document.execCommand).not.toHaveBeenCalled()
	})

	test('a refused async write falls back to execCommand', async () => {
		const document = fakeDoc()
		const env = { isSecureContext: true, navigator: { clipboard: { writeText: () => Promise.reject(new Error('denied')) } }, document }
		await expect(copyText('x', env)).resolves.toBe(true)
		expect(document.clipboard['text/plain']).toBe('x')
	})

	test('reports failure when nothing reached the clipboard', async () => {
		expect(execCopy('x', { document: fakeDoc({ execResult: false }) })).toBe(false)
		expect(execCopy('x', { document: fakeDoc({ fireEvent: false }) })).toBe(false)
		const throwing = fakeDoc()
		throwing.execCommand = () => { throw new Error('nope') }
		expect(execCopy('x', { document: throwing })).toBe(false)
		await expect(copyText('', { document: fakeDoc() })).resolves.toBe(false)
	})
})

describe('paste', () => {
	test('reading the clipboard needs a secure context', async () => {
		expect(canReadClipboard({ isSecureContext: false, navigator: { clipboard: { readText: () => Promise.resolve('a') } } })).toBe(false)
		await expect(readClipboard({ isSecureContext: false, navigator: {} })).resolves.toBeNull()
		await expect(readClipboard({ isSecureContext: true, navigator: { clipboard: { readText: () => Promise.resolve('echo hi') } } })).resolves.toBe('echo hi')
		await expect(readClipboard({ isSecureContext: true, navigator: { clipboard: { readText: () => Promise.reject(new Error('denied')) } } })).resolves.toBeNull()
	})

	test('line counting ignores one trailing newline and handles CRLF', () => {
		expect(pasteLineCount('')).toBe(0)
		expect(pasteLineCount('ls')).toBe(1)
		expect(pasteLineCount('ls\n')).toBe(1)
		expect(pasteLineCount('a\r\nb\r\n')).toBe(2)
		expect(pasteLineCount('a\nb\nc')).toBe(3)
	})

	test('multi-line warning only when the shell would run the lines', () => {
		expect(pasteNeedsConfirm('a\nb')).toBe(true)
		expect(pasteNeedsConfirm('a\nb', { bracketed: true })).toBe(false)
		expect(pasteNeedsConfirm('a\nb', { disabled: true })).toBe(false)
		expect(pasteNeedsConfirm('echo one line\n')).toBe(false)
	})

	test('preview is capped', () => {
		const text = Array.from({ length: 10 }, (_, i) => 'line ' + i).join('\n')
		const p = pastePreview(text, 3)
		expect(p.lines).toEqual(['line 0', 'line 1', 'line 2'])
		expect(p.more).toBe(7)
		expect(pastePreview('x'.repeat(300), 6, 10).lines[0]).toHaveLength(10)
	})
})
