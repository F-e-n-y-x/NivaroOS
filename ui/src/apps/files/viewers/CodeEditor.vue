<!-- src/apps/files/viewers/CodeEditor.vue -->
<!--
	Ported in full from src/components/filebrowser/viewers/CodeEditor.vue
	- same CodeMirror mode/addon imports, same $api.file.getContent
	(download)/$api.file.update load/save calls, same Ctrl/Cmd-S
	shortcut. New chrome only: the Save button moves into
	ViewerChrome's `actions` slot, and the "unsaved changes, save before
	closing?" prompt is now a small in-window dialog instead of
	$buefy.dialog.confirm (which renders viewport-wide, not confined to
	the Files window - the same fix already applied to FilesApp's own
	delete confirmation and to MarkdownEditor.vue).
-->
<template>
	<files-viewer-chrome @download="downloadFile(item)" @viewer-resize="onViewerResize">
		<template #actions>
			<b-icon icon="content-save" custom-size="mdi-18px" class="is-clickable" @click.native="saveFile(false)"></b-icon>
		</template>
		<div class="code-editor-body">
			<codemirror ref="cmEditor" v-model="code" :options="cmOptions" @input="onCmCodeChange" @ready="onCmReady" />
		</div>
		<files-dialog-overlay v-if="showUnsavedDialog" :title="$t('Want to save?')" @close="showUnsavedDialog = false">
			<p>{{ $t('Your changes will be lost if you don’t save them.') }}</p>
			<div class="unsaved-actions">
				<button class="btn-secondary" @click="discardAndClose">{{ $t('Don’t Save') }}</button>
				<button class="btn-primary" @click="saveFile(true)">{{ $t('Save') }}</button>
			</div>
		</files-dialog-overlay>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import DialogOverlay from '../DialogOverlay.vue'

// Core
import { codemirror } from 'vue-codemirror'
import 'codemirror/lib/codemirror.css'
// theme css
import 'codemirror/theme/monokai.css'
// require active-line.js
import 'codemirror/addon/selection/active-line.js'

// styleSelectedText
import 'codemirror/addon/selection/mark-selection.js'
import 'codemirror/addon/search/searchcursor.js'

// hint
import 'codemirror/addon/hint/show-hint.js'
import 'codemirror/addon/hint/show-hint.css'
import 'codemirror/addon/hint/javascript-hint.js'

// lint
import 'codemirror/addon/lint/css-lint.js'
import 'codemirror/addon/lint/html-lint.js'
import 'codemirror/addon/lint/javascript-lint.js'
import 'codemirror/addon/lint/json-lint.js'
import 'codemirror/addon/lint/yaml-lint.js'
import 'codemirror/addon/lint/lint.js'
import 'codemirror/addon/lint/lint.css'

// highlightSelectionMatches
import 'codemirror/addon/scroll/annotatescrollbar.js'
import 'codemirror/addon/scroll/simplescrollbars'
import 'codemirror/addon/scroll/simplescrollbars.css'
import 'codemirror/addon/search/matchesonscrollbar.js'
import 'codemirror/addon/search/match-highlighter.js'

// keyMap
import 'codemirror/mode/clike/clike.js'
import 'codemirror/addon/edit/matchbrackets.js'
import 'codemirror/addon/comment/comment.js'
import 'codemirror/addon/dialog/dialog.js'
import 'codemirror/addon/dialog/dialog.css'
import 'codemirror/addon/search/search.js'
import 'codemirror/keymap/sublime.js'

// foldGutter
import 'codemirror/addon/fold/foldgutter.css'
import 'codemirror/addon/fold/brace-fold.js'
import 'codemirror/addon/fold/comment-fold.js'
import 'codemirror/addon/fold/foldcode.js'
import 'codemirror/addon/fold/foldgutter.js'
import 'codemirror/addon/fold/indent-fold.js'
import 'codemirror/addon/fold/markdown-fold.js'
import 'codemirror/addon/fold/xml-fold.js'

// Mode
import 'codemirror/mode/javascript/javascript'
import 'codemirror/mode/clike/clike'
import 'codemirror/mode/go/go'
import 'codemirror/mode/htmlmixed/htmlmixed'
import 'codemirror/mode/htmlembedded/htmlembedded'
import 'codemirror/mode/http/http'
import 'codemirror/mode/php/php'
import 'codemirror/mode/python/python'
import 'codemirror/mode/sql/sql'
import 'codemirror/mode/vue/vue'
import 'codemirror/mode/xml/xml'
import 'codemirror/mode/yaml/yaml'
import 'codemirror/mode/css/css'
import 'codemirror/mode/cmake/cmake'
import 'codemirror/mode/markdown/markdown'
import 'codemirror/mode/lua/lua'
import 'codemirror/mode/ruby/ruby'
import 'codemirror/mode/rust/rust'
import 'codemirror/mode/shell/shell'
// Added: these have real extensions in mixins/mixin.js's typeMap (routed
// here by filePanelMap) that had no imported mode at all - they fell back
// to plain, unhighlighted text every time, silently, since CodeMirror just
// renders content as-is for a mode it doesn't recognize rather than erroring.
import 'codemirror/mode/perl/perl'
import 'codemirror/mode/vb/vb'
import 'codemirror/mode/vbscript/vbscript'
import 'codemirror/mode/swift/swift'
import 'codemirror/mode/r/r'
import 'codemirror/mode/stex/stex'
import 'codemirror/mode/diff/diff'
import 'codemirror/mode/dockerfile/dockerfile'
import 'codemirror/mode/toml/toml'
import 'codemirror/mode/properties/properties'

// Lint libs
import { CSSLint } from 'csslint'
import { JSHINT } from 'jshint'
import jsonlint from 'jsonlint-mod'
import jsyaml from 'js-yaml'

window.CSSLint = CSSLint
window.JSHINT = JSHINT
window.jsonlint = jsonlint
window.jsyaml = jsyaml

// Explicit extension -> CodeMirror mode, replacing the old mime.getType(ext)
// lookup: the `mime` package's type names frequently don't match a
// CodeMirror mode name at all (e.g. mime has no opinion on "go" or "rs",
// and the couple of extensions it does resolve, like text/x-python, only
// worked here because they happened to collide with CodeMirror's own mode
// name) - real coverage was down to whichever handful of extensions someone
// had already special-cased, not everything typeMap routes here. Every
// extension below has a mode actually imported above; anything not listed
// here still opens fine, just as plain unhighlighted text (never an error).
const EXT_MODE_MAP = {
	js: 'text/javascript', json: 'application/json', jsonld: 'application/json',
	vue: 'text/x-vue',
	go: 'text/x-go',
	py: 'text/x-python',
	rb: 'text/x-ruby',
	rs: 'text/x-rustsrc', rust: 'text/x-rustsrc',
	php: 'application/x-httpd-php',
	lua: 'text/x-lua',
	sql: 'text/x-sql',
	sh: 'text/x-sh',
	yaml: 'text/x-yaml', yml: 'text/x-yaml',
	xml: 'application/xml', rss: 'application/xml', atom: 'application/xml',
	html: 'text/html', htm: 'text/html', shtml: 'text/html', shtm: 'text/html',
	css: 'text/css',
	less: 'text/x-less',
	scss: 'text/x-scss', sass: 'text/x-sass',
	c: 'text/x-csrc', h: 'text/x-csrc',
	cpp: 'text/x-c++src',
	cs: 'text/x-csharp',
	java: 'text/x-java',
	perl: 'text/x-perl', pl: 'text/x-perl',
	vb: 'text/x-vb', vbs: 'text/vbscript',
	swift: 'text/x-swift',
	r: 'text/x-rsrc',
	tex: 'text/x-stex',
	diff: 'text/x-diff',
	dockerfile: 'text/x-dockerfile', makefile: 'text/x-cmake', cmake: 'text/x-cmake',
	toml: 'text/x-toml',
	ini: 'text/x-properties', cfg: 'text/x-properties', conf: 'text/x-properties',
	properties: 'text/x-properties', gitconfig: 'text/x-properties',
	asp: 'application/x-aspx', aspx: 'application/x-aspx', jsp: 'application/x-jsp',
}

export default {
	name: 'files-code-editor',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, FilesDialogOverlay: DialogOverlay, codemirror },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			code: '',
			isChange: false,
			showUnsavedDialog: false,
			cmOptions: {
				tabSize: 4,
				styleActiveLine: true,
				lineNumbers: true,
				styleSelectedText: false,
				line: true,
				lint: true,
				foldGutter: true,
				gutters: ['CodeMirror-linenumbers', 'CodeMirror-foldgutter', 'CodeMirror-lint-markers'],
				highlightSelectionMatches: { showToken: /\w/, annotateScrollbar: true },
				mode: 'text/javascript',
				hintOptions: {
					completeSingle: false,
				},
				keyMap: 'sublime',
				matchBrackets: true,
				showCursorWhenSelecting: true,
				theme: 'monokai',
				extraKeys: {
					Ctrl: 'autocomplete',
					'Ctrl-S': () => {
						this.saveFile()
					},
					'Cmd-S': () => {
						this.saveFile()
					},
				},
				scrollbarStyle: 'overlay',
			},
		}
	},
	computed: {
		codemirror() {
			return this.$refs.cmEditor.codemirror
		},
	},
	mounted() {
		this.readFile()
	},
	methods: {
		onCmCodeChange() {
			this.isChange = true
		},
		onCmReady() {
			this.isChange = false
		},
		// CodeMirror 5 measures character/line dimensions from its wrapper
		// element once and caches them - its own docs call out exactly this
		// scenario ("if you...resize it") as needing an explicit refresh().
		// Without this, a resized window left the gutter/line-wrapping
		// stale at the old width until a click or keystroke forced
		// CodeMirror to remeasure on its own.
		onViewerResize() {
			this.codemirror && this.codemirror.refresh()
		},
		readFile() {
			const ext = this.getFileExt(this.item).toLowerCase()
			const mode = EXT_MODE_MAP[ext] || 'text/plain'
			this.codemirror.setOption('mode', mode)
			this.$api.file.download(this.item.path).then((res) => {
				this.code = typeof res.data === 'object' ? JSON.stringify(res.data, null, 2) : String(res.data)
				this.$nextTick(() => {
					this.isChange = false
				})
			})
		},
		saveFile(leave = false) {
			const content = this.codemirror.getValue()
			this.$api.file.update(this.item.path, content).then((res) => {
				if (res.data.success == 200) {
					this.isChange = false
					this.showUnsavedDialog = false
					this.$buefy.toast.open({
						message: this.$t('Saved'),
						type: 'is-success',
					})
					if (leave) {
						this.$emit('close')
					}
				} else {
					this.$buefy.toast.open({
						message: res.data.message,
						type: 'is-danger',
					})
				}
			})
		},
		discardAndClose() {
			this.showUnsavedDialog = false
			this.$emit('close')
		},
		requestClose() {
			if (this.isChange) {
				this.showUnsavedDialog = true
			} else {
				this.$emit('close')
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.code-editor-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	::v-deep .CodeMirror {
		width: 100%;
		height: 100%;
	}
}
.unsaved-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
	margin-top: var(--space-4);
}
.btn-secondary,
.btn-primary {
	border: none;
	border-radius: var(--radius-sm);
	padding: var(--space-2) var(--space-4);
	font-size: var(--font-base);
	cursor: pointer;
}
.btn-secondary {
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
	color: var(--theme-text-primary, #2c3e50);
	&:hover {
		background: var(--color-border-strong, rgba(0, 0, 0, 0.1));
	}
}
.btn-primary {
	background: var(--color-primary, #3273dc);
	color: #fff;
	&:hover {
		background: #2366d1;
	}
}
</style>
