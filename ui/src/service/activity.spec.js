import { describe, test, expect, beforeEach, vi } from 'vitest'

function fakeStorage() {
	const data = {}
	return {
		getItem: k => (k in data ? data[k] : null),
		setItem: (k, v) => { data[k] = String(v) },
		removeItem: k => { delete data[k] },
		data
	}
}

describe('activityService', () => {
	let storage
	let service
	let listeners

	beforeEach(async () => {
		vi.resetModules()
		storage = fakeStorage()
		listeners = {}
		globalThis.localStorage = storage
		globalThis.window = { addEventListener: (type, fn) => { listeners[type] = fn } }
		service = (await import('./activity.js')).default
	})

	test('keeps history per signed-in user', () => {
		storage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
		service.add({ title: 'Alice event', icon: '/icon.png' })
		expect(service.getAll()).toHaveLength(1)
		expect(service.getAll()[0].icon).toBe('/icon.png')

		storage.setItem('user', JSON.stringify({ id: 2, username: 'bob' }))
		expect(service.getAll()).toHaveLength(0)
		service.add({ title: 'Bob event' })

		storage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
		expect(service.getAll().map(a => a.title)).toEqual(['Alice event'])
	})

	test('reloads when another tab writes the same history', () => {
		storage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
		service.add({ title: 'First' })
		const key = Object.keys(storage.data).find(k => k.startsWith('nivaroos_activity_history:'))
		const other = JSON.parse(storage.getItem(key))
		other.unshift({ id: 'x', title: 'From other tab', message: '', read: false, timestamp: new Date().toISOString() })
		storage.setItem(key, JSON.stringify(other))
		const seen = []
		service.subscribe(list => seen.push(list.length))
		listeners.storage({ key })
		expect(service.getAll()[0].title).toBe('From other tab')
		expect(seen).toContain(2)
	})
})
