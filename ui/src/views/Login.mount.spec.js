// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import UI from '@/ui'
import Login from './Login.vue'

const tick = async () => { for (let i = 0; i < 4; i++) await nextTick() }

describe('Login', () => {
	test('one login while busy, a short password is allowed, and the error stays up', async () => {
		vi.useFakeTimers()
		let reject
		const login = vi.fn(() => new Promise((_, rej) => (reject = rej)))
		const el = document.createElement('div')
		document.body.appendChild(el)
		const app = createApp({ render: () => h(Login) })
		app.use(UI)
		Object.assign(app.config.globalProperties, {
			$t: (s) => s,
			$assetUrl: (s) => s,
			$store: { state: {}, commit() {} },
			$api: { users: { login, getUserStatus: () => new Promise(() => {}) } }
		})
		app.mount(el)
		await tick()
		const [user, pass] = el.querySelectorAll('input')
		user.value = 'owner'
		user.dispatchEvent(new Event('input'))
		pass.value = 'abc'
		pass.dispatchEvent(new Event('input'))
		await tick()

		const btn = el.querySelector('button.is-primary')
		btn.click()
		btn.click()
		pass.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter' }))
		await tick()
		expect(login).toHaveBeenCalledTimes(1)
		expect(btn.classList.contains('is-loading')).toBe(true)

		reject({ response: { data: { message: 'Invalid password' } } })
		await tick()
		expect(btn.classList.contains('is-loading')).toBe(false)
		await vi.advanceTimersByTimeAsync(5000)
		await tick()
		const alert = el.querySelector('[role=alert]')
		expect(alert && alert.style.display !== 'none' && alert.textContent).toContain('Invalid password')
		expect(el.querySelector('.is-success')).toBeNull()
		app.unmount()
		vi.useRealTimers()
	})
})
