// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import UI from '@/ui'
import Menu from './WidgetFieldsMenu.vue'

describe('widget fields menu', () => {
	test('closes when the widget column scrolls, not when the menu itself scrolls', async () => {
		const el = document.createElement('div')
		const column = document.createElement('div')
		column.appendChild(el)
		document.body.appendChild(column)
		const app = createApp({ render: () => h(Menu, { fields: [{ key: 'a', label: 'A', on: true }] }) })
		app.use(UI)
		app.config.globalProperties.$t = (s) => s
		app.mount(el)
		const open = () => document.querySelector('.dropdown-menu[aria-hidden="false"]')

		el.querySelector('button.widget-icon-btn').click()
		await new Promise((r) => setTimeout(r, 10))
		await nextTick()
		expect(open()).not.toBeNull()

		open().dispatchEvent(new Event('scroll'))
		await nextTick()
		expect(open()).not.toBeNull()

		column.dispatchEvent(new Event('scroll'))
		await nextTick()
		expect(open()).toBeNull()
		app.unmount()
	})
})
