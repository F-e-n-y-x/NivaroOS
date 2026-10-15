// @vitest-environment jsdom
// The guard after a server restart: only the auth service saying the
// session is over may sign out - not an outage, and not a login that
// happened while the server was still coming up.
import { beforeEach, describe, expect, test, vi } from 'vitest'

vi.mock('@/views/Home.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/Login.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/Welcome.vue', () => ({ default: { render: () => null } }))

const jwt = (exp) => `h.${btoa(JSON.stringify({ exp }))}.s`
let answer
const fail = (config) => {
	const e = new Error('Request failed')
	e.config = config
	e.isAxiosError = true
	if (answer !== 'network') e.response = { status: answer, data: '', config, headers: {} }
	return Promise.reject(e)
}

async function boot() {
	vi.resetModules()
	const { instance } = await import('@/service/service.js')
	instance.defaults.adapter = fail
	const router = (await import('@/router')).default
	await router.push('/')
	await new Promise((r) => setTimeout(r, 20))
	return router
}

// The first import transforms the whole service layer: slow on a busy box.
describe('router guard across a server restart', { timeout: 30000 }, () => {
	beforeEach(() => {
		localStorage.clear()
		localStorage.setItem('access_token', jwt(Math.floor(Date.now() / 1000) + 3600))
		localStorage.setItem('refresh_token', 'r1')
	})

	test.each(['network', 502, 503, 404])('server unreachable (%s): the desktop stays, signed in', async (a) => {
		answer = a
		localStorage.setItem('version', '1')
		const router = await boot()
		expect(router.currentRoute.value.path).toBe('/')
		expect(localStorage.getItem('access_token')).toBeTruthy()
	})

	// Login stored the tokens but its version lookup hit the main service
	// while it was restarting; the guard then threw the session away.
	test('a session without a stored version is kept', async () => {
		answer = 502
		const router = await boot()
		expect(router.currentRoute.value.path).toBe('/')
		expect(localStorage.getItem('access_token')).toBeTruthy()
	})
})
