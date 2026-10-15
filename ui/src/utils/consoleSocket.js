// The WebSocket under a noVNC console (Host Desktop, VM console), watched
// for a dead link. A browser keeps calling a socket "open" for minutes
// after the laptop slept or the network changed, so the viewer showed a
// frozen picture as if it were live. vm-sidecar sends an empty text frame
// when the session starts and whenever the screen has been still for 4 s
// (RFB ignores it); once a socket has sent one, silence for STALE_MS
// means the link is gone.
// Against an older sidecar (no heartbeats) the watch never arms.
export const STALE_MS = 12000

export function watchedSocket(url, onStale, now = () => Date.now()) {
	const ws = new WebSocket(url)
	let last = now()
	let heartbeats = false
	let timer = null
	const stop = () => {
		clearInterval(timer)
		timer = null
	}
	const check = () => {
		if (heartbeats && timer && ws.readyState === 1 && now() - last > STALE_MS) {
			stop()
			onStale()
		}
	}
	ws.addEventListener('message', (e) => {
		last = now()
		if (typeof e.data === 'string') heartbeats = true
	})
	ws.addEventListener('close', stop)
	timer = setInterval(check, 2000)
	return { ws, check, stop }
}
