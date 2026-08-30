<!-- src/components/files/viewers/PdfViewer.vue -->
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
		<div class="pdf-viewer-body">
			<vue-office-pdf :src="src" @rendered="rendered" />
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
			src: this.getFileUrl(this.item),
		}
	},
	methods: {
		rendered() {
			this.isLoading = false
		},
	},
}
</script>

<style lang="scss" scoped>
.pdf-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	background: #fff;
}
::v-deep .vue-office-pdf {
	height: 100%;
	width: 100%;
}
</style>
