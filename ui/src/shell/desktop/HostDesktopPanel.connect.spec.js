// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'

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
		vi.useRealTimers()
	})

	function mount() {
		vi.useFakeTimers()
		const el = document.createElement('div')
		document.body.appendChild(el)
		app = createApp({ render: () => h(Panel) })
		app.use(Buefy)
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
