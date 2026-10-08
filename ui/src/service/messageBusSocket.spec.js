import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest'
import { createMessageBusSocket, fromWire, sourcesOf, eventPath } from './messageBusSocket'

class FakeWS {
	static all = []
	constructor(url) {
		this.url = url
		this.closed = false
		FakeWS.all.push(this)
	}
	close() {
		this.closed = true
	}
	// server side
	accept() {
		this.onopen && this.onopen()
	}
	send(obj) {
		this.onmessage && this.onmessage({ data: JSON.stringify(obj) })
	}
	drop() {
		this.closed = true
		this.onclose && this.onclose()
	}
}

const flush = async () => {
	for (let i = 0; i < 5; i++) await Promise.resolve()
}
const live = () => FakeWS.all.filter((w) => !w.closed)

function setup({ token = 'T1', sources = ['nivaroos', 'app-management'] } = {}) {
	let current = token
	let onToken = () => {}
	const listSources = vi.fn(async () => sources)
	const bus = createMessageBusSocket({
		getToken: () => current,
		listSources,
		wsBase: 'ws://box',
		watchToken: (cb) => (onToken = cb),
		WebSocketImpl: FakeWS,
	})
	return {
		bus,
		listSources,
		setToken(t) {
			current = t
			onToken(t)
		},
	}
}

beforeEach(() => {
	FakeWS.all = []
	vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
})
afterEach(() => vi.useRealTimers())

describe('messageBusSocket', () => {
	test('logged out: no sockets until a token appears', async () => {
		const s = setup({ token: '' })
		await flush()
		expect(s.listSources).not.toHaveBeenCalled()
		expect(FakeWS.all).toHaveLength(0)
		s.setToken('T9')
		await flush()
		expect(FakeWS.all.map((w) => w.url)).toEqual(['ws://box/v2/message_bus/event/nivaroos?token=T9', 'ws://box/v2/message_bus/event/app-management?token=T9'])
	})

	test('one socket per source; connect once all are open; events in the UI shape', async () => {
		const { bus } = setup()
		await flush()
		const onConnect = vi.fn()
		const onUtil = vi.fn()
		bus.on('connect', onConnect)
		bus.on('nivaroos:system:utilization', onUtil)
		FakeWS.all[0].accept()
		expect(bus.connected).toBe(false)
		FakeWS.all[1].accept()
		expect(bus.connected).toBe(true)
		expect(onConnect).toHaveBeenCalledTimes(1)
		FakeWS.all[0].send({ sourceID: 'nivaroos', name: 'nivaroos:system:utilization', properties: { a: '1' }, timestamp: '2026-10-08T00:00:10Z', uuid: 'u' })
		expect(onUtil).toHaveBeenCalledWith({ SourceID: 'nivaroos', Name: 'nivaroos:system:utilization', Properties: { a: '1' }, Timestamp: Date.parse('2026-10-08T00:00:10Z') / 1000, uuid: 'u' })
		bus.off('nivaroos:system:utilization', onUtil)
		FakeWS.all[0].send({ name: 'nivaroos:system:utilization' })
		expect(onUtil).toHaveBeenCalledTimes(1)
	})

	test('a dropped socket closes the rest and reconnects with backoff, listing sources again', async () => {
		const s = setup()
		await flush()
		FakeWS.all.forEach((w) => w.accept())
		const onDisconnect = vi.fn()
		s.bus.on('disconnect', onDisconnect)
		FakeWS.all[1].drop()
		expect(onDisconnect).toHaveBeenCalledTimes(1)
		expect(live()).toHaveLength(0)
		expect(s.listSources).toHaveBeenCalledTimes(1)
		vi.advanceTimersByTime(1000)
		await flush()
		expect(s.listSources).toHaveBeenCalledTimes(2)
		expect(live()).toHaveLength(2)
		// fails again before opening: the next try waits longer
		live()[0].drop()
		vi.advanceTimersByTime(1000)
		await flush()
		expect(s.listSources).toHaveBeenCalledTimes(2)
		vi.advanceTimersByTime(1000)
		await flush()
		expect(s.listSources).toHaveBeenCalledTimes(3)
	})

	test('listing the sources fails: retried', async () => {
		const s = setup()
		s.listSources.mockRejectedValueOnce(new Error('502'))
		s.bus.open()
		await flush()
		expect(FakeWS.all).toHaveLength(0)
		vi.advanceTimersByTime(1000)
		await flush()
		expect(live()).toHaveLength(2)
	})

	test('logout disconnects and stops retrying', async () => {
		const s = setup()
		await flush()
		FakeWS.all.forEach((w) => w.accept())
		s.setToken('')
		expect(s.bus.connected).toBe(false)
		expect(live()).toHaveLength(0)
		vi.advanceTimersByTime(60000)
		await flush()
		expect(s.listSources).toHaveBeenCalledTimes(1)
	})
})

describe('helpers', () => {
	test('sourcesOf dedupes', () => {
		expect(sourcesOf([{ sourceID: 'a' }, { sourceID: 'b' }, { sourceID: 'a' }, null])).toEqual(['a', 'b'])
	})
	test('eventPath escapes', () => {
		expect(eventPath('a b')).toBe('/v2/message_bus/event/a%20b')
	})
	test('fromWire without timestamp', () => {
		expect(fromWire({ name: 'x' })).toEqual({ SourceID: undefined, Name: 'x', Properties: {}, Timestamp: 0, uuid: undefined })
	})
})
