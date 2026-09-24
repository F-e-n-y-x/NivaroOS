// The message bus's persisted notification feed in the web UI.
//
// The server keeps outcomes worth telling someone about (a backup failed,
// an app was installed, a format finished) for 30 days, with read and
// dismissed state per user - see
// services/message-bus/service/notification_classify.go for which events
// count. This module
//   - loads the feed (GET /v2/message_bus/notifications) into
//     activityService, next to the browser's own local entries;
//   - adds entries live from the socket event message-bus:notification:created;
//   - reloads when message-bus:notification:state says this user's state
//     changed on another device, and when the socket reconnects (it may
//     have missed events);
//   - sends read / dismiss / mark-all-read / clear-all to the server
//     (activityService calls the backend given here).
//
// Pure of Vue and axios: transport, activity, t/te and the window helpers
// are passed in (see notificationFeed.spec.js).

export const FEED_BASE = '/v2/message_bus/notifications'
export const FEED_PAGE = 100
export const FEED_EVENTS = Object.freeze({
	CREATED: 'message-bus:notification:created',
	STATE: 'message-bus:notification:state'
})

// App-management events the server persists (mirror of appOutcomes in
// notification_classify.go). While the feed works, ContainerInstallStatus
// doesn't log these locally too - they arrive from the feed instead.
const PERSISTED_APP_EVENTS = new Set([
	'app:install-end', 'app:install-error',
	'app:uninstall-end', 'app:uninstall-error',
	'app:update-end', 'app:update-error',
	'app:apply-changes-error', 'app:start-error', 'app:stop-error', 'app:restart-error'
])

export function isPersistedEvent(event) {
	const name = event && (event.Name || event.name)
	if (!PERSISTED_APP_EVENTS.has(name)) return false
	const props = (event && (event.Properties || event.properties)) || {}
	// "Already up to date" isn't stored: keep showing it locally.
	return !(name === 'app:update-end' && props['docker:image:updated'] === 'false')
}

// English UI strings (en_US keys) for the server's app/storage keys.
const APP_TITLES = Object.freeze({
	'notify.app.installed': 'App installed',
	'notify.app.install_failed': 'App installation failed',
	'notify.app.uninstalled': 'App uninstalled',
	'notify.app.uninstall_failed': 'App uninstall failed',
	'notify.app.updated': 'App updated',
	'notify.app.update_failed': 'App update failed',
	'notify.app.apply_failed': 'Applying app settings failed',
	'notify.app.start_failed': 'App failed to start',
	'notify.app.stop_failed': 'App failed to stop',
	'notify.app.restart_failed': 'App failed to restart'
})

const TYPE_OF_CATEGORY = Object.freeze({ backup: 'backup', app: 'app', storage: 'storage', system: 'system' })
const LEVELS = new Set(['info', 'success', 'warning', 'error'])

function parseJSONObject(v) {
	if (v && typeof v === 'object' && !Array.isArray(v)) return v
	if (typeof v !== 'string' || !v) return null
	try {
		const o = JSON.parse(v)
		return o && typeof o === 'object' && !Array.isArray(o) ? o : null
	} catch (e) {
		return null
	}
}

function localizedAppTitle(args, pickI18n) {
	const raw = args && args.app_title
	const titles = parseJSONObject(raw)
	const picked = titles && pickI18n ? pickI18n(titles) : ''
	return picked || (args && args.app) || ''
}

/**
 * Turns a feed entry (the REST shape, or the socket event's properties)
 * into an activityService entry.
 *
 * @param {object} raw
 * @param {object} deps
 * @param {Function} deps.t              vue-i18n $t
 * @param {Function} [deps.te]           vue-i18n $te
 * @param {Function} [deps.renderBackupMessage]  (t, {key, args}) => text
 * @param {Function} [deps.backupWindow] (t, kind, props) => OPEN_WINDOW payload
 * @param {Function} [deps.pickI18n]     {en_us: ..} => localized string
 */
export function feedItemToActivity(raw, deps) {
	const { t, te = () => false, renderBackupMessage, backupWindow, pickI18n } = deps
	const feedId = Number(raw.id)
	if (!feedId) return null
	const args = parseJSONObject(raw.args) || {}
	const action = parseJSONObject(raw.action)
	const category = TYPE_OF_CATEGORY[raw.category] || 'system'
	const level = LEVELS.has(raw.level) ? raw.level : 'info'

	let title = raw.title || ''
	let message = raw.message || ''
	const key = raw.key || ''
	if (APP_TITLES[key]) {
		title = t(APP_TITLES[key])
		const app = localizedAppTitle(args, pickI18n)
		message = args.error ? t('{name}: {error}', { name: app, error: args.error }) : app
	} else if (key === 'notify.storage.job_done' || key === 'notify.storage.job_failed') {
		const failed = key === 'notify.storage.job_failed'
		const format = args.kind === 'format'
		title = t(failed ? (format ? 'Formatting failed' : 'Storage setup failed') : (format ? 'Formatting finished' : 'Storage setup finished'))
	} else if (key.startsWith('backup.') && te(key)) {
		title = t('backup.app.title')
		const rendered = renderBackupMessage ? renderBackupMessage(t, { key, args }) : t(key, args)
		if (rendered) message = rendered
	}

	let act = null
	if (action && action.target === 'backup' && backupWindow) {
		const w = parseJSONObject(action.window) || {}
		try {
			act = { label: t('View'), window: backupWindow(t, w.kind || 'app', w.props || {}) }
		} catch (e) {
			act = { label: t('View'), window: backupWindow(t, 'app', { section: 'activity' }) }
		}
	} else if (action && action.target === 'settings') {
		const props = parseJSONObject(action.props) || {}
		act = {
			label: t('View'),
			window: { id: 'settings', title: t('Settings'), component: 'SettingsApp', width: 760, height: 540, props }
		}
	}

	return {
		id: 'feed-' + feedId,
		feedId,
		title: String(title),
		message: String(message),
		type: category,
		status: level,
		timestamp: raw.time || new Date().toISOString(),
		read: !!raw.read,
		action: act,
		icon: raw.icon ? String(raw.icon) : '',
		sourceId: raw.source_id || '',
		eventName: raw.event_name || ''
	}
}

/**
 * @param {object} opts
 * @param {{get: Function, post: Function}} opts.transport  axios-like: get(url, {params}), post(url, body)
 * @param {object} opts.activity   activityService
 * @param {() => object} opts.deps  feedItemToActivity deps (read at use, so a language change applies)
 * @param {() => (number|string|null)} [opts.currentUserId]
 * @param {(fn: Function, ms: number) => any} [opts.setTimer]
 */
export function createNotificationFeed({ transport, activity, deps, currentUserId = () => null, setTimer = setTimeout, clearTimer = clearTimeout }) {
	let available = false
	let loading = null
	let reloadAgain = false
	let timer = null
	let generation = 0

	const toItems = list => (Array.isArray(list) ? list : [])
		.map(raw => feedItemToActivity(raw, deps()))
		.filter(Boolean)

	const post = (path, body) => transport.post(FEED_BASE + path, body)

	activity.attachRemote({
		markRead: ids => post('/read', { ids: ids.map(Number).filter(Boolean) }),
		markAllRead: upTo => post('/read', upTo ? { all: true, up_to: upTo } : { all: true }),
		dismiss: ids => post('/dismiss', { ids: ids.map(Number).filter(Boolean) }),
		dismissAll: upTo => post('/dismiss', upTo ? { all: true, up_to: upTo } : { all: true })
	})

	// One load at a time; a request for another while one runs is served
	// by a second load right after it (it may carry newer state).
	function load() {
		if (loading) {
			reloadAgain = true
			return loading
		}
		const mine = generation
		loading = Promise.resolve()
			.then(() => transport.get(FEED_BASE, { params: { limit: FEED_PAGE } }))
			.then(res => {
				if (mine !== generation) return
				const body = (res && res.data) || {}
				available = true
				activity.setRemote(toItems(body.data))
			})
			.catch(() => {
				// Old message bus (no feed) or a transient failure: keep
				// the local history working on its own.
				if (mine === generation) available = false
			})
			.finally(() => {
				loading = null
				if (reloadAgain) {
					reloadAgain = false
					load()
				}
			})
		return loading
	}

	function scheduleReload(ms = 250) {
		if (timer) clearTimer(timer)
		timer = setTimer(() => {
			timer = null
			load()
		}, ms)
	}

	return {
		load,
		get available() {
			return available
		},
		// Signed out: forget the feed (activityService drops server entries
		// on its own when the account changes).
		reset() {
			generation++
			available = false
			if (timer) clearTimer(timer)
			timer = null
			activity.setRemote([])
		},
		onCreated(props) {
			if (!props) return
			const item = feedItemToActivity(props, deps())
			if (!item) return
			// A created event may reach us before the first load returned.
			available = true
			activity.upsertRemote(item)
		},
		onState(props) {
			const who = props && props.user_id
			const me = currentUserId()
			if (who !== undefined && who !== null && who !== '' && me !== null && me !== undefined && String(who) !== String(me)) return
			scheduleReload()
		},
		// The socket (re)connected: we may have missed events meanwhile.
		onReconnect() {
			scheduleReload(0)
		},
		// Whether an app-management event will arrive from the feed, so the
		// caller shouldn't log it locally as well.
		persists(event) {
			return available && isPersistedEvent(event)
		}
	}
}
