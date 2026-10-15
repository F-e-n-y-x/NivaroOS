import { describe, it, expect, vi, afterEach } from 'vitest'
import { watchedSocket, STALE_MS } from './consoleSocket'

class FakeWS {
	constructor(url) {
		this.url = url
		this.readyState = 1
		this.l = {}
		FakeWS.last = this
	}

	addEventListener(t, f) {
		;(this.l[t] = this.l[t] || []).push(f)
	}

	emit(t, e) {
		;(this.l[t] || []).forEach((f) => f(e))
	}
}

describe('watchedSocket', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('reports a link that went silent after heartbeats', () => {
		vi.stubGlobal('WebSocket', FakeWS)
		let t = 0
		const onStale = vi.fn()
		const s = watchedSocket('ws://x/host/console', onStale, () => t)
		FakeWS.last.emit('message', { data: '' }) // heartbeat
		t = STALE_MS - 1
		s.check()
		expect(onStale).not.toHaveBeenCalled()
		t = STALE_MS + 1
		s.check()
		expect(onStale).toHaveBeenCalledTimes(1)
		s.check()
		expect(onStale).toHaveBeenCalledTimes(1)
	})

	it('never fires against a sidecar without heartbeats, or once closed', () => {
		vi.stubGlobal('WebSocket', FakeWS)
		let t = 0
		const onStale = vi.fn()
		const s = watchedSocket('ws://x', onStale, () => t)
		FakeWS.last.emit('message', { data: new ArrayBuffer(4) })
		t = 10 * STALE_MS
		s.check()
		expect(onStale).not.toHaveBeenCalled()

		const s2 = watchedSocket('ws://y', onStale, () => t)
		FakeWS.last.emit('message', { data: '' })
		FakeWS.last.emit('close', {})
		t += 10 * STALE_MS
		s2.check()
		expect(onStale).not.toHaveBeenCalled()
		s.stop()
	})
})
