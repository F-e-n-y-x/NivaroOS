// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h } from 'vue'
import Buefy from '@ntohq/buefy-next'
import axios from 'axios'
import UpdatesSection from './UpdatesSection.vue'

vi.mock('axios', () => ({ default: { get: vi.fn() } }))

const flush = () => new Promise(r => setTimeout(r, 0))
let app

async function mount(versionData, release) {
	axios.get.mockReset()
	if (release) axios.get.mockResolvedValue({ data: release })
	else axios.get.mockRejectedValue(new Error('rate limited'))
	const el = document.createElement('div')
	app = createApp({ render: () => h(UpdatesSection) })
	app.use(Buefy)
	app.config.globalProperties.$t = s => s
	app.config.globalProperties.$store = { commit() {} }
	const ok = data => Promise.resolve({ data: { success: 200, data } })
	app.config.globalProperties.$api = {
		sys: {
			getVersion: () => (versionData instanceof Error ? Promise.reject(versionData) : ok(versionData)),
			getPackageUpdates: () => ok({ count: 0, packages: [] }),
			getPackageUpgradeStatus: () => ok({ running: false, logs: [] })
		}
	}
	app.mount(el)
	await flush()
	await flush()
	return el
}
const text = (el, t) => el.querySelector(`[data-test="${t}"]`)?.textContent.trim()

const build = { commit: 'abc1234def', short: 'abc1234', date: '2026-10-16T10:00:00Z', version: '2026.10.16-abc1234' }

afterEach(() => app && app.unmount())

describe('Settings › Updates', () => {
	// The bug: Installed 1.0.0 vs the Android release 1.4.16 always said "update".
	test('a newer Android release is not a server update', async () => {
		const el = await mount(
			{ build, update: { state: 'up_to_date', ahead_by: 0 }, current_version: build.version, need_update: false },
			{ tag_name: 'v1.4.16', assets: [{ name: 'NivaroOS.apk', browser_download_url: 'https://x/NivaroOS.apk' }] }
		)
		expect(text(el, 'nivaro-status')).toBe('NivaroOS is up to date')
		expect(text(el, 'nivaro-build')).toMatch(/^Build abc1234, /)
		expect(el.querySelector('[data-test="nivaro-update"]')).toBeNull()
		expect(text(el, 'android-release')).toBe('Android app 1.4.16')
		expect(el.querySelector('a[href="https://x/NivaroOS.apk"]')).not.toBeNull()
	})

	test('master ahead: count, latest change and the Update button', async () => {
		const el = await mount({ build, update: { state: 'behind', ahead_by: 3, latest_commit: 'fff0000aaa', latest_message: 'fix the thing' } })
		expect(text(el, 'nivaro-status')).toBe('3 new changes on GitHub since your build')
		expect(text(el, 'nivaro-latest')).toBe('fix the thing')
		expect(el.querySelector('[data-test="nivaro-update"]')).not.toBeNull()
		expect(text(el, 'android-release')).toBeUndefined()
	})

	test("GitHub or the server unreachable: couldn't check, no Update button", async () => {
		for (const data of [{ build, update: { state: 'unknown', reason: 'GitHub rate limit, try again later' } }, new Error('offline')]) {
			const el = await mount(data)
			expect(text(el, 'nivaro-status')).toBe("Couldn't check for NivaroOS updates")
			expect(el.querySelector('[data-test="nivaro-update"]')).toBeNull()
			app.unmount()
			app = null
		}
	})
})
