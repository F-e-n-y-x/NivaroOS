<!-- src/apps/files/viewers/DocViewer.vue -->
<!-- Ported from src/components/filebrowser/viewers/DocViewer.vue - same @vue-office/docx rendering, new chrome only. -->
<template>
	<files-viewer-chrome @download="downloadFile(item)" @viewer-resize="onViewerResize">
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<div v-if="hasError" class="viewer-error-state">
			<b-icon icon="file-alert-outline" custom-size="mdi-48px"></b-icon>
			<p>{{ $t('This document couldn’t be opened - it may be corrupted or in an unsupported format.') }}</p>
			<b-button type="is-primary" icon-left="download-outline" @click="downloadFile(item)">{{ $t('Download') }}</b-button>
		</div>
		<div v-else class="doc-viewer-body">
			<vue-office-docx :key="renderKey" :src="docx" @rendered="rendered" @error="onError" />
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import VueOfficeDocx from '@vue-office/docx'
import '@vue-office/docx/lib/index.css'

export default {
	name: 'files-doc-viewer',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, VueOfficeDocx },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			isLoading: true,
			hasError: false,
			// NOT computed here as `docx: this.getFileUrl(this.item)` - see
			// the identical fix + full explanation in PdfViewer.vue's data().
			// Confirmed live (browser network panel): this made every
			// request 404 on the literal URL "undefinedfile?path=...".
			docx: '',
			renderKey: 0,
		}
	},
	created() {
		this.docx = this.getFileUrl(this.item)
	},
	beforeDestroy() {
		clearTimeout(this.resizeSettleTimer)
	},
	methods: {
		rendered() {
			this.isLoading = false
		},
		// @vue-office/docx emits this for a file it can't parse (corrupted,
		// password-protected, or genuinely not a valid .docx despite the
		// extension) - previously unhandled, so isLoading just stayed true
		// forever with nothing on screen explaining why.
		onError() {
			this.isLoading = false
			this.hasError = true
		},
		// @vue-office/docx exposes no resize/redraw API and doesn't observe
		// its own container, so a resized window left the document's page
		// layout stuck at whatever width it first rendered at. There's
		// nothing to call here the way viewerjs/CodeMirror/Artplayer allow
		// - re-keying to force a full remount is the only way to make an
		// opaque third-party component like this one re-render at a new
		// size. Debounced to when resizing has actually stopped (not on
		// every ResizeObserver tick) since a full re-render/re-parse per
		// tick during a drag would be real, visible jank, not just wasted
		// work.
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
.doc-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
}
::v-deep .vue-office-docx {
	height: 100%;
	width: 100%;
	.docx-wrapper {
		background-color: var(--theme-bg-window, #fff);
		> section.docx {
			box-shadow: none;
		}
	}
}
</style>
