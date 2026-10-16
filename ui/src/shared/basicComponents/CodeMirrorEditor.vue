<!--
	The one CodeMirror 6 editor of the UI: v-model text, `lang` (a file
	name/path or a language name like 'yaml'; unknown = plain text, loaded
	lazily from @codemirror/language-data), `readonly`, Ctrl/Cmd-S -> `save`.
	Colours are the app's own tokens, so it follows light/dark by itself.
-->
<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { basicSetup } from 'codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { Compartment, EditorState, Transaction } from '@codemirror/state'
import { indentWithTab } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { jsonParseLinter } from '@codemirror/lang-json'
import { linter, lintGutter } from '@codemirror/lint'
import { search } from '@codemirror/search'
import { tags as t } from '@lezer/highlight'
import { parseDocument } from 'yaml'
import { findLanguage } from './findLanguage'

const props = defineProps({
	modelValue: { type: String, default: '' },
	lang: { type: String, default: '' },
	readonly: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'save'])

// Linting a multi-MB file on every pause would stall typing.
const LINT_MAX = 1 << 20
const yamlLinter = (view) => {
	return parseDocument(view.state.doc.toString()).errors.map((e) => ({
		from: Math.min(e.pos[0], view.state.doc.length),
		to: Math.min(e.pos[1], view.state.doc.length),
		severity: 'error',
		message: e.message,
	}))
}
const LINTERS = { JSON: jsonParseLinter(), YAML: yamlLinter }

const highlight = HighlightStyle.define([
	{ tag: [t.keyword, t.operatorKeyword, t.modifier, t.controlKeyword], color: 'var(--color-accent-fg)' },
	{ tag: [t.string, t.special(t.string), t.regexp], color: 'var(--color-success-fg)' },
	{ tag: [t.number, t.bool, t.atom, t.null], color: 'var(--color-warning-fg)' },
	{ tag: [t.propertyName, t.attributeName, t.definition(t.variableName), t.labelName], color: 'var(--color-info-fg)' },
	{ tag: [t.typeName, t.className, t.tagName, t.heading, t.function(t.variableName)], color: 'var(--color-primary-fg)' },
	{ tag: [t.comment, t.meta, t.processingInstruction], color: 'var(--color-text-muted)', fontStyle: 'italic' },
	{ tag: t.heading, fontWeight: 'bold' },
	{ tag: t.emphasis, fontStyle: 'italic' },
	{ tag: t.strong, fontWeight: 'bold' },
	{ tag: t.link, textDecoration: 'underline' },
	{ tag: t.invalid, color: 'var(--color-danger-fg)' },
])

const theme = EditorView.theme({
	'&': { height: '100%', color: 'var(--theme-text-primary)', backgroundColor: 'var(--theme-bg-window-opaque)' },
	'.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', lineHeight: '1.5' },
	'.cm-gutters': { backgroundColor: 'var(--theme-bg-subtle)', color: 'var(--color-text-muted)', borderRight: '1px solid var(--color-border)' },
	'.cm-activeLine': { backgroundColor: 'var(--theme-card-hover)' },
	'.cm-activeLineGutter': { backgroundColor: 'var(--theme-card-hover)', color: 'var(--theme-text-primary)' },
	'.cm-cursor': { borderLeftColor: 'var(--theme-text-primary)' },
	'&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--color-primary-soft) !important' },
	'.cm-selectionMatch': { backgroundColor: 'var(--color-primary-soft)' },
	'.cm-panels': { backgroundColor: 'var(--theme-bg-subtle)', color: 'var(--theme-text-primary)' },
	'.cm-panel.cm-search': { fontSize: '14px' },
	'.cm-panel.cm-search .cm-textfield, .cm-panel.cm-search .cm-button': { fontSize: '14px', minHeight: '32px', padding: '2px 8px' },
	'.cm-tooltip': { backgroundColor: 'var(--theme-dropdown-bg)', border: '1px solid var(--theme-dropdown-border)' },
})

const host = ref(null)
const isDark = () => document.documentElement.getAttribute('data-theme') === 'dark'
const langConf = new Compartment()
const lintConf = new Compartment()
const roConf = new Compartment()
const darkConf = new Compartment()
let view = null
let lastEmitted = props.modelValue

const roExt = (ro) => [EditorState.readOnly.of(ro), EditorView.editable.of(!ro)]

async function loadLang(name) {
	const desc = name ? findLanguage(name) : null
	const support = desc ? await desc.load().catch(() => []) : []
	if (!view || name !== props.lang) return
	const lint = desc && LINTERS[desc.name] && view.state.doc.length <= LINT_MAX ? [lintGutter(), linter(LINTERS[desc.name])] : []
	view.dispatch({ effects: [langConf.reconfigure(support), lintConf.reconfigure(lint)] })
}

const onTheme = () => view?.dispatch({ effects: darkConf.reconfigure(EditorView.darkTheme.of(isDark())) })

onMounted(() => {
	view = new EditorView({
		parent: host.value,
		state: EditorState.create({
			doc: props.modelValue,
			extensions: [
				// Top: the viewer's floating toolbar sits over the bottom edge.
				search({ top: true }),
				basicSetup,
				keymap.of([indentWithTab, { key: 'Mod-s', preventDefault: true, run: () => {
					emit('save')
					return true
				} }]),
				EditorState.tabSize.of(4),
				theme,
				syntaxHighlighting(highlight),
				darkConf.of(EditorView.darkTheme.of(isDark())),
				roConf.of(roExt(props.readonly)),
				langConf.of([]),
				lintConf.of([]),
				EditorView.updateListener.of((u) => {
					if (!u.docChanged) return
					const v = u.state.doc.toString()
					// Only user edits are emitted, not the parent's own value coming back.
					if (v === lastEmitted) return
					lastEmitted = v
					emit('update:modelValue', v)
				}),
			],
		}),
	})
	loadLang(props.lang)
	window.addEventListener('nivaroos:theme-change', onTheme)
})

onBeforeUnmount(() => {
	window.removeEventListener('nivaroos:theme-change', onTheme)
	view?.destroy()
	view = null
})

watch(() => props.modelValue, (v) => {
	if (!view || v === lastEmitted) return
	lastEmitted = v
	// Not undoable: Ctrl-Z right after opening a file must not blank it.
	view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v }, annotations: Transaction.addToHistory.of(false) })
	// The lint decision depends on the size of what was just loaded.
	loadLang(props.lang)
})
watch(() => props.lang, loadLang)
watch(() => props.readonly, (ro) => view?.dispatch({ effects: roConf.reconfigure(roExt(ro)) }))

defineExpose({ getView: () => view })
</script>

<template>
	<div ref="host" class="code-mirror-editor"></div>
</template>

<style scoped>
.code-mirror-editor {
	width: 100%;
	height: 100%;
	overflow: hidden;
}
</style>
