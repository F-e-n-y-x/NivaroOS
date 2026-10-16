// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import UI from '@/ui'
import RestoredApps from './RestoredApps.vue'

describe('RestoredApps', () => {
	test('each app with its state, reason and missing data; follows updates', async () => {
		const status = reactive({ state: 'running', apps: [{ name: 'immich', status: 'installing' }, { name: 'plex', status: 'waiting' }] })
		const el = document.createElement('div')
		const app = createApp({ render: () => h(RestoredApps, { status }) })
		app.use(UI)
		app.config.globalProperties.$t = s => s
		app.mount(el)
		const rows = () => [...el.querySelectorAll('li')].map(li => [...li.childNodes].filter(n => n.nodeType !== 8).map(n => (n.nodeName === 'P' ? ' | ' : '') + n.textContent).join('').replace(/\s+/g, ' ').trim())
		expect(rows()).toEqual(['immich settings_restore.app_installing', 'plex settings_restore.app_waiting'])
		expect(el.querySelector('ul').getAttribute('aria-busy')).toBe('true')

		status.state = 'done'
		status.apps = [
			{ name: 'immich', status: 'ok', data_missing: true },
			{ name: 'plex', status: 'failed', reason: 'pull failed' },
			{ name: 'ha', status: 'skipped', reason: 'already installed' },
			{ name: 'odd', status: 'new-state' }
		]
		await nextTick()
		expect(rows()).toEqual([
			'immich settings_restore.app_ok | settings_restore.data_missing',
			'plex settings_restore.app_failed: pull failed',
			'ha settings_restore.app_skipped: already installed',
			'odd settings_restore.app_waiting'
		])
		expect(el.querySelector('span.has-text-danger:not(.icon)').textContent).toContain('app_failed')
		expect(el.querySelector('ul').getAttribute('aria-busy')).toBe('false')
		app.unmount()
	})
})
