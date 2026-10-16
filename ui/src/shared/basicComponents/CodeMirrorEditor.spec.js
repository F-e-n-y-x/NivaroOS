// @vitest-environment jsdom
import { afterEach, expect, test } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import { EditorView } from '@codemirror/view'
import CodeMirrorEditor from './CodeMirrorEditor.vue'
import { findLanguage } from './findLanguage'

let app
afterEach(() => {
	app?.unmount()
	document.body.innerHTML = ''
	document.documentElement.removeAttribute('data-theme')
})

function mount(props) {
	const editor = ref(null)
	app = createApp({ render: () => h(CodeMirrorEditor, { ref: editor, ...props.value }) })
	app.mount(document.body.appendChild(document.createElement('div')))
	return () => editor.value.getView()
}

test('v-model: prop updates replace the text, user edits are emitted (and nothing else)', async () => {
	const emitted = []
	const props = ref({ "modelValue": 'a: 1', "lang": 'x.yml', 'onUpdate:modelValue': (v) => emitted.push(v) })
	const view = mount(props)
	expect(view().state.doc.toString()).toBe('a: 1')

	props.value = { ...props.value, modelValue: 'b: 2' }
	await nextTick()
	expect(view().state.doc.toString()).toBe('b: 2')
	expect(emitted).toEqual([])

	view().dispatch({ changes: { from: 0, insert: 'x' } })
	expect(emitted).toEqual(['xb: 2'])
})

test('readonly toggles editing', async () => {
	const props = ref({ modelValue: 'text', readonly: true })
	const view = mount(props)
	expect(view().state.readOnly).toBe(true)
	expect(view().contentDOM.getAttribute('contenteditable')).toBe('false')
	props.value = { ...props.value, readonly: false }
	await nextTick()
	expect(view().state.readOnly).toBe(false)
	expect(view().contentDOM.getAttribute('contenteditable')).toBe('true')
})

test('follows the app theme switch', () => {
	document.documentElement.setAttribute('data-theme', 'light')
	const view = mount(ref({ modelValue: '' }))
	expect(view().state.facet(EditorView.darkTheme)).toBe(false)
	document.documentElement.setAttribute('data-theme', 'dark')
	window.dispatchEvent(new CustomEvent('nivaroos:theme-change'))
	expect(view().state.facet(EditorView.darkTheme)).toBe(true)
})

test('Ctrl-S emits save', () => {
	let saved = 0
	const view = mount(ref({ modelValue: '', onSave: () => saved++ }))
	view().contentDOM.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true, cancelable: true }))
	expect(saved).toBe(1)
})

test('language lookup by file name, path and language name', () => {
	const name = (n) => findLanguage(n)?.name ?? null
	expect(name('docker-compose.yml')).toBe('YAML')
	expect(name('/DATA/a/config.json')).toBe('JSON')
	expect(name('run.sh')).toBe('Shell')
	expect(name('README.md')).toBe('Markdown')
	expect(name('/etc/nginx/sites-enabled/site.conf')).toBe('Nginx')
	expect(name('/etc/foo/app.conf')).toBe('Properties files')
	expect(name('settings.ini')).toBe('Properties files')
	expect(name('Dockerfile')).toBe('Dockerfile')
	expect(name('yaml')).toBe('YAML')
	expect(name('notes.txt')).toBe(null)
})
