// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'

vi.mock('@/api/downloadSidecar', async (orig) => ({
	...(await orig()),
	downloadSidecar: { getSettings: async () => ({}), probe: async () => ({}) }
}))
const { default: AddWindow } = await import('./DsAddDownloadWindow.vue')

async function mount(props) {
	const el = document.createElement('div')
	const app = createApp({ render: () => h(AddWindow, props) })
	app.use(Buefy)
	app.config.globalProperties.$t = (s) => s
	app.config.globalProperties.$store = { state: { windows: [] }, commit() {} }
	app.mount(el)
	for (let i = 0; i < 4; i++) await nextTick()
	return { el, app }
}

describe('Add Download window', () => {
	test('a plain Add URL has no empty "captured link" row, and a bare settings reply still gives a folder', async () => {
		const { el, app } = await mount({})
		expect(el.querySelector('textarea')).not.toBeNull()
		expect(el.querySelector('.captured-link')).toBeNull()
		expect(el.querySelector('input[aria-label="Save to"]').value).toBe('/DATA/Downloads')
		app.unmount()
	})

	test('a captured link shows the link row instead of the text box', async () => {
		const { el, app } = await mount({ url: 'https://example.com/a.zip', rbCapture: 'c1' })
		expect(el.querySelector('textarea')).toBeNull()
		expect(el.querySelector('.captured-link').textContent).toContain('https://example.com/a.zip')
		app.unmount()
	})
})
