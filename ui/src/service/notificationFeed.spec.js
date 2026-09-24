import { describe, test, expect, beforeEach, vi } from 'vitest'
import { createNotificationFeed, feedItemToActivity, isPersistedEvent, FEED_BASE } from './notificationFeed'

function fakeStorage() {
	const data = {}
	return {
		getItem: k => (k in data ? data[k] : null),
		setItem: (k, v) => { data[k] = String(v) },
		removeItem: k => { delete data[k] },
		data
	}
}

const t = (key, args) => (args ? `${key}|${JSON.stringify(args)}` : key)
const deps = () => ({
	t,
	te: key => key.startsWith('backup.notify.'),
	renderBackupMessage: (tt, msg) => `rendered:${msg.key}:${msg.args.job}`,
	backupWindow: (tt, kind, props) => ({ id: 'backup', component: kind === 'run' ? 'BackupRunWindow' : 'BackupApp', props }),
	pickI18n: titles => titles.en_us
})

const backupItem = (id, extra = {}) => ({
	id,
	time: '2026-09-24T10:00:00Z',
	source_id: 'nivaroos-backup',
	event_name: 'nivaroos:backup:notify',
	category: 'backup',
	level: 'error',
	title: 'Backup & Sync',
	message: 'Documents failed (English)',
	key: 'backup.notify.failed',
	args: { job: 'Documents' },
	action: { target: 'backup', window: { kind: 'run', props: { runId: 'r1' } } },
	icon: '',
	read: false,
	...extra
})

describe('feedItemToActivity', () => {
	test('renders a backup entry in the viewer language with its window', () => {
		const a = feedItemToActivity(backupItem(7), deps())
		expect(a).toMatchObject({
			id: 'feed-7', feedId: 7, type: 'backup', status: 'error', read: false,
			title: 'backup.app.title', message: 'rendered:backup.notify.failed:Documents',
			timestamp: '2026-09-24T10:00:00Z'
		})
		expect(a.action).toEqual({ label: 'View', window: { id: 'backup', component: 'BackupRunWindow', props: { runId: 'r1' } } })
	})

	test('accepts the socket event shape (every property a string)', () => {
		const a = feedItemToActivity({
			id: '12', time: '2026-09-24T10:00:00Z', category: 'app', level: 'error', title: 'App installation failed',
			message: 'immich: boom', key: 'notify.app.install_failed',
			args: JSON.stringify({ app: 'immich', error: 'boom', app_title: JSON.stringify({ en_us: 'Immich' }) }),
			action: '', icon: 'https://x/icon.png'
		}, deps())
		expect(a).toMatchObject({ id: 'feed-12', feedId: 12, type: 'app', status: 'error', icon: 'https://x/icon.png', action: null })
		expect(a.title).toBe('App installation failed')
		expect(a.message).toBe('{name}: {error}|{"name":"Immich","error":"boom"}')
	})

	test('storage results open the storage settings', () => {
		const a = feedItemToActivity({ id: 3, category: 'storage', level: 'success', key: 'notify.storage.job_done', title: 'Formatting finished', message: '/dev/sdb', args: { kind: 'format' }, action: { target: 'settings', props: { section: 'storage' } } }, deps())
		expect(a.title).toBe('Formatting finished')
		expect(a.message).toBe('/dev/sdb')
		expect(a.action.window).toMatchObject({ id: 'settings', component: 'SettingsApp', props: { section: 'storage' } })
	})

	test('unknown keys, categories and levels fall back to the server text', () => {
		const a = feedItemToActivity({ id: 4, category: 'weird', level: 'loud', key: 'x.y', title: 'Hello', message: 'World', args: 'nope' }, deps())
		expect(a).toMatchObject({ type: 'system', status: 'info', title: 'Hello', message: 'World', action: null })
		expect(feedItemToActivity({ id: 0, title: 'no id' }, deps())).toBe(null)
	})
})

describe('isPersistedEvent', () => {
	test('mirrors the server list', () => {
		expect(isPersistedEvent({ Name: 'app:install-end', Properties: {} })).toBe(true)
		expect(isPersistedEvent({ Name: 'app:restart-error' })).toBe(true)
		expect(isPersistedEvent({ Name: 'app:start-end' })).toBe(false)
		expect(isPersistedEvent({ Name: 'app:update-end', Properties: { 'docker:image:updated': 'false' } })).toBe(false)
		expect(isPersistedEvent({ Name: 'app:update-end', Properties: { 'docker:image:updated': 'true' } })).toBe(true)
		expect(isPersistedEvent(null)).toBe(false)
	})
})

describe('createNotificationFeed', () => {
	let storage
	let activity
	let transport
	let serverItems
	let timers

	beforeEach(async () => {
		vi.resetModules()
		storage = fakeStorage()
		globalThis.localStorage = storage
		globalThis.window = { addEventListener: () => {} }
		storage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
		activity = (await import('./activity.js')).default
		serverItems = [backupItem(2, { message: 'Photos failed' }), backupItem(1, { read: true })]
		transport = {
			get: vi.fn(async () => ({ data: { data: serverItems, unread_count: 1, latest_id: 2, has_more: false } })),
			post: vi.fn(async () => ({ data: { unread_count: 0 } }))
		}
		timers = []
	})

	const makeFeed = () => createNotificationFeed({
		transport,
		activity,
		deps,
		currentUserId: () => 1,
		setTimer: (fn) => { timers.push(fn); return timers.length },
		clearTimer: () => {}
	})

	test('loads the feed next to local entries and syncs read state', async () => {
		const feed = makeFeed()
		activity.add({ title: 'Local only' })
		await feed.load()

		expect(transport.get).toHaveBeenCalledWith(FEED_BASE, { params: { limit: 100 } })
		expect(feed.available).toBe(true)
		const all = activity.getAll()
		expect(all.map(a => a.id)).toEqual(expect.arrayContaining(['feed-2', 'feed-1']))
		expect(all).toHaveLength(3)
		expect(activity.getUnreadCount()).toBe(2) // local + feed-2

		// Server entries are never written to localStorage.
		const key = Object.keys(storage.data).find(k => k.startsWith('nivaroos_activity_history:'))
		expect(JSON.parse(storage.getItem(key)).map(a => a.title)).toEqual(['Local only'])

		activity.markAsRead('feed-2')
		expect(transport.post).toHaveBeenLastCalledWith(FEED_BASE + '/read', { ids: [2] })
		expect(activity.getUnreadCount()).toBe(1)

		activity.markAllAsRead()
		expect(activity.getUnreadCount()).toBe(0)
		// Local-only unread count left nothing to send for the feed.
		expect(transport.post).toHaveBeenCalledTimes(1)
	})

	test('mark all read and clear all send a watermark', async () => {
		const feed = makeFeed()
		await feed.load()
		activity.markAllAsRead()
		expect(transport.post).toHaveBeenLastCalledWith(FEED_BASE + '/read', { all: true, up_to: 2 })

		activity.remove('feed-1')
		expect(transport.post).toHaveBeenLastCalledWith(FEED_BASE + '/dismiss', { ids: [1] })
		expect(activity.getAll().map(a => a.id)).toEqual(['feed-2'])

		activity.clear()
		expect(transport.post).toHaveBeenLastCalledWith(FEED_BASE + '/dismiss', { all: true, up_to: 2 })
		expect(activity.getAll()).toEqual([])
	})

	test('adds live entries and ignores duplicates', async () => {
		const feed = makeFeed()
		await feed.load()
		feed.onCreated({ id: '3', time: '2026-09-24T11:00:00Z', category: 'backup', level: 'warning', title: 'Backup & Sync', message: 'stale', key: '', args: '{}', action: '' })
		feed.onCreated({ id: '3', time: '2026-09-24T11:00:00Z', category: 'backup', level: 'warning', title: 'Backup & Sync', message: 'stale', key: '', args: '{}', action: '' })
		const all = activity.getAll()
		expect(all[0]).toMatchObject({ id: 'feed-3', message: 'stale', status: 'warning' })
		expect(all.filter(a => a.id === 'feed-3')).toHaveLength(1)
	})

	test('reloads on its own user state changes and on reconnect only', async () => {
		const feed = makeFeed()
		await feed.load()
		feed.onState({ user_id: '2', change: 'read' })
		expect(timers).toHaveLength(0)
		feed.onState({ user_id: '1', change: 'read_all' })
		expect(timers).toHaveLength(1)
		serverItems = [backupItem(2, { read: true }), backupItem(1, { read: true })]
		timers[0]()
		await vi.waitFor(() => expect(activity.getUnreadCount()).toBe(0))
		feed.onReconnect()
		expect(timers).toHaveLength(2)
	})

	test('without a feed the local history keeps working and app events stay local', async () => {
		transport.get = vi.fn(async () => { throw new Error('404') })
		const feed = makeFeed()
		await feed.load()
		expect(feed.available).toBe(false)
		expect(feed.persists({ Name: 'app:install-end' })).toBe(false)
		activity.add({ title: 'Local' })
		expect(activity.getAll()).toHaveLength(1)

		transport.get = vi.fn(async () => ({ data: { data: [] } }))
		await feed.load()
		expect(feed.persists({ Name: 'app:install-end' })).toBe(true)
		expect(feed.persists({ Name: 'app:start-end' })).toBe(false)
	})

	test('overlapping loads collapse into one more', async () => {
		let release
		transport.get = vi.fn(() => new Promise(resolve => { release = () => resolve({ data: { data: serverItems } }) }))
		const feed = makeFeed()
		const first = feed.load()
		feed.load()
		feed.load()
		await vi.waitFor(() => expect(transport.get).toHaveBeenCalledTimes(1))
		release()
		await first
		await vi.waitFor(() => expect(transport.get).toHaveBeenCalledTimes(2))
		release()
	})

	test('signing out or in as someone else drops the feed', async () => {
		const feed = makeFeed()
		await feed.load()
		expect(activity.getAll()).toHaveLength(2)
		storage.setItem('user', JSON.stringify({ id: 2, username: 'bob' }))
		expect(activity.getAll()).toHaveLength(0)
		storage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
		await feed.load()
		feed.reset()
		expect(activity.getAll()).toHaveLength(0)
		expect(feed.available).toBe(false)
	})
})
