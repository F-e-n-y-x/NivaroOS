// What to do when a request comes back 401: refresh the access token once
// (however many requests are waiting), retry them with the new token, and
// if the refresh fails, reject them all - and log out only when the
// session really ended (err.sessionEnded, see tokenCoordinator.js), never
// because a refresh couldn't be done right now.
//
// The previous version kept waiting requests in an array that was only
// ever resolved on success: after a failed refresh they stayed pending
// forever (keeping the components that made them in memory), `isRefreshing`
// never reset so no later refresh could happen, and a retried request lost
// its headers.
export function makeUnauthorizedHandler({ refresh, retry, logout }) {
	let inFlight = null
	let loggedOut = false
	const once = (why) => {
		if (loggedOut) return
		loggedOut = true
		setTimeout(() => (loggedOut = false), 5000)
		logout(why)
	}
	// The same single refresh, for callers that have no failed request to
	// retry (the message-bus socket after its handshake was refused):
	// shares the in-flight refresh with 401 handling so a rotating refresh
	// token is never spent twice. Rejects on failure; does not log out.
	// sentToken: the access token that was just refused (lets the refresh
	// use newer tokens another tab stored instead of refreshing again).
	const refreshNow = (sentToken) => {
		if (!inFlight) {
			inFlight = Promise.resolve()
				.then(() => refresh(sentToken))
				.finally(() => {
					inFlight = null
				})
		}
		return inFlight
	}
	async function handleUnauthorized(error) {
		const config = error && error.config
		// The refresh call's own answer is the coordinator's to judge.
		if (!config || config._retried || /\/users\/refresh$/.test(config.url || '')) {
			throw error
		}
		let token
		try {
			token = await refreshNow((config.headers && config.headers.Authorization) || '')
		} catch (e) {
			if (e && e.sessionEnded) once(e)
			throw error
		}
		config._retried = true
		config.headers = { ...(config.headers || {}), Authorization: token }
		return retry(config)
	}
	handleUnauthorized.refreshNow = refreshNow
	return handleUnauthorized
}
