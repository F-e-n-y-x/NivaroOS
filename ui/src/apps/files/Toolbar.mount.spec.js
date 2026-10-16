// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import UI from '@/ui'
import Toolbar from './Toolbar.vue'

globalThis.ResizeObserver ??= class {
	observe() {}
	disconnect() {}
}

describe('files toolbar', () => {
	test.each([
		['trash', 'Trash'],
		['shared', 'FilesShare'],
	])('%s shows no folder actions, even with a folder selection left over', async (section, title) => {
		const controller = reactive({ activeSection: 'browser', currentPath: '/DATA/Photos', breakpoints: { toolbarCollapsed: false }, navigate() {} })
		const el = document.createElement('div')
		document.body.appendChild(el)
		const props = reactive({ selectionSummary: null, selectedItems: [] })
		const app = createApp({ render: () => h(Toolbar, props) })
		app.use(UI)
		app.provide('filesController', controller)
		app.config.globalProperties.$t = (s) => s
		app.config.globalProperties.$store = { state: { operateObject: null, viewMode: 'grid', showHidden: false } }
		app.mount(el)
		expect(el.textContent).toContain('Upload')

		controller.activeSection = section
		// the folder view's selection is still there behind Trash
		props.selectionSummary = { count: 1 }
		props.selectedItems = [{ name: 'a.jpg' }]
		await nextTick()
		for (const word of ['New Folder', 'Upload', 'Paste', 'Copy', 'Cut', 'Rename', 'Delete']) expect(el.textContent).not.toContain(word)
		expect(el.querySelector('.selection-bar')).toBeNull()
		expect(el.querySelector('.actions')).toBeNull()
		expect(el.querySelector('.crumb.current').textContent).toBe(title)
		app.unmount()
	})
})
