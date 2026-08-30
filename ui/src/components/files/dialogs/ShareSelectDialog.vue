<!-- src/components/files/dialogs/ShareSelectDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Share a folder')" @close="$emit('close')">
		<div class="share-select-dialog">
			<ul class="folder-list scrollbars-light">
				<folder-tree picker @pick="selectedPath = $event"></folder-tree>
			</ul>
			<div class="dialog-actions">
				<b-button
					:disabled="!selectedPath"
					:label="$t('Submit')"
					:loading="isSaving"
					rounded
					type="is-primary"
					@click="createShare"
				></b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'
import FolderTree from '../FolderTree.vue'

export default {
	name: 'share-select-dialog',
	components: { FilesDialogOverlay: DialogOverlay, FolderTree },
	data() {
		return {
			selectedPath: null,
			isSaving: false,
		}
	},
	methods: {
		createShare() {
			if (!this.selectedPath) return
			this.isSaving = true
			// Verified against legacy SelectShareModal.vue's saveShares(): it always POSTs an
			// ARRAY of { path, anonymous } objects (built from its multi-select checkbox list)
			// to $api.samba.createShare, never a bare `{ path }` object as the task brief's
			// step 2 literally suggested - a bare object would not match what the real
			// /samba/shares POST endpoint (backed by that same array-shaped payload) expects.
			// This dialog only lets the user pick one folder at a time (see FolderTree.vue's
			// new `picker` prop), so it sends a single-element array here.
			this.$api.samba
				.createShare([{ path: this.selectedPath, anonymous: true }])
				.then(() => {
					this.isSaving = false
					this.$emit('created')
					this.$emit('close')
				})
				.catch((error) => {
					this.isSaving = false
					// Matches legacy SelectShareModal.vue's saveShares() error handling exactly.
					this.$buefy.toast.open({
						message: error.response.data.message,
						type: 'is-danger',
					})
				})
		},
	},
}
</script>

<style lang="scss" scoped>
.folder-list {
	background: #f8f8f8;
	border: 1px solid rgba(0, 0, 0, 0.1);
	border-radius: 0.75rem;
	padding: 0.5rem;
	max-height: 20rem;
	overflow-y: auto;
}
.dialog-actions {
	display: flex;
	justify-content: flex-end;
	margin-top: 1rem;
}
</style>
