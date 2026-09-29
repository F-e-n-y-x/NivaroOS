// Client side of the persistent terminal session protocol
// (docs/specs/2026-09-29-terminal-sessions.md).
//
// Server -> client on /attach:
//   BINARY                      terminal output
//   TEXT "\0" + JSON            control: hello | live | session | exit
//   TEXT (anything else)        a human-readable status line
// Client -> server (wsterm v2): BINARY input, TEXT "\0"+JSON resize.
//
// TermSocket keeps one viewer attached to one session: it reattaches by
// itself when the connection drops (the shell keeps running on the
// server), reattaches at once when the server drops a slow viewer (1013),
// and finds out whether a failed handshake means "gone" (404) or just
// "can't reach the server right now" by asking the REST API, since a
// browser WebSocket can't see the handshake's HTTP status.

export const CLOSE_TOO_SLOW = 1013
export const RETRY_DELAYS = [500, 1000, 2000, 4000, 8000, 15000]

export function retryDelay(attempt) {
	return RETRY_DELAYS[Math.min(Math.max(0, attempt), RETRY_DELAYS.length - 1)]
}

export function parseFrame(data) {
	if (typeof data === 'string') {
		if (data.charCodeAt(0) === 0) {
			try {
				const msg = JSON.parse(data.slice(1))
				if (msg && typeof msg === 'object' && typeof msg.type === 'string') return { kind: 'control', msg }
			} catch (e) {
				/* malformed control frame */
			}
			return { kind: 'ignore' }
		}
		return { kind: 'text', text: data }
	}
	if (data instanceof ArrayBuffer) return { kind: 'output', data: new Uint8Array(data) }
	if (ArrayBuffer.isView(data)) return { kind: 'output', data: new Uint8Array(data.buffer, data.byteOffset, data.byteLength) }
	return { kind: 'ignore' }
}

export function controlFrame(msg) {
	return '\u0000' + JSON.stringify(msg)
}

// States:
//   connecting    socket opening (first time or a reattach)
//   restoring     hello received, replaying scrollback
//   live          replay done, live output
//   reconnecting  connection lost, next attempt scheduled (detail.delay)
//   exited        the session ended (detail: {code, reason})
//   gone          the session no longer exists on the server
//   ended         (non-persistent/legacy only) the socket closed
//   closed        detach() called
export class TermSocket {
	constructor(opts) {
		this.opts = Object.assign({
			persistent: true,
			WebSocketImpl: typeof WebSocket !== 'undefined' ? WebSocket : null,
			setTimeout: (fn, ms) => setTimeout(fn, ms),
			clearTimeout: (t) => clearTimeout(t),
			// () => Promise<'gone' | 'alive' | 'unknown'>
			verify: null,
		}, opts)
		this.state = 'idle'
		this.socket = null
		this.attempt = 0
		this.everLive = false
		this.retryTimer = null
		this.opened = false
	}

	_emit(name, ...args) {
		const fn = this.opts[name]
		if (typeof fn === 'function') fn(...args)
	}

	_setState(state, detail) {
		this.state = state
		this._emit('onState', state, detail || {})
	}

	open() {
		if (this.state === 'closed') return
		this._clearRetry()
		this._connect()
	}

	_connect() {
		this._dropSocket()
		this.opened = false
		this._setState('connecting', { attempt: this.attempt })
		let ws
		try {
			ws = new this.opts.WebSocketImpl(this.opts.url())
		} catch (e) {
			this._onClose(ws, { code: 1006, reason: '' })
			return
		}
		ws.binaryType = 'arraybuffer'
		this.socket = ws
		ws.onopen = () => {
			if (ws !== this.socket) return
			this.opened = true
			if (!this.opts.persistent) {
				this._setState('live', {})
			}
			this._emit('onOpen')
		}
		ws.onmessage = (ev) => {
			if (ws !== this.socket) return
			this._onFrame(parseFrame(ev.data))
		}
		ws.onerror = () => {}
		ws.onclose = (ev) => this._onClose(ws, ev || {})
	}

	_onFrame(f) {
		if (f.kind === 'output') {
			this._emit('onOutput', f.data)
		} else if (f.kind === 'text') {
			this._emit('onText', f.text)
		} else if (f.kind === 'control') {
			const m = f.msg
			if (m.type === 'hello') {
				this._setState('restoring', { replayBytes: m.replay_bytes || 0 })
				this._emit('onHello', m.session || null, m.replay_bytes || 0)
			} else if (m.type === 'live') {
				this.attempt = 0
				this.everLive = true
				this._setState('live', {})
				this._emit('onLive')
			} else if (m.type === 'session') {
				if (m.session) this._emit('onSession', m.session)
			} else if (m.type === 'exit') {
				const detail = { code: typeof m.code === 'number' ? m.code : -1, reason: m.reason || 'exited' }
				this._setState('exited', detail)
				this._emit('onExit', detail)
			}
		}
	}

	_onClose(ws, ev) {
		if (ws && ws !== this.socket) return
		this.socket = null
		if (this.state === 'closed' || this.state === 'exited' || this.state === 'gone') return
		const wasOpen = this.opened
		this.opened = false
		if (!this.opts.persistent) {
			this._setState('ended', { code: ev.code, reason: ev.reason || '', opened: wasOpen })
			return
		}
		if (ev.code === CLOSE_TOO_SLOW) {
			// Not a connection problem: come straight back for a fresh replay.
			this.attempt = 0
			this._connect()
			return
		}
		if (!wasOpen && typeof this.opts.verify === 'function') {
			// The handshake failed: 404 (gone), 401 (token), or no network.
			const pending = this.opts.verify()
			Promise.resolve(pending).then((res) => {
				if (this.state === 'closed' || this.socket) return
				if (res === 'gone') this._setState('gone', {})
				else this._scheduleRetry()
			}, () => {
				if (this.state === 'closed' || this.socket) return
				this._scheduleRetry()
			})
			return
		}
		this._scheduleRetry()
	}

	_scheduleRetry() {
		this._clearRetry()
		const delay = retryDelay(this.attempt)
		this.attempt++
		this._setState('reconnecting', { delay, attempt: this.attempt, at: Date.now() + delay })
		this.retryTimer = this.opts.setTimeout(() => {
			this.retryTimer = null
			if (this.state === 'reconnecting') this._connect()
		}, delay)
	}

	// Skip the wait (the page became visible, the network came back, the
	// user pressed "Retry now").
	retryNow() {
		if (this.state !== 'reconnecting') return
		this._clearRetry()
		this._connect()
	}

	_clearRetry() {
		if (this.retryTimer) {
			this.opts.clearTimeout(this.retryTimer)
			this.retryTimer = null
		}
	}

	_dropSocket() {
		const s = this.socket
		this.socket = null
		if (s) {
			s.onopen = s.onmessage = s.onerror = s.onclose = null
			try { s.close() } catch (e) { /* already closed */ }
		}
	}

	isOpen() {
		const OPEN = (this.opts.WebSocketImpl && this.opts.WebSocketImpl.OPEN) || 1
		return !!this.socket && this.socket.readyState === OPEN
	}

	send(bytes) {
		if (!this.isOpen()) return false
		this.socket.send(bytes)
		return true
	}

	sendControl(msg) {
		if (!this.isOpen()) return false
		this.socket.send(controlFrame(msg))
		return true
	}

	// Leave the session running on the server and stop for good.
	detach() {
		this._clearRetry()
		this._dropSocket()
		this.state = 'closed'
	}
}
