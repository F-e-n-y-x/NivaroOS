<!-- src/apps/files/viewers/ViewerChrome.vue -->
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
		<div ref="viewerBody" class="viewer-body" :class="{ 'no-overflow': noOverflow }">
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
	props: {
		noOverflow: {
			type: Boolean,
			default: false
		}
	},
	computed: {
		hasActions() {
			return !!this.$slots.actions
		},
	},
	// Every viewer here embeds a third-party rendering library (viewerjs,
	// Artplayer, CodeMirror, @vue-office/*) that draws into a canvas or
	// otherwise computes its layout in JS rather than relying purely on
	// CSS - most of them size that layout once, at mount/init time, and
	// never re-check it afterwards. A NivaroOS desktop window resizing
	// only ever changes this element's CSS width/height (see
	// DesktopWindow.vue's drag-resize handles) - the browser's own
	// `window` never fires a resize event, which is the ONLY signal most
	// of these libraries listen for (if they listen for anything at all).
	// That mismatch is what made every one of these viewers look "stuck"
	// at its original size, cropped or off-center, after resizing its
	// window - not a bug in any single viewer, but this shared assumption
	// none of them make explicitly.
	//
	// A ResizeObserver on the actual content box (not `window`) is the
	// correct fix regardless of which library is inside: it fires for
	// this exact scenario (and window resize, sidebar toggle, tab
	// switch - anything that changes this box's real rendered size),
	// so each viewer can re-sync itself the same way it already knows how
	// to (an update()/refresh()/resize() call, or an internal API) via
	// the `viewer-resize` event, without every viewer needing to
	// duplicate its own observer.
	mounted() {
		if (typeof ResizeObserver === 'undefined') return
		let frame = null
		this.resizeObserver = new ResizeObserver((entries) => {
			// rAF-coalesced: a drag-resize fires many observer callbacks in
			// quick succession (one per frame the mouse moves), and forcing
			// every one of those straight into a library's own re-layout
			// call would fight the drag itself for CPU instead of just
			// tracking the final size.
			if (frame) cancelAnimationFrame(frame)
			frame = requestAnimationFrame(() => {
				frame = null
				const entry = entries[entries.length - 1]
				const { width, height } = entry.contentRect
				if (width > 0 && height > 0) this.$emit('viewer-resize', { width, height })
			})
		})
		this.resizeObserver.observe(this.$refs.viewerBody)
	},
	beforeDestroy() {
		this.resizeObserver && this.resizeObserver.disconnect()
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
	overflow: hidden;
}
.viewer-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	display: flex;
	align-items: center;
	justify-content: center;
	position: relative;

	&.no-overflow {
		overflow: hidden !important;
	}
}
.viewer-toolbar {
	position: absolute;
	left: 50%;
	bottom: 1.25rem;
	transform: translateX(-50%);
	z-index: 5;
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-2) var(--space-4);
	background: rgba(30, 30, 30, 0.85);
	backdrop-filter: blur(10px);
	border-radius: var(--radius-pill);
	box-shadow: var(--shadow-md);
	color: #fff;
}
.viewer-actions {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}
.toolbar-divider {
	width: 1px;
	height: 1.1rem;
	background: rgba(255, 255, 255, 0.25);
}
</style>
