import { describe, test, expect, vi } from 'vitest'

vi.mock('./service.js', () => ({ instance: { get: vi.fn(), post: vi.fn() } }))

import { createFansClient, toFansError, FansError, FANS_BASE } from './fans'

function transport(responses) {
	const calls = []
	const reply = (method, url, body) => {
		calls.push({ method, url, body })
		const r = responses[`${method} ${url}`]
		if (r instanceof Error) return Promise.reject(r)
		return Promise.resolve({ data: r })
	}
	return {
		calls,
		get: (url) => reply('get', url),
		post: (url, body) => reply('post', url, body)
	}
}

describe('fans client', () => {
	test('status and changes go to /v1/fans', async () => {
		const t = transport({ 'get /v1/fans/status': { fans: [] }, 'post /v1/fans/fan': { fans: [1] }, 'post /v1/fans/preset': {}, 'post /v1/fans/settings': {}, 'post /v1/fans/identify': { tach: 'fan2' } })
		const c = createFansClient(t)
		expect(await c.status()).toEqual({ fans: [] })
		expect(await c.updateFan('hw-x-pwm2', { mode: 'fixed', fixed_pct: 60 })).toEqual({ fans: [1] })
		expect(t.calls[1]).toEqual({ method: 'post', url: `${FANS_BASE}/fan`, body: { id: 'hw-x-pwm2', mode: 'fixed', fixed_pct: 60 } })
		await c.applyPreset('quiet')
		expect(t.calls[2].body).toEqual({ preset: 'quiet' })
		await c.setCritical(80)
		expect(t.calls[3].body).toEqual({ critical_cpu_c: 80 })
		expect((await c.identify('hw-x-pwm2')).tach).toBe('fan2')
	})

	test('errors carry the service message; no answer means unavailable', async () => {
		const err = Object.assign(new Error('x'), { response: { status: 400, data: { error: 'a fixed speed must be between 20% and 100%' } } })
		const c = createFansClient(transport({ 'post /v1/fans/fan': err }))
		await expect(c.updateFan('a', { fixed_pct: 5 })).rejects.toMatchObject({ message: /between 20%/, status: 400 })
		expect(toFansError(new Error('Network Error')).unavailable).toBe(true)
		expect(toFansError({ response: { status: 502, data: { error: 'fan service unavailable' } } }).unavailable).toBe(true)
		expect(toFansError({ response: { status: 404, data: { error: 'no such fan' } } }).unavailable).toBe(false)
		expect(toFansError({ response: { status: 403, data: { error: 'administrator' } } })).toBeInstanceOf(FansError)
	})
})
