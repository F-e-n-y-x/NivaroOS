// Persistent activity & notification history manager for NivaroOS
const STORAGE_KEY = 'nivaroos_activity_history'
const MAX_HISTORY = 100

class ActivityService {
	constructor() {
		this.listeners = new Set()
		this._activities = this.load()
	}

	load() {
		if (typeof window === 'undefined') return []
		try {
			const raw = localStorage.getItem(STORAGE_KEY)
			return raw ? JSON.parse(raw) : []
		} catch (e) {
			console.error('Failed to load activity history:', e)
			return []
		}
	}

	save() {
		if (typeof window === 'undefined') return
		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify(this._activities.slice(0, MAX_HISTORY)))
		} catch (e) {
			console.error('Failed to persist activity history:', e)
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
				fn(this._activities)
			} catch (e) {
				console.error(e)
			}
		})
	}

	getAll() {
		return [...this._activities]
	}

	getUnreadCount() {
		return this._activities.filter(a => !a.read).length
	}

	add({ title, message = '', type = 'system', status = 'info', action = null }) {
		if (!title) return
		const item = {
			id: 'act-' + Date.now() + '-' + Math.random().toString(36).substr(2, 6),
			title,
			message: String(message),
			type, // 'app' | 'storage' | 'usb' | 'vm' | 'schedule' | 'system'
			status, // 'info' | 'success' | 'warning' | 'error'
			timestamp: new Date().toISOString(),
			read: false,
			action
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
		const act = this._activities.find(a => a.id === id)
		if (act && !act.read) {
			act.read = true
			this.save()
		}
	}

	markAllAsRead() {
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
		const prevLen = this._activities.length
		this._activities = this._activities.filter(a => a.id !== id)
		if (this._activities.length !== prevLen) {
			this.save()
		}
	}

	clear() {
		this._activities = []
		this.save()
	}
}

export const activityService = new ActivityService()
export default activityService
