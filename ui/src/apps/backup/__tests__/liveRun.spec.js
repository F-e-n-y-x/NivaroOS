import { describe, test, expect, vi } from 'vitest'

vi.mock('../../../service/service.js', () => ({ instance: { request: vi.fn() } }))

import { watchRun, watchJobs, subscribe } from '../liveRun'
import { BACKUP_EVENTS } from '../events'

// A socket.io client stand-in: on/off/emit and a connected flag.
function fakeSocket(connected = true) {
	const handlers = {}
	return {
		connected,
		on(name, fn) {
			;(handlers[name] = handlers[name] || []).push(fn)
		},
		off(name, fn) {
			handlers[name] = (handlers[name] || []).filter(h => h !== fn)
		},
		emit(name, props) {
			;(handlers[name] || []).slice().forEach(h => h({ Properties: props }))
		},
		count() {
			return Object.values(handlers).reduce((n, l) => n + l.length, 0)
		}
	}
}

// Manual interval timers.
function fakeTimers() {
	const t = { fns: new Map(), next: 1 }
	return {
		setInterval(fn) {
			const id = t.next++
			t.fns.set(id, fn)
			return id
		},
		clearInterval(id) {
			t.fns.delete(id)
		},
		tick() {
			for (const fn of [...t.fns.values()]) fn()
		},
		active() {
			return t.fns.size
		}
	}
}

const flush = () => new Promise(r => setTimeout(r, 0))

function runClient(statuses) {
	let i = 0
	return {
		getRun: vi.fn(async id => {
			const status = statuses[Math.min(i++, statuses.length - 1)]
			return { id, status, phase: status === 'running' ? 'transfer' : 'post_hooks', steps: [] }
		})
	}
}

describe('watchRun', () => {
	test('loads once, then follows the socket; progress only refetches on a phase change', async () => {
		const socket = fakeSocket(true)
		const timers = fakeTimers()
		const client = runClient(['running', 'running', 'success'])
		const onRun = vi.fn()
		const onLive = vi.fn()
		const w = watchRun({ runId: 'run_1', client, socket, onRun, onLive, timers })
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(1)
		expect(onRun).toHaveBeenCalledTimes(1)

		socket.emit(BACKUP_EVENTS.RUN_PROGRESS, { run_id: 'run_1', phase: 'transfer', status: 'running', bytes: '10', total_bytes: '100' })
		await flush()
		expect(onLive).toHaveBeenCalledWith(expect.objectContaining({ bytes: 10, total_bytes: 100 }), expect.anything())
		expect(client.getRun).toHaveBeenCalledTimes(1)

		// Other runs' events are ignored.
		socket.emit(BACKUP_EVENTS.RUN_END, { run_id: 'run_2' })
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(1)

		socket.emit(BACKUP_EVENTS.RUN_PROGRESS, { run_id: 'run_1', phase: 'prune', status: 'running' })
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)

		socket.emit(BACKUP_EVENTS.RUN_END, { run_id: 'run_1', status: 'success' })
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(3)
		expect(onRun.mock.calls[2][0].status).toBe('success')
		expect(w.final).toBe(true)
		// A final run stops polling.
		expect(timers.active()).toBe(0)
		w.stop()
		expect(socket.count()).toBe(0)
	})

	test('polls only while the socket is down and the window visible', async () => {
		const socket = fakeSocket(false)
		const timers = fakeTimers()
		const client = runClient(['running'])
		let visible = true
		const w = watchRun({ runId: 'r', client, socket, isVisible: () => visible, onRun: () => {}, timers })
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(1)
		timers.tick()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)
		visible = false
		timers.tick()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)
		visible = true
		socket.connected = true
		timers.tick()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)
		// Reconnecting catches up once.
		socket.emit('connect', {})
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(3)
		w.stop()
		timers.tick()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(3)
	})

	test('concurrent refreshes collapse into one follow-up request', async () => {
		let release
		const client = { getRun: vi.fn(() => new Promise(r => (release = () => r({ id: 'r', status: 'running', phase: 'transfer' })))) }
		const w = watchRun({ runId: 'r', client, socket: null, onRun: () => {}, timers: fakeTimers() })
		w.refresh()
		w.refresh()
		w.refresh()
		expect(client.getRun).toHaveBeenCalledTimes(1)
		release()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)
		release()
		await flush()
		expect(client.getRun).toHaveBeenCalledTimes(2)
		w.stop()
	})

	test('errors are reported, not thrown', async () => {
		const onError = vi.fn()
		const client = { getRun: vi.fn(() => Promise.reject(Object.assign(new Error('x'), { code: 'not_found' }))) }
		const w = watchRun({ runId: 'r', client, socket: null, onRun: () => {}, onError, timers: fakeTimers() })
		await flush()
		expect(onError).toHaveBeenCalledWith(expect.objectContaining({ code: 'not_found' }))
		w.stop()
	})
})

describe('watchJobs', () => {
	test('job and run events ask for a reload; progress goes to onLive', () => {
		const socket = fakeSocket(true)
		const timers = fakeTimers()
		const onChange = vi.fn()
		const onLive = vi.fn()
		const w = watchJobs({ socket, onChange, onLive, timers })
		socket.emit(BACKUP_EVENTS.JOB_CHANGED, { job_id: 'bk_1', change: 'updated', revision: '4' })
		socket.emit(BACKUP_EVENTS.RUN_END, { run_id: 'r', job_id: 'bk_1' })
		socket.emit(BACKUP_EVENTS.RUN_PROGRESS, { run_id: 'r', bytes: '5' })
		expect(onChange.mock.calls.map(c => c[0])).toEqual(['job', 'run'])
		expect(onLive).toHaveBeenCalledWith('r', expect.objectContaining({ bytes: 5 }), expect.anything())
		timers.tick()
		expect(onChange).toHaveBeenCalledTimes(2)
		socket.connected = false
		timers.tick()
		expect(onChange.mock.calls[2][0]).toBe('poll')
		w.stop()
		expect(socket.count()).toBe(0)
	})

	test('no socket is a no-op subscription', () => {
		expect(subscribe(null, { a: () => {} })).toBeTypeOf('function')
	})
})
