<!-- src/components/files/viewers/CodeEditor.vue -->
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
	<files-viewer-chrome @download="downloadFile(item)">
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

import mime from 'mime'
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

// Lint libs
import { CSSLint } from 'csslint'
import { JSHINT } from 'jshint'
import jsonlint from 'jsonlint-mod'
import jsyaml from 'js-yaml'

window.CSSLint = CSSLint
window.JSHINT = JSHINT
window.jsonlint = jsonlint
window.jsyaml = jsyaml

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
		readFile() {
			const ext = this.getFileExt(this.item)
			let mode = mime.getType(ext) == null ? 'text/javascript' : mime.getType(ext)
			if (ext.toLowerCase() == 'makefile') {
				mode = 'text/x-cmake'
			} else if (ext.toLowerCase() == 'py') {
				mode = 'text/x-python'
			} else if (ext.toLowerCase() == 'go') {
				mode = 'text/x-go'
			} else if (ext.toLowerCase() == 'vue') {
				mode = 'text/x-vue'
			}
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
	gap: 0.5rem;
	margin-top: 1rem;
}
.btn-secondary,
.btn-primary {
	border: none;
	border-radius: 6px;
	padding: 0.45rem 0.9rem;
	font-size: 0.85rem;
	cursor: pointer;
}
.btn-secondary {
	background: rgba(0, 0, 0, 0.06);
	color: #2c3e50;
	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}
.btn-primary {
	background: #3273dc;
	color: #fff;
	&:hover {
		background: #2366d1;
	}
}
</style>
