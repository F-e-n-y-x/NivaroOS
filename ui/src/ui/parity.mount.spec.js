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
// Empty class/id/style attributes count as absent (Buefy's checkbox put
// id="" on its input).
function normalize(root) {
	// Live state isn't in the markup: record it.
	for (const n of root.querySelectorAll('input, select, textarea')) n.setAttribute('data-live', `${n.checked}|${n.value}|${n.type}`)
	for (const n of [...root.querySelectorAll('*')]) {
		const attrs = [...n.attributes].map((a) => [a.name, a.name === 'class' ? a.value.split(/\s+/).filter(Boolean).sort().join(' ') : a.value])
		for (const [k] of attrs) n.removeAttribute(k)
		for (const [k, v] of attrs.sort()) if (!(['class', 'id', 'style'].includes(k) && !v)) n.setAttribute(k, v)
	}
	// Tab ids hang off component uids, which differ between the two runs.
	return root.innerHTML.replace(/"\d+-(content|label)"/g, '"N-$1"').replace(/<!--[^]*?-->/g, '').replace(/\s+/g, ' ').replace(/> </g, '><').trim()
}

async function mount(tpl, buefyNames, act) {
	const el = document.createElement('div')
	document.body.appendChild(el)
	const app = createApp({ template: tpl, data: () => ({ v: 'abc', n: 3, t: 'is-danger', m: 'Bad name', on: true, off: false, list: ['a'], sel: null, s2: 'b', busy: true, shown: true, pct: 73.456, pickd: null, items: [{ n: 'A' }, { n: 'B' }], tab: 0, got: '', file: null, gib: 20, q: '', folders: [{ name: 'Media' }, { name: 'Tools' }], picked: '' }) })
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
	// The whole body (append-to-body menus live outside el), once it stops
	// changing: transitions end on timers, slower on a loaded machine.
	let html = normalize(document.body)
	for (let i = 0; i < 50; i++) {
		await new Promise((r) => setTimeout(r, 30))
		const next = normalize(document.body)
		if (next === html && !/-(enter|leave)-(from|active|to)/.test(next)) break
		html = next
	}
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

const click = async function click(el) { el.querySelector('input').click() }
const clickLabel = async function clickLabel(el) { el.querySelector('label').click() }
const pick = async function pick(el) { const s = el.querySelector('select'); s.value = 'b'; s.dispatchEvent(new Event('change')) }

same(['BSwitch'], [
	'<b-switch v-model="on" type="is-primary" size="is-small" class="c" aria-label="A" />',
	'<b-switch :model-value="off" disabled title="t" />',
	'<b-switch v-model="on">Label</b-switch>',
	['<b-switch :model-value="off" />', click],
	['<b-switch v-model="off" />', clickLabel],
	['<b-switch v-model="list" native-value="b" />', click],
])

same(['BCheckbox'], [
	'<b-checkbox v-model="on" size="is-small">Keep</b-checkbox>',
	'<b-checkbox :model-value="off" disabled :indeterminate="true" class="c" />',
	'<b-checkbox v-model="list" native-value="a" type="is-danger">A</b-checkbox>',
	['<b-checkbox v-model="off">x</b-checkbox>', click],
])

const opts = '<option value="a">A</option><option value="b">B</option>'
same(['BSelect', 'BField', 'BIcon'], [
	`<b-select v-model="s2" size="is-small" expanded class="c" aria-label="L" data-test="t">${opts}</b-select>`,
	`<b-select v-model="sel" placeholder="Pick">${opts}</b-select>`,
	`<b-select v-model="sel" icon="magnify">${opts}</b-select>`,
	`<b-field label="Exp"><b-select v-model="s2" expanded>${opts}</b-select></b-field>`,
	[`<b-select v-model="sel">${opts}</b-select>`, pick],
])

const hover = async function hover(el) { el.querySelector('.tooltip-trigger').dispatchEvent(new Event('mouseenter')) }
const leave = async function leave(el) { await hover(el); el.querySelector('.tooltip-trigger').dispatchEvent(new Event('mouseleave')) }
same(['BTooltip'], [
	'<b-tooltip label="Create" position="is-right" type="is-dark"><a class="add">x</a></b-tooltip>',
	['<b-tooltip label="Create" position="is-left" type="is-dark"><button>x</button></b-tooltip>', hover],
	['<b-tooltip label="Create"><button>x</button></b-tooltip>', leave],
	'<b-tooltip :always="true" :animated="true" label="Running" :triggers="[\'hover\']" animation="fade1" class="in-card" type="is-white"><div>x</div></b-tooltip>',
	['<b-tooltip :always="false" label="L" :triggers="[]" class="in-card" type="is-white"><div>x</div></b-tooltip>', hover],
])

same(['BImage'], [
	'<b-image src="/a.svg" alt="pending" class="is-160x160 mb-2" />',
	'<b-image src="/a.png" src-fallback="/d.svg" alt="" webp-fallback=".jpg" ratio="1by1" class="hero-img" />',
	'<b-image :lazy="false" src="/a.png" style="width: 19px; height: 19px;" />',
	'<b-image src="/a.svg" class="is-128x128" rounded />',
	['<b-image src="/broken.png" src-fallback="/d.svg" />', async function fail(el) { el.querySelector('img').dispatchEvent(new Event('error')) }],
])

same(['BLoading'], [
	'<b-loading v-model="busy" :is-full-page="false" />',
	'<b-loading v-model="busy" />',
	'<b-loading :model-value="busy" :can-cancel="false" :is-full-page="false" class="op80" style="top: auto"><img src="/w.svg" alt="Loading" /></b-loading>',
	'<b-loading :model-value="false" />',
])

same(['BProgress'], [
	'<b-progress :value="pct" size="is-small" class="mb-2" />',
	'<b-progress size="is-small" class="mb-2" />',
	'<b-progress :value="20" />',
])

same(['BNotification'], [
	'<b-notification v-model="shown" aria-close-label="Close notification" role="alert" type="is-danger">Wrong password</b-notification>',
	['<b-notification v-model="shown" aria-close-label="Close" type="is-danger">x</b-notification>', async function closeIt(el) { el.querySelector('.delete').click() }],
])

const tick = () => new Promise((r) => setTimeout(r, 40))
const openDd = async function openDd(el) { el.querySelector('.dropdown-trigger').click(); await tick() }
const pickB = async function pickB(el) { await openDd(el); document.querySelectorAll('.dropdown-item')[1].click(); await tick() }
const outside = async function outside(el) { await openDd(el); document.body.click(); await tick() }
const esc = async function esc(el) { await openDd(el); document.dispatchEvent(new KeyboardEvent('keyup', { key: 'Escape' })); await tick() }
const ddItems = '<b-dropdown-item aria-role="listitem" @click="x = 1">One</b-dropdown-item><b-dropdown-item aria-role="menuitem" :disabled="true">Two</b-dropdown-item>'
const ddPick = '<template #trigger="{ active }"><button>{{ active ? "up" : "down" }}</button></template><b-dropdown-item v-for="i in items" :key="i.n" :value="i" :class="{ \'is-active\': pickd === i }">{{ i.n }}</b-dropdown-item>'
same(['BDropdown', 'BDropdownItem', 'BIcon'], [
	`<b-dropdown aria-role="list"><template #trigger><b-icon icon="dots-horizontal" /></template>${ddItems}</b-dropdown>`,
	[`<b-dropdown aria-role="list" position="is-bottom-left"><template #trigger><span>t</span></template>${ddItems}</b-dropdown>`, openDd],
	[`<b-dropdown v-model="pickd" aria-role="list" class="source-dropdown">${ddPick}</b-dropdown>`, openDd],
	[`<b-dropdown v-model="pickd" aria-role="list">${ddPick}</b-dropdown>`, pickB],
	[`<b-dropdown aria-role="list"><template #trigger><span>t</span></template>${ddItems}</b-dropdown>`, outside],
	[`<b-dropdown aria-role="list"><template #trigger><span>t</span></template>${ddItems}</b-dropdown>`, esc],
	`<b-dropdown position="is-bottom-left" append-to-body aria-role="menu" class="browser-menu"><template #trigger><button>m</button></template>${ddItems}</b-dropdown>`,
	[`<b-dropdown position="is-bottom-left" append-to-body :close-on-click="false" aria-role="menu" class="w"><template #trigger><button>m</button></template><div class="t">Show</div><b-dropdown-item aria-role="menuitemcheckbox" aria-checked="true" class="wi">A</b-dropdown-item></b-dropdown>`, async function pickStay(el) { await openDd(el); document.querySelector('.wi').click(); await tick() }],
])

const tab2 = async function tab2(el) { el.querySelectorAll('.tabs a')[1].click() }
const keyTab = async function keyTab(el) { const a = el.querySelectorAll('.tabs a'); a[0].dispatchEvent(new Event('focus')); a[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })); document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })) }
const tabsTpl = '<b-tabs :animated="false" @update:modelValue="got = $event"><b-tab-item label="Terminal" value="terminal">T</b-tab-item><b-tab-item label="Logs" value="logs">L</b-tab-item></b-tabs><i>{{ got }}</i>'
const tabsNum = '<b-tabs v-model="tab" :animated="false"><b-tab-item label="Docker Compose">C</b-tab-item><b-tab-item label="Docker CLI">D</b-tab-item></b-tabs><i>{{ tab }}</i>'
same(['BTabs', 'BTabItem'], [
	tabsTpl,
	[tabsTpl, tab2],
	tabsNum,
	[tabsNum, tab2],
	[tabsNum, keyTab],
])

const drop = (name) => async function dropFile(el) {
	const e = new Event('drop', { bubbles: true, cancelable: true })
	e.dataTransfer = { files: [new File(['x'], name, { type: '' })] }
	el.querySelector('.upload-draggable').dispatchEvent(e)
}
const upTpl = '<b-upload v-model="file" accept=".yaml,.yml" drag-drop expanded @invalid="got = \'bad\'"><section class="section">Drop</section></b-upload><i>{{ file && file.name }}{{ got }}</i>'
same(['BUpload', 'BIcon'], [
	'<b-upload v-model="file" accept=".iso,application/x-iso9660-image" :disabled="false"><a class="create-btn"><b-icon icon="upload-outline" /><span>Upload ISO</span></a></b-upload>',
	'<b-upload v-model="file" :disabled="true"><a>x</a></b-upload>',
	upTpl,
	[upTpl, drop('compose.yml')],
	[upTpl, drop('evil.exe')],
	[upTpl, async function hoverZone(el) { el.querySelector('.upload-draggable').dispatchEvent(new Event('dragenter')) }],
])

const press = (sel) => async function pressBtn(el) { const b = el.querySelector(sel + ' button'); b.dispatchEvent(new MouseEvent('mousedown', { button: 0 })); b.parentNode.dispatchEvent(new MouseEvent('mouseup')) }
const numTpl = '<b-numberinput aria-minus-label="Decrease" aria-plus-label="Increase" class="vm-disk-size" :model-value="gib" @update:modelValue="gib = $event" :min="20" :max="22" size="is-small" type="is-light" controls-position="compact" /><i>{{ gib }}</i>'
same(['BNumberinput', 'BInput', 'BIcon'], [
	numTpl,
	[numTpl, press('.plus')],
	[numTpl, async function twice(el) { await press('.plus')(el); await press('.plus')(el); await press('.plus')(el) }],
	[numTpl, press('.minus')],
	[numTpl, async function keyPlus(el) { el.querySelector('.plus button').dispatchEvent(new MouseEvent('click', { detail: 0 })) }],
	[numTpl, async function typeNum(el) { const i = el.querySelector('input'); i.value = '21'; i.dispatchEvent(new Event('input')) }],
	'<b-numberinput :model-value="null" :disabled="true" />',
])

const focusIn = async function focusIn(el) { const i = el.querySelector('input'); i.focus(); i.dispatchEvent(new Event('focus')) }
const typeQ = async function typeQ(el) { await focusIn(el); const i = el.querySelector('input'); i.value = 'Me'; i.dispatchEvent(new Event('input')) }
const downEnter = async function downEnter(el) { await focusIn(el); const i = el.querySelector('input'); i.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' })); await nextTick(); i.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' })); await nextTick(); i.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' })) }
const clickOpt = async function clickOpt(el) { await focusIn(el); await nextTick(); document.querySelectorAll('.autocomplete .dropdown-item')[1].click() }
const acFolder = '<b-field label="Folder"><b-autocomplete ref="input" v-model="q" :data="folders" :open-on-focus="true" placeholder="Existing or new folder name" append-to-body expanded field="name" maxlength="30" @select="o => (picked = o ? o.name : picked)"><template #empty>No matching folders</template></b-autocomplete></b-field><i>{{ q }}|{{ picked }}</i>'
const acLink = '<b-autocomplete id="weblink-address" v-model="q" :data="folders" :open-on-focus="true" placeholder="Local or public URL" field="name" max-height="120px" size="is-small" icon="link-variant" expanded required aria-describedby="hint" />'
same(['BAutocomplete', 'BField', 'BInput', 'BIcon'], [
	acFolder,
	[acFolder, focusIn],
	[acFolder, typeQ],
	[acFolder, downEnter],
	[acFolder, clickOpt],
	acLink,
	[acLink, focusIn],
	[acLink, downEnter],
	['<b-autocomplete v-model="q" :data="[]" :open-on-focus="true"><template #empty>None</template></b-autocomplete>', focusIn],
])
