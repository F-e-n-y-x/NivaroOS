// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import UI from '@/ui'
import List from './CloudAccountsList.vue'
import Steps from './OAuthSignInSteps.vue'

const flush = async () => {
	for (let i = 0; i < 5; i++) await nextTick()
}

const testingWhy = 'Your Google app is in Testing mode, so Google signs it out every 7 days. Publish it (Google Cloud › OAuth consent screen › Publish app), then Reconnect.'

function mount(comp, props, accounts) {
	const el = document.createElement('div')
	const app = createApp({ render: () => h(comp, props) })
	app.use(UI)
	app.config.globalProperties.$t = (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s)
	app.config.globalProperties.$store = { commit() {} }
	app.config.globalProperties.$EventBus = { $emit() {} }
	app.config.globalProperties.$buefy = { toast: { open() {} } }
	app.config.globalProperties.$api = {
		cloud: {
			list: () => Promise.resolve({ data: { success: 200, data: accounts } }),
			providers: () => Promise.resolve({ data: { success: 200, data: [{ type: 'drive', label: 'Google Drive', auth_kind: 'token' }] } })
		}
	}
	app.mount(el)
	return { el, app }
}

describe('Online Storage account list', () => {
	test('an expired sign-in stays listed, says why, and Reconnect opens the sign-in steps', async () => {
		const { el, app } = mount(List, {}, [
			{ fs: 'gd', name: 'My Drive', type: 'drive', mount_point: '/mnt/gd', own_client: true, client_id: '383322-abc.apps.googleusercontent.com', status: 'sign_in_expired', problem: testingWhy },
			{ fs: 'ok', name: 'Work', type: 'drive', mount_point: '/mnt/ok' }
		])
		await flush()
		const rows = el.querySelectorAll('.setting-row')
		expect(rows.length).toBe(2)
		expect(rows[0].textContent).toContain('Sign-in expired')
		expect(rows[0].textContent).toContain('Testing mode')
		expect(rows[1].textContent).not.toContain('Sign-in expired')
		expect(rows[1].querySelector('.reconnect-cta')).toBeNull()

		const cta = rows[0].querySelector('.reconnect-cta')
		expect(cta.textContent).toContain('Reconnect')
		cta.click()
		await flush()
		expect(el.querySelector('.reconnect-form .oauth-steps')).not.toBeNull()
		// The account uses its own app: the publish warning is right there.
		expect(el.querySelector('.reconnect-form').textContent).toContain('Publish the app or it expires in 7 days.')
		// ...and its saved app is reused, not asked for again.
		expect(el.querySelector('.reconnect-form').textContent).toContain('Using your saved app (383322…)')
		app.unmount()
	})
})

describe('OAuthSignInSteps publish warning', () => {
	test("only for your own Google app, not rclone's or other providers", async () => {
		for (const [props, want] of [
			[{ type: 'drive', provider: 'Google Drive', ownClient: true }, true],
			[{ type: 'drive', provider: 'Google Drive', ownClient: false }, false],
			[{ type: 'dropbox', provider: 'Dropbox', ownClient: true }, false]
		]) {
			const { el, app } = mount(Steps, props, [])
			await flush()
			expect(!!el.querySelector('.publish-warning')).toBe(want)
			app.unmount()
		}
	})
})
