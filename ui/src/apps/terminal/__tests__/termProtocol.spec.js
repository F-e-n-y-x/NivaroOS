import { describe, test, expect, vi, beforeEach } from 'vitest'
import { TermSocket, parseFrame, controlFrame, retryDelay, RETRY_DELAYS } from '../termProtocol'

class FakeWS {
	static OPEN = 1
	static instances = []
	constructor(url) {
		this.url = url
		this.readyState = 0
		this.sent = []
		this.closed = false
		FakeWS.instances.push(this)
	}
	send(d) { this.sent.push(d) }
	close() { this.closed = true; this.readyState = 3 }
	// test helpers
	open() { this.readyState = 1; this.onopen && this.onopen() }
	msg(data) { this.onmessage && this.onmessage({ data }) }
	ctl(obj) { this.msg(controlFrame(obj)) }
	drop(code = 1006, reason = '') { this.readyState = 3; this.onclose && this.onclose({ code, reason }) }
}

function harness(extra = {}) {
	const timers = []
	const events = []
	const out = []
	const sock = new TermSocket({
		url: () => 'ws://h/v1/sys/terminal-sessions/abc/attach?token=t',
		WebSocketImpl: FakeWS,
		setTimeout: (fn, ms) => { const t = { fn, ms }; timers.push(t); return t },
		clearTimeout: (t) => { const i = timers.indexOf(t); if (i >= 0) timers.splice(i, 1) },
		onState: (s, d) => events.push([s, d]),
		onOutput: (b) => out.push(new TextDecoder().decode(b)),
		onText: (t) => out.push('TEXT:' + t),
		onHello: vi.fn(),
		onLive: vi.fn(),
		onSession: vi.fn(),
		onExit: vi.fn(),
		...extra,
	})
	return { sock, timers, events, out, states: () => events.map((e) => e[0]), ws: () => FakeWS.instances[FakeWS.instances.length - 1] }
}

const flush = () => new Promise((r) => setTimeout(r, 0))
const enc = (s) => new TextEncoder().encode(s).buffer

beforeEach(() => { FakeWS.instances = [] })

describe('frames', () => {
	test('parses output, control, status text', () => {
		expect(parseFrame(enc('hi')).kind).toBe('output')
		expect(parseFrame('\u0000{"type":"live"}')).toEqual({ kind: 'control', msg: { type: 'live' } })
		expect(parseFrame('\u0000not json').kind).toBe('ignore')
		expect(parseFrame('\u0000{"no":"type"}').kind).toBe('ignore')
		expect(parseFrame('docker: not running')).toEqual({ kind: 'text', text: 'docker: not running' })
	})

	test('backoff is capped', () => {
		expect(retryDelay(0)).toBe(RETRY_DELAYS[0])
		expect(retryDelay(99)).toBe(RETRY_DELAYS[RETRY_DELAYS.length - 1])
	})
})

describe('attach sequence', () => {
	test('hello -> replay -> live -> output; controls are never printed', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		h.ws().ctl({ type: 'hello', session: { id: 'abc', title: 'T1' }, replay_bytes: 5 })
		h.ws().msg(enc('old\r\n'))
		expect(h.sock.state).toBe('restoring')
		h.ws().ctl({ type: 'live' })
		h.ws().msg(enc('$ '))
		h.ws().ctl({ type: 'session', session: { id: 'abc', title: 'renamed', clients: 2 } })
		expect(h.out).toEqual(['old\r\n', '$ '])
		expect(h.sock.opts.onHello).toHaveBeenCalledWith({ id: 'abc', title: 'T1' }, 5)
		expect(h.sock.opts.onSession).toHaveBeenCalledWith({ id: 'abc', title: 'renamed', clients: 2 })
		expect(h.states()).toEqual(['connecting', 'restoring', 'live'])
	})

	test('input is binary, resize is NUL+JSON text', () => {
		const h = harness()
		h.sock.open()
		expect(h.sock.send(new Uint8Array([1]))).toBe(false) // not open yet
		h.ws().open()
		h.sock.send(new Uint8Array([108, 115]))
		h.sock.sendControl({ type: 'resize', cols: 100, rows: 30 })
		expect(h.ws().sent[0]).toBeInstanceOf(Uint8Array)
		expect(h.ws().sent[1]).toBe('\u0000{"type":"resize","cols":100,"rows":30}')
	})

	test('exit ends it for good: no reconnect after the close that follows', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		h.ws().ctl({ type: 'live' })
		h.ws().ctl({ type: 'exit', code: 7, reason: 'exited' })
		h.ws().drop(1000, 'exited')
		expect(h.sock.state).toBe('exited')
		expect(h.sock.opts.onExit).toHaveBeenCalledWith({ code: 7, reason: 'exited' })
		expect(h.timers).toHaveLength(0)
		expect(FakeWS.instances).toHaveLength(1)
	})
})

describe('reconnect', () => {
	test('a dropped connection reattaches with backoff, and resets the backoff once live', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		h.ws().ctl({ type: 'live' })
		h.ws().drop(1006)
		expect(h.sock.state).toBe('reconnecting')
		expect(h.timers[0].ms).toBe(RETRY_DELAYS[0])
		h.timers.shift().fn()
		expect(FakeWS.instances).toHaveLength(2)
		h.ws().open()
		h.ws().ctl({ type: 'hello', replay_bytes: 0 })
		h.ws().ctl({ type: 'live' })
		expect(h.sock.attempt).toBe(0)
	})

	test('1013 "viewer too slow" reattaches at once', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		h.ws().ctl({ type: 'live' })
		h.ws().drop(1013, 'viewer too slow, reattach')
		expect(FakeWS.instances).toHaveLength(2)
		expect(h.timers).toHaveLength(0)
	})

	test('failed handshake + probe says gone (404) -> gone, no more retries', async () => {
		const verify = vi.fn(() => Promise.resolve('gone'))
		const h = harness({ verify })
		h.sock.open()
		h.ws().drop(1006)
		await flush()
		expect(verify).toHaveBeenCalled()
		expect(h.sock.state).toBe('gone')
		expect(h.timers).toHaveLength(0)
	})

	test('failed handshake + server unreachable -> keeps retrying', async () => {
		const verify = vi.fn(() => Promise.resolve('unknown'))
		const h = harness({ verify })
		h.sock.open()
		h.ws().drop(1006)
		await flush()
		expect(h.sock.state).toBe('reconnecting')
		h.timers.shift().fn()
		h.ws().drop(1006)
		await flush()
		expect(h.timers[0].ms).toBe(RETRY_DELAYS[1])
	})

	test('retryNow skips the wait', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		h.ws().drop(1006)
		h.sock.retryNow()
		expect(FakeWS.instances).toHaveLength(2)
		expect(h.timers).toHaveLength(0)
	})

	test('detach closes the socket and never reconnects', () => {
		const h = harness()
		h.sock.open()
		h.ws().open()
		const ws = h.ws()
		h.sock.detach()
		expect(ws.closed).toBe(true)
		ws.drop(1006)
		expect(h.timers).toHaveLength(0)
		expect(h.sock.state).toBe('closed')
		h.sock.open()
		expect(FakeWS.instances).toHaveLength(1)
	})
})

describe('legacy (non-persistent) endpoint', () => {
	test('open = live, close = ended, no reconnect', () => {
		const h = harness({ persistent: false })
		h.sock.open()
		h.ws().open()
		expect(h.sock.state).toBe('live')
		h.ws().msg('error: container not running')
		h.ws().drop(1011, 'failed')
		expect(h.sock.state).toBe('ended')
		expect(h.events[h.events.length - 1][1]).toMatchObject({ code: 1011, opened: true })
		expect(h.out).toEqual(['TEXT:error: container not running'])
		expect(h.timers).toHaveLength(0)
	})
})
