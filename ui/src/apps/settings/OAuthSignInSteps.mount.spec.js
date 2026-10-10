// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'
import Steps from './OAuthSignInSteps.vue'

// Real Buefy inputs: a paste must reach `change` with the pasted value
// (the inputs' own input event fires before v-model updates).
describe('OAuthSignInSteps in the DOM', () => {
	test('one paste enables Connect with the pasted token', async () => {
		const got = []
		const el = document.createElement('div')
		const app = createApp({ render: () => h(Steps, { type: 'drive', provider: 'Google Drive', onChange: v => got.push(v) }) })
		app.use(Buefy)
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
