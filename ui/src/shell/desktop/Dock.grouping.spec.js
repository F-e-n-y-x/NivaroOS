// @vitest-environment jsdom
// One dock icon per app: an app's child windows (Add Download, the backup
// job wizard...) group under that app's icon instead of adding a second one.
import { describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { createStore } from 'vuex'
import UI from '@/ui'
import mutations from '@/store/mutations'
import { backupWindow } from '@/apps/backup/windows'
import { appIdOf, dockClickTarget } from '@/utils/desktop/windowRegistry'

const pins = vi.hoisted(() => ({ list: [] }))
vi.mock('@/utils/downloadStationInstalled', () => ({ checkDownloadStationInstalled: async () => true }))
vi.mock('@/utils/backupInstalled', () => ({ checkBackupInstalled: async () => true }))
vi.mock('@/mixins/app/Business_ShowNewAppTag', () => ({ default: {} }))
vi.mock('@/mixins/app/Business_OpenThirdApp', () => ({ default: {} }))
vi.mock('@/mixins/app/Business_LinkApp', () => ({ default: { methods: { getLinkAppList: async () => [] } } }))
vi.mock('@/mixins/app/Business_LegacyAppOverrides', () => ({ default: { methods: { getLegacyAppOverrides: async () => ({}) } } }))
vi.mock('@/mixins/app/Business_DockPins', async (orig) => ({
	...(await orig()),
	default: { methods: { getDockPins: async () => pins.list, setDockPinned: async () => {} } }
}))
const { default: Dock } = await import('./Dock.vue')

const t = (s) => s
// The exact payload DownloadStationApp.openAddDownload commits.
const addDownload = () => ({ id: 'ds-add-' + Math.random(), title: 'Add Download', component: 'DsAddDownloadWindow', props: {}, width: 560, height: 520 })

async function mountDock(pinned) {
	pins.list = pinned
	localStorage.clear()
	const store = createStore({ state: { windows: [], nextWindowZIndex: 1, isMobile: false, isTablet: false }, mutations })
	const el = document.createElement('div')
	const app = createApp({ render: () => h(Dock) })
	app.use(UI)
	app.use(store)
	Object.assign(app.config.globalProperties, {
		$t: t,
		$assetUrl: (p) => p,
		$EventBus: { $on() {}, $off() {}, $emit() {} },
		$openAPI: { appGrid: { getAppGrid: async () => ({ data: { data: [] } }) } }
	})
	app.mount(el)
	await new Promise((r) => setTimeout(r, 0))
	await nextTick()
	const extras = () => [...el.querySelectorAll('.dock > .dock-item')]
	const pinnedItems = () => [...el.querySelectorAll('.dock-pinned-list .dock-item')]
	return { store, el, extras, pinnedItems, app }
}

describe('dock groups windows by app', () => {
	test('Add Download and New backup job add no second icon when the apps are open (unpinned)', async () => {
		const { store, extras, app } = await mountDock([])
		store.commit('OPEN_WINDOW', { id: 'download-station', title: 'Download Station', component: 'DownloadStationApp' })
		store.commit('OPEN_WINDOW', addDownload())
		store.commit('OPEN_WINDOW', backupWindow(t, 'app', {}))
		store.commit('OPEN_WINDOW', backupWindow(t, 'wizard', {}))
		store.commit('OPEN_WINDOW', backupWindow(t, 'storagePicker', {}))
		await nextTick()
		expect(store.state.windows).toHaveLength(5)
		expect(extras()).toHaveLength(2) // one Download Station, one Backup & Sync
		app.unmount()
	})

	test('a pinned app shows no extra icon for its child windows; its pin is lit', async () => {
		const { store, extras, pinnedItems, app } = await mountDock(['Download Station'])
		store.commit('OPEN_WINDOW', addDownload()) // deep link, app window not open
		await nextTick()
		expect(extras()).toHaveLength(0)
		expect(pinnedItems()[0].querySelector('.dock-dot')).not.toBeNull()
		app.unmount()
	})

	test('pin click opens the app window if only a child is open, then raises/cycles the app windows', async () => {
		const { store, pinnedItems, app } = await mountDock(['Download Station'])
		store.commit('OPEN_WINDOW', addDownload())
		await nextTick()
		pinnedItems()[0].click()
		const ids = () => store.state.windows.map((w) => w.id)
		expect(ids()).toContain('download-station')
		const top = () => store.state.windows.reduce((a, b) => (b.zIndex > a.zIndex ? b : a))
		expect(top().id).toBe('download-station')
		// Another app comes to the front; clicking the pin raises Download Station again.
		store.commit('OPEN_WINDOW', { id: 'files', title: 'Files', component: 'FilesApp' })
		pinnedItems()[0].click()
		expect(top().id).toBe('download-station')
		// Already in front: the next click cycles to the Add Download window.
		pinnedItems()[0].click()
		expect(top().component).toBe('DsAddDownloadWindow')
		expect(store.state.windows).toHaveLength(3)
		app.unmount()
	})
})

describe('window app identity', () => {
	test('child windows inherit their app; unknown windows have none', () => {
		expect(appIdOf({ component: 'DsAddDownloadWindow' })).toBe('download-station')
		expect(appIdOf({ component: 'BackupJobWizardWindow' })).toBe('backup')
		expect(appIdOf({ component: 'StoragePickerWindow', appId: 'backup' })).toBe('backup')
		expect(appIdOf({ component: 'ScheduledTaskWindow' })).toBe('settings')
		expect(appIdOf({ component: 'ImageViewer' })).toBe(null)
	})
	test('dockClickTarget raises, cycles, then minimizes a lone front window', () => {
		const a = { id: 'a', zIndex: 1 }
		const b = { id: 'b', zIndex: 2 }
		expect(dockClickTarget([a, b], 5)).toEqual({ id: 'b', minimize: false })
		expect(dockClickTarget([a, b], 2)).toEqual({ id: 'a', minimize: false })
		expect(dockClickTarget([b], 2)).toEqual({ id: 'b', minimize: true })
		expect(dockClickTarget([{ ...b, minimized: true }], 2)).toEqual({ id: 'b', minimize: false })
		expect(dockClickTarget([], 2)).toBe(null)
	})
})
