import { describe, expect, test, vi } from 'vitest'
import { createReconnect, isOutage } from './reconnect'

describe('reconnect', () => {
	test('outages are no answer or a 502/503/504; a 401 or a cancel is not', () => {
		expect(isOutage({ message: 'Network Error' })).toBe(true)
		expect(isOutage({ response: { status: 502 } })).toBe(true)
		expect(isOutage({ response: { status: 504 } })).toBe(true)
		expect(isOutage({ response: { status: 401 } })).toBe(false)
		expect(isOutage({ response: { status: 500 } })).toBe(false)
		expect(isOutage({ code: 'ERR_CANCELED' })).toBe(false)
	})

	test('says so once, probes with backoff, and clears when the server answers', async () => {
		const timers = []
		const changes = []
		const answers = [false, false, true]
		const probe = vi.fn(() => (answers.shift() ? Promise.resolve() : Promise.reject(new Error('down'))))
		const r = createReconnect({ probe, onChange: (v) => changes.push(v), setTimer: (fn, ms) => timers.push([fn, ms]) })
		r.failed()
		r.failed()
		expect(timers.length).toBe(1)
		expect(changes).toEqual([])
		const run = async () => {
			const [fn] = timers.shift()
			fn()
			await new Promise((res) => setTimeout(res))
		}
		await run()
		expect(changes).toEqual([true])
		await run()
		await run()
		expect(probe).toHaveBeenCalledTimes(3)
		expect(changes).toEqual([true, false])
		expect(timers).toEqual([])
	})

	test('a request failing while the server still answers shows nothing', async () => {
		const changes = []
		const timers = []
		const r = createReconnect({ probe: () => Promise.resolve(), onChange: (v) => changes.push(v), setTimer: (fn) => timers.push(fn) })
		r.failed()
		timers.shift()()
		await new Promise((res) => setTimeout(res))
		expect(changes).toEqual([])
		expect(timers).toEqual([])
	})

	test('backoff grows to the cap and resets after recovery', async () => {
		const delays = []
		const timers = []
		const r = createReconnect({ probe: () => Promise.reject(new Error('down')), onChange: () => {}, minMs: 1000, maxMs: 4000, setTimer: (fn, ms) => {
			delays.push(ms)
			return timers.push(fn)
		} })
		r.failed()
		for (let i = 0; i < 4; i++) {
			timers.shift()()
			await new Promise((res) => setTimeout(res))
		}
		expect(delays).toEqual([1000, 2000, 4000, 4000, 4000])
		r.ok()
		r.failed()
		expect(delays.length).toBe(5) // a timer is still pending; no second one
	})
})
