// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import UI from '@/ui'
import Steps from './OAuthSignInSteps.vue'

// Real UI inputs: a paste must reach `change` with the pasted value
// (the inputs' own input event fires before v-model updates).
describe('OAuthSignInSteps in the DOM', () => {
	test('one paste enables Connect with the pasted token', async () => {
		const got = []
		const el = document.createElement('div')
		const app = createApp({ render: () => h(Steps, { type: 'drive', provider: 'Google Drive', onChange: v => got.push(v) }) })
		app.use(UI)
		app.config.globalProperties.$t = (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s)
		app.config.globalProperties.$store = { commit() {} }
		app.mount(el)
		const ta = el.querySelector('textarea')
		ta.value = '{"access_token":"ya29.a","refresh_token":"1//r"}'
		ta.dispatchEvent(new Event('input'))
		await nextTick()
		await nextTick()
		expect(got.at(-1)).toEqual({ token: '{"access_token":"ya29.a","refresh_token":"1//r"}', client_id: '', client_secret: '' })
		ta.value = ''
		ta.dispatchEvent(new Event('input'))
		await nextTick()
		await nextTick()
		expect(got.at(-1)).toBeNull()
		app.unmount()
	})
})

function mountSteps(props, cloud = {}) {
	const got = []
	const el = document.createElement('div')
	const app = createApp({ render: () => h(Steps, { type: 'drive', provider: 'Google Drive', onChange: v => got.push(v), ...props }) })
	app.use(UI)
	app.config.globalProperties.$t = (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s)
	app.config.globalProperties.$store = { commit() {} }
	app.config.globalProperties.$api = { cloud }
	app.mount(el)
	return { el, app, got }
}
const flush = async () => {
	for (let i = 0; i < 5; i++) await nextTick()
}

describe('Reconnect with the saved own app', () => {
	test('reuses the saved app: no keys asked, only the account name goes to the server, only the token comes back', async () => {
		const calls = []
		const token = '{"access_token":"ya29.a","refresh_token":"1//r"}'
		const cloud = {
			authorize(name) {
				calls.push(['authorize', name])
				return Promise.resolve({ data: { success: 200, data: { running: true, path: '/auth?state=xyz' } } })
			},
			authorizeStatus(name) {
				calls.push(['status', name])
				return Promise.resolve({ data: { success: 200, data: { running: false, path: '/auth?state=xyz', token } } })
			},
		}
		vi.useFakeTimers()
		const { el, app, got } = mountSteps({ ownClient: true, name: 'gdrive', savedClientId: '383322-abc.apps.googleusercontent.com' }, cloud)
		await flush()
		expect(el.textContent).toContain('Using your saved app (383322…)')
		expect(el.querySelector('input[type=password]')).toBeNull()
		expect(el.querySelector('.authorize-cmd')).toBeNull()

		;[...el.querySelectorAll('button')].find(b => b.textContent.includes('Sign in with your saved app')).click()
		await flush()
		expect(el.querySelector('.saved-link').getAttribute('href')).toBe(window.location.origin + '/v1/rclone-auth/auth?state=xyz')
		vi.advanceTimersByTime(2000)
		await flush()
		expect(calls).toEqual([['authorize', 'gdrive'], ['status', 'gdrive']])
		// No client keys: the server keeps the saved ones.
		expect(got.at(-1)).toEqual({ token })
		app.unmount()
		vi.useRealTimers()
	})

	test('"Use different keys" asks for them again', async () => {
		const { el, app } = mountSteps({ ownClient: true, name: 'gdrive', savedClientId: '383322-abc.apps.googleusercontent.com' })
		await flush()
		;[...el.querySelectorAll('a')].find(a => a.textContent.includes('Use different keys')).click()
		await flush()
		expect(el.querySelector('input[type=password]')).not.toBeNull()
		app.unmount()
	})

	test('own app with empty keys: a clear message, no command to run', async () => {
		const { el, app, got } = mountSteps({ ownClient: true })
		await flush()
		expect(el.querySelector('.creds-needed').textContent).toContain('client ID and client secret')
		expect(el.querySelector('.authorize-cmd')).toBeNull()
		expect(el.textContent).not.toContain('Run it in Terminal')
		expect(got.at(-1)).toBeNull()
		app.unmount()
	})
})
