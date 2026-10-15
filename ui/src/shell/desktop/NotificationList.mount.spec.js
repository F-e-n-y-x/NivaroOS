// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import Buefy from '@ntohq/buefy-next'

const feed = vi.hoisted(() => {
	let list = []
	const subs = []
	return {
		set(l) { list = l; subs.forEach((f) => f(list)) },
		service: {
			getAll: () => list,
			subscribe: (f) => { subs.push(f); return () => {} },
			markAllAsRead() { feed.set(list.map((a) => ({ ...a, read: true }))) },
			markAsRead(id) { feed.set(list.map((a) => (a.id === id ? { ...a, read: true } : a))) }
		}
	}
})
vi.mock('@/service/activity', () => ({ activityService: feed.service }))
const { default: List } = await import('./NotificationList.vue')

describe('notification list', () => {
	test('what was new when the list opened stays marked new after it is marked read', async () => {
		vi.useFakeTimers()
		feed.set([
			{ id: 1, title: 'Backup failed', read: false, type: 'backup', timestamp: Date.now() },
			{ id: 2, title: 'Old one', read: true, type: 'app', timestamp: Date.now() - 1e6 }
		])
		const el = document.createElement('div')
		const app = createApp({ render: () => h(List) })
		app.use(Buefy)
		Object.assign(app.config.globalProperties, { $t: (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s), $store: { state: {}, getters: {}, commit() {} } })
		app.mount(el)
		await vi.advanceTimersByTimeAsync(2000)
		await nextTick()
		expect(feed.service.getAll().every((a) => a.read)).toBe(true)
		expect(el.querySelectorAll('.notif-item.is-unread').length).toBe(1)
		expect(el.querySelector('.unread-pill').textContent).toContain('1 new')

		el.querySelector('.notif-item.is-unread .notif-main').click()
		await nextTick()
		expect(el.querySelectorAll('.notif-item.is-unread').length).toBe(0)
		app.unmount()
		vi.useRealTimers()
	})
})
