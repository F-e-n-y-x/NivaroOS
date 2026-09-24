// An update replaces the web files under any open tab. The tab keeps
// running the old build, whose lazily loaded pieces (windows, widgets)
// no longer exist on the server - they fail to load, and parts of the
// desktop stay empty until a manual reload. This watches for both signs:
//
//  - a chunk that fails to load: reload once (guarded, so a real outage
//    can't turn into a reload loop);
//  - the server coming back with a different build (after the message-bus
//    socket reconnects, or the tab becomes visible again): offer a reload,
//    since reloading on its own could throw away what someone is typing.

const RELOAD_GUARD_KEY = 'nvos_chunk_reload_at'
const RELOAD_GUARD_MS = 60 * 1000
const CHECK_THROTTLE_MS = 60 * 1000

// The hashed name of the main bundle identifies the build.
export function buildIdFromHtml(html) {
	const m = /<script[^>]+src="[^"]*\/(app\.[0-9a-f]+\.js)"/.exec(html || '')
	return m ? m[1] : ''
}

export function currentBuildId(doc = document) {
	const s = doc.querySelector('script[src*="/app."]')
	return s ? buildIdFromHtml(s.outerHTML) : ''
}

export function isChunkLoadError(err) {
	if (!err) return false
	const text = `${err.name || ''} ${err.message || err}`
	return /ChunkLoadError|Loading (CSS )?chunk [\w-]+ failed/i.test(text)
}

function reloadOnce() {
	let last = 0
	try {
		last = Number(sessionStorage.getItem(RELOAD_GUARD_KEY)) || 0
	} catch (e) {}
	if (Date.now() - last < RELOAD_GUARD_MS) return false
	try {
		sessionStorage.setItem(RELOAD_GUARD_KEY, String(Date.now()))
	} catch (e) {}
	window.location.reload()
	return true
}

/**
 * @param {{ router?: object, socket?: object, onUpdate: () => void }} opts
 *   onUpdate is called once when a newer build is found on the server.
 */
export function watchForUpdates({ router, socket, onUpdate }) {
	const running = currentBuildId()
	let lastCheck = 0
	let notified = false

	const check = async (force) => {
		if (!running || notified) return
		if (!force && Date.now() - lastCheck < CHECK_THROTTLE_MS) return
		lastCheck = Date.now()
		try {
			const res = await fetch('/index.html', { cache: 'no-store', credentials: 'same-origin' })
			if (!res.ok) return
			const served = buildIdFromHtml(await res.text())
			if (served && served !== running) {
				notified = true
				onUpdate()
			}
		} catch (e) {
			// Server still restarting: the next reconnect checks again.
		}
	}

	if (router && router.onError) {
		router.onError((err) => {
			if (isChunkLoadError(err)) reloadOnce()
		})
	}
	window.addEventListener('unhandledrejection', (e) => {
		if (isChunkLoadError(e.reason)) reloadOnce()
	})
	// Async components (desktop windows) swallow the rejection and only
	// warn; the failed <script>/<link> element is still reported here.
	window.addEventListener(
		'error',
		(e) => {
			const el = e.target
			if (!el || (el.tagName !== 'SCRIPT' && el.tagName !== 'LINK')) return
			const url = el.src || el.href || ''
			if (url.startsWith(window.location.origin)) check(true)
		},
		true
	)

	if (socket && socket.on) {
		let wasDisconnected = false
		socket.on('disconnect', () => (wasDisconnected = true))
		socket.on('connect', () => {
			if (wasDisconnected) {
				wasDisconnected = false
				check(true)
			}
		})
	}
	document.addEventListener('visibilitychange', () => {
		if (!document.hidden) check(false)
	})
}
