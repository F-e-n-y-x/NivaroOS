<!-- src/components/files/dialogs/ExtractDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Extract')" @close="$emit('close')">
		<div class="extract-dialog">
			<b-field :message="errors" :type="errorType" expanded>
				<b-input
					v-model="folderName"
					ref="input"
					v-on:keyup.enter.native="extract"
					@input.native="folderName = folderName.replace(/\//g, '')"
				></b-input>
			</b-field>
			<div class="dialog-actions">
				<b-button :label="$t('Extract')" :loading="isLoading" rounded type="is-primary" @click="extract"></b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'
import { joinPath } from '@/utils/files/path'

// Archive extensions that can have a second, longer suffix worth stripping
// together (foo.tar.gz -> "foo", not "foo.tar") - checked longest-first so
// "tar.gz" matches before the plain "gz"/"tar" fallback would.
const COMPOUND_EXTENSIONS = ['tar.gz', 'tar.bz2', 'tar.xz', 'tar.lz4', 'tar.sz', 'tar.zst', 'tar.br']

export default {
	name: 'extract-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		currentPath: { type: String, required: true },
		item: { type: Object, required: true },
	},
	data() {
		return {
			folderName: this.defaultName(),
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
		defaultName() {
			const name = this.item.name
			const lower = name.toLowerCase()
			const compound = COMPOUND_EXTENSIONS.find((ext) => lower.endsWith('.' + ext))
			if (compound) return name.slice(0, -(compound.length + 1))
			const dot = name.lastIndexOf('.')
			return dot > 0 ? name.slice(0, dot) : name
		},
		extract() {
			this.isLoading = true
			const destination = joinPath(this.currentPath, this.folderName)
			this.$api.file
				.unarchive(this.item.path, destination)
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
					this.errorType = 'is-danger'
					this.errors = err.response ? err.response.data.message : String(err)
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
