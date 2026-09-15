<!-- src/apps/files/DetailWindow.vue -->
<!--
	Was DetailDialog.vue, rendered inside FilesApp.vue's own DialogOverlay -
	a backdrop covering the whole Files window (click it, or its own close
	button, to get rid of it) that blocked interacting with anything else in
	Files while it was open, and couldn't be moved out of the way. Detail is
	just information about one file - there's no reason looking at it should
	stop you browsing, and every other per-file window in this app (the
	viewers) already opens as its own real desktop window instead: movable,
	closable independently, never blocking the Files window underneath.
	Registered in windowRegistry.js's COMPONENT_REGISTRY like those - this
	has no v-if/DialogOverlay wrapper of its own because DesktopWindow.vue
	already supplies the draggable titlebar and close button for any
	component that isn't in OWN_TITLEBAR_COMPONENTS.
-->
<template>
	<div class="detail-window is-flex is-flex-direction-column is-align-items-center">
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
</template>

<script>
import { mixin } from '@/mixins/mixin'

export default {
	name: 'detail-window',
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
					// showing a size, rather than crashing the window.
					console.log(`${e} in getFolderSize`)
				})
				.finally(() => {
					this.sizeLoading = false
				})
		},
		download() {
			this.downloadFile(this.item)
		},
	},
}
</script>

<style lang="scss" scoped>
.detail-window {
	width: 100%;
	height: 100%;
	overflow: auto;
	padding: var(--space-5);
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-primary, #2c3e50);
	box-sizing: border-box;
}
.cover {
	min-height: 4rem;
}
.info-list {
	width: 100%;
	max-width: 20rem;
	margin-top: var(--space-2);
}
.info-row {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	padding: var(--space-1) 0;
	font-size: var(--font-base);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	&:last-child {
		border-bottom: none;
	}
}
.label {
	flex: 0 0 3.5rem;
	color: var(--theme-text-secondary, rgba(0, 0, 0, 0.55));
}
.value {
	flex: 1 1 auto;
	min-width: 0;
	word-break: break-all;
	text-align: right;
}
</style>
