// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { REASONS } from './streamMode'
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
		vi.unstubAllGlobals()
		vi.useRealTimers()
	})

	async function mount(props = {}) {
		el = document.createElement('div')
		document.body.appendChild(el)
		const on = (name) => (e) => events.push([name, e])
		app = createApp({ render: () => h(HostDesktopWebRTC, { ...props, onState: on('state'), onReady: on('ready'), onStats: on('stats'), onClipboard: on('clipboard'), onSize: on('size') }) })
		app.mount(el)
		await flush()
	}
	const states = () => events.filter(([n]) => n === 'state').map(([, e]) => e)
	const lastState = () => states().at(-1)

	test('offers with the app token, non-trickle SDP, fps and bitrate; no STUN/TURN', async () => {
		await mount({ fps: 60, maxKbps: 20000 })
		const call = fetchMock.mock.calls.find(([u]) => u.endsWith('/v1/vm-sidecar/host/webrtc/offer'))
		expect(call[1].headers.Authorization).toBe('jwt-from-app')
		expect(JSON.parse(call[1].body)).toEqual({ sdp: 'v=0 offer', type: 'offer', fps: 60, max_kbps: 20000 })
		expect(pcs[0].config).toBeUndefined()
		expect(pcs[0].transceiver).toEqual({ kind: 'video', init: { direction: 'recvonly' } })
		expect(pcs[0].dc.label).toBe('input')
		expect(pcs[0].remoteDescription).toEqual({ type: 'answer', sdp: 'v=0 answer' })
		expect(lastState()).toEqual({ state: 'connecting', reason: '' })
	})

	test('Auto: no first frame within 4 s falls back with a reason and closes the peer', async () => {
		await mount({ fallback: true })
		await vi.advanceTimersByTimeAsync(4000)
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

	test('Smooth: no first frame re-offers with backoff instead of falling back', async () => {
		await mount({ fallback: false })
		await vi.advanceTimersByTimeAsync(5000)
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
		expect(events.filter(([n]) => n === 'stats').at(-1)[1]).toEqual({ encoder: 'nvh264enc', fps: 30, rttMs: 4 })
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

	test('live, heartbeats fine but the picture freezes for 3 s: re-offers (even in Auto, no fallback)', async () => {
		await mount({ fallback: true })
		pcs[0].frames(1)
		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(lastState().state).toBe('live')
		for (let i = 0; i < 4; i++) {
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
		// The app's settings win over this browser's, from the first offer.
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
		expect(posted).toContainEqual({ t: 'stats', encoder: 'x264enc', fps: 30, rttMs: 4 })
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
		app.unmount()
		el.remove()
		delete window.__nivaroosStream
	})
})
