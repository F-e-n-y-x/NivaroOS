// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'

const { rfbs, vm } = vi.hoisted(() => ({ rfbs: [], vm: { state: 'running' } }))
vi.mock('@novnc/novnc', () => ({
	// Like noVNC: disconnect() ends the session with a 'disconnect' event.
	default: class FakeRFB extends EventTarget {
		constructor(target, ws) {
			super()
			this.ws = ws
			rfbs.push(this)
		}

		disconnect() {
			this.dispatchEvent(new Event('disconnect'))
		}
	},
}))
vi.mock('@/api/vmSidecar', () => ({
	vmSidecar: new Proxy(
		{ consoleUrl: (n) => `ws://x/vms/${n}/console`, getVM: async () => ({ ...vm }) },
		{ get: (t, k) => t[k] || (async () => ({})) },
	),
}))

const { default: Panel } = await import('./VmConsolePanel.vue')

class FakeWS {
	constructor(url) {
		this.url = url
		this.readyState = 1
		this.l = {}
	}

	addEventListener(t, f) {
		;(this.l[t] = this.l[t] || []).push(f)
	}

	emit(t, e) {
		;(this.l[t] || []).forEach((f) => f(e))
	}
}

async function flush() {
	for (let i = 0; i < 6; i++) {
		await vi.advanceTimersByTimeAsync(0)
		await nextTick()
	}
}

describe('VM console reconnect', () => {
	let app
	afterEach(() => {
		app && app.unmount()
		rfbs.length = 0
		vm.state = 'running'
		vi.useRealTimers()
		vi.unstubAllGlobals()
	})

	async function mountLive() {
		vi.useFakeTimers()
		vi.stubGlobal('WebSocket', FakeWS)
		vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
		const el = document.createElement('div')
		document.body.appendChild(el)
		app = createApp({ render: () => h(Panel, { vmName: 'test-vm' }) })
		app.use(Buefy)
		app.config.globalProperties.$t = (s) => s
		app.config.globalProperties.$store = { state: { windows: [] }, getters: {}, commit() {} }
		app.mount(el)
		await flush()
		expect(rfbs.length).toBe(1)
		rfbs[0].dispatchEvent(new Event('connect'))
		rfbs[0].ws.emit('message', { data: '' }) // sidecar heartbeat
		await flush()
		expect(el.querySelector('.console-status')).toBeNull()
		return el
	}

	test('a silent link shows "Reconnecting..." with a Reconnect button, then reconnects on its own', async () => {
		const el = await mountLive()
		await vi.advanceTimersByTimeAsync(14000)
		await flush()
		expect(el.querySelector('.console-status').textContent).toContain('Reconnecting...')
		expect([...el.querySelectorAll('.reconnect-btn')].map((b) => b.textContent.trim())).toContain('Reconnect')

		await vi.advanceTimersByTimeAsync(1000)
		await flush()
		expect(rfbs.length).toBe(2)
		rfbs[1].dispatchEvent(new Event('connect'))
		await flush()
		expect(el.querySelector('.console-status')).toBeNull()
	})

	test('Reconnect connects at once', async () => {
		const el = await mountLive()
		rfbs[0].disconnect()
		await flush()
		el.querySelector('.reconnect-btn').click()
		await flush()
		expect(rfbs.length).toBe(2)
	})

	test('a VM that was turned off is not retried', async () => {
		const el = await mountLive()
		vm.state = 'shutoff'
		await vi.advanceTimersByTimeAsync(3000) // state poll
		rfbs[0].disconnect()
		await vi.advanceTimersByTimeAsync(30000)
		await flush()
		expect(rfbs.length).toBe(1)
		expect(el.querySelector('.console-status').textContent).toContain('turned off')
	})
})
