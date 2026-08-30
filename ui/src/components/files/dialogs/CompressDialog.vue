<!-- src/components/files/dialogs/CompressDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Compress to Zip')" @close="$emit('close')">
		<div class="compress-dialog">
			<b-field :message="errors" :type="errorType" expanded>
				<b-input
					v-model="zipName"
					ref="input"
					v-on:keyup.enter.native="compress"
					@input.native="zipName = zipName.replace(/\//g, '')"
				></b-input>
			</b-field>
			<div class="dialog-actions">
				<b-button :label="$t('Compress')" :loading="isLoading" rounded type="is-primary" @click="compress"></b-button>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import DialogOverlay from '../DialogOverlay.vue'
import { joinPath } from '@/utils/files/path'

export default {
	name: 'compress-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		currentPath: { type: String, required: true },
		// Full item objects (from ContentView.listing) to bundle into the zip.
		items: { type: Array, required: true },
	},
	data() {
		return {
			// A single item gets a name matching it (foo.txt -> foo.zip); a
			// batch has no single name to borrow, so it falls back to a
			// generic "Archive.zip" like most desktop file managers do.
			zipName: this.defaultName(),
			errorType: 'is-success',
			errors: '',
			isLoading: false,
		}
	},
	mounted() {
		this.$nextTick(() => {
			const input = this.$refs.input.getElement()
			input.focus()
			// Select just the base name, not the .zip extension - matches
			// the rename dialog's behavior for the same reason (an accidental
			// full-selection edit would leave the extension a stray typo away
			// from breaking auto-detection on unarchive).
			const dot = this.zipName.lastIndexOf('.')
			input.setSelectionRange(0, dot > -1 ? dot : this.zipName.length)
		})
	},
	methods: {
		defaultName() {
			if (this.items.length === 1) {
				const name = this.items[0].name
				const dot = name.lastIndexOf('.')
				return (dot > 0 ? name.slice(0, dot) : name) + '.zip'
			}
			return 'Archive.zip'
		},
		compress() {
			this.isLoading = true
			const destination = joinPath(this.currentPath, this.zipName)
			this.$api.file
				.archive(this.items.map((item) => item.path), destination)
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
