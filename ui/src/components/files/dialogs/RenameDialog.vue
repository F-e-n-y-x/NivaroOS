<!-- src/components/files/dialogs/RenameDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Rename')" @close="$emit('close')">
		<div class="rename-dialog">
			<div class="cover is-flex is-justify-content-center is-align-items-center">
				<div :class="item | coverType">
					<img :class="item | iconType" :src="getIconFile(item)" alt="folder" />
				</div>
			</div>
			<b-field :message="errors" :type="errorType" class="mt-4" expanded>
				<b-input ref="input" v-model="fileName" v-on:keyup.enter.native="saveNewName" @input.native="fileName = fileName.replace(/\//g, '')"></b-input>
			</b-field>
			<div class="dialog-actions">
				<b-button :label="$t('Submit')" :loading="isLoading" rounded type="is-primary" @click="saveNewName"></b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import DialogOverlay from '../DialogOverlay.vue'
import { parentPath, joinPath } from '@/utils/files/path'

export default {
	name: 'rename-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	mixins: [mixin],
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			fileName: this.item.name,
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
		saveNewName() {
			if (this.item.name === this.fileName) {
				this.$emit('close')
				return
			}
			// Matches legacy RenameModal.vue: always uses $api.file.rename
			// regardless of item.is_dir. Verified against the NivaroOS backend
			// (route/v1.go) that both `/v1/file/name` and `/v1/folder/name`
			// PUT routes are registered to the exact same handler
			// (v1.RenamePath -> service.System().RenameFile, a plain
			// os.Rename) - so there is no folder-vs-file distinction to make
			// here; either API call is equivalent for a directory.
			this.isLoading = true
			const dir = parentPath(this.item.path) || '/'
			const newPath = joinPath(dir, this.fileName)
			this.$api.file
				.rename(this.item.path, newPath)
				.then((res) => {
					if (res.data.success == 200) {
						this.$emit('renamed')
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
.cover {
	min-height: 4rem;
}
.dialog-actions {
	display: flex;
	justify-content: flex-end;
	margin-top: 1rem;
}
</style>
