// The web UI's one socket.io connection to the message bus
// (/v2/message_bus/socket.io/) - every live update (widgets, notifications,
// file operations, app installs, backup progress) arrives through it.
//
// The bus requires an access token on every subscription (a browser can't
// set headers on a WebSocket, so it rides in ?token=, which engine.io
// repeats on each polling and websocket request). This keeps that token
// current:
//   - no token (login page): stay disconnected instead of being refused
//     every few seconds;
//   - every reconnect attempt reads the current token, so after the
//     axios 401 flow refreshed it, the next reconnect uses the new one;
//   - a refused handshake (connect_error) with a token in hand refreshes
//     it once per failure streak and reconnects;
//   - token set (login / refresh) while disconnected: connect right away;
//     token cleared (logout): disconnect.
//
// Written against socket.io-client 2.x (engine.io 3): the handshake query
// lives in manager.opts.query and is read each time an engine is created.
// Pure of Vue/axios - everything is passed in (see messageBusSocket.spec.js).

export const MESSAGE_BUS_SOCKET_PATH = '/v2/message_bus/socket.io/'

export function tokenQuery(token) {
	return token ? { token } : {}
}

/**
 * @param {object} opts
 * @param {Function} opts.io            socket.io-client's io()
 * @param {() => string} opts.getToken  current access token ('' when logged out)
 * @param {() => Promise} [opts.refreshToken]  refresh it (single-flight); resolves when stored
 * @param {(cb: (token: string) => void) => void} [opts.watchToken]  calls cb when the token changes
 * @returns the socket.io client socket (not yet connected when there's no token)
 */
export function createMessageBusSocket({ io, getToken, refreshToken, watchToken, path = MESSAGE_BUS_SOCKET_PATH }) {
	const initial = getToken() || ''
	const socket = io({
		transports: ['websocket', 'polling'],
		path,
		autoConnect: false,
		query: tokenQuery(initial),
	})
	const manager = socket.io
	let openedWith = initial
	let refreshedThisStreak = false
	let refreshing = false

	const applyToken = () => {
		const token = getToken() || ''
		manager.opts.query = tokenQuery(token)
		openedWith = token
		return token
	}

	const busy = () => manager.readyState === 'opening' || manager.reconnecting

	// (Re)connect with the current token. An attempt already under way
	// with this same token is left alone.
	const reconnect = () => {
		if (socket.connected) return
		const token = getToken() || ''
		if (!token) return
		if (busy() && token === openedWith) return
		applyToken()
		socket.close()
		socket.open()
	}

	socket.on('reconnect_attempt', applyToken)

	socket.on('connect', () => {
		refreshedThisStreak = false
	})

	socket.on('connect_error', () => {
		if (refreshedThisStreak || refreshing || !refreshToken || !getToken()) return
		refreshedThisStreak = true
		refreshing = true
		Promise.resolve()
			.then(refreshToken)
			.then(
				() => {
					refreshing = false
					reconnect()
				},
				() => {
					// Refresh refused: keep backing off with what we have; the
					// next API call's 401 handling logs the user out.
					refreshing = false
				}
			)
	})

	if (watchToken) {
		watchToken((token) => {
			if (!token) {
				manager.opts.query = {}
				openedWith = ''
				if (socket.connected || busy()) socket.close()
				return
			}
			if (socket.connected) {
				// Already authenticated; the next reconnect picks it up.
				manager.opts.query = tokenQuery(token)
				openedWith = token
				return
			}
			reconnect()
		})
	}

	if (initial) socket.open()

	return socket
}
