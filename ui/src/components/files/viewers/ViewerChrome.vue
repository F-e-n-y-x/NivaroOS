<!-- src/components/files/viewers/ViewerChrome.vue -->
<!--
	Shared wrapper for every file viewer. Each viewer opens as its own
	standalone desktop window (DesktopWindow.vue's COMPONENT_REGISTRY), so
	the window's own titlebar already shows the file name and provides
	close/minimize/maximize - this only supplies a floating control bar
	(download, plus whatever viewer-specific controls a given viewer needs:
	ImageViewer's zoom/rotate/prev-next, CodeEditor's Save), pinned to the
	bottom of the content like a media player's on-screen controls, rather
	than a bar docked at the top.
-->
<template>
	<div class="viewer-shell">
		<div class="viewer-body">
			<slot></slot>
		</div>
		<div class="viewer-toolbar">
			<div v-if="hasActions" class="viewer-actions">
				<slot name="actions"></slot>
			</div>
			<span v-if="hasActions" class="toolbar-divider"></span>
			<b-icon icon="download-outline" custom-size="mdi-18px" class="is-clickable" @click.native="$emit('download')"></b-icon>
		</div>
	</div>
</template>

<script>
export default {
	name: 'files-viewer-chrome',
	computed: {
		hasActions() {
			return !!this.$slots.actions
		},
	},
}
</script>

<style lang="scss" scoped>
.viewer-shell {
	position: absolute;
	inset: 0;
	background: #1e1e1e;
	display: flex;
	flex-direction: column;
}
.viewer-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	display: flex;
	align-items: center;
	justify-content: center;
	position: relative;
}
.viewer-toolbar {
	position: absolute;
	left: 50%;
	bottom: 1.25rem;
	transform: translateX(-50%);
	z-index: 5;
	display: flex;
	align-items: center;
	gap: 0.75rem;
	padding: 0.5rem 0.9rem;
	background: rgba(30, 30, 30, 0.85);
	backdrop-filter: blur(10px);
	border-radius: 999px;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
	color: #fff;
}
.viewer-actions {
	display: flex;
	align-items: center;
	gap: 0.6rem;
}
.toolbar-divider {
	width: 1px;
	height: 1.1rem;
	background: rgba(255, 255, 255, 0.25);
}
</style>
