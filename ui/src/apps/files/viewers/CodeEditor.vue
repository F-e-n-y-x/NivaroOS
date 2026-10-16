<!-- src/apps/files/viewers/CodeEditor.vue -->
<!--
	Text/code editor for Files: the shared CodeMirrorEditor (CodeMirror 6)
	plus $api.file.download/$api.file.update load/save, Ctrl/Cmd-S saves.
	New chrome only: the Save button moves into
	ViewerChrome's `actions` slot, and the "unsaved changes, save before
	closing?" prompt is now a small in-window dialog instead of
	$buefy.dialog.confirm (which renders viewport-wide, not confined to
	the Files window - the same fix already applied to FilesApp's own
	delete confirmation and to MarkdownEditor.vue).
-->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<template #actions>
			<b-icon icon="content-save" custom-size="mdi-18px" class="is-clickable" @click="saveFile(false)"></b-icon>
		</template>
		<div class="code-editor-body">
			<code-mirror-editor v-model="content" :lang="item.path || item.name" @update:model-value="isChange = true" @save="saveFile()" />
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
import CodeMirrorEditor from '@/shared/basicComponents/CodeMirrorEditor.vue'

export default {
	emits: ['close'],
	name: 'files-code-editor',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, FilesDialogOverlay: DialogOverlay, CodeMirrorEditor },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			isChange: false,
			showUnsavedDialog: false,
			content: '',
		}
	},
	mounted() {
		this.readFile()
	},
	methods: {
		readFile() {
			this.$api.file.download(this.item.path).then((res) => {
				this.content = typeof res.data === 'object' ? JSON.stringify(res.data, null, 2) : String(res.data)
			})
		},
		saveFile(leave = false) {
			this.$api.file.update(this.item.path, this.content).then((res) => {
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
