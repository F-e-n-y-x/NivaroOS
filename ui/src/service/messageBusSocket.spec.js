import { describe, test, expect, vi } from 'vitest'
import { createMessageBusSocket, tokenQuery, MESSAGE_BUS_SOCKET_PATH } from './messageBusSocket'

// A socket.io-client 2.x stand-in: the socket, its manager (socket.io)
// with opts/readyState/reconnecting, and a log of the handshake queries
// each open() would have used.
function fakeIo() {
	const handlers = {}
	const opened = []
	const socket = {
		connected: false,
		on(name, fn) {
			;(handlers[name] = handlers[name] || []).push(fn)
		},
		emit(name, ...args) {
			;(handlers[name] || []).forEach((fn) => fn(...args))
		},
		open: vi.fn(() => {
			socket.io.readyState = 'opening'
			opened.push({ ...socket.io.opts.query })
		}),
		close: vi.fn(() => {
			socket.connected = false
			socket.io.readyState = 'closed'
			socket.io.reconnecting = false
		}),
	}
	const io = vi.fn((opts) => {
		socket.io = { opts, readyState: 'closed', reconnecting: false }
		return socket
	})
	// simulate the server accepting / refusing the handshake
	socket.accept = () => {
		socket.connected = true
		socket.io.readyState = 'open'
		socket.emit('connect')
	}
	socket.refuse = () => {
		socket.io.readyState = 'closed'
		socket.io.reconnecting = true
		socket.emit('connect_error', new Error('websocket error'))
	}
	return { io, socket, opened }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

function setup({ token = 'T1', refresh } = {}) {
	const f = fakeIo()
	let current = token
	let onToken = () => {}
	const refreshToken = refresh || vi.fn(async () => {
		current = 'T2'
	})
	const socket = createMessageBusSocket({
		io: f.io,
		getToken: () => current,
		refreshToken,
		watchToken: (cb) => (onToken = cb),
	})
	return {
		...f,
		socket,
		refreshToken,
		setToken(t) {
			current = t
			onToken(t)
		},
		setStoredToken(t) {
			current = t
		},
	}
}

describe('message-bus socket', () => {
	test('connects to the bus path with the token in the handshake query', () => {
		const { io, socket, opened } = setup()
		expect(io).toHaveBeenCalledWith(expect.objectContaining({ path: MESSAGE_BUS_SOCKET_PATH, autoConnect: false, transports: ['websocket', 'polling'] }))
		expect(socket.open).toHaveBeenCalledTimes(1)
		expect(opened).toEqual([{ token: 'T1' }])
	})

	test('without a token it stays disconnected, and connects on login', () => {
		const t = setup({ token: '' })
		expect(t.socket.open).not.toHaveBeenCalled()
		expect(t.socket.io.opts.query).toEqual({})
		t.setToken('LOGIN')
		expect(t.opened).toEqual([{ token: 'LOGIN' }])
	})

	test('each reconnect attempt uses the current token (e.g. after the axios 401 refresh)', () => {
		const t = setup()
		t.socket.accept()
		t.setStoredToken('FRESH') // refreshed elsewhere, not yet seen by the socket
		t.socket.emit('reconnect_attempt', 1)
		expect(t.socket.io.opts.query).toEqual({ token: 'FRESH' })
	})

	test('a refused handshake refreshes the token once per failure streak and reconnects with it', async () => {
		const t = setup()
		t.socket.refuse()
		await flush()
		expect(t.refreshToken).toHaveBeenCalledTimes(1)
		expect(t.opened[t.opened.length - 1]).toEqual({ token: 'T2' })

		t.socket.refuse() // still refused (server down): no refresh loop
		await flush()
		expect(t.refreshToken).toHaveBeenCalledTimes(1)

		t.socket.accept()
		t.socket.refuse() // a new streak after a good connection
		await flush()
		expect(t.refreshToken).toHaveBeenCalledTimes(2)
	})

	test('a failed refresh is swallowed and does not reconnect', async () => {
		const refresh = vi.fn(() => Promise.reject(new Error('refresh refused')))
		const t = setup({ refresh })
		const opens = t.socket.open.mock.calls.length
		t.socket.refuse()
		await flush()
		expect(refresh).toHaveBeenCalledTimes(1)
		expect(t.socket.open.mock.calls.length).toBe(opens)
	})

	test('no refresh attempt while logged out', async () => {
		const t = setup({ token: '' })
		t.socket.refuse()
		await flush()
		expect(t.refreshToken).not.toHaveBeenCalled()
	})

	test('a token change while connected keeps the connection but is used next time', () => {
		const t = setup()
		t.socket.accept()
		t.setToken('T9')
		expect(t.socket.close).not.toHaveBeenCalled()
		expect(t.socket.io.opts.query).toEqual({ token: 'T9' })
	})

	test('a new token while an attempt with the old one is pending restarts it', () => {
		const t = setup()
		t.setToken('T5')
		expect(t.socket.close).toHaveBeenCalledTimes(1)
		expect(t.opened).toEqual([{ token: 'T1' }, { token: 'T5' }])
		t.setToken('T5') // same token again: leave the pending attempt alone
		expect(t.opened.length).toBe(2)
	})

	test('logout disconnects and drops the token from the query', () => {
		const t = setup()
		t.socket.accept()
		t.setToken('')
		expect(t.socket.close).toHaveBeenCalledTimes(1)
		expect(t.socket.io.opts.query).toEqual({})
	})

	test('tokenQuery', () => {
		expect(tokenQuery('')).toEqual({})
		expect(tokenQuery(null)).toEqual({})
		expect(tokenQuery('x')).toEqual({ token: 'x' })
	})
})
