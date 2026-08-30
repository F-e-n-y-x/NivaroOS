<!-- src/components/files/viewers/DocViewer.vue -->
<!-- Ported from src/components/filebrowser/viewers/DocViewer.vue - same @vue-office/docx rendering, new chrome only. -->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<div class="doc-viewer-body">
			<vue-office-docx :src="docx" @rendered="rendered" />
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
			docx: this.getFileUrl(this.item),
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
.doc-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
}
::v-deep .vue-office-docx {
	height: 100%;
	width: 100%;
	.docx-wrapper {
		background-color: #fff;
		> section.docx {
			box-shadow: none;
		}
	}
}
</style>
