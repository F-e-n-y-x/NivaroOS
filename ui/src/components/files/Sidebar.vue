<!-- src/components/files/Sidebar.vue -->
<template>
	<aside class="files-sidebar" :class="{ collapsed: isCollapsed }">
		<div class="sidebar-header">
			<span v-if="!isCollapsed" class="sidebar-title">{{ $t('Files') }}</span>
			<b-icon
				:icon="isCollapsed ? 'chevron-right' : 'chevron-left'"
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
				<b-icon icon="share-outline" pack="casa" class="casa-color-blue" custom-size="casa-22px"></b-icon>
				<span v-if="!isCollapsed">{{ $t('FilesShare') }}</span>
			</button>
			<button
				class="nav-entry"
				:class="{ active: filesController.activeSection === 'drop', rail: isCollapsed }"
				:title="$t('FilesDrop')"
				@click="toggleSection('drop')"
			>
				<b-icon icon="drop" pack="casa" class="casa-color-blue" custom-size="casa-22px"></b-icon>
				<span v-if="!isCollapsed">{{ $t('FilesDrop') }}</span>
			</button>
		</div>
	</aside>
</template>

<script>
export default {
	name: 'files-sidebar',
	inject: ['filesController'],
	computed: {
		isCollapsed() {
			return this.filesController.sidebarCollapsed || this.filesController.breakpoints.sidebarCollapsed
		},
	},
	methods: {
		// Clicking an already-active section switches back to browsing - without
		// this, Share/Drop were one-way doors (their only other exit was
		// navigating to a folder via the tree/mounts, which also resets back to
		// 'browser', but that's not obvious from the Share/Drop screens themselves).
		toggleSection(section) {
			this.filesController.setActiveSection(this.filesController.activeSection === section ? 'browser' : section)
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
}
.sidebar-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 0.85rem 0.4rem;
}
.sidebar-title {
	font-size: 0.7rem;
	font-weight: 700;
	letter-spacing: 0.05em;
	text-transform: uppercase;
	color: rgba(0, 0, 0, 0.4);
}
.sidebar-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
	padding: 0 0.5rem;
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
	border-top: 1px solid rgb(228 233 237);
	padding: 0.5rem;
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}
.nav-entry {
	// <button> elements don't inherit the page's font by default in most
	// browsers (they use the OS's system UI font instead) - without this,
	// "SMB Share"/"Files Drop" render in a visibly different font/weight
	// than the plain-<div> tree-node items above them (DATA, Downloads, etc).
	font: inherit;
	font-size: 0.85rem;
	display: flex;
	align-items: center;
	justify-content: flex-start;
	gap: 0.6rem;
	width: 100%;
	padding: 0.4rem 0.6rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 6px;
	color: rgba(0, 0, 0, 0.7);
	&:hover { background: rgba(0, 0, 0, 0.05); }
	&.active {
		background: rgba(50, 115, 220, 0.14);
		color: #3273dc;
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
</style>
