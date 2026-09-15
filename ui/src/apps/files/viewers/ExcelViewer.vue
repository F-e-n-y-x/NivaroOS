<!-- src/apps/files/viewers/ExcelViewer.vue -->
<!-- Ported from src/components/filebrowser/viewers/ExcelViewer.vue - same @vue-office/excel rendering, new chrome only. -->
<template>
	<files-viewer-chrome @download="downloadFile(item)" @viewer-resize="onViewerResize">
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<div v-if="hasError" class="viewer-error-state">
			<b-icon icon="file-alert-outline" custom-size="mdi-48px"></b-icon>
			<p>{{ $t('This spreadsheet couldn’t be opened - it may be corrupted or in an unsupported format.') }}</p>
			<b-button type="is-primary" icon-left="download-outline" @click="downloadFile(item)">{{ $t('Download') }}</b-button>
		</div>
		<div v-else class="excel-viewer-body">
			<vue-office-excel :key="renderKey" :src="src" @rendered="rendered" @error="onError" />
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import VueOfficeExcel from '@vue-office/excel'
import '@vue-office/excel/lib/index.css'

export default {
	name: 'files-excel-viewer',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, VueOfficeExcel },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			isLoading: true,
			hasError: false,
			// NOT computed here as `src: this.getFileUrl(this.item)` - see
			// the identical fix + full explanation in PdfViewer.vue's data().
			// Confirmed live (browser network panel): this made every
			// request 404 on the literal URL "undefinedfile?path=...".
			src: '',
			renderKey: 0,
		}
	},
	created() {
		this.src = this.getFileUrl(this.item)
	},
	beforeDestroy() {
		clearTimeout(this.resizeSettleTimer)
	},
	methods: {
		rendered() {
			this.isLoading = false
		},
		// @vue-office/excel emits this for a file it can't parse - previously
		// unhandled, so isLoading just stayed true forever with nothing on
		// screen explaining why.
		onError() {
			this.isLoading = false
			this.hasError = true
		},
		// Same reasoning as DocViewer.vue's onViewerResize: @vue-office/excel
		// has no resize API or container observer of its own, so a
		// debounced re-key (forcing a full remount) is the only way to get
		// its spreadsheet grid to re-lay-out at a new container size.
		onViewerResize() {
			clearTimeout(this.resizeSettleTimer)
			this.resizeSettleTimer = setTimeout(() => {
				this.isLoading = true
				this.renderKey++
			}, 500)
		},
	},
}
</script>

<style lang="scss" scoped>
.viewer-error-state {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-6);
	max-width: 24rem;
	text-align: center;
	color: #fff;
}
.excel-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	background: var(--theme-bg-window, #fff);
}
::v-deep .vue-office-excel {
	height: 100%;
	width: 100%;
}
</style>
