<!-- src/components/files/viewers/MarkdownEditor.vue -->
<!--
	Ported from src/components/filebrowser/viewers/MarkdownEditor.vue.
	Per the plan's own Reference note: .md files aren't in filePanelMap
	(src/mixins/mixin.js), so this viewer isn't reachable from a file
	click today, same as legacy - it's wired up for Task 19's dispatch
	table to follow the same pattern, and in case a future task adds a
	filePanelMap entry for it. Two real bugs fixed while porting (both
	trivial, and pointless to preserve in a "for future use" component):
	legacy's template called a `saveFile` method that was never defined
	anywhere (would have thrown if ever clicked), and `isChange` was
	read in close() but never initialized in data() (so the "save
	before closing?" prompt could never actually fire). The close
	confirmation itself is now a small in-window dialog instead of
	$buefy.dialog.confirm, which renders viewport-wide rather than
	confined to the Files window - the same fix already applied to
	CodeEditor.vue and to FilesApp's own delete confirmation.
-->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<template #actions>
			<b-icon icon="content-save" custom-size="mdi-18px" class="is-clickable" @click.native="saveFile(false)"></b-icon>
		</template>
		<div class="markdown-editor-body">
			<editor-content :editor="editor" class="mark-container" />
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
import { Editor, EditorContent } from '@tiptap/vue-2'
import StarterKit from '@tiptap/starter-kit'
import Highlight from '@tiptap/extension-highlight'
import Typography from '@tiptap/extension-typography'

export default {
	name: 'files-markdown-editor',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, FilesDialogOverlay: DialogOverlay, EditorContent },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			editor: null,
			code: '',
			isChange: false,
			showUnsavedDialog: false,
		}
	},
	async mounted() {
		const content = await this.readFile()
		this.editor = new Editor({
			extensions: [StarterKit, Highlight, Typography],
			content,
			onUpdate: () => {
				this.isChange = true
			},
		})
	},
	beforeDestroy() {
		this.editor && this.editor.destroy()
	},
	methods: {
		async readFile() {
			const res = await this.$api.file.download(this.item.path)
			this.code = String(res.data)
			this.$nextTick(() => {
				this.isChange = false
			})
			return this.code
		},
		saveFile(leave) {
			const content = this.editor.getHTML()
			this.$api.file.update(this.item.path, content).then((res) => {
				if (res.data.success === 200) {
					this.isChange = false
					this.showUnsavedDialog = false
					this.$buefy.toast.open({ message: this.$t('Saved'), type: 'is-success' })
					if (leave) this.$emit('close')
				} else {
					this.$buefy.toast.open({ message: res.data.message, type: 'is-danger' })
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
.markdown-editor-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	padding: 1.5rem;
	background: #fff;
}
.mark-container {
	max-width: 48rem;
	margin: 0 auto;
	height: 100%;
}
// tiptap/ProseMirror renders content into a subtree scoped CSS's own
// attribute selectors can't reach (it's inserted by the library, not
// present in this component's template) - ::v-deep is required here,
// same as legacy's equivalent (unscoped) block.
.mark-container::v-deep .ProseMirror {
	width: 100%;
	height: 100%;
	outline: none;
	> * + * {
		margin-top: 0.75em;
	}
	ul,
	ol {
		padding: 0 1rem;
	}
	h1,
	h2,
	h3,
	h4,
	h5,
	h6 {
		line-height: 1.1;
	}
	code {
		background-color: rgba(97, 97, 97, 0.1);
		color: #616161;
	}
	pre {
		background: #0d0d0d;
		color: #fff;
		padding: 0.75rem 1rem;
		border-radius: 0.5rem;
		code {
			color: inherit;
			padding: 0;
			background: none;
			font-size: 0.8rem;
		}
	}
	img {
		max-width: 100%;
		height: auto;
	}
	hr {
		margin: 1rem 0;
	}
	blockquote {
		padding-left: 1rem;
		border-left: 2px solid rgba(13, 13, 13, 0.1);
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
