<!-- src/components/files/FolderTree.vue -->
<template>
	<div class="folder-tree" :class="{ rail }">
		<!-- Root Start -->
		<!--
			In picker mode, Root is shown (for context/parity with the browser sidebar) but is
			NOT selectable - this mirrors legacy SelectShareModal.vue, which renders its
			`rootDataList` (just "Root") with a `disbiled` class (opacity + pointer-events:none,
			checkbox always unchecked/disabled) while only its `dataList` (DATA + shortcuts) items
			are actually clickable/shareable.
		-->
		<div
			v-for="item in rootDataList"
			v-show="item.visible"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), rail, disabled: picker, 'drop-target': dragHoverPath === item.path }"
			:title="rail ? item.name : null"
			@click="openRoot(item.path)"
			@contextmenu.prevent="onContextMenu(item, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon">
				<b-icon :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="casa-22px"></b-icon>
			</span>
			<span v-if="!rail" class="tree-node-label one-line">{{ item.name }}</span>
		</div>
		<!-- Root End -->

		<!-- Data (built-in folders + shortcuts) Start -->
		<div
			v-for="item in dataList"
			v-show="item.visible"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), rail, 'drop-target': dragHoverPath === item.path }"
			:title="rail ? item.name : null"
			@click="open(item.path)"
			@contextmenu.prevent="onContextMenu(item, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon is-relative">
				<b-icon :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="casa-22px"></b-icon>
				<span v-if="!rail && checkSharevisibility(item)" class="share-badge">
					<b-icon
						pack="casa"
						icon="share"
						custom-size="casa-10px"
						class="casa-color-green casa-shape-rounded casa-shape-12px"
					></b-icon>
				</span>
			</span>
			<span v-if="!rail" class="tree-node-label one-line">{{ item.name }}</span>
		</div>
		<!-- Data End -->

		<sidebar-context-menu ref="contextMenu"></sidebar-context-menu>
	</div>
</template>

<script>
import events from '@/events/events'
import has from 'lodash/has'
import { isFilesDragEvent, getFilesDragData } from '@/utils/files/dragDrop'
import SidebarContextMenu from './SidebarContextMenu.vue'

// How long a files drag has to hover a sidebar location before it
// auto-navigates there (a "spring-loaded folder", matching Explorer/
// Finder) - long enough that passing over items on the way to another
// one doesn't trigger unwanted navigation, short enough to not feel
// sluggish when it's the intended target.
const HOVER_OPEN_DELAY = 700

export default {
	name: 'folder-tree',
	inject: ['filesController'],
	components: {
		SidebarContextMenu,
	},
	props: {
		// Renders icon-only nodes (no labels/badges) for the collapsed icon-rail sidebar mode.
		rail: {
			type: Boolean,
			default: false,
		},
		// Picker mode (added for Task 16's ShareSelectDialog): clicking a (non-Root) node
		// emits `pick(path)` instead of navigating the main browser. This still only offers
		// the same fixed set of shortcuts this component always shows - FolderTree is a flat
		// pinned-shortcut list, not a recursive/arbitrary folder browser (see Task 9), which
		// matches what legacy SelectShareModal.vue's own inline folder list already offered.
		picker: {
			type: Boolean,
			default: false,
		},
	},
	data() {
		return {
			// Only used in picker mode, to highlight the currently-picked node.
			pickedPath: null,
			rootDataList: [
				{
					name: 'Root',
					icon: 'root-outline',
					pack: 'casa',
					path: '/',
					visible: true,
					selected: true,
					extensions: null,
				},
			],

			initFolders: [
				{
					name: 'DATA',
					icon: 'data-outline',
					pack: 'casa',
					path: '/DATA',
					visible: true,
					selected: true,
					extensions: null,
				},
				{
					// Real folder on disk (/DATA/Desktop), created automatically
					// if missing (see ensureDesktopFolder()) so it's always
					// present by default - the actual drop target for dragging
					// files/folders onto the OS desktop background.
					name: 'Desktop',
					icon: 'computer-outline',
					pack: 'casa',
					path: '/DATA/Desktop',
					visible: true,
					selected: true,
					extensions: null,
				},
				{
					name: 'Documents',
					icon: 'files-outline',
					pack: 'casa',
					path: '/DATA/Documents',
					visible: true,
					selected: true,
					extensions: null,
				},
				{
					name: 'Downloads',
					icon: 'downloads-outline',
					pack: 'casa',
					path: '/DATA/Downloads',
					visible: true,
					selected: true,
					extensions: null,
				},
				{
					name: 'Gallery',
					icon: 'gallery-outline',
					pack: 'casa',
					path: '/DATA/Gallery',
					visible: true,
					selected: true,
					extensions: null,
				},
				{
					name: 'Media',
					icon: 'media-outline',
					pack: 'casa',
					path: '/DATA/Media',
					visible: true,
					selected: true,
					extensions: null,
				},
			],
			dataList: [],
			shortcutList: [],
			dragHoverPath: null,
			hoverTimer: null,
		}
	},
	async created() {
		// Get the shortcut detail for the first time and save it to store
		try {
			await this.$store.dispatch('GET_SHORTCUT_DATA')
		} catch (e) {
			console.log(e)
		}
		if (!this.picker) await this.ensureDesktopFolder()
		this.getNewList()
	},

	mounted() {
		this.$EventBus.$on(events.RELOAD_FILE_LIST, this.getNewList)

		this.shortcutList = this.$store.state.shortcutData || []
		const initPaths = new Set(this.initFolders.map((f) => f.path))
		const uniqueShortcuts = this.shortcutList.filter((s) => !initPaths.has(s.path))
		this.dataList = [...this.initFolders, ...uniqueShortcuts]
	},

	beforeDestroy() {
		// Unlike the legacy singleton sidebar, this component is created/destroyed each time
		// the sidebar collapses into (and back out of) icon-rail mode, so the listener must be removed.
		this.$EventBus.$off(events.RELOAD_FILE_LIST, this.getNewList)
		clearTimeout(this.hoverTimer)
	},

	methods: {
		async getNewList() {
			const newList = await this.$api.folder.getList(this.rootDataList[0].path)
			const dataList = await this.$api.folder.getList(this.initFolders[0].path)

			this.shortcutList = this.$store.state.shortcutData || []
			const initPaths = new Set(this.initFolders.map((f) => f.path))
			const uniqueShortcuts = this.shortcutList.filter((s) => !initPaths.has(s.path))

			this.dataList = [...this.initFolders, ...uniqueShortcuts]
			let contactList = []
			contactList.push(...(newList.data?.data?.content || []), ...(dataList.data?.data?.content || []), ...uniqueShortcuts)
			this.dataList.forEach((dir) => {
				dir.icon = dir.icon == 'folder' ? 'folder-outline' : dir.icon
				dir.visible = contactList.some((item) => item.path == dir.path && item.is_dir)
				const isInArray = contactList.find((item) => item.path == dir.path && item.is_dir)
				dir.extensions = isInArray ? isInArray.extensions : null
			})
		},

		// Creates /DATA/Desktop once if it doesn't already exist, so the
		// sidebar's "Desktop" entry (initFolders above) - and the desktop
		// background's own drop target (WindowManager.vue) - always have a
		// real folder to point at, without requiring the user to have
		// dragged something onto the desktop first.
		async ensureDesktopFolder() {
			try {
				const res = await this.$api.folder.getList('/DATA')
				const exists = (res.data.data.content || []).some((item) => item.path === '/DATA/Desktop' && item.is_dir)
				if (!exists) await this.$api.folder.create('/DATA/Desktop')
			} catch (e) {
				// Best-effort - if this fails, Desktop just won't show until
				// the folder exists some other way, same as any other
				// initFolders entry pointing at a genuinely-missing folder.
			}
		},

		checkSharevisibility(item) {
			const extensions = item.extensions
			if (extensions === null) {
				return false
			} else {
				if (has(extensions, 'share')) {
					return extensions.share.shared === 'true'
				} else {
					return false
				}
			}
		},

		// Mirrors legacy TreeListItem's `isActived` computed, reading the injected
		// filesController.currentPath instead of the page-level isActive prop / $store.state.currentPath.
		isItemActive(item) {
			if (this.picker) {
				return item.path === this.pickedPath
			}
			const currentPath = this.filesController.currentPath
			if (item.path === currentPath) {
				return true
			}
			if (item.path !== '/' && item.path !== '/DATA') {
				return currentPath.indexOf(`${item.path}/`) !== -1
			}
			return false
		},

		// Root is never selectable in picker mode (see template comment above).
		openRoot(path) {
			if (this.picker) return
			this.filesController.navigate(path)
		},

		open(path) {
			if (this.picker) {
				this.pickedPath = path
				this.$emit('pick', path)
				return
			}
			this.filesController.navigate(path)
		},

		onDragOver(item, event) {
			if (this.picker || !isFilesDragEvent(event)) return
			event.preventDefault()
			if (this.dragHoverPath === item.path) return
			this.dragHoverPath = item.path
			clearTimeout(this.hoverTimer)
			this.hoverTimer = setTimeout(() => {
				this.filesController.navigate(item.path)
			}, HOVER_OPEN_DELAY)
		},
		onDragLeave(item) {
			if (this.dragHoverPath !== item.path) return
			this.dragHoverPath = null
			clearTimeout(this.hoverTimer)
		},
		// A drop directly on a sidebar location pastes there immediately,
		// without waiting for the hover-navigate timer - the timer is only
		// for "keep dragging further in", not required just to drop here.
		onDrop(item, event) {
			this.dragHoverPath = null
			clearTimeout(this.hoverTimer)
			if (this.picker || !isFilesDragEvent(event)) return
			event.preventDefault()
			event.stopPropagation()
			const payload = getFilesDragData(event)
			if (!payload) return
			if (payload.from === item.path || payload.items.includes(item.path)) return
			this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: item.path })
		},

		// Only user-added shortcuts (from shortcutData) can be unpinned - the
		// built-in Root/DATA/Documents/Downloads/Gallery/Media entries live in
		// `initFolders`, not `shortcutList`, so they never match here.
		isShortcut(item) {
			return this.shortcutList.some((s) => s.path === item.path)
		},
		onContextMenu(item, event) {
			if (this.picker) return
			if (this.$refs.contextMenu) {
				this.$refs.contextMenu.open(event, { ...item, is_dir: true })
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.folder-tree {
	padding: 0.25rem 0.5rem;
	// In rail mode, .sidebar-body (the parent) already provides 0.5rem of
	// horizontal inset - this element's OWN horizontal padding was stacking
	// on top of that, unaccounted for, leaving each .tree-node.rail box far
	// less room than its width assumed (it was actually wider than the
	// space left for it, not centered with a gap at all).
	&.rail {
		padding: 0.25rem 0;
	}
}
.tree-node {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.35rem 0.5rem;
	border-radius: 6px;
	cursor: pointer;

	&:hover {
		background: rgba(0, 0, 0, 0.05);
	}
	&.active {
		background: rgba(50, 115, 220, 0.14);
		color: #3273dc;
		font-weight: 600;
	}
	&.drop-target {
		background: rgba(50, 115, 220, 0.25);
		outline: 2px solid #3273dc;
		outline-offset: -2px;
	}
	&.rail {
		justify-content: center;
		width: 2.25rem;
		height: 2.25rem;
		padding: 0;
		margin: 0 auto 0.4rem;
	}
	&.disabled {
		opacity: 0.35;
		pointer-events: none;
		cursor: default;
	}
}
.tree-node-icon {
	flex-shrink: 0;
	display: flex;
	align-items: center;
}
.tree-node-label {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-size: 0.85rem;
}
.share-badge {
	position: absolute;
	right: -0.15rem;
	bottom: -0.1rem;
}
</style>
