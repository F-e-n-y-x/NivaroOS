// Download Station is an optional sidecar: its dock entry only exists when
// the sidecar answers its (unauthenticated) health check, asked through
// the gateway's same-origin route (so it also works over https and behind
// a reverse proxy or tunnel). Any failure = not installed.
// Cached per page load (shared by the Dock and the app grid); callers pass
// force=true on RELOAD_APP_LIST to re-check.
let dsInstalledPromise = null
export function checkDownloadStationInstalled(force = false) {
	if (force || !dsInstalledPromise) {
		const url = '/v1/download-station/health'
		const ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null
		const timer = ctrl ? setTimeout(() => ctrl.abort(), 4000) : 0
		dsInstalledPromise = fetch(url, { cache: 'no-store', credentials: 'omit', signal: ctrl ? ctrl.signal : undefined })
			.then(res => (res.ok ? res.json() : null))
			.then(body => !!(body && body.installed))
			.catch(() => false)
			.finally(() => clearTimeout(timer))
	}
	return dsInstalledPromise
}
