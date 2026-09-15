<!-- src/apps/files/viewers/UnsupportedViewer.vue -->
<!--
	Opens for any file extension getPanelType() (mixins/mixin.js) doesn't
	recognize - archives, disk images, executables, PSD/AI, or anything
	else with no registered viewer. Previously these silently fell through
	to FilesApp.vue's Detail dialog on double-click, a small metadata modal
	with no download action built the way every real viewer has one - this
	gives every file type the same windowed-viewer experience (consistent
	with "double-click always opens a window", not "usually a window,
	sometimes a small modal") and a clear next step, instead of a dead end.
	Detail dialog itself is unaffected - it's still reachable from the
	right-click menu's own "Detail" action, a separate code path.
-->
<template>
	<files-viewer-chrome @download="downloadFile(item)">
		<div class="unsupported-viewer-body">
			<img :src="getIconFile(item)" :alt="item.name" class="file-icon" />
			<p class="file-name" :title="item.name">{{ item.name }}</p>
			<p class="file-meta">{{ extensionLabel }} · {{ item.size | renderSize }}</p>
			<p class="no-preview-text">{{ $t('No preview available for this file type.') }}</p>
			<b-button type="is-primary" icon-left="download-outline" @click="downloadFile(item)">{{ $t('Download') }}</b-button>
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'

export default {
	name: 'files-unsupported-viewer',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome },
	props: {
		item: { type: Object, required: true },
	},
	computed: {
		extensionLabel() {
			const ext = this.getFileExt(this.item)
			return ext ? ext.toUpperCase() : this.$t('File')
		},
	},
}
</script>

<style lang="scss" scoped>
.unsupported-viewer-body {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-6);
	text-align: center;
	color: #fff;
}
.file-icon {
	width: 4.5rem;
	height: 4.5rem;
	object-fit: contain;
	margin-bottom: var(--space-2);
	filter: drop-shadow(0 4px 12px rgba(0, 0, 0, 0.4));
}
.file-name {
	font-size: var(--font-lg);
	font-weight: 600;
	max-width: 28rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.file-meta {
	font-size: var(--font-sm);
	color: rgba(255, 255, 255, 0.6);
}
.no-preview-text {
	font-size: var(--font-sm);
	color: rgba(255, 255, 255, 0.5);
	margin: var(--space-2) 0 var(--space-4);
}
</style>
