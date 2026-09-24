// What to do when a request comes back 401: refresh the access token once
// (however many requests are waiting), retry them with the new token, and
// if the refresh fails, reject them all and log out.
//
// The previous version kept waiting requests in an array that was only
// ever resolved on success: after a failed refresh they stayed pending
// forever (keeping the components that made them in memory), `isRefreshing`
// never reset so no later refresh could happen, and a retried request lost
// its headers.
export function makeUnauthorizedHandler({ refresh, retry, logout }) {
	let inFlight = null
	let loggedOut = false
	const once = () => {
		if (loggedOut) return
		loggedOut = true
		setTimeout(() => (loggedOut = false), 5000)
		logout()
	}
	// The same single refresh, for callers that have no failed request to
	// retry (the message-bus socket after its handshake was refused):
	// shares the in-flight refresh with 401 handling so a rotating refresh
	// token is never spent twice. Rejects on failure; does not log out.
	const refreshNow = () => {
		if (!inFlight) {
			inFlight = Promise.resolve()
				.then(refresh)
				.finally(() => {
					inFlight = null
				})
		}
		return inFlight
	}
	async function handleUnauthorized(error) {
		const config = error && error.config
		if (!config || config._retried || /\/users\/refresh$/.test(config.url || '')) {
			if (config && /\/users\/refresh$/.test(config.url || '')) once()
			throw error
		}
		let token
		try {
			token = await refreshNow()
		} catch (e) {
			once()
			throw error
		}
		config._retried = true
		config.headers = { ...(config.headers || {}), Authorization: token }
		return retry(config)
	}
	handleUnauthorized.refreshNow = refreshNow
	return handleUnauthorized
}
