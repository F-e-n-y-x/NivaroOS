<!-- src/apps/files/viewers/PdfViewer.vue -->
<!--
	Ported from src/components/filebrowser/viewers/PdfViewer.vue - same
	@vue-office/pdf rendering, new chrome only. Legacy's own scoped style
	block targeted `.vue-office-docx` here too (copy-paste leftover from
	DocViewer.vue, so it never actually matched anything in this file) -
	this is fresh CSS for fresh markup, so it correctly targets
	`.vue-office-pdf` instead.
-->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<div v-if="hasError" class="viewer-error-state">
			<b-icon icon="file-alert-outline" custom-size="mdi-48px"></b-icon>
			<p>{{ $t('This PDF couldn’t be opened - it may be corrupted, password-protected, or in an unsupported format.') }}</p>
			<b-button type="is-primary" icon-left="download-outline" @click="downloadFile(item)">{{ $t('Download') }}</b-button>
		</div>
		<div v-else class="pdf-viewer-body">
			<vue-office-pdf :src="src" @rendered="rendered" @error="onError" />
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import VueOfficePdf from '@vue-office/pdf'

export default {
	name: 'files-pdf-viewer',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, VueOfficePdf },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			isLoading: true,
			hasError: false,
			// NOT computed here as `src: this.getFileUrl(this.item)` (this
			// component's original form, before this fix) - getFileUrl()
			// reads this.baseUrl, itself a data() property supplied by the
			// `mixin` above. Vue 2 calls every data() function (this
			// component's own, and each mixin's) to collect their returned
			// plain objects *before* assigning any of them onto the
			// instance/making them reactive - so a data() function can never
			// read another data() property via `this.x`, from its own mixins
			// included, no matter the declaration order. this.baseUrl was
			// silently undefined at this exact point, so the built URL
			// became the literal string "undefinedfile?path=...&token=..."
			// - a real, confirmed 404 on every single PDF (caught live via
			// the browser network panel, not guessed). created() runs after
			// data initialization fully completes, once this.baseUrl is a
			// real, reactive property - see below.
			src: '',
		}
	},
	created() {
		this.src = this.getFileUrl(this.item)
	},
	methods: {
		rendered() {
			this.isLoading = false
		},
		// @vue-office/pdf emits this for a file it can't parse - previously
		// unhandled, so isLoading just stayed true forever with nothing on
		// screen explaining why.
		onError() {
			this.isLoading = false
			this.hasError = true
		},
	},
}
</script>

<style lang="scss" scoped>
// This sits directly on ViewerChrome's own dark (#1e1e1e) shell background
// (it's a v-else sibling of .pdf-viewer-body, not nested inside it) - unlike
// .pdf-viewer-body/.vue-office-pdf below, which paint their own light
// var(--theme-bg-window) background over that shell, this needs light text
// to stay readable, the same as DocViewer/ExcelViewer's identical state.
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
.pdf-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	background: var(--theme-bg-window, #fff);
}
::v-deep .vue-office-pdf {
	height: 100%;
	width: 100%;
}
</style>
