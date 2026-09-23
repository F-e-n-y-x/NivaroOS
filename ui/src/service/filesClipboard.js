// Files clipboard (Copy / Cut -> Paste), shared across every tab, browser
// and device of the same user.
//
// It used to live only in this tab's Vuex memory, so copying in one browser
// tab (or on a laptop) and pasting in another (or on a phone) could never
// work. The Vuex `operateObject` stays the one thing components read; this
// module mirrors it to localStorage (other tabs of this browser, instantly
// via the `storage` event) and to per-user server storage (other browsers
// and devices, picked up when a window regains focus).
import users from './users'

const LOCAL_KEY = 'nivaroos_files_clipboard'
const SERVER_KEY = 'files_clipboard'
// A clipboard older than this is stale - nobody expects a Cut from
// yesterday to still be pending.
const MAX_AGE_MS = 12 * 60 * 60 * 1000

let store = null
let applying = false
let current = { ts: 0 }

function valid(entry) {
	return entry && typeof entry.ts === 'number' && Date.now() - entry.ts < MAX_AGE_MS
}

function apply(entry) {
	if (!valid(entry) || entry.ts <= current.ts) return
	current = entry
	applying = true
	try {
		store.commit('SET_OPERATE_OBJECT', entry.op || null)
	} finally {
		applying = false
	}
}

async function pullServer() {
	try {
		const res = await users.getCustomStorage(SERVER_KEY)
		const entry = res && res.data && res.data.data
		if (entry && typeof entry === 'object') apply(entry)
	} catch (e) {}
}

let pushTimer = null
function push(entry) {
	try {
		localStorage.setItem(LOCAL_KEY, JSON.stringify(entry))
	} catch (e) {}
	clearTimeout(pushTimer)
	pushTimer = setTimeout(() => {
		users.setCustomStorage(SERVER_KEY, entry).catch(() => {})
	}, 300)
}

export function startClipboardSync(vuexStore) {
	if (store) return
	store = vuexStore
	try {
		const local = JSON.parse(localStorage.getItem(LOCAL_KEY) || 'null')
		if (valid(local)) apply(local)
	} catch (e) {}

	store.subscribe((mutation) => {
		if (mutation.type !== 'SET_OPERATE_OBJECT' || applying) return
		current = { ts: Date.now(), op: mutation.payload || null }
		push(current)
	})

	window.addEventListener('storage', (e) => {
		if (e.key !== LOCAL_KEY || !e.newValue) return
		try {
			apply(JSON.parse(e.newValue))
		} catch (err) {}
	})
	const onFocus = () => {
		if (document.visibilityState === 'visible') pullServer()
	}
	window.addEventListener('focus', onFocus)
	document.addEventListener('visibilitychange', onFocus)
	pullServer()
}
