// @vitest-environment jsdom
// Our b-* components render the DOM Buefy's did (same classes), so Bulma,
// Buefy's SCSS and our overrides style them the same.
import { expect, test } from 'vitest'
import { createApp } from 'vue'
import UI from './index'

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
