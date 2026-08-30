<!-- src/components/files/dialogs/NewFolderDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('New Folder')" @close="$emit('close')">
		<div class="new-folder-dialog">
			<b-field :message="errors" :type="errorType" expanded>
				<b-input
					v-model="folderName"
					ref="input"
					v-on:keyup.enter.native="createFolder"
					@input.native="folderName = folderName.replace(/\//g, '')"
				></b-input>
			</b-field>
			<div class="dialog-actions">
				<b-button :label="$t('Submit')" :loading="isLoading" rounded type="is-primary" @click="createFolder"></b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'
import { joinPath } from '@/utils/files/path'

export default {
	name: 'new-folder-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		currentPath: { type: String, required: true },
	},
	data() {
		return {
			folderName: 'New Folder',
			errorType: 'is-success',
			errors: '',
			isLoading: false,
		}
	},
	mounted() {
		this.$nextTick(() => {
			this.$refs.input.getElement().select()
		})
	},
	methods: {
		createFolder() {
			this.isLoading = true
			const newPath = joinPath(this.currentPath, this.folderName)
			this.$api.folder
				.create(newPath)
				.then((res) => {
					if (res.data.success == 200) {
						this.$emit('created')
						this.$emit('close')
					} else {
						this.errorType = 'is-danger'
						this.errors = res.data.message
					}
					this.isLoading = false
				})
				.catch((err) => {
					console.log(err)
					this.isLoading = false
				})
		},
	},
}
</script>

<style lang="scss" scoped>
.dialog-actions {
	display: flex;
	justify-content: flex-end;
	margin-top: 1rem;
}
</style>
