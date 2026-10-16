// @vitest-environment jsdom
import { describe, expect, test, beforeEach } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import CasaWallpaper from './CasaWallpaper.vue'
import { setIssuer } from '@/service/mediaTicket'

const tick = async () => { for (let i = 0; i < 4; i++) await nextTick() }
const PIC = '/DATA/Gallery/Wallpaper/mine.png'

function mount(state) {
	const el = document.createElement('div')
	const app = createApp({ render: () => h(CasaWallpaper) })
	app.component('context-menu', { render: () => null })
	app.directive('animate-css', {})
	Object.assign(app.config.globalProperties, {
		$store: { state: reactive(state), commit() {} },
		$protocol: 'http:',
		$baseURL: 'box',
		$EventBus: { $on() {}, $off() {} },
	})
	app.mount(el)
	return { bg: () => el.querySelector('#background').style.backgroundImage, app }
}

describe('wallpaper', () => {
	beforeEach(() => localStorage.clear())

	test('signed out: the saved server picture comes from the public endpoint', async () => {
		setIssuer(() => Promise.reject(new Error('401'))) // no session on the login screen
		const saved = { path: PIC, dualMode: false }
		const { bg, app } = mount({ wallpaperObject: saved, publicWallpaper: saved })
		await tick()
		expect(bg()).toContain('http://box/v1/users/wallpaper?theme=')
		app.unmount()
	})

	test('signed in: same URL as the login screen (no reload, no flash)', async () => {
		setIssuer(async () => 'T1')
		const saved = { path: PIC, dualMode: true, light: { path: '/img/x.jpg' }, dark: { path: PIC } }
		localStorage.setItem('uiThemeMode', 'dark')
		const { bg, app } = mount({ wallpaperObject: saved, publicWallpaper: saved })
		await tick()
		expect(bg()).toBe('url("http://box/v1/users/wallpaper?theme=dark")')
		expect(bg()).not.toMatch(/token=|mt=/)
		app.unmount()
	})

	test("another user's own picture: the ticket link", async () => {
		setIssuer(async () => 'T1')
		const { bg, app } = mount({ wallpaperObject: { path: PIC, dualMode: false }, publicWallpaper: { path: '/DATA/other.png' } })
		await tick()
		expect(bg()).toBe('url("http://box/v1/image?path=%2FDATA%2FGallery%2FWallpaper%2Fmine.png&mt=T1")')
		app.unmount()
	})
})
