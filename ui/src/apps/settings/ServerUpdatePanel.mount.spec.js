// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h } from 'vue'
import Buefy from '@ntohq/buefy-next'
import ServerUpdatePanel from './ServerUpdatePanel.vue'

vi.mock('@/service/backup.js', () => ({ default: { listRuns: vi.fn(() => Promise.resolve({ runs: [] })) } }))

const flush = () => new Promise((r) => setTimeout(r, 0))
let app
let requests

async function mount(state, extra = {}) {
	requests = []
	const el = document.createElement('div')
	app = createApp({ render: () => h(ServerUpdatePanel) })
	app.use(Buefy)
	app.config.globalProperties.$t = (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s)
	const ok = (data) => Promise.resolve({ data: { success: 200, data } })
	app.config.globalProperties.$api = {
		sys: {
			getServerUpdate: () => ok({ state, settings: { channel: 'stable', auto: 'install', window: '04:00' }, history: [], running: false, ...extra }),
			requestServerUpdate: (action, body) => {
				requests.push([action, body])
				return ok({})
			},
			setServerUpdateSettings: (s) => ok(s)
		}
	}
	app.mount(el)
	await flush()
	await flush()
	return el
}
const text = (el, t) => el.querySelector(`[data-test="${t}"]`)?.textContent.trim()

afterEach(() => app && app.unmount())

describe('Settings › Updates › server releases', () => {
	test('a newer release: Update now asks the updater to install it', async () => {
		const el = await mount({ state: 'available', current: '1.4.0', available: { version: '1.5.0', channel: 'stable', size: 134217728, notes_md: '- faster' } })
		expect(text(el, 'server-update-title')).toBe('NivaroOS 1.5.0 is available')
		el.querySelector('[data-test="server-update-now"]').click()
		await flush()
		await flush()
		expect(requests).toEqual([['apply', { mode: 'wait' }]])
	})

	test('during an update: the step, no Update button', async () => {
		const el = await mount({ state: 'probation', current: '1.4.0', available: { version: '1.5.0' } })
		expect(text(el, 'server-update-title')).toBe('Updating NivaroOS')
		expect(text(el, 'server-update-step')).toContain('Installed - watching it for 10 minutes')
		expect(el.querySelector('[data-test="server-update-now"]')).toBeNull()
	})

	test('afterwards: why it was rolled back', async () => {
		const el = await mount({ state: 'idle', current: '1.4.0', last_result: { result: 'rolled_back', from: '1.4.0', to: '1.5.0', reason: 'failed: health check' } })
		expect(el.textContent).toContain('1.5.0 was rolled back to 1.4.0: failed: health check')
		expect(text(el, 'server-update-title')).toBe('No newer server release')
	})
})
