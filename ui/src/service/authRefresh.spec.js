import { describe, test, expect, vi } from 'vitest'
import { makeUnauthorizedHandler } from './authRefresh'

const err401 = (url = '/v1/x', extra = {}) => ({ config: { url, headers: {}, ...extra }, response: { status: 401 } })

describe('401 handling', () => {
	test('concurrent 401s share one refresh and are all retried with the new token', async () => {
		const refresh = vi.fn(() => Promise.resolve('new-token'))
		const retry = vi.fn((cfg) => Promise.resolve({ ok: cfg.headers.Authorization }))
		const handle = makeUnauthorizedHandler({ refresh, retry, logout: vi.fn() })
		const res = await Promise.all([handle(err401('/a')), handle(err401('/b')), handle(err401('/c'))])
		expect(refresh).toHaveBeenCalledTimes(1)
		expect(res.map((r) => r.ok)).toEqual(['new-token', 'new-token', 'new-token'])
	})

	// A failed refresh left every waiting request pending forever (and the
	// components behind them in memory) and blocked all later refreshes.
	test('an ended session rejects every waiting request and logs out once', async () => {
		const logout = vi.fn()
		const ended = Object.assign(new Error('ended'), { sessionEnded: true, reason: 'password_changed' })
		const handle = makeUnauthorizedHandler({ refresh: () => Promise.reject(ended), retry: vi.fn(), logout })
		const results = await Promise.allSettled([handle(err401('/a')), handle(err401('/b'))])
		expect(results.every((r) => r.status === 'rejected')).toBe(true)
		expect(logout).toHaveBeenCalledTimes(1)
		expect(logout.mock.calls[0][0].reason).toBe('password_changed')
	})

	// A refresh that couldn't be done (user service restarting, offline)
	// signed the browser out.
	test('a refresh that fails for another reason rejects the requests but keeps the session', async () => {
		const logout = vi.fn()
		const handle = makeUnauthorizedHandler({ refresh: () => Promise.reject(new Error('HTTP 502')), retry: vi.fn(), logout })
		const results = await Promise.allSettled([handle(err401('/a')), handle(err401('/b'))])
		expect(results.every((r) => r.status === 'rejected')).toBe(true)
		expect(logout).not.toHaveBeenCalled()
	})

	test('the refused token is handed to the refresh', async () => {
		const refresh = vi.fn(() => Promise.resolve('t'))
		const handle = makeUnauthorizedHandler({ refresh, retry: (c) => Promise.resolve(c), logout: vi.fn() })
		await handle(err401('/a', { headers: { Authorization: 'old' } }))
		expect(refresh).toHaveBeenCalledWith('old')
	})

	test('after a failed refresh the next 401 tries refreshing again', async () => {
		let n = 0
		const refresh = vi.fn(() => (++n === 1 ? Promise.reject(new Error('x')) : Promise.resolve('t2')))
		const handle = makeUnauthorizedHandler({ refresh, retry: (c) => Promise.resolve(c.headers.Authorization), logout: vi.fn() })
		await handle(err401()).catch(() => {})
		expect(await handle(err401())).toBe('t2')
	})

	test('a retried request that is still unauthorized is not retried again', async () => {
		const retry = vi.fn()
		const handle = makeUnauthorizedHandler({ refresh: () => Promise.resolve('t'), retry, logout: vi.fn() })
		await expect(handle(err401('/a', { _retried: true }))).rejects.toBeTruthy()
		expect(retry).not.toHaveBeenCalled()
	})

	test('the refresh call itself failing with 401 is not queued behind itself, and is not a sign-out on its own', async () => {
		const logout = vi.fn()
		const refresh = vi.fn()
		const handle = makeUnauthorizedHandler({ refresh, retry: vi.fn(), logout })
		await expect(handle(err401('/v1/users/refresh'))).rejects.toBeTruthy()
		expect(refresh).not.toHaveBeenCalled()
		expect(logout).not.toHaveBeenCalled()
	})

	test('refreshNow shares the in-flight refresh with 401 handling and never logs out', async () => {
		let resolve
		const refresh = vi.fn(() => new Promise((r) => (resolve = r)))
		const logout = vi.fn()
		const handle = makeUnauthorizedHandler({ refresh, retry: (c) => Promise.resolve(c.headers.Authorization), logout })
		const a = handle(err401())
		const b = handle.refreshNow()
		await new Promise((r) => setTimeout(r, 0)) // refresh() starts on a microtask
		resolve('t3')
		expect(await a).toBe('t3')
		expect(await b).toBe('t3')
		expect(refresh).toHaveBeenCalledTimes(1)

		const failing = makeUnauthorizedHandler({ refresh: () => Promise.reject(new Error('x')), retry: vi.fn(), logout })
		await expect(failing.refreshNow()).rejects.toThrow('x')
		expect(logout).not.toHaveBeenCalled()
	})
})
