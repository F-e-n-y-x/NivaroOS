// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import Buefy from '@ntohq/buefy-next'
import Toolbar from './Toolbar.vue'

globalThis.ResizeObserver ??= class {
	observe() {}
	disconnect() {}
}

describe('files toolbar', () => {
	test('Trash shows no folder actions, even with a folder selection left over', async () => {
		const controller = reactive({ activeSection: 'browser', currentPath: '/DATA/Photos', breakpoints: { toolbarCollapsed: false }, navigate() {} })
		const el = document.createElement('div')
		document.body.appendChild(el)
		const props = reactive({ selectionSummary: null, selectedItems: [] })
		const app = createApp({ render: () => h(Toolbar, props) })
		app.use(Buefy)
		app.provide('filesController', controller)
		app.config.globalProperties.$t = (s) => s
		app.config.globalProperties.$store = { state: { operateObject: null, viewMode: 'grid', showHidden: false } }
		app.mount(el)
		expect(el.textContent).toContain('Upload')

		controller.activeSection = 'trash'
		// the folder view's selection is still there behind Trash
		props.selectionSummary = { count: 1 }
		props.selectedItems = [{ name: 'a.jpg' }]
		await nextTick()
		for (const word of ['New Folder', 'Upload', 'Paste', 'Copy', 'Cut', 'Rename', 'Delete']) expect(el.textContent).not.toContain(word)
		expect(el.querySelector('.selection-bar')).toBeNull()
		expect(el.querySelector('.actions')).toBeNull()
		expect(el.textContent).toContain('Trash')
		app.unmount()
	})
})
