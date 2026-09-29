import { describe, it, expect, vi } from 'vitest'
import {
	parseFrameHeader,
	wsUrl,
	modsOf,
	shortcutOf,
	keyMessage,
	wheelDelta,
	InputCoalescer,
	looksLikeUrl,
	normalizeAddress,
	splitForDisplay,
	buildContextMenu,
	stepMenu,
	copyText,
	RbSession,
	FRAME_JPEG,
	FRAME_CLIP_PNG
} from './rbClient'

function header(kind, tab, frame, w, h, q, cssW, cssH, payload = [0xff, 0xd8]) {
	const b = new Uint8Array(18 + payload.length)
	const dv = new DataView(b.buffer)
	b[0] = kind
	dv.setUint32(1, tab)
	dv.setUint32(5, frame)
	dv.setUint16(9, w)
	dv.setUint16(11, h)
	b[13] = q
	dv.setUint16(14, cssW)
	dv.setUint16(16, cssH)
	b.set(payload, 18)
	return b.buffer
}

const key = (o) => ({ key: '', code: '', keyCode: 0, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...o })

describe('frames', () => {
	it('parses the server frame header', () => {
		const f = parseFrameHeader(header(FRAME_JPEG, 7, 99, 2560, 1600, 60, 1280, 800))
		expect(f).toMatchObject({ kind: FRAME_JPEG, tab: 7, frame: 99, w: 2560, h: 1600, quality: 60, cssW: 1280, cssH: 800 })
		expect(Array.from(f.data)).toEqual([0xff, 0xd8])
	})
	it('passes a copied image through', () => {
		const f = parseFrameHeader(new Uint8Array([FRAME_CLIP_PNG, 1, 2, 3]).buffer)
		expect(f.kind).toBe(FRAME_CLIP_PNG)
		expect(Array.from(f.data)).toEqual([1, 2, 3])
	})
	it('rejects a short frame', () => {
		expect(parseFrameHeader(new Uint8Array([1, 2, 3]).buffer)).toBe(null)
	})
})

describe('addresses', () => {
	it('builds a same-origin websocket URL (works over https and tunnels)', () => {
		expect(wsUrl({ protocol: 'https:', host: 'nas.example.com' }, 'a b')).toBe('wss://nas.example.com/v1/download-station/rb/ws?token=a%20b')
		expect(wsUrl({ protocol: 'http:', host: '192.168.1.5:8080' }, '')).toBe('ws://192.168.1.5:8080/v1/download-station/rb/ws')
	})
	it('tells addresses from searches', () => {
		expect(looksLikeUrl('example.com')).toBe(true)
		expect(looksLikeUrl('192.168.1.1:8080/admin')).toBe(true)
		expect(looksLikeUrl('localhost:3000')).toBe(true)
		expect(looksLikeUrl('how to download')).toBe(false)
		expect(looksLikeUrl('file.zip')).toBe(true)
		expect(looksLikeUrl('hello')).toBe(false)
	})
	it('normalizes what was typed', () => {
		expect(normalizeAddress('example.com/x')).toBe('https://example.com/x')
		expect(normalizeAddress('192.168.1.1')).toBe('http://192.168.1.1')
		expect(normalizeAddress('nas.local:5000')).toBe('http://nas.local:5000')
		expect(normalizeAddress('http://a.com')).toBe('http://a.com')
		expect(normalizeAddress('linux iso')).toBe('https://duckduckgo.com/?q=linux%20iso')
		expect(normalizeAddress('linux iso', 'google')).toBe('https://www.google.com/search?q=linux%20iso')
		expect(normalizeAddress('  ')).toBe('')
	})
	it('splits a URL for the highlighted host display', () => {
		expect(splitForDisplay('https://www.example.com/a?b=1')).toEqual({ before: '', host: 'www.example.com', after: '/a?b=1' })
		expect(splitForDisplay('http://10.0.0.2:8080/')).toEqual({ before: 'http://', host: '10.0.0.2:8080', after: '' })
		expect(splitForDisplay('about:blank').host).toBe('')
	})
})

describe('keyboard', () => {
	it('maps browser shortcuts', () => {
		expect(shortcutOf(key({ key: 'l', ctrlKey: true }))).toBe('focusUrl')
		expect(shortcutOf(key({ key: 'F6' }))).toBe('focusUrl')
		expect(shortcutOf(key({ key: 'F5' }))).toBe('reload')
		expect(shortcutOf(key({ key: 'r', ctrlKey: true, shiftKey: true }))).toBe('hardReload')
		expect(shortcutOf(key({ key: 'ArrowLeft', altKey: true }))).toBe('back')
		expect(shortcutOf(key({ key: 't', altKey: true }))).toBe('newTab')
		expect(shortcutOf(key({ key: 'w', ctrlKey: true }))).toBe('closeTab')
		expect(shortcutOf(key({ key: 'T', ctrlKey: true, shiftKey: true }))).toBe('reopenTab')
		expect(shortcutOf(key({ key: 'Tab', ctrlKey: true }))).toBe('nextTab')
		expect(shortcutOf(key({ key: 'Tab', ctrlKey: true, shiftKey: true }))).toBe('prevTab')
		expect(shortcutOf(key({ key: 'f', ctrlKey: true }))).toBe('find')
		expect(shortcutOf(key({ key: 'c', ctrlKey: true }))).toBe('copy')
		expect(shortcutOf(key({ key: 'C', ctrlKey: true, shiftKey: true }))).toBe('copy')
		expect(shortcutOf(key({ key: 'x', ctrlKey: true }))).toBe('cut')
		expect(shortcutOf(key({ key: '=', ctrlKey: true }))).toBe('zoomIn')
		expect(shortcutOf(key({ key: '0', ctrlKey: true }))).toBe('zoomReset')
		expect(shortcutOf(key({ key: 'F12' }))).toBe('swallow')
		expect(shortcutOf(key({ key: 'I', ctrlKey: true, shiftKey: true }))).toBe('swallow')
		expect(shortcutOf(key({ key: 'F10', shiftKey: true }))).toBe('contextMenu')
		// Everything else goes to the page, Ctrl+V included (its paste event
		// carries the text without a clipboard permission prompt).
		expect(shortcutOf(key({ key: 'v', ctrlKey: true }))).toBe(null)
		expect(shortcutOf(key({ key: 'a', ctrlKey: true }))).toBe(null)
		expect(shortcutOf(key({ key: 'a' }))).toBe(null)
		expect(shortcutOf(key({ key: 'Backspace' }))).toBe(null)
	})
	it('builds key messages', () => {
		expect(keyMessage(key({ key: 'a', code: 'KeyA', keyCode: 65 }), 'down')).toMatchObject({ t: 'key', type: 'down', key: 'a', text: 'a', mods: 0 })
		expect(keyMessage(key({ key: 'a', keyCode: 65, ctrlKey: true }), 'down').text).toBe('')
		expect(keyMessage(key({ key: 'Enter', keyCode: 13 }), 'down').text).toBe('\r')
		expect(keyMessage(key({ key: 'A', keyCode: 65, shiftKey: true }), 'down')).toMatchObject({ text: 'A', mods: 8 })
		expect(keyMessage(key({ key: 'a', keyCode: 65 }), 'up').text).toBe('')
		// Composition belongs to the IME events.
		expect(keyMessage(key({ key: 'Process', keyCode: 229 }), 'down')).toBe(null)
		expect(keyMessage(key({ key: 'x', isComposing: true }), 'down')).toBe(null)
		expect(modsOf(key({ altKey: true, ctrlKey: true, metaKey: true, shiftKey: true }))).toBe(15)
	})
})

describe('pointer', () => {
	it('converts wheel modes to pixels', () => {
		expect(wheelDelta({ deltaMode: 0, deltaX: 0, deltaY: 53 })).toEqual({ dx: 0, dy: 53 })
		expect(wheelDelta({ deltaMode: 1, deltaX: 1, deltaY: 3 })).toEqual({ dx: 40, dy: 120 })
		expect(wheelDelta({ deltaMode: 2, deltaX: 0, deltaY: 1 }, 700)).toEqual({ dx: 0, dy: 700 })
	})
	it('coalesces moves and wheel per frame, and flushes a move before a click', () => {
		const sent = []
		const c = new InputCoalescer(m => sent.push(m))
		c.queueMove({ type: 'move', x: 1, y: 1 })
		c.queueMove({ type: 'move', x: 5, y: 5 })
		c.queueWheel({ type: 'wheel', x: 5, y: 5, dx: 0, dy: 10 })
		c.queueWheel({ type: 'wheel', x: 6, y: 6, dx: 2, dy: 30 })
		c.flush()
		expect(sent).toEqual([{ type: 'move', x: 5, y: 5 }, { type: 'wheel', x: 6, y: 6, dx: 2, dy: 40 }])
		c.queueMove({ type: 'move', x: 9, y: 9 })
		c.flushMove()
		c.flush()
		expect(sent.length).toBe(3)
	})
})

describe('context menu', () => {
	const ids = sections => sections.flat().map(i => i.id)
	it('offers link actions, including Download Station', () => {
		const m = buildContextMenu({ link: 'https://a.com/f.zip', linkText: 'Get it' }, { canBack: true })
		expect(ids(m)).toEqual(expect.arrayContaining(['openLinkNewTab', 'openLinkBackground', 'copyLink', 'copyLinkText', 'downloadLink', 'back', 'forward', 'reload', 'copyPageAddress']))
		expect(m.flat().find(i => i.id === 'forward').disabled).toBe(true)
		expect(m.flat().find(i => i.id === 'back').disabled).toBe(false)
	})
	it('only shows the sections that apply', () => {
		const plain = ids(buildContextMenu({}, {}))
		expect(plain).not.toContain('copyLink')
		expect(plain).not.toContain('copyImage')
		expect(plain).not.toContain('paste')
		const img = ids(buildContextMenu({ image: 'https://a.com/x.png', imageRect: [0, 0, 10, 10] }, {}))
		expect(img).toEqual(expect.arrayContaining(['openImageNewTab', 'copyImage', 'copyImageAddress', 'saveImage']))
		expect(ids(buildContextMenu({ image: 'data:image/png;base64,xx' }, {}))).not.toContain('saveImage')
		expect(ids(buildContextMenu({ media: 'blob:https://a.com/1' }, {}))).not.toContain('downloadMedia')
		expect(ids(buildContextMenu({ media: 'https://a.com/v.mp4', mediaKind: 'video' }, {}))).toContain('downloadMedia')
	})
	it('editable fields get edit actions; selections get copy and search', () => {
		const ed = buildContextMenu({ editable: true }, {}).flat()
		expect(ed.find(i => i.id === 'paste').disabled).toBeFalsy()
		expect(ed.find(i => i.id === 'copy').disabled).toBe(true)
		const sel = buildContextMenu({ selection: 'example.com' }, {}).flat()
		expect(sel.map(i => i.id)).toEqual(expect.arrayContaining(['copy', 'openSelection', 'searchGoogle', 'searchDuck']))
		const long = buildContextMenu({ selection: 'a much longer piece of selected text here' }, {}).flat()
		expect(long.find(i => i.id === 'searchGoogle').label).toContain('…')
		expect(long.map(i => i.id)).not.toContain('openSelection')
	})
	it('steps over disabled items with the keyboard', () => {
		const items = [{ id: 'a' }, { id: 'b', disabled: true }, { id: 'c' }]
		expect(stepMenu(items, 0, 1)).toBe(2)
		expect(stepMenu(items, 2, 1)).toBe(0)
		expect(stepMenu(items, 0, -1)).toBe(2)
		expect(stepMenu([{ disabled: true }], 0, 1)).toBe(-1)
	})
})

describe('clipboard', () => {
	it('uses the async clipboard when there is one', async () => {
		const writeText = vi.fn(() => Promise.resolve())
		expect(await copyText('hi', null, { clipboard: { writeText } })).toBe(true)
		expect(writeText).toHaveBeenCalledWith('hi')
	})
	it('falls back to execCommand on plain http', async () => {
		const appended = []
		const doc = {
			activeElement: null,
			createElement: () => ({ style: {}, setAttribute() {}, select() {} }),
			body: { appendChild: e => appended.push(e), removeChild: () => {} },
			execCommand: vi.fn(() => true)
		}
		expect(await copyText('hi', doc, {})).toBe(true)
		expect(doc.execCommand).toHaveBeenCalledWith('copy')
		expect(appended[0].value).toBe('hi')
	})
})

class FakeWS {
	constructor(url) {
		this.url = url
		this.sent = []
		this.readyState = 0
		FakeWS.last = this
	}
	open() {
		this.readyState = 1
		this.onopen()
	}
	send(d) {
		this.sent.push(JSON.parse(d))
	}
	close() {
		this.readyState = 3
		this.onclose && this.onclose({})
	}
}

describe('session', () => {
	it('says hello on open, parses messages and frames, and reconnects', () => {
		vi.useFakeTimers()
		const msgs = []
		const frames = []
		const states = []
		const s = new RbSession({
			url: () => 'ws://x/rb/ws',
			hello: () => ({ w: 800, h: 600, dpr: 1 }),
			onMessage: m => msgs.push(m),
			onFrame: f => frames.push(f),
			onState: st => states.push(st),
			WebSocketImpl: FakeWS
		})
		s.connect()
		const ws = FakeWS.last
		ws.open()
		expect(ws.sent[0]).toEqual({ t: 'hello', w: 800, h: 600, dpr: 1 })
		ws.onmessage({ data: '{"t":"tabs","tabs":[]}' })
		ws.onmessage({ data: header(FRAME_JPEG, 1, 1, 10, 10, 80, 10, 10) })
		expect(msgs[0].t).toBe('tabs')
		expect(frames[0].tab).toBe(1)
		// Dropped connection: retried with backoff.
		ws.readyState = 3
		ws.onclose({})
		expect(states).toContain('reconnecting')
		vi.advanceTimersByTime(600)
		expect(FakeWS.last).not.toBe(ws)
		s.close()
		vi.useRealTimers()
	})
	it('does not reconnect after a fatal message', () => {
		const states = []
		const s = new RbSession({ url: 'ws://x', hello: {}, onState: st => states.push(st), WebSocketImpl: FakeWS })
		s.connect()
		const ws = FakeWS.last
		ws.open()
		ws.onmessage({ data: '{"t":"fatal","reason":"busy"}' })
		ws.readyState = 3
		ws.onclose({})
		expect(states[states.length - 1]).toBe('fatal')
		expect(FakeWS.last).toBe(ws)
	})
})
