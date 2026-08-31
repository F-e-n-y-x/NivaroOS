import { describe, test, expect, beforeEach, vi } from 'vitest'
import mutations from './mutations'

function makeState(win) {
	return { windows: [win], nextWindowZIndex: 1 }
}

describe('window mutations persistence', () => {
	beforeEach(() => {
		global.localStorage = { setItem: vi.fn(), getItem: vi.fn() }
	})

	test('UPDATE_WINDOW_RECT updates the rect without touching localStorage', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 0, y: 0, width: 900, height: 600, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.UPDATE_WINDOW_RECT(state, { id: 'settings', x: 10, y: 20, width: 950, height: 650 })
		expect(state.windows[0]).toMatchObject({ x: 10, y: 20, width: 950, height: 650 })
		expect(global.localStorage.setItem).not.toHaveBeenCalled()
	})

	test('PERSIST_WINDOWS writes the persistable window fields to localStorage', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 10, y: 20, width: 950, height: 650, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.PERSIST_WINDOWS(state)
		expect(global.localStorage.setItem).toHaveBeenCalledTimes(1)
		const [key, json] = global.localStorage.setItem.mock.calls[0]
		expect(key).toBe('nivaroos_open_windows')
		expect(JSON.parse(json)).toEqual([{ id: 'settings', title: 'Settings', component: 'SettingsApp', x: 10, y: 20, width: 950, height: 650, minimized: false }])
	})

	test('CLOSE_WINDOW still persists immediately (unaffected by the drag/resize throttle fix)', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 0, y: 0, width: 900, height: 600, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.CLOSE_WINDOW(state, 'settings')
		expect(global.localStorage.setItem).toHaveBeenCalledTimes(1)
		expect(state.windows).toHaveLength(0)
	})
})
