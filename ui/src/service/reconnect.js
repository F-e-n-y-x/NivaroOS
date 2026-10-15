// The server stopped answering (restarting, a reboot, the network): that is
// not a sign-out. Keep the session, say "Reconnecting...", and ask again
// with backoff until it answers; the open desktop carries on from there.
// Only the auth service refusing the session signs out (authRefresh.js).
// A failed request only triggers a probe: one service being down (a
// sidecar's 502) shows nothing while the server itself still answers.

// No answer at all, or the gateway saying the service behind it is down.
export function isOutage(error) {
	if (!error || error.__CANCEL__ || error.code === 'ERR_CANCELED') return false
	const status = error.response && error.response.status
	return !error.response || status === 502 || status === 503 || status === 504
}

export function createReconnect({ probe, onChange, setTimer = setTimeout, minMs = 2000, maxMs = 30000 }) {
	let down = false
	let delay = minMs
	let timer = null
	const set = (v) => {
		if (v === down) return
		down = v
		onChange(v)
	}
	const up = () => {
		delay = minMs
		set(false)
	}
	const schedule = () => {
		if (!timer) timer = setTimer(tick, delay)
	}
	function tick() {
		timer = null
		probe().then(up, () => {
			set(true)
			delay = Math.min(delay * 2, maxMs)
			schedule()
		})
	}
	return { failed: schedule, ok: up }
}
