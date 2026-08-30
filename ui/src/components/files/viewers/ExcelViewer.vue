<!-- src/components/files/viewers/ExcelViewer.vue -->
<!-- Ported from src/components/filebrowser/viewers/ExcelViewer.vue - same @vue-office/excel rendering, new chrome only. -->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<div class="excel-viewer-body">
			<vue-office-excel :src="src" @rendered="rendered" />
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
.excel-viewer-body {
	width: 100%;
	height: 100%;
	overflow: auto;
	background: #fff;
}
::v-deep .vue-office-excel {
	height: 100%;
	width: 100%;
}
</style>
