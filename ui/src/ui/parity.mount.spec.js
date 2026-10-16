// @vitest-environment jsdom
// Each of our b-* components renders the DOM Buefy's did, for the ways this
// app uses them. (Goes when Buefy does; the DOM is then pinned in
// components.mount.spec.js.)
import { describe, expect, test } from 'vitest'
import { createApp, nextTick } from 'vue'
import * as Buefy from '@ntohq/buefy-next'
import UI from './index'
import { iconPacks } from './icons'

// Attribute and class order don't matter; Vue's comment placeholders don't.
function normalize(root) {
	for (const n of [...root.querySelectorAll('*')]) {
		const attrs = [...n.attributes].map((a) => [a.name, a.name === 'class' ? a.value.split(/\s+/).filter(Boolean).sort().join(' ') : a.value])
		for (const [k] of attrs) n.removeAttribute(k)
		for (const [k, v] of attrs.sort()) if (!(k === 'class' && !v)) n.setAttribute(k, v)
	}
	return root.innerHTML.replace(/<!--[^]*?-->/g, '').replace(/\s+/g, ' ').replace(/> </g, '><').trim()
}

async function mount(tpl, buefyNames, act) {
	const el = document.createElement('div')
	document.body.appendChild(el)
	const app = createApp({ template: tpl, data: () => ({ v: 'abc', n: 3, t: 'is-danger', m: 'Bad name' }) })
	app.use(UI)
	if (buefyNames) {
		app.config.globalProperties.$buefy.config.setOptions({ customIconPacks: iconPacks })
		for (const n of buefyNames) {
			delete app._context.components[n]
			app.component(n, Buefy[n])
		}
	}
	app.mount(el)
	await nextTick()
	if (act) await act(el)
	await nextTick()
	await nextTick()
	const html = normalize(el)
	app.unmount()
	el.remove()
	return html
}

// A case is a template, or [template, act(el)] to compare after an interaction.
const same = (names, cases) =>
	describe(names.join('+'), () => {
		for (const c of cases) {
			const [tpl, act] = Array.isArray(c) ? c : [c]
			test(tpl + (act ? ` (${act.name})` : ''), async () => {
				expect(await mount(tpl, null, act)).toBe(await mount(tpl, names, act))
			})
		}
	})
const blur = async function blur(el) { el.querySelector('input,textarea').dispatchEvent(new Event('blur')) }
const typeIn = async function typeIn(el) { const i = el.querySelector('input,textarea'); i.focus(); i.dispatchEvent(new Event('focus')); i.value = 'hello'; i.dispatchEvent(new Event('input')) }
const reveal = async function reveal(el) { el.querySelector('.icon.is-right').click() }

same(['BIcon'], [
	'<b-icon icon="home" />',
	'<b-icon icon="close" pack="casa" size="is-small" class="x" aria-hidden="true" />',
	'<b-icon icon="a" pack="casa" size="is-20" custom-class="c" />',
	'<b-icon icon="a" pack="mdi" size="is-18" />',
	'<b-icon icon="a" pack="mdi" custom-size="mdi-18px" />',
	'<b-icon icon="a" type="is-primary" />',
])

same(['BButton'], [
	'<b-button type="is-primary" size="is-small" rounded :loading="true" class="x">Go</b-button>',
	'<b-button label="L" icon-left="check" native-type="submit" outlined expanded />',
	'<b-button icon-left="check" size="is-large" />',
	'<b-button tag="a" href="/x" target="_blank">A</b-button>',
	'<b-button tag="a" href="/x" disabled>A</b-button>',
	'<b-button :disabled="false" title="t" aria-label="a">A</b-button>',
])

same(['BField', 'BInput', 'BIcon', 'BButton'], [
	'<b-input v-model="v" size="is-small" placeholder="p" class="c" id="i" />',
	'<b-input v-model="v" type="password" password-reveal />',
	'<b-input v-model="v" type="textarea" rows="3" maxlength="20" />',
	'<b-input v-model="n" type="number" maxlength="5" />',
	'<b-input v-model="v" icon="magnify" icon-pack="mdi" size="is-small" expanded />',
	'<b-input v-model="v" icon="magnify" size="is-large" rounded custom-class="cc" />',
	'<b-field label="Name" label-for="x"><b-input v-model="v" id="x" /></b-field>',
	'<b-field :label="m" :type="t" :message="m" expanded><b-input v-model="v" /></b-field>',
	'<b-field :type="{ \'is-danger\': true }" :message="[m, { other: true, no: false }]"><b-input v-model="v" type="password" password-reveal /></b-field>',
	'<b-field label="Share"><b-input v-model="v" readonly expanded /><p class="control"><b-button icon-left="check" /></p></b-field>',
	'<b-field :type="\'\'" message=""><b-input v-model="v" /></b-field>',
	['<b-field label="x"><b-input :model-value="\'\'" required /></b-field>', blur],
	['<b-field label="x" type="is-success" message="ok"><b-input :model-value="\'\'" required /></b-field>', blur],
	['<b-field label="x"><b-input v-model="v" type="email" /></b-field>', async function badEmail(el) { await typeIn(el); await blur(el) }],
	['<b-input v-model="v" maxlength="8" />', typeIn],
	['<b-input v-model="v" type="password" password-reveal />', reveal],
])
