<!-- src/apps/files/Sidebar.vue -->
<template>
	<aside
		class="files-sidebar"
		:class="{ collapsed: isCollapsed, resizing: isResizing }"
		:style="!isCollapsed ? { width: sidebarWidth + 'px' } : null"
	>
		<div class="sidebar-header">
			<b-icon
				:icon="isCollapsed ? 'chevron-right' : 'chevron-left'"
				:title="isCollapsed ? $t('Expand sidebar') : $t('Collapse sidebar')"
				custom-size="mdi-18px"
				class="is-clickable"
				@click.native="filesController.toggleSidebar()"
			></b-icon>
		</div>
		<div class="sidebar-body scrollbars-light">
			<slot v-if="!isCollapsed"></slot>
			<slot v-else name="rail"></slot>
		</div>
		<div class="sidebar-nav">
			<button
				class="nav-entry"
				:class="{ active: filesController.activeSection === 'shared', rail: isCollapsed }"
				:title="$t('FilesShare')"
				@click="toggleSection('shared')"
			>
				<b-icon icon="share-variant-outline" pack="mdi" custom-size="mdi-22px" class="share-glyph"></b-icon>
				<span v-if="!isCollapsed">{{ $t('FilesShare') }}</span>
			</button>
			<button
				class="nav-entry"
				:class="{ active: filesController.activeSection === 'trash', rail: isCollapsed }"
				:title="$t('Trash')"
				@click="toggleSection('trash')"
			>
				<b-icon icon="delete-outline" pack="mdi" custom-size="mdi-22px" class="trash-glyph"></b-icon>
				<span v-if="!isCollapsed">{{ $t('Trash') }}</span>
			</button>
		</div>
		<div v-if="!isCollapsed" class="resize-handle" @mousedown="startResize"></div>
	</aside>
</template>

<script>
const SIDEBAR_WIDTH_KEY = 'files-sidebar-width'
const MIN_SIDEBAR_WIDTH = 180
const MAX_SIDEBAR_WIDTH = 480
const DEFAULT_SIDEBAR_WIDTH = 240 // matches the old fixed 15rem at the default 16px root font size

export default {
	name: 'files-sidebar',
	inject: ['filesController'],
	data() {
		const stored = parseInt(localStorage.getItem(SIDEBAR_WIDTH_KEY), 10)
		return {
			sidebarWidth: stored >= MIN_SIDEBAR_WIDTH && stored <= MAX_SIDEBAR_WIDTH ? stored : DEFAULT_SIDEBAR_WIDTH,
			isResizing: false,
			resizeStartX: 0,
			resizeStartWidth: 0,
		}
	},
	computed: {
		isCollapsed() {
			return this.filesController.sidebarCollapsed || this.filesController.breakpoints.sidebarCollapsed
		},
	},
	beforeDestroy() {
		window.removeEventListener('mousemove', this.onResizeMove)
		window.removeEventListener('mouseup', this.onResizeEnd)
	},
	methods: {
		// Clicking an already-active section switches back to browsing - without
		// this, Share was a one-way door (its only other exit was navigating to
		// a folder via the tree/mounts, which also resets back to 'browser', but
		// that's not obvious from the Share screen itself).
		toggleSection(section) {
			this.filesController.setActiveSection(this.filesController.activeSection === section ? 'browser' : section)
		},
		startResize(event) {
			this.isResizing = true
			this.resizeStartX = event.clientX
			this.resizeStartWidth = this.sidebarWidth
			window.addEventListener('mousemove', this.onResizeMove)
			window.addEventListener('mouseup', this.onResizeEnd)
			event.preventDefault()
		},
		onResizeMove(event) {
			const next = this.resizeStartWidth + (event.clientX - this.resizeStartX)
			this.sidebarWidth = Math.min(MAX_SIDEBAR_WIDTH, Math.max(MIN_SIDEBAR_WIDTH, next))
		},
		onResizeEnd() {
			this.isResizing = false
			window.removeEventListener('mousemove', this.onResizeMove)
			window.removeEventListener('mouseup', this.onResizeEnd)
			localStorage.setItem(SIDEBAR_WIDTH_KEY, String(this.sidebarWidth))
		},
	},
}
</script>

<style lang="scss" scoped>
.files-sidebar {
	flex-shrink: 0;
	width: 15rem;
	display: flex;
	flex-direction: column;
	position: relative;
	background: rgba(0, 0, 0, 0.015);
	border-right: 1px solid rgb(228 233 237);
	transition: width 0.15s ease;
	&.collapsed { width: 4rem; }
	// No animated width transition while actively dragging - it fights the
	// mousemove handler's own updates and lags visibly behind the cursor.
	&.resizing { transition: none; }
}
.resize-handle {
	position: absolute;
	top: 0;
	right: -3px;
	width: 6px;
	height: 100%;
	cursor: col-resize;
	z-index: 5;
	&:hover {
		background: rgba(50, 115, 220, 0.25);
	}
}
.files-sidebar.resizing .resize-handle {
	background: rgba(50, 115, 220, 0.35);
}
.sidebar-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: flex-end;
	padding: var(--space-2) var(--space-3);
}

.files-sidebar.collapsed .sidebar-header {
	justify-content: center;
	padding: var(--space-2) 0;
}
.sidebar-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
	padding: 0 var(--space-2);
}
// In the narrow collapsed rail, an 8px scrollbar (the same width used
// everywhere else via .scrollbars-light) eats a disproportionate chunk
// of the ~36px available width and visually shoves the icon column off
// to one side - hide it here (scroll still works via wheel/touch), the
// same way compact icon-only sidebars (VS Code's activity bar, etc.)
// usually do.
.files-sidebar.collapsed .sidebar-body {
	scrollbar-width: none;
	&::-webkit-scrollbar {
		display: none;
	}
}
.sidebar-nav {
	flex-shrink: 0;
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));
	padding: var(--space-2);
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}
.nav-entry {
	// <button> elements don't inherit the page's font by default in most
	// browsers (they use the OS's system UI font instead) - without this,
	// "Share" renders in a visibly different font/weight than the plain-
	// <div> tree-node items above it (DATA, Downloads, etc).
	font: inherit;
	font-size: var(--font-sm);
	display: flex;
	align-items: center;
	justify-content: flex-start;
	gap: var(--space-2);
	width: 100%;
	padding: var(--space-2) var(--space-2);
	border: none;
	background: none;
	cursor: pointer;
	border-radius: var(--radius-sm);
	color: rgba(0, 0, 0, 0.7);
	&:hover { background: rgba(0, 0, 0, 0.05); }
	&.active {
		background: rgba(50, 115, 220, 0.14);
		color: var(--color-primary, #3273dc);
		font-weight: 600;
	}
	// Compact/rail mode: a tight square hugging just the icon, centered in
	// the rail, instead of a full-width bar behind a small centered icon -
	// same fix as FolderTree.vue's own .rail nodes.
	&.rail {
		justify-content: center;
		width: 2.25rem;
		height: 2.25rem;
		padding: 0;
		margin: 0 auto;
	}
}
.share-glyph {
	color: #3b82f6;
}
.trash-glyph {
	color: var(--theme-text-muted, #64748b);
}
</style>
