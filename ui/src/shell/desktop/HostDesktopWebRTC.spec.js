// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { HIDDEN_GRACE_MS, REASONS } from './streamMode'
import HostDesktopWebRTC from './HostDesktopWebRTC.vue'

let pcs = []
class FakePC {
	constructor(config) {
		this.config = config
		this.connectionState = 'new'
		this.iceGatheringState = 'complete'
		this.stats = new Map()
		this.closed = false
		pcs.push(this)
	}

	addTransceiver(kind, init) {
		this.transceiver = { kind, init }
	}

	createDataChannel(label, opts) {
		this.dc = { label, opts, readyState: 'open', sent: [], send: (m) => this.dc.sent.push(JSON.parse(m)), close: () => (this.dc.readyState = 'closed') }
		return this.dc
	}

	async createOffer() {
		return { type: 'offer', sdp: 'v=0 offer' }
	}

	async setLocalDescription(d) {
		this.localDescription = d
	}

	async setRemoteDescription(d) {
		this.remoteDescription = d
	}

	async getStats() {
		return this.stats
	}

	addEventListener() {}
	close() {
		this.closed = true
		this.connectionState = 'closed'
	}

	// Test helpers.
	frames(n) {
		this.stats.set('in', { type: 'inbound-rtp', kind: 'video', framesDecoded: n, framesPerSecond: 30 })
		this.stats.set('t', { type: 'transport', selectedCandidatePairId: 'cp' })
		this.stats.set('cp', { type: 'candidate-pair', id: 'cp', currentRoundTripTime: 0.004 })
	}

	msg(m) {
		this.dc.onmessage({ data: JSON.stringify(m) })
	}
}

const json = (status, body) => ({ status, ok: status >= 200 && status < 300, json: async () => body })
let offerReply
let fetchMock

async function flush() {
	for (let i = 0; i < 6; i++) {
		await vi.advanceTimersByTimeAsync(0)
		await nextTick()
	}
}

describe('HostDesktopWebRTC', () => {
	let app
	let el
	let events

	beforeEach(() => {
		vi.useFakeTimers()
		pcs = []
		events = []
		offerReply = json(200, { type: 'answer', sdp: 'v=0 answer' })
		fetchMock = vi.fn(async (url) => (url.endsWith('/host/stream') ? json(200, { modes: { webrtc: { available: true, encoder: 'nvh264enc', reason: '' } } }) : offerReply))
		vi.stubGlobal('fetch', fetchMock)
		vi.stubGlobal('RTCPeerConnection', FakePC)
		window.__nivaroosToken = 'jwt-from-app'
	})
	afterEach(() => {
		app && app.unmount()
		app = null
		el && el.remove()
		delete window.__nivaroosToken
		delete document.hidden // back to jsdom's own (setHidden)
		vi.unstubAllGlobals()
		vi.useRealTimers()
	})

	async function mount(props = {}) {
		el = document.createElement('div')
		document.body.appendChild(el)
		const on = (name) => (e) => events.push([name, e])
		app = createApp({
			render: () =>
				h(HostDesktopWebRTC, { ...props, onState: on('state'), onReady: on('ready'), onStats: on('stats'), onClipboard: on('clipboard'), onSize: on('size'), onAdapt: on('adapt'), onCaps: on('caps'), onCursor: on('cursor') }),
		})
		app.mount(el)
		await flush()
	}
	const states = () => events.filter(([n]) => n === 'state').map(([, e]) => e)
	const lastState = () => states().at(-1)

	test('offers with the app token, non-trickle SDP and the preset (Balanced by default); no STUN/TURN', async () => {
		await mount()
		const call = fetchMock.mock.calls.find(([u]) => u.endsWith('/v1/vm-sidecar/host/webrtc/offer'))
		expect(call[1].headers.Authorization).toBe('jwt-from-app')
		expect(JSON.parse(call[1].body)).toEqual({ sdp: 'v=0 offer', type: 'offer', preset: 'balanced' })
		expect(events.find(([n]) => n === 'caps')[1]).toMatchObject({ encoder: 'nvh264enc' })
		expect(pcs[0].config).toBeUndefined()
		expect(pcs[0].transceiver).toEqual({ kind: 'video', init: { direction: 'recvonly' } })
		expect(pcs[0].dc.label).toBe('input')
		expect(pcs[0].remoteDescription).toEqual({ type: 'answer', sdp: 'v=0 answer' })
		expect(lastState()).toEqual({ state: 'connecting', reason: '' })
	})

	test('Custom offers the raw fps and bitrate instead of a preset', async () => {
		await mount({ preset: 'custom', fps: 60, maxKbps: 20000 })
		const call = fetchMock.mock.calls.find(([u]) => u.endsWith('/host/webrtc/offer'))
		expect(JSON.parse(call[1].body)).toEqual({ sdp: 'v=0 offer', type: 'offer', fps: 60, max_kbps: 20000 })
	})

	// One second of a live stream: a frame, a heartbeat, the RTT.
	async function liveSecond(pc, i, rtt) {
		pc.frames(i)
		pc.stats.get('cp').currentRoundTripTime = rtt
		pc.msg({ t: 'hb' })
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
	}

	test('network adaptation: high RTT steps the preset down on the data channel, a stable network steps it back up', async () => {
		await mount({ preset: 'high' })
		const pc = pcs[0]
		const presets = () => pc.dc.sent.filter((m) => m.t === 'preset')
		let i = 1
		for (; i <= 3; i++) await liveSecond(pc, i, 0.004)
		expect(presets()).toEqual([])
		for (const end = i + 3; i < end; i++) await liveSecond(pc, i, 0.4) // 400 ms RTT
		expect(presets()).toEqual([{ t: 'preset', preset: 'balanced' }])
		expect(events.filter(([n]) => n === 'adapt').map(([, a]) => a)).toEqual([{ preset: 'balanced', dir: 'down' }])
		pc.msg({ t: 'enc', encoder: 'nvh264enc', preset: 'balanced', fps: 30 })
		await liveSecond(pc, i++, 0.004)
		expect(events.filter(([n]) => n === 'stats').at(-1)[1]).toMatchObject({ encoder: 'nvh264enc', preset: 'balanced' })
		// Still bad right after the change: it settles 10 s first (no step to Low yet).
		for (const end = i + 5; i < end; i++) await liveSecond(pc, i, 0.4)
		expect(presets()).toHaveLength(1)
		// 30 good seconds: back to High, never above it.
		for (const end = i + 45; i < end; i++) await liveSecond(pc, i, 0.004)
		expect(presets()).toEqual([
			{ t: 'preset', preset: 'balanced' },
			{ t: 'preset', preset: 'high' },
		])
		expect(events.filter(([n]) => n === 'adapt').at(-1)[1]).toEqual({ preset: 'high', dir: 'up' })
		expect(pcs).toHaveLength(1) // the same peer throughout
	})

	// The tab: hidden or shown (jsdom's document.hidden is fixed otherwise).
	function setHidden(v) {
		Object.defineProperty(document, 'hidden', { configurable: true, get: () => v })
		document.dispatchEvent(new Event('visibilitychange'))
	}

	test('switching tabs keeps the same peer: hidden, no frames (throttled) is not frozen; shown, it goes on', async () => {
		await mount()
		const pc = pcs[0]
		await liveSecond(pc, 1, 0.004)
		expect(lastState().state).toBe('live')
		setHidden(true)
		// 30 s in a background tab: heartbeats arrive, decoded frames don't.
		for (let i = 0; i < 30; i++) {
			pc.msg({ t: 'hb' })
			await vi.advanceTimersByTimeAsync(1000)
			await flush()
		}
		setHidden(false)
		await flush()
		await liveSecond(pc, 2, 0.004)
		expect(pcs).toHaveLength(1)
		expect(pc.closed).toBe(false)
		expect(states().filter((s) => s.state !== 'live')).toEqual([{ state: 'connecting', reason: '' }]) // only the first one
		expect(fetchMock.mock.calls.filter(([u]) => u.endsWith('/offer'))).toHaveLength(1)
		setHidden(false)
	})

	test('hidden past the grace: the stream stops, says paused when shown, and a click resumes it', async () => {
		await mount()
		const pc = pcs[0]
		await liveSecond(pc, 1, 0.004)
		setHidden(true)
		for (let t = 0; t < HIDDEN_GRACE_MS; t += 2000) {
			pc.msg({ t: 'hb' }) // the server's, every 2 s
			await vi.advanceTimersByTimeAsync(2000)
		}
		await flush()
		expect(pc.closed).toBe(true)
		expect(lastState()).toEqual({ state: 'paused', reason: REASONS.paused })
		setHidden(false)
		await flush()
		expect(lastState()).toEqual({ state: 'paused', reason: REASONS.paused })
		expect(pcs).toHaveLength(1) // no offer until asked
		el.querySelector('video').dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, cancelable: true, buttons: 1 }))
		await flush()
		expect(pcs).toHaveLength(2)
		expect(lastState().state).toBe('connecting')
		await liveSecond(pcs[1], 1, 0.004)
		expect(lastState().state).toBe('live')
	})

	test('adaptation off, or Custom: the preset never changes', async () => {
		await mount({ preset: 'high', adapt: false })
		for (let i = 1; i < 8; i++) await liveSecond(pcs[0], i, 0.4)
		expect(pcs[0].dc.sent.filter((m) => m.t === 'preset')).toEqual([])
		app.unmount()
		el.remove()
		await mount({ preset: 'custom' })
		for (let i = 1; i < 8; i++) await liveSecond(pcs.at(-1), i, 0.4)
		expect(pcs.at(-1).dc.sent.filter((m) => m.t === 'preset')).toEqual([])
	})

	test('Auto: connected but no first frame within 4 s falls back with a reason and closes the peer', async () => {
		await mount({ fallback: true })
		pcs[0].connectionState = 'connected' // seen by the 1 s tick
		await vi.advanceTimersByTimeAsync(5000)
		await flush()
		expect(lastState().state).toBe('connecting')
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(lastState()).toEqual({ state: 'fallback', reason: REASONS.noVideo })
		expect(pcs[0].closed).toBe(true)
		expect(pcs).toHaveLength(1)
	})

	test('no WebRTC on the server, or the offer refused: fallback with its reason', async () => {
		fetchMock.mockImplementationOnce(async () => json(200, { modes: { webrtc: { available: false, reason: 'No H.264 encoder' } } }))
		await mount()
		expect(lastState()).toEqual({ state: 'fallback', reason: 'No H.264 encoder' })
		expect(pcs).toHaveLength(0)
		app.unmount()
		el.remove()
		events = []
		offerReply = json(503, { error_code: 'webrtc_unavailable', reason: 'Encoder failed to start' })
		await mount()
		expect(lastState()).toEqual({ state: 'fallback', reason: 'Encoder failed to start' })
	})

	test('the server says every encoder failed: Auto falls back, Smooth retries', async () => {
		await mount({ fallback: true })
		pcs[0].msg({ t: 'err', code: 'encoder_failed' })
		await flush()
		expect(lastState()).toEqual({ state: 'fallback', reason: REASONS.encoder })
		app.unmount()
		el.remove()
		events = []
		await mount({ fallback: false })
		pcs.at(-1).msg({ t: 'err', code: 'encoder_failed' })
		await flush()
		expect(lastState()).toEqual({ state: 'reconnecting', reason: REASONS.encoder })
	})

	test('a slow connection setup gets 8 s before Auto gives up', async () => {
		await mount({ fallback: true })
		await vi.advanceTimersByTimeAsync(7000)
		await flush()
		expect(lastState().state).toBe('connecting')
		await vi.advanceTimersByTimeAsync(2000)
		await flush()
		expect(lastState()).toEqual({ state: 'fallback', reason: REASONS.noVideo })
	})

	test('Smooth: no first frame re-offers with backoff instead of falling back', async () => {
		await mount({ fallback: false })
		pcs[0].connectionState = 'connected'
		await vi.advanceTimersByTimeAsync(6000)
		await flush()
		expect(lastState()).toEqual({ state: 'reconnecting', reason: REASONS.noVideo })
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(pcs).toHaveLength(2)
		expect(pcs[0].closed).toBe(true)
	})

	test('live, then heartbeats stop: re-offers; stats report encoder, fps and RTT', async () => {
		await mount()
		pcs[0].frames(1)
		pcs[0].msg({ t: 'hb' })
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(lastState()).toEqual({ state: 'live', reason: '' })
		expect(events.filter(([n]) => n === 'stats').at(-1)[1]).toEqual({ encoder: 'nvh264enc', fps: 30, rttMs: 4, preset: 'balanced' })
		// Frames keep coming, heartbeats don't.
		for (let i = 2; i < 9; i++) {
			pcs[0].frames(i)
			await vi.advanceTimersByTimeAsync(1000)
			await flush()
		}
		expect(states().some((s) => s.state === 'reconnecting' && s.reason === REASONS.heartbeat)).toBe(true)
		expect(pcs).toHaveLength(2)
		expect(pcs[0].closed).toBe(true)
	})

	// Owner bug (2026-10-16): a preset switch, resize or PLI restarts the
	// server's encoder (NVENC: a second or two without frames) and the
	// viewer took that pause for a freeze and re-offered.
	test("the server's encoder restart (enc) is not a freeze", async () => {
		await mount({ fallback: true })
		pcs[0].frames(1)
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		// Last frame at 1 s; without the enc at 3 s this re-offers at 6 s.
		for (let i = 0; i < 6; i++) {
			pcs[0].msg({ t: 'hb' })
			if (i === 2) pcs[0].msg({ t: 'enc', encoder: 'nvh264enc', preset: 'high', fps: 60, max_kbps: 25000, w: 3398, h: 1220 })
			await vi.advanceTimersByTimeAsync(1000)
			await flush()
		}
		expect(states().find((s) => s.state === 'reconnecting')).toBeUndefined()
		expect(pcs).toHaveLength(1)
		pcs[0].msg({ t: 'hb' })
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(states().find((s) => s.state === 'reconnecting')).toEqual({ state: 'reconnecting', reason: REASONS.frozen })
	})

	test('live, heartbeats fine but the picture freezes for 4 s: re-offers (even in Auto, no fallback)', async () => {
		await mount({ fallback: true })
		pcs[0].frames(1)
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(lastState().state).toBe('live')
		for (let i = 0; i < 5; i++) {
			pcs[0].msg({ t: 'hb' })
			await vi.advanceTimersByTimeAsync(1000)
			await flush()
		}
		expect(states().find((s) => s.state === 'reconnecting')).toEqual({ state: 'reconnecting', reason: REASONS.frozen })
		expect(states().some((s) => s.state === 'fallback')).toBe(false)
		expect(pcs).toHaveLength(2)
	})

	test('input messages: pointer in remote pixels with RFB mask, wheel, keys, clipboard, release on close', async () => {
		await mount()
		const dc = pcs[0].dc
		pcs[0].msg({ t: 'size', w: 1920, h: 1080 })
		expect(events.find(([n]) => n === 'size')[1]).toEqual({ w: 1920, h: 1080 })
		const video = el.querySelector('video')
		expect(video.autoplay && video.muted && video.hasAttribute('playsinline')).toBe(true)
		video.getBoundingClientRect = () => ({ left: 0, top: 0, width: 960, height: 540 })

		const fire = (type, init) => video.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, ...init }))
		fire('pointerdown', { clientX: 100, clientY: 50, buttons: 1 })
		fire('pointermove', { clientX: 101, clientY: 51, buttons: 3 })
		fire('pointerup', { clientX: 101, clientY: 51, buttons: 0 })
		expect(dc.sent).toEqual([
			{ t: 'p', x: 200, y: 100, m: 1 },
			{ t: 'p', x: 202, y: 102, m: 5 },
			{ t: 'p', x: 202, y: 102, m: 0 },
		])

		dc.sent.length = 0
		const wheel = new WheelEvent('wheel', { bubbles: true, cancelable: true, clientX: 0, clientY: 0, deltaY: 100, deltaMode: 0 })
		video.dispatchEvent(wheel)
		expect(wheel.defaultPrevented).toBe(true)
		expect(dc.sent).toEqual([
			{ t: 'p', x: 0, y: 0, m: 16 },
			{ t: 'p', x: 0, y: 0, m: 0 },
			{ t: 'p', x: 0, y: 0, m: 16 },
			{ t: 'p', x: 0, y: 0, m: 0 },
		])

		// noVNC's keyboard handler turns DOM keys into X keysyms.
		dc.sent.length = 0
		const root = el.querySelector('.hd-webrtc')
		root.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', code: 'KeyA', bubbles: true, cancelable: true }))
		root.dispatchEvent(new KeyboardEvent('keyup', { key: 'a', code: 'KeyA', bubbles: true, cancelable: true }))
		expect(dc.sent).toEqual([
			{ t: 'k', ks: 0x61, d: true },
			{ t: 'k', ks: 0x61, d: false },
		])

		// The toolbar's RFB-shaped handle.
		dc.sent.length = 0
		const rfb = events.find(([n]) => n === 'ready')[1]
		rfb.sendKey(0xff0d, 'Enter')
		rfb.clipboardPasteFrom('héllo')
		expect(dc.sent).toEqual([
			{ t: 'k', ks: 0xff0d, d: true },
			{ t: 'k', ks: 0xff0d, d: false },
			{ t: 'clip', text: 'héllo' },
		])
		pcs[0].msg({ t: 'clip', text: 'from host' })
		expect(events.find(([n]) => n === 'clipboard')[1]).toBe('from host')

		dc.sent.length = 0
		rfb.disconnect()
		expect(dc.sent).toEqual([{ t: 'release' }])
		expect(pcs[0].closed).toBe(true)
	})

	test('a window drag / edge resize: every move with the button held goes out, edges map to the last pixel, no native drag', async () => {
		await mount()
		const dc = pcs[0].dc
		pcs[0].msg({ t: 'size', w: 3398, h: 1220 })
		const video = el.querySelector('video')
		// 3398x1220 fit into 1280x700: scale ~0.3767, letterboxed top and bottom.
		video.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1280, height: 700 })
		const fire = (type, init) => video.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, ...init }))
		const top = (700 - 1220 * (1280 / 3398)) / 2
		fire('pointerdown', { clientX: 1279.9, clientY: top + 100, buttons: 1 })
		for (let i = 1; i <= 20; i++) fire('pointermove', { clientX: 1279.9 - i, clientY: top + 100, buttons: 1 })
		fire('pointerup', { clientX: 1259.9, clientY: top + 100, buttons: 0 })
		expect(dc.sent).toHaveLength(22)
		expect(dc.sent[0]).toEqual({ t: 'p', x: 3397, y: 265, m: 1 })
		expect(dc.sent.slice(1, 21).every((m) => m.m === 1)).toBe(true)
		expect(dc.sent.at(-1).m).toBe(0)
		expect(new Set(dc.sent.map((m) => m.x)).size).toBe(21) // each CSS px a new remote x
		expect(video.getAttribute('draggable')).toBe('false')
		const drag = new Event('dragstart', { bubbles: true, cancelable: true })
		video.dispatchEvent(drag)
		expect(drag.defaultPrevented).toBe(true)
	})

	test("the host's cursor shape becomes the CSS cursor, scaled with the picture (hotspot too)", async () => {
		const drawn = []
		vi.stubGlobal(
			'Image',
			class {
				set src(v) {
					this._src = v
					setTimeout(() => this.onload && this.onload())
				}
			},
		)
		const getContext = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function () {
			return { drawImage: (img, x, y, w, h) => drawn.push([img._src, x, y, w, h, this.width, this.height]) }
		})
		const toDataURL = vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockImplementation(() => 'data:image/png;base64,SCALED')
		try {
			await mount()
			const root = el.querySelector('.hd-webrtc')
			const video = el.querySelector('video')
			// Before any shape: the browser's own.
			expect(root.style.cursor).toBe('')
			// 1:1 (no layout yet): the PNG as is, hotspot as sent.
			pcs[0].msg({ t: 'cursor', w: 32, h: 32, x: 10, y: 20, png: 'AAAA' })
			await nextTick()
			expect(root.style.cursor).toBe('url("data:image/png;base64,AAAA") 10 20, default')
			expect(events.find(([n]) => n === 'cursor')[1]).toMatchObject({ w: 32, h: 32, x: 10, y: 20, png: 'AAAA' })
			// Fit at half size: a 16x16 copy, hotspot halved.
			pcs[0].msg({ t: 'size', w: 1920, h: 1080 })
			video.getBoundingClientRect = () => ({ left: 0, top: 0, width: 960, height: 600 })
			pcs[0].msg({ t: 'cursor', w: 32, h: 32, x: 10, y: 20, png: 'BBBB' })
			await flush()
			expect(drawn.at(-1)).toEqual(['data:image/png;base64,BBBB', 0, 0, 16, 16, 16, 16])
			expect(root.style.cursor).toBe('url("data:image/png;base64,SCALED") 5 10, default')
			// The host hid its cursor (w 0): none.
			pcs[0].msg({ t: 'cursor' })
			await nextTick()
			expect(root.style.cursor).toBe('none')
		} finally {
			getContext.mockRestore()
			toDataURL.mockRestore()
		}
	})

	test('an expired token says so instead of falling back', async () => {
		fetchMock.mockImplementationOnce(async () => json(401, { message: 'token is invalid' }))
		await mount()
		expect(lastState()).toEqual({ state: 'error', reason: 'Your session expired - sign in again' })
	})
})

describe('#/host-desktop/embed', () => {
	afterEach(() => {
		delete window.NivaroosBridge
		vi.unstubAllGlobals()
		vi.useRealTimers()
	})

	test('posts connecting, then fallback with the reason, to NivaroosBridge', async () => {
		vi.useFakeTimers()
		const posted = []
		window.NivaroosBridge = { postMessage: (m) => posted.push(JSON.parse(m)) }
		vi.stubGlobal('RTCPeerConnection', FakePC)
		vi.stubGlobal('fetch', async () => json(200, { modes: { webrtc: { available: false, reason: 'No H.264 encoder' } } }))
		const { default: Embed } = await import('@/views/HostDesktopEmbed.vue')
		const el = document.createElement('div')
		document.body.appendChild(el)
		const app = createApp({ render: () => h(Embed) })
		app.config.globalProperties.$t = (s) => s
		app.mount(el)
		await flush()
		expect(posted).toEqual([
			{ state: 'connecting', reason: '' },
			{ state: 'fallback', reason: 'No H.264 encoder' },
		])
		expect(el.textContent).toContain('No H.264 encoder')
		app.unmount()
		el.remove()
	})

	test("the app's keyboard types through nivaroosEmbed.key; host copies reach the bridge", async () => {
		vi.useFakeTimers()
		const posted = []
		window.NivaroosBridge = { postMessage: (m) => posted.push(JSON.parse(m)) }
		vi.stubGlobal('RTCPeerConnection', FakePC)
		vi.stubGlobal('fetch', async (url) =>
			String(url).includes('/offer')
				? json(200, { type: 'answer', sdp: 'v=0 answer' })
				: json(200, { modes: { webrtc: { available: true, encoder: 'nvh264enc' } } }),
		)
		const { default: Embed } = await import('@/views/HostDesktopEmbed.vue')
		const el = document.createElement('div')
		document.body.appendChild(el)
		const app = createApp({ render: () => h(Embed) })
		app.config.globalProperties.$t = (s) => s
		app.mount(el)
		await flush()
		const pc = pcs.at(-1)
		window.nivaroosEmbed.key(0x61, true)
		expect(pc.dc.sent.at(-1)).toEqual({ t: 'k', ks: 0x61, d: true })
		pc.msg({ t: 'clip', text: 'from host' })
		expect(posted.at(-1)).toEqual({ t: 'clip', text: 'from host' })
		// The app draws its own cursor: it gets the host's shape.
		pc.msg({ t: 'cursor', w: 24, h: 24, x: 1, png: 'QUJD' })
		expect(posted.at(-1)).toEqual({ t: 'cursor', w: 24, h: 24, x: 1, y: 0, png: 'QUJD' })
		app.unmount()
		el.remove()
		expect(window.nivaroosEmbed).toBeUndefined()
	})

	test('the app drives pointer, clipboard, stream settings and placement; size and stats go back', async () => {
		vi.useFakeTimers()
		const posted = []
		window.NivaroosBridge = { postMessage: (m) => posted.push(JSON.parse(m)) }
		window.__nivaroosStream = { fps: 60, maxKbps: 8000 }
		vi.stubGlobal('RTCPeerConnection', FakePC)
		const offers = []
		vi.stubGlobal('fetch', async (url, init) => {
			if (!String(url).includes('/offer')) return json(200, { modes: { webrtc: { available: true, encoder: 'x264enc' } } })
			offers.push(JSON.parse(init.body))
			return json(200, { type: 'answer', sdp: 'v=0 answer' })
		})
		const { default: Embed } = await import('@/views/HostDesktopEmbed.vue')
		const el = document.createElement('div')
		document.body.appendChild(el)
		const app = createApp({ render: () => h(Embed) })
		app.config.globalProperties.$t = (s) => s
		app.mount(el)
		await flush()
		// The app's settings win over this browser's, from the first offer
		// (an older app's raw fps / bitrate: Custom).
		expect(offers.at(-1)).toMatchObject({ fps: 60, max_kbps: 8000 })
		const pc = pcs.at(-1)
		const e = window.nivaroosEmbed

		e.pointer(10.4, 20.6, 1)
		e.pointer(-5, 3, 16)
		e.clip('héllo')
		expect(pc.dc.sent.slice(-3)).toEqual([
			{ t: 'p', x: 10, y: 21, m: 1 },
			{ t: 'p', x: 0, y: 3, m: 16 },
			{ t: 'clip', text: 'héllo' },
		])

		pc.msg({ t: 'size', w: 2400, h: 1080 })
		pc.frames(1)
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(posted).toContainEqual({ t: 'size', w: 2400, h: 1080 })
		expect(posted).toContainEqual({ t: 'stats', encoder: 'x264enc', fps: 30, rttMs: 4, preset: 'custom' })
		// Unchanged stats aren't posted again.
		await vi.advanceTimersByTimeAsync(2000)
		await flush()
		expect(posted.filter((m) => m.t === 'stats')).toHaveLength(1)

		// The app's fit / zoom / pan: the video goes where it says.
		const video = el.querySelector('video')
		e.view({ x: -100, y: 12, w: 1600, h: 720 })
		expect(video.style.cssText).toContain('left: -100px')
		expect(video.style.width).toBe('1600px')
		expect(video.style.objectFit).toBe('fill')
		e.view(null)
		expect(video.getAttribute('style')).toBeNull()

		// New fps / bitrate: offered again with them; a value it doesn't know changes nothing.
		e.setStream({ fps: 30, maxKbps: 20000 })
		await flush()
		expect(offers.at(-1)).toMatchObject({ fps: 30, max_kbps: 20000 })
		expect(pcs.at(-2).closed).toBe(true)
		const n = offers.length
		e.setStream({ fps: 45, maxKbps: 'lots' })
		await flush()
		expect(offers.at(-1)).toMatchObject({ fps: 30, max_kbps: 20000 })
		expect(offers).toHaveLength(n)

		// A preset (what the app sends now): offered with it alone.
		e.setStream({ preset: 'low', adapt: true })
		await flush()
		expect(offers.at(-1)).toEqual({ sdp: 'v=0 offer', type: 'offer', preset: 'low' })
		app.unmount()
		el.remove()
		delete window.__nivaroosStream
	})
})
