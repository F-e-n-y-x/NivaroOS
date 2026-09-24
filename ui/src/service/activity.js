// Persistent activity & notification history manager for NivaroOS.
//
// History is kept per signed-in account (localStorage key suffixed with
// the user's id, or username when no id is known) so two accounts using
// the same browser never see each other's notifications. The signed-in
// user is resolved lazily on every access - this singleton is created at
// import time, before login - from the `user` record Login/Welcome store
// in localStorage (the Vuex copy is empty again after a reload).
//
// Several tabs of the same account stay in sync through the `storage`
// event: a write in one tab reloads the list in every other tab.
const KEY_PREFIX = 'nivaroos_activity_history'
// Pre-per-user history: nobody can tell which account it belonged to, so
// it is dropped rather than shown to whoever signs in next.
const LEGACY_KEY = 'nivaroos_activity_history'
const MAX_HISTORY = 100

function currentUserKey() {
	if (typeof window === 'undefined') return null
	try {
		const raw = localStorage.getItem('user')
		if (!raw) return null
		const user = JSON.parse(raw) || {}
		const id = user.id !== undefined && user.id !== null && String(user.id) !== '0' ? `id-${user.id}` : ''
		const name = user.username ? `u-${user.username}` : ''
		const who = id || name
		return who ? `${KEY_PREFIX}:${who}` : null
	} catch (e) {
		return null
	}
}

class ActivityService {
	constructor() {
		this.listeners = new Set()
		this._key = undefined
		this._activities = []
		if (typeof window !== 'undefined') {
			try { localStorage.removeItem(LEGACY_KEY) } catch (e) { /* storage blocked */ }
			window.addEventListener('storage', (e) => {
				// Another tab changed this account's history (or signed a
				// different account in/out - `user` changed).
				if (e.key === null || e.key === 'user' || e.key === this._key) {
					this._key = undefined
					this._sync()
					this.notify()
				}
			})
		}
		this._sync()
	}

	// Re-reads the list when the signed-in account changed since the last access.
	_sync() {
		const key = currentUserKey()
		if (key === this._key) return false
		this._key = key
		this._activities = this.load()
		return true
	}

	load() {
		if (typeof window === 'undefined' || !this._key) return []
		try {
			const raw = localStorage.getItem(this._key)
			const list = raw ? JSON.parse(raw) : []
			return Array.isArray(list) ? list : []
		} catch (e) {
			console.error('Failed to load activity history:', e)
			return []
		}
	}

	save() {
		if (typeof window !== 'undefined' && this._key) {
			try {
				localStorage.setItem(this._key, JSON.stringify(this._activities.slice(0, MAX_HISTORY)))
			} catch (e) {
				console.error('Failed to persist activity history:', e)
			}
		}
		this.notify()
	}

	subscribe(fn) {
		this.listeners.add(fn)
		return () => this.listeners.delete(fn)
	}

	notify() {
		this.listeners.forEach(fn => {
			try {
				fn(this.getAll())
			} catch (e) {
				console.error(e)
			}
		})
	}

	getAll() {
		if (this._sync()) this.notify()
		return [...this._activities]
	}

	getUnreadCount() {
		this._sync()
		return this._activities.filter(a => !a.read).length
	}

	add({ title, message = '', type = 'system', status = 'info', action = null, icon = '' }) {
		if (!title) return
		this._sync()
		const item = {
			id: 'act-' + Date.now() + '-' + Math.random().toString(36).substr(2, 6),
			title: String(title),
			message: message === null || message === undefined ? '' : String(message),
			type, // 'app' | 'storage' | 'usb' | 'vm' | 'schedule' | 'system'
			status, // 'info' | 'success' | 'warning' | 'error'
			timestamp: new Date().toISOString(),
			read: false,
			action,
			icon: icon ? String(icon) : ''
		}

		// Prevent exact duplicates within 2 seconds
		if (this._activities.length > 0) {
			const prev = this._activities[0]
			if (prev.title === item.title && prev.message === item.message && (Date.now() - new Date(prev.timestamp).getTime()) < 2000) {
				return prev
			}
		}

		this._activities.unshift(item)
		if (this._activities.length > MAX_HISTORY) {
			this._activities = this._activities.slice(0, MAX_HISTORY)
		}
		this.save()
		return item
	}

	markAsRead(id) {
		this._sync()
		const act = this._activities.find(a => a.id === id)
		if (act && !act.read) {
			act.read = true
			this.save()
		}
	}

	markAllAsRead() {
		this._sync()
		let changed = false
		this._activities.forEach(a => {
			if (!a.read) {
				a.read = true
				changed = true
			}
		})
		if (changed) this.save()
	}

	remove(id) {
		this._sync()
		const prevLen = this._activities.length
		this._activities = this._activities.filter(a => a.id !== id)
		if (this._activities.length !== prevLen) {
			this.save()
		}
	}

	clear() {
		this._sync()
		this._activities = []
		this.save()
	}
}

export const activityService = new ActivityService()
export default activityService
