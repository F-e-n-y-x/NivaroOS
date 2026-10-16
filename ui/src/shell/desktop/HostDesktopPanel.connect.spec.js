// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import UI from '@/ui'

// A stream socket that opens but never handshakes: noVNC fires neither
// connect nor disconnect.
const { sockets, sidecar } = vi.hoisted(() => ({ sockets: [], sidecar: { status: {} } }))
vi.mock('@novnc/novnc', () => ({
	default: class FakeRFB extends EventTarget {
		constructor() {
			super()
			this.disconnected = false
			sockets.push(this)
		}

		disconnect() {
			this.disconnected = true
		}
	}
}))
vi.mock('@/service/service', () => ({
	instance: {
		defaults: {},
		get: async (url) => ({ data: url.endsWith('/installed') ? { installed: true } : url.endsWith('/de-status') ? { state: 'supported' } : sidecar.status }),
		post: async () => ({ data: {} }),
		put: async () => ({ data: {} })
	}
}))

const { default: Panel } = await import('./HostDesktopPanel.vue')

async function flush() {
	for (let i = 0; i < 6; i++) {
		await vi.advanceTimersByTimeAsync(0)
		await nextTick()
	}
}

describe('Host Desktop connect watchdog', () => {
	let app
	afterEach(() => {
		app && app.unmount()
		sockets.length = 0
		localStorage.clear()
		vi.useRealTimers()
	})

	function mount() {
		vi.useFakeTimers()
		// These are about the noVNC lane.
		localStorage.setItem('host_desktop_stream', JSON.stringify({ v: 2, mode: 'compatible' }))
		const el = document.createElement('div')
		document.body.appendChild(el)
		app = createApp({ render: () => h(Panel) })
		app.use(UI)
		app.config.globalProperties.$t = (s) => s
		app.config.globalProperties.$store = { state: { windows: [] }, getters: {}, commit() {} }
		app.mount(el)
		return el
	}

	test('a handshake that never comes ends in "disconnected" with Reconnect, then retries', async () => {
		sidecar.status = { installed: true, service_active: true }
		const el = mount()
		await flush()
		expect(sockets.length).toBe(1)
		expect(el.querySelector('.console-status').textContent).toContain('Connecting...')

		await vi.advanceTimersByTimeAsync(20000)
		await flush()
		expect(sockets[0].disconnected).toBe(true)
		expect(el.querySelector('.console-status').textContent).toContain('did not answer')
		expect([...el.querySelectorAll('.reconnect-btn')].map((b) => b.textContent.trim())).toContain('Reconnect')

		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(sockets.length).toBe(2)
		// The retry still says why and keeps the buttons.
		expect(el.querySelector('.console-status').textContent).toContain('did not answer')
		expect(el.querySelector('.console-status-actions')).not.toBeNull()
	})

	test('waiting for a Wayland "Allow" at the server is not timed out', async () => {
		sidecar.status = { installed: true, service_active: true, needs_consent: true }
		const el = mount()
		await flush()
		await vi.advanceTimersByTimeAsync(60000)
		await flush()
		expect(sockets.length).toBe(1)
		expect(sockets[0].disconnected).toBe(false)
		expect(el.querySelector('.console-status').textContent).toContain('Allow screen sharing')
	})
})

describe('Host Desktop WebRTC lane', () => {
	let app
	afterEach(() => {
		app && app.unmount()
		sockets.length = 0
		localStorage.clear()
		vi.unstubAllGlobals()
		vi.useRealTimers()
	})

	function mount() {
		vi.useFakeTimers()
		const el = document.createElement('div')
		document.body.appendChild(el)
		app = createApp({ render: () => h(Panel) })
		app.use(UI)
		app.config.globalProperties.$t = (s) => s
		app.config.globalProperties.$store = { state: { windows: [] }, getters: {}, commit() {} }
		app.mount(el)
		return el
	}

	test('Auto (the default) opens WebRTC instead of noVNC, and falls back to noVNC with a reason when no frame comes', async () => {
		sidecar.status = { installed: true, service_active: true }
		const pcs = []
		vi.stubGlobal(
			'RTCPeerConnection',
			class {
				constructor() {
					this.connectionState = 'new'
					this.iceGatheringState = 'complete'
					pcs.push(this)
				}

				addTransceiver() {}
				createDataChannel() {
					return { readyState: 'open', send() {}, close() {} }
				}

				async createOffer() {
					return { type: 'offer', sdp: 'o' }
				}

				async setLocalDescription(d) {
					this.localDescription = d
				}

				async setRemoteDescription() {}
				async getStats() {
					return new Map()
				}

				addEventListener() {}
				close() {
					this.connectionState = 'closed'
				}
			}
		)
		const ok = (body) => ({ status: 200, ok: true, json: async () => body })
		vi.stubGlobal('fetch', async (url) => ok(url.endsWith('/host/stream') ? { modes: { webrtc: { available: true, encoder: 'x264enc', presets: { low: { fps: 20, max_kbps: 2000, max_width: 1280 } } } } } : { type: 'answer', sdp: 'a' }))

		const el = mount()
		await flush()
		expect(pcs.length).toBe(1)
		expect(sockets.length).toBe(0)
		expect(el.querySelector('.hd-webrtc video')).not.toBeNull()
		expect(el.querySelector('.status-pill.is-smooth').textContent).toContain('CPU · x264')
		// The quality menu: the presets (with the server's numbers) and the encoder, not the VNC qualities.
		const qualityBtn = [...el.querySelectorAll('button')].find((b) => b.getAttribute('aria-label') === 'Display & Bandwidth Quality')
		qualityBtn.click()
		await flush()
		const menu = el.querySelector('.quality-menu')
		expect([...menu.querySelectorAll('.quality-menu-title')].map((t) => t.textContent)).toEqual(['Low bandwidth', 'Balanced', 'High quality', 'Custom'])
		expect(menu.textContent).toContain('1280 wide · 20 fps · 2 Mbit/s')
		expect(menu.querySelector('.quality-menu-encoder').textContent).toContain('CPU · x264')
		expect(menu.querySelector('#hd-noxdamage')).toBeNull()

		await vi.advanceTimersByTimeAsync(9000)
		await flush()
		expect(sockets.length).toBe(1)
		expect(el.querySelector('.hd-webrtc')).toBeNull()
		expect(el.querySelector('.status-pill.is-compatible').textContent).toContain('Compatible mode')
		expect(el.querySelector('.console-statusbar').textContent).toContain('No video arrived')
	})

	test('Compatible (an explicit choice) never touches WebRTC', async () => {
		localStorage.setItem('host_desktop_stream', JSON.stringify({ v: 2, mode: 'compatible' }))
		sidecar.status = { installed: true, service_active: true }
		const fetchSpy = vi.fn()
		vi.stubGlobal('fetch', fetchSpy)
		const el = mount()
		await flush()
		expect(sockets.length).toBe(1)
		expect(fetchSpy).not.toHaveBeenCalled()
		expect(el.querySelector('.status-pill.is-compatible')).toBeNull()
	})
})
