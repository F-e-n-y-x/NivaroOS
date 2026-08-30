<!-- src/components/files/EmptyFolder.vue -->
<!--
	Ground-up replacement for the legacy EmptyHolder.vue (which relied on
	full-page global CSS and a `filePanel.uploaderInstance` DOM-id hack for
	its "Upload Files"/"Upload Folder" buttons - both assumptions broken in
	this windowed context). Real file managers (Explorer/Finder) don't show
	a busy button grid for an empty folder - New Folder/New File/Upload are
	already one click away in the toolbar above, so this is deliberately
	minimal instead of duplicating them here.
-->
<template>
	<div class="empty-folder">
		<b-icon icon="folder-open-outline" custom-size="mdi-48px" class="empty-icon"></b-icon>
		<p class="empty-title">{{ $t('This folder is empty') }}</p>
		<p class="empty-hint">{{ $t('Drag files here to upload') }}</p>
	</div>
</template>

<script>
export default {
	name: 'empty-folder',
}
</script>

<style lang="scss" scoped>
.empty-folder {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	height: 100%;
	color: rgba(0, 0, 0, 0.35);
	text-align: center;
	padding: 1rem;
}
.empty-icon {
	margin-bottom: 0.5rem;
	opacity: 0.6;
}
.empty-title {
	font-size: 0.95rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.55);
}
.empty-hint {
	font-size: 0.8rem;
	margin-top: 0.25rem;
}
</style>
