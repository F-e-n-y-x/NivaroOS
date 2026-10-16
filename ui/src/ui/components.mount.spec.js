// @vitest-environment jsdom
// Our b-* components render the DOM Buefy's did (same classes), so Bulma,
// Buefy's SCSS and our overrides style them the same.
import { expect, test } from 'vitest'
import { createApp, nextTick } from 'vue'
import UI from './index'
import A11yLabels from '@/plugins/a11yLabels'

const render = (tpl) => {
	const el = document.createElement('div')
	const app = createApp({ template: tpl })
	app.use(UI)
	app.mount(el)
	return el.innerHTML.replace(/<!--.*?-->/g, '')
}

test('b-icon: pack prefix and size classes', () => {
	expect(render('<b-icon icon="home"></b-icon>')).toBe('<span class="icon"><i class="mdi mdi-home mdi-24px"></i></span>')
	expect(render('<b-icon icon="close" pack="casa" size="is-small" class="x" aria-hidden="true"></b-icon>'))
		.toBe('<span class="icon is-small x" aria-hidden="true"><i class="casa casa-close"></i></span>')
	expect(render('<b-icon icon="a" pack="mdi" size="is-18"></b-icon>')).toBe('<span class="icon is-18"><i class="mdi mdi-a mdi-24px"></i></span>')
	expect(render('<b-icon icon="a" :pack="null" custom-size="mdi-18px" custom-class="c"></b-icon>')).toBe('<span class="icon"><i class="mdi mdi-a mdi-18px c"></i></span>')
})

test('b-button: Bulma classes, native type, icon, disabled link', () => {
	expect(render('<b-button type="is-primary" size="is-small" rounded :loading="true" class="x">Go</b-button>'))
		.toBe('<button class="button is-small is-primary is-rounded is-loading x" type="button"><span>Go</span></button>')
	expect(render('<b-button label="L" icon-left="check" native-type="submit" outlined expanded></b-button>'))
		.toBe('<button class="button is-outlined is-fullwidth" type="submit"><span class="icon is-small"><i class="mdi mdi-check mdi-16px"></i></span><span>L</span></button>')
	expect(render('<b-button tag="a" href="/x">A</b-button>')).toBe('<a class="button" href="/x"><span>A</span></a>')
	expect(render('<b-button tag="a" href="/x" disabled>A</b-button>')).toBe('<button class="button" type="button" href="/x" disabled=""><span>A</span></button>')
})

test('b-input in a b-field: HTML5 validity shows on blur, unless the field has its own type', async () => {
	const el = document.createElement('div')
	const app = createApp({ template: '<b-field label="a"><b-input model-value="" required /></b-field><b-field type="is-success"><b-input model-value="" required /></b-field>' })
	app.use(UI)
	app.mount(el)
	for (const i of el.querySelectorAll('input')) i.dispatchEvent(new Event('blur'))
	await nextTick()
	await nextTick()
	const [a, b] = el.querySelectorAll('.field')
	expect(a.querySelector('.help.is-danger').textContent).toBeTruthy()
	expect(a.querySelector('input').className).toBe('input is-danger')
	expect(b.querySelector('input').className).toBe('input is-success')
})

test('a11yLabels still names our switch, checkbox and select', async () => {
	// jsdom has no innerText.
	Object.defineProperty(HTMLElement.prototype, 'innerText', { configurable: true, get() { return this.textContent } })
	const el = document.createElement('div')
	const app = createApp({
		template: '<div class="setting-row"><span class="setting-title">Auto update</span><b-switch :model-value="true" /></div>'
			+ '<b-checkbox aria-label="Pick" /><div class="setting-row"><span class="setting-title">Mode</span><b-select :model-value="null"><option>a</option></b-select></div>',
	})
	app.use(UI)
	app.use(A11yLabels)
	app.mount(el)
	await nextTick()
	await nextTick()
	expect([...el.querySelectorAll('input, select')].map((i) => i.getAttribute('aria-label'))).toEqual(['Auto update', 'Pick', 'Mode'])
})

test('b-dropdown: opens on click, picks into v-model and closes; append-to-body sits on body', async () => {
	const tick = () => new Promise((r) => setTimeout(r, 40))
	// The leave transition ends on a timer: wait for it, however loaded the machine is.
	const until = async (f) => {
		for (let i = 0; i < 100 && !f(); i++) await tick()
	}
	const el = document.body.appendChild(document.createElement('div'))
	const app = createApp({
		data: () => ({ pick: null }),
		template: '<b-dropdown v-model="pick" append-to-body><template #trigger><button>t</button></template><b-dropdown-item value="a">A</b-dropdown-item><b-dropdown-item value="b">B</b-dropdown-item></b-dropdown>',
	})
	app.use(UI)
	const vm = app.mount(el)
	const menu = () => document.querySelector('body > div > .dropdown > .dropdown-menu')
	expect(menu().style.display).toBe('none')
	el.querySelector('.dropdown-trigger').click()
	await until(() => menu().style.display === '')
	expect(menu().style.display).toBe('')
	expect(el.querySelector('.dropdown').classList.contains('is-active')).toBe(true)
	menu().querySelectorAll('.dropdown-item')[1].click()
	await tick()
	expect(vm.pick).toBe('b')
	await until(() => menu().style.display === 'none')
	expect(menu().style.display).toBe('none')
	expect(menu().querySelectorAll('.dropdown-item.is-active').length).toBe(1)
	app.unmount()
	expect(menu()).toBeNull()
	el.remove()
})

test('b-numberinput: buttons step within min/max', async () => {
	const el = document.createElement('div')
	const app = createApp({ data: () => ({ n: 21 }), template: '<b-numberinput v-model="n" :min="20" :max="22" />' })
	app.use(UI)
	const vm = app.mount(el)
	const press = (c) => {
		const b = el.querySelector(`.${c} button`)
		b.dispatchEvent(new MouseEvent('mousedown', { button: 0 }))
		b.parentNode.dispatchEvent(new MouseEvent('mouseup'))
	}
	press('plus')
	await nextTick()
	expect(vm.n).toBe(22)
	expect(el.querySelector('.plus button').disabled).toBe(true)
	press('minus')
	press('minus')
	await nextTick()
	expect(vm.n).toBe(20)
	expect(el.querySelector('.minus button').disabled).toBe(true)
})
