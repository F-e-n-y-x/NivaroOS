<!-- src/components/files/dialogs/DetailDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Detail')" @close="$emit('close')">
		<div class="detail-dialog is-flex is-flex-direction-column is-align-items-center">
			<div class="cover is-unselectable is-flex is-justify-content-center is-align-items-center">
				<div :class="item | coverType">
					<img :class="item | iconType" :src="getIconFile(item)" alt="folder" />
				</div>
			</div>
			<div class="info mt-3 is-flex is-flex-direction-column is-align-items-center">
				<p class="title is-6 has-text-centered">{{ item.name }}</p>
				<div class="info-list">
					<div class="info-row">
						<span class="label">{{ $t('Type') }}</span>
						<span class="value">{{ typeLabel }}</span>
					</div>
					<div class="info-row">
						<span class="label">{{ $t('Date') }}</span>
						<span class="value">{{ item.date | dateFmt }}</span>
					</div>
					<div class="info-row">
						<span class="label">{{ $t('Path') }}</span>
						<span class="value" :title="item.path">{{ item.path }}</span>
					</div>
					<div v-if="!item.is_dir || sizeLoading || folderSize !== null" class="info-row">
						<span class="label">{{ $t('Size') }}</span>
						<span class="value">
							<template v-if="item.is_dir">
								<b-icon v-if="sizeLoading" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
								<template v-else-if="folderSize !== null">{{ folderSize | renderSize }}</template>
							</template>
							<template v-else>{{ item.size | renderSize }}</template>
						</span>
					</div>
				</div>
				<div class="buttons is-justify-content-center">
					<b-button type="is-primary" @click="download">{{ $t('Download') }}</b-button>
				</div>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import DialogOverlay from '../DialogOverlay.vue'

// Previously ported verbatim from src/components/filebrowser/modals/DetailModal.vue, which
// only ever showed name + size (via the Download button label) - no date/path/type. That was
// misleading for folders in particular: `item.size` on a directory is the raw filesystem
// inode block size (~4096 bytes) from the flat folder-listing response, not the folder's
// actual recursive content size, so the old "Download 4.0 KB" label on a folder full of data
// was simply wrong. Rewritten (per direct user follow-up, not the original 20-task plan) to
// show Type/Date/Path always, and to fetch the real recursive size for folders via
// $api.folder.getFolderSize() (confirmed via live GET /folder/size that this endpoint returns
// the actual recursive size, unlike the listing's item.size) instead of displaying the
// misleading inode size.
export default {
	name: 'detail-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	mixins: [mixin],
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			folderSize: null,
			sizeLoading: false,
		}
	},
	computed: {
		typeLabel() {
			if (this.item.is_dir) {
				return this.$t('Folder')
			}
			const ext = this.getFileExt(this.item)
			return ext ? ext.toUpperCase() : this.$t('File')
		},
	},
	watch: {
		item: {
			immediate: true,
			handler() {
				this.fetchFolderSize()
			},
		},
	},
	methods: {
		fetchFolderSize() {
			this.folderSize = null
			if (!this.item || !this.item.is_dir) {
				this.sizeLoading = false
				return
			}
			this.sizeLoading = true
			this.$api.folder
				.getFolderSize(this.item.path)
				.then((res) => {
					this.folderSize = res.data.data
				})
				.catch((e) => {
					// Permission errors (or any other failure) fall back to simply not
					// showing a size, rather than crashing the dialog.
					console.log(`${e} in getFolderSize`)
				})
				.finally(() => {
					this.sizeLoading = false
				})
		},
		download() {
			this.downloadFile(this.item)
			this.$emit('close')
		},
	},
}
</script>

<style lang="scss" scoped>
.cover {
	min-height: 4rem;
}
.info-list {
	width: 100%;
	margin-top: 0.5rem;
}
.info-row {
	display: flex;
	align-items: flex-start;
	gap: 0.5rem;
	padding: 0.25rem 0;
	font-size: 0.85rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
	&:last-child {
		border-bottom: none;
	}
}
.label {
	flex: 0 0 3.5rem;
	color: rgba(0, 0, 0, 0.55);
}
.value {
	flex: 1 1 auto;
	min-width: 0;
	word-break: break-all;
	text-align: right;
}
</style>
