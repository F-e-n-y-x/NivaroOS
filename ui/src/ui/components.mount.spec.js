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

test('b-button: Bulma classes, native type, icon, disabled link', () => {
	expect(render('<b-button type="is-primary" size="is-small" rounded :loading="true" class="x">Go</b-button>'))
		.toBe('<button class="button is-small is-primary is-rounded is-loading x" type="button"><span>Go</span></button>')
	expect(render('<b-button label="L" icon-left="check" native-type="submit" outlined expanded></b-button>'))
		.toBe('<button class="button is-outlined is-fullwidth" type="submit"><span class="icon is-small"><i class="mdi mdi-check mdi-16px"></i></span><span>L</span></button>')
	expect(render('<b-button tag="a" href="/x">A</b-button>')).toBe('<a class="button" href="/x"><span>A</span></a>')
	expect(render('<b-button tag="a" href="/x" disabled>A</b-button>')).toBe('<button class="button" type="button" href="/x" disabled=""><span>A</span></button>')
})
