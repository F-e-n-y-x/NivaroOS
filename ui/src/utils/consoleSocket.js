// The WebSocket under a noVNC console (Host Desktop, VM console), watched
// for a dead link. A browser keeps calling a socket "open" for minutes
// after the laptop slept or the network changed, so the viewer showed a
// frozen picture as if it were live. vm-sidecar sends an empty text frame
// when the session starts and whenever the screen has been still for 4 s
// (RFB ignores it); once a socket has sent one, silence for STALE_MS
// means the link is gone.
// Against an older sidecar (no heartbeats) the watch never arms.
export const STALE_MS = 12000
export const CHECK_MS = 2000
// After a background tab or a frozen page the first check runs before the
// messages queued meanwhile: the link gets this long to show it's alive.
export const RESUME_GRACE_MS = 5000

export function watchedSocket(url, onStale, now = () => Date.now()) {
	const ws = new WebSocket(url)
	let last = now()
	let lastCheck = last
	let heartbeats = false
	let timer = null
	const stop = () => {
		clearInterval(timer)
		timer = null
	}
	const check = () => {
		const t = now()
		// Checks this late mean throttled timers (hidden tab) or a frozen
		// page, not a dead link (switching tabs must not reconnect).
		if (t - lastCheck > 3 * CHECK_MS) last = Math.max(last, t - STALE_MS + RESUME_GRACE_MS)
		lastCheck = t
		if (heartbeats && timer && ws.readyState === 1 && t - last > STALE_MS) {
			stop()
			onStale()
		}
	}
	ws.addEventListener('message', (e) => {
		last = now()
		if (typeof e.data === 'string') heartbeats = true
	})
	ws.addEventListener('close', stop)
	timer = setInterval(check, CHECK_MS)
	return { ws, check, stop }
}

// Backoff between automatic reconnects: 1 s, 2 s, 4 s, 8 s, then 15 s.
export const reconnectDelay = (attempt) => Math.min(15000, 1000 * 2 ** attempt)
