// The web UI's live connection to the message bus - every live update
// (widgets, notifications, file operations, app installs, backup progress)
// arrives through it.
//
// Plain WebSockets, one per event source (GET /v2/message_bus/event/{source},
// the endpoint the phone app uses too); the sources come from the
// registered event types. The bus requires an access token on every
// subscription - a browser can't set headers on a WebSocket, so it rides in
// ?token=. Listing the sources goes through the API client, so an expired
// token is refreshed there (its 401 handling) before the sockets open.
//
//   - no token (login page): stay disconnected;
//   - any socket drops: close the rest, list the sources again and reopen
//     all of them, backing off 1 s, 2 s ... 30 s;
//   - token set (login / refresh) while disconnected: connect right away;
//     token cleared (logout): disconnect.
//
// Handlers get the event in the shape the UI has always used
// ({ SourceID, Name, Properties, Timestamp, uuid }). Pure of Vue/axios -
// everything is passed in (see messageBusSocket.spec.js).

export const EVENT_TYPES_PATH = '/v2/message_bus/event_type'

export function eventPath(sourceID) {
	return `/v2/message_bus/event/${encodeURIComponent(sourceID)}`
}

// fromWire maps the WebSocket JSON (codegen.Event) to the UI's shape.
export function fromWire(msg) {
	const t = msg.timestamp ? Date.parse(msg.timestamp) : NaN
	return {
		SourceID: msg.sourceID,
		Name: msg.name,
		Properties: msg.properties || {},
		Timestamp: Number.isNaN(t) ? 0 : Math.floor(t / 1000),
		uuid: msg.uuid,
	}
}

export function sourcesOf(eventTypes) {
	return [...new Set((eventTypes || []).map((t) => t && t.sourceID).filter(Boolean))]
}

/**
 * @param {object} opts
 * @param {() => string} opts.getToken          current access token ('' when logged out)
 * @param {() => Promise<string[]>} opts.listSources  event source IDs to subscribe to
 * @param {string} opts.wsBase                  e.g. "ws://host:port"
 * @param {(cb: (token: string) => void) => void} [opts.watchToken]  calls cb when the token changes
 * @returns {{on, off, connected: boolean, open, close}}
 */
export function createMessageBusSocket({ getToken, listSources, wsBase, watchToken, WebSocketImpl = globalThis.WebSocket, timers = globalThis, maxBackoffMs = 30000 }) {
	const handlers = {}
	let sockets = []
	let generation = 0
	let attempt = 0
	let retryTimer = null
	let connected = false

	const emit = (name, ...args) => (handlers[name] || []).slice().forEach((fn) => fn(...args))

	const closeSockets = () => {
		const old = sockets
		sockets = []
		old.forEach((ws) => {
			try {
				ws.close()
			} catch (e) {}
		})
	}

	// Stop the current cycle; its sockets' late events are ignored.
	const stop = () => {
		generation++
		timers.clearTimeout(retryTimer)
		retryTimer = null
		closeSockets()
		if (connected) {
			connected = false
			emit('disconnect')
		}
	}

	const scheduleRetry = () => {
		timers.clearTimeout(retryTimer)
		if (!getToken()) return
		const delay = Math.min(maxBackoffMs, 1000 * 2 ** attempt++)
		retryTimer = timers.setTimeout(connect, delay)
	}

	async function connect() {
		stop()
		const gen = generation
		if (!getToken()) return
		let sources
		try {
			sources = await listSources()
		} catch (e) {
			sources = null
		}
		if (gen !== generation) return
		const token = getToken()
		if (!sources || !sources.length || !token) return scheduleRetry()
		let opened = 0
		sockets = sources.map((source) => {
			const ws = new WebSocketImpl(`${wsBase}${eventPath(source)}?token=${encodeURIComponent(token)}`)
			ws.onopen = () => {
				if (gen !== generation || ++opened < sources.length) return
				attempt = 0
				connected = true
				emit('connect')
			}
			ws.onmessage = (e) => {
				if (gen !== generation) return
				let msg
				try {
					msg = JSON.parse(e.data)
				} catch (err) {
					return
				}
				if (msg && msg.name) emit(msg.name, fromWire(msg))
			}
			ws.onclose = () => {
				if (gen !== generation) return
				stop()
				scheduleRetry()
			}
			return ws
		})
	}

	const bus = {
		on(name, fn) {
			;(handlers[name] = handlers[name] || []).push(fn)
		},
		off(name, fn) {
			handlers[name] = (handlers[name] || []).filter((h) => h !== fn)
		},
		get connected() {
			return connected
		},
		open() {
			attempt = 0
			return connect()
		},
		close: stop,
	}

	if (watchToken) {
		watchToken((token) => {
			if (!token) stop()
			else if (!connected) bus.open()
		})
	}

	if (getToken()) bus.open()

	return bus
}
