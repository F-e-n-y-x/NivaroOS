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
	return async function handleUnauthorized(error) {
		const config = error && error.config
		if (!config || config._retried || /\/users\/refresh$/.test(config.url || '')) {
			if (config && /\/users\/refresh$/.test(config.url || '')) once()
			throw error
		}
		if (!inFlight) {
			inFlight = Promise.resolve()
				.then(refresh)
				.finally(() => {
					inFlight = null
				})
		}
		let token
		try {
			token = await inFlight
		} catch (e) {
			once()
			throw error
		}
		config._retried = true
		config.headers = { ...(config.headers || {}), Authorization: token }
		return retry(config)
	}
}
