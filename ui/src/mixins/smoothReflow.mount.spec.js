// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import smoothReflow from './smoothReflow'

const rows = ref(1)
function mount() {
	const host = document.createElement('div')
	document.body.appendChild(host)
	const app = createApp({
		mixins: [smoothReflow],
		mounted() { this.$smoothReflow({ el: '.box', property: ['height'] }) },
		render: () => h('div', { class: 'box' }, Array.from({ length: rows.value }, () => h('p'))),
	})
	app.mount(host)
	const box = host.querySelector('.box')
	// jsdom has no layout: height follows the number of rows
	box.getBoundingClientRect = () => ({ height: box.children.length * 10, width: 100 })
	return { app, box }
}

describe('smoothReflow mixin', () => {
	afterEach(() => {
		rows.value = 1
		vi.unstubAllGlobals()
	})

	test('a content change animates from the old height to the new one, then cleans up', async () => {
		const { app, box } = mount()
		rows.value = 3
		await nextTick()
		await nextTick()
		expect(box.style.height).toBe('30px')
		expect(box.style.transition).toContain('height .5s')
		expect(box.style.overflow).toBe('hidden')
		box.dispatchEvent(Object.assign(new Event('transitionend'), { propertyName: 'height' }))
		expect(box.style.height).toBe('')
		expect(box.style.transition).toBe('')
		app.unmount()
	})

	test('prefers-reduced-motion: no animation', async () => {
		vi.stubGlobal('matchMedia', (q) => ({ matches: q.includes('reduce') }))
		const { app, box } = mount()
		rows.value = 3
		await nextTick()
		await nextTick()
		expect(box.style.height).toBe('')
		expect(box.style.transition).toBe('')
		app.unmount()
	})
})
