<template>
	<div ref="root" class="files-app">
		<files-tab-bar
			:tabs="tabs"
			:active-tab-id="activeTabId"
			@switch="switchTab"
			@close="closeTab"
			@new-tab="newTab"
			@new-window="openNewWindow"
			@drag-start="$emit('drag-start', $event)"
			@minimize-window="$emit('minimize')"
			@close-window="$emit('close')"
		></files-tab-bar>
		<files-toolbar
			:selection-summary="activeContentView && activeContentView.summary"
			:selected-items="selectedItems()"
			@new-folder="onNewFolder"
			@new-file="onNewFile"
			@upload="onUpload"
			@set-view="onSetView"
			@paste="onPaste"
			@clear-selection="onClearSelection"
			@copy-selection="onCopySelection"
			@move-selection="onMoveSelection"
			@download-selection="onDownloadSelection"
			@delete-selection="onDeleteSelection"
			@rename-selection="onRenameSelection"
			@open-selection-window="onOpenSelectionWindow"
			@compress-selection="onCompressSelection"
			@extract-selection="onExtractSelection"
		></files-toolbar>
		<div class="files-body">
			<files-sidebar>
				<div class="sidebar-section-label">{{ $t('Favorites') }}</div>
				<folder-tree></folder-tree>
				<div class="sidebar-section-label">{{ $t('Locations') }}</div>
				<mount-list></mount-list>
				<template #rail>
					<folder-tree rail></folder-tree>
				</template>
			</files-sidebar>
			<files-content-view
				v-for="tab in tabs"
				:key="tab.id"
				ref="contentView"
				v-show="controller.activeSection === 'browser' && tab.id === activeTabId"
				:path="tab.id === activeTabId ? controller.currentPath : tab.path"
				@open-file="onOpenFile"
				@new-folder="onNewFolder"
				@new-file="onNewFile"
				@rename-request="onRenameRequest"
				@detail-request="onDetailRequest"
				@delete-request="onDeleteRequest"
				@open-new-tab-request="onOpenInNewTab"
				@copy-selection="onCopySelection"
				@move-selection="onMoveSelection"
				@download-selection="onDownloadSelection"
				@delete-selection="onDeleteSelection"
				@compress-selection="onCompressSelection"
				@compress-request="onCompressRequest"
				@extract-request="onExtractRequest"
			></files-content-view>
			<files-shared-view ref="sharedView" v-show="controller.activeSection === 'shared'" @add-share="activeDialog = 'share-select'"></files-shared-view>
			<files-drop-view v-show="controller.activeSection === 'drop'"></files-drop-view>
			<operation-tray></operation-tray>
			<slot></slot>
		</div>
		<new-folder-dialog v-if="activeDialog === 'new-folder'" :current-path="controller.currentPath" @created="onDialogCreated" @close="activeDialog = null"></new-folder-dialog>
		<new-file-dialog v-if="activeDialog === 'new-file'" :current-path="controller.currentPath" @created="onDialogCreated" @close="activeDialog = null"></new-file-dialog>
		<rename-dialog v-if="activeDialog === 'rename'" :item="dialogItem" @renamed="onDialogCreated" @close="activeDialog = null"></rename-dialog>
		<detail-dialog v-if="activeDialog === 'detail'" :item="dialogItem" @close="activeDialog = null"></detail-dialog>
		<compress-dialog v-if="activeDialog === 'compress'" :current-path="controller.currentPath" :items="dialogItem" @created="onDialogCreated" @close="activeDialog = null"></compress-dialog>
		<extract-dialog v-if="activeDialog === 'extract'" :current-path="controller.currentPath" :item="dialogItem" @created="onDialogCreated" @close="activeDialog = null"></extract-dialog>
		<share-select-dialog v-if="activeDialog === 'share-select'" @created="onShareCreated" @close="activeDialog = null"></share-select-dialog>
		<confirm-dialog
			v-if="activeDialog === 'confirm-delete'"
			:title="$t('Deleting files')"
			:message="$t('Are you sure you want to <b>delete</b> these files? This action cannot be undone.')"
			:confirm-text="$t('Delete')"
			@confirm="performDelete"
			@cancel="activeDialog = null"
		></confirm-dialog>
	</div>
</template>

<script>
import { classifyWidth } from '@/utils/files/breakpoints'
import { mixin } from '@/mixins/mixin'
import FilesToolbar from './Toolbar.vue'
import FilesTabBar from './TabBar.vue'
import FilesSidebar from './Sidebar.vue'
import FolderTree from './FolderTree.vue'
import MountList from './MountList.vue'
import FilesContentView from './ContentView.vue'
import FilesSharedView from './SharedView.vue'
import FilesDropView from './DropView.vue'
import OperationTray from './OperationTray.vue'
import NewFolderDialog from './dialogs/NewFolderDialog.vue'
import NewFileDialog from './dialogs/NewFileDialog.vue'
import RenameDialog from './dialogs/RenameDialog.vue'
import DetailDialog from './dialogs/DetailDialog.vue'
import ShareSelectDialog from './dialogs/ShareSelectDialog.vue'
import ConfirmDialog from './dialogs/ConfirmDialog.vue'
import CompressDialog from './dialogs/CompressDialog.vue'
import ExtractDialog from './dialogs/ExtractDialog.vue'

// getPanelType() (from the mixin) returns one of these keys; each maps to a
// component registered in DesktopWindow.vue's COMPONENT_REGISTRY, plus a
// reasonable default window size for that kind of content.
const VIEWER_WINDOW_CONFIG = {
	'image-viewer': { component: 'ImageViewer', width: 900, height: 640 },
	'video-player': { component: 'VideoPlayer', width: 900, height: 600 },
	'code-editor': { component: 'CodeEditor', width: 900, height: 650 },
	'doc-viewer': { component: 'DocViewer', width: 850, height: 650 },
	'excel-viewer': { component: 'ExcelViewer', width: 850, height: 650 },
	'pdf-viewer': { component: 'PdfViewer', width: 800, height: 680 },
}

export default {
	name: 'files-app',
	// Added per task-15 (verified NOT already present on this component,
	// despite the task-15 brief's claim to the contrary): FilesApp.vue is
	// about to call this.operate()/this.downloadFile()/this.deleteItem()
	// directly below, and Task 18/19 add this.getPanelType()/downloadFile()
	// calls on it too - none of those exist here without the mixin.
	mixins: [mixin],
	components: {
		FilesToolbar,
		FilesTabBar,
		FilesSidebar,
		FolderTree,
		MountList,
		FilesContentView,
		FilesSharedView,
		FilesDropView,
		OperationTray,
		NewFolderDialog,
		NewFileDialog,
		RenameDialog,
		DetailDialog,
		ShareSelectDialog,
		ConfirmDialog,
		CompressDialog,
		ExtractDialog,
	},
	provide() {
		return {
			filesController: this.controller,
		}
	},
	data() {
		const initialPath = this.$store.state.currentPath || '/DATA'
		return {
			controller: {
				currentPath: initialPath,
				breakpoints: classifyWidth(960),
				sidebarCollapsed: localStorage.getItem('files-sidebar-collapsed') === 'true',
				activeSection: 'browser',
				navigate: this.navigate,
				setActiveSection: this.setActiveSection,
				toggleSidebar: this.toggleSidebar,
				openNewFolder: this.onNewFolder,
				openNewFile: this.onNewFile,
				openUpload: this.onUpload,
				openNewWindow: this.openNewWindow,
			},
			resizeObserver: null,
			activeDialog: null,
			dialogItem: null,
			// One entry per open tab, each independently tracking its own
			// folder. controller.currentPath always mirrors whichever tab is
			// active (see navigate()/switchTab()) - Toolbar/Sidebar/FolderTree
			// keep reading/calling filesController.currentPath/navigate()
			// exactly as before, unaware tabs exist at all.
			tabs: [{ id: 1, path: initialPath }],
			activeTabId: 1,
			nextTabId: 2,
			// $refs aren't populated yet during this component's very first
			// render (refs are only set once the initial DOM patch completes),
			// so activeContentView below resolves to null on that first pass
			// and Vue caches it - with no other reactive dependency of this
			// component's render ever changing afterward, that null could
			// stick forever, permanently freezing Toolbar's selection-summary
			// prop. Bumping this in mounted() (once refs actually exist) gives
			// activeContentView a reactive dependency to invalidate on.
			refsReady: 0,
		}
	},
	mounted() {
		this.resizeObserver = new ResizeObserver((entries) => {
			const width = entries[0].contentRect.width
			this.controller.breakpoints = classifyWidth(width)
		})
		this.resizeObserver.observe(this.$refs.root)
		if (!this.$store.state.currentPath) {
			this.navigate('/DATA')
		}
		this.refsReady++
	},
	beforeDestroy() {
		this.resizeObserver && this.resizeObserver.disconnect()
	},
	computed: {
		// Resolves the ContentView instance for whichever tab is currently
		// active. `ref="contentView"` is repeated across the v-for in the
		// template, so Vue collects all instances into one array in source
		// order - matched here by index against `tabs` (same source array,
		// same order) rather than needing each ContentView to know its own
		// tab id.
		activeContentView() {
			// eslint-disable-next-line no-unused-expressions
			this.refsReady // reactive dependency only - see data()'s comment
			const index = this.tabs.findIndex((t) => t.id === this.activeTabId)
			const refs = this.$refs.contentView
			if (!refs || index === -1) return null
			return Array.isArray(refs) ? refs[index] : refs
		},
	},
	methods: {
		navigate(path) {
			this.controller.currentPath = path
			this.$store.commit('SET_CURRENT_PATH', path)
			// Navigating to a folder always means "go back to browsing" -
			// without this, there was no way out of the Shared/Drop sections
			// (their sidebar nav entries only ever set activeSection forward,
			// never back) once you clicked into them.
			this.controller.activeSection = 'browser'
			const activeTab = this.tabs.find((t) => t.id === this.activeTabId)
			if (activeTab) activeTab.path = path
		},
		newTab(path = '/DATA') {
			const id = this.nextTabId++
			this.tabs.push({ id, path })
			this.activeTabId = id
			this.controller.currentPath = path
			this.$store.commit('SET_CURRENT_PATH', path)
			this.controller.activeSection = 'browser'
		},
		onOpenInNewTab(item) {
			this.newTab(item.path)
		},
		closeTab(tabId) {
			if (this.tabs.length <= 1) return
			const index = this.tabs.findIndex((t) => t.id === tabId)
			if (index === -1) return
			this.tabs.splice(index, 1)
			if (this.activeTabId === tabId) {
				const next = this.tabs[Math.max(0, index - 1)]
				this.switchTab(next.id)
			}
		},
		switchTab(tabId) {
			const target = this.tabs.find((t) => t.id === tabId)
			if (!target) return
			this.activeTabId = tabId
			this.controller.currentPath = target.path
			this.$store.commit('SET_CURRENT_PATH', target.path)
			this.controller.activeSection = 'browser'
		},
		openNewWindow(path) {
			// A fresh FilesApp instance seeds its initial tab from
			// $store.state.currentPath (see data() above) - setting it here
			// first is how a folder-specific "Open in New Window" launches
			// already navigated there, instead of always opening at /DATA.
			if (path) this.$store.commit('SET_CURRENT_PATH', path)
			this.$store.commit('OPEN_WINDOW', {
				id: 'files-' + Date.now(),
				title: this.$t('Files'),
				component: 'FilesApp',
				width: 960,
				height: 620,
			})
		},
		setActiveSection(section) {
			this.controller.activeSection = section
		},
		toggleSidebar() {
			this.controller.sidebarCollapsed = !this.controller.sidebarCollapsed
			localStorage.setItem('files-sidebar-collapsed', String(this.controller.sidebarCollapsed))
		},
		onNewFolder() {
			this.activeDialog = 'new-folder'
		},
		onNewFile() {
			this.activeDialog = 'new-file'
		},
		onDialogCreated() {
			this.activeContentView && this.activeContentView.reload()
			this.activeDialog = null
		},
		onShareCreated() {
			this.activeDialog = null
			this.$refs.sharedView && this.$refs.sharedView.getSharedList()
		},
		onRenameRequest(item) {
			this.dialogItem = item
			this.activeDialog = 'rename'
		},
		onDetailRequest(item) {
			this.dialogItem = item
			this.activeDialog = 'detail'
		},
		onUpload() {
			this.activeContentView && this.activeContentView.triggerUpload()
		},
		onSetView(mode) {
			this.$store.commit('SET_VIEW_MODE', mode)
		},
		onPaste() {
			this.activeContentView && this.activeContentView.paste()
		},
		// getPanelType (from the mixin) preserves an existing legacy quirk
		// deliberately: .md files aren't in filePanelMap, so they fall
		// through to the Detail dialog rather than opening
		// MarkdownEditor - not "fixed" here, matches current production
		// behavior exactly. Viewers open as their own desktop window
		// (registered in DesktopWindow.vue's COMPONENT_REGISTRY), matching
		// how double-clicking a file opens a separate app window on a real
		// desktop, rather than replacing the Files window's own content.
		onOpenFile(item) {
			const type = this.getPanelType(item)
			const config = VIEWER_WINDOW_CONFIG[type]
			if (!config) {
				this.dialogItem = item
				this.activeDialog = 'detail'
				return
			}
			const props = { item }
			if (type === 'image-viewer') {
				props.list = (this.activeContentView && this.activeContentView.listing) || []
			}
			this.$store.commit('OPEN_WINDOW', {
				id: 'viewer-' + Date.now(),
				title: item.name,
				component: config.component,
				width: config.width,
				height: config.height,
				props,
			})
		},
		onClearSelection() {
			this.activeContentView && this.activeContentView.clearSelection()
		},
		// Maps ContentView's `selection` (an array of paths) back to the full
		// item objects from its `listing` - `operate`/`downloadFile`/
		// `deleteItem` (all from src/mixins/mixin.js, mixed in above) expect
		// item objects (each with a `.path`), not bare path strings.
		selectedItems() {
			const contentView = this.activeContentView
			if (!contentView) return []
			return contentView.listing.filter((item) => contentView.selection.includes(item.path))
		},
		onCopySelection() {
			this.operate('copy', this.selectedItems())
		},
		onMoveSelection() {
			// 'move' (not 'cut') is the literal type string $api.batch.task's
			// operateObject.type must carry for the backend/paste flow to
			// recognize it (see src/mixins/ListViewMixin.js:279's
			// `operateObject.type == "move"` check, and ContextMenu.vue's
			// already-corrected 'cut' menu item, which calls
			// `this.operate('move', this.item)` for the same reason). The
			// task-15 brief's own step 4 text says
			// `this.operate('copy'|'cut', selectedItems)` here, but 'cut' would
			// be wrong - corrected to 'move' to match that established fix.
			this.operate('move', this.selectedItems())
		},
		onDownloadSelection() {
			this.downloadFile(this.selectedItems())
		},
		onDeleteSelection() {
			const items = this.selectedItems()
			if (!items.length) return
			this.dialogItem = items
			this.activeDialog = 'confirm-delete'
		},
		// Both only ever shown by Toolbar.vue when exactly one item is
		// selected (its own singleItem computed), so selectedItems()[0] is
		// always the right target here.
		onRenameSelection() {
			const items = this.selectedItems()
			if (items.length === 1) this.onRenameRequest(items[0])
		},
		// A folder opens into a new Files window browsing it; a file has no
		// listing to browse into, so it opens in its own viewer window
		// instead (same as double-click's onOpenFile) - just forced into a
		// new window rather than reusing one already open for that viewer.
		onOpenSelectionWindow() {
			const items = this.selectedItems()
			if (items.length !== 1) return
			if (items[0].is_dir) {
				this.openNewWindow(items[0].path)
			} else {
				this.onOpenFile(items[0])
			}
		},
		// CompressDialog takes the full array (a batch bundles into one zip);
		// ExtractDialog takes a single item - both from either the toolbar
		// (current selection) or the right-click menu (that one item).
		onCompressSelection() {
			const items = this.selectedItems()
			if (items.length) this.onCompressRequest(items)
		},
		onExtractSelection() {
			const items = this.selectedItems()
			if (items.length === 1) this.onExtractRequest(items[0])
		},
		onCompressRequest(itemOrItems) {
			this.dialogItem = Array.isArray(itemOrItems) ? itemOrItems : [itemOrItems]
			this.activeDialog = 'compress'
		},
		onExtractRequest(item) {
			this.dialogItem = item
			this.activeDialog = 'extract'
		},
		// Single-item delete (from ContextMenu's right-click "Delete") and the
		// toolbar's batch delete above both route through this same in-window
		// confirm dialog now, instead of each calling Buefy's global
		// $buefy.dialog.confirm() directly - that API renders a viewport-wide
		// overlay over the whole desktop, not confined to the Files window,
		// which is exactly the "confined to the window" problem every other
		// dialog in this app (New Folder/File, Rename, Detail, Share) already
		// solved via DialogOverlay. deleteItem() (the mixin method) already
		// handles both a single item Object and an Array the same way.
		onDeleteRequest(item) {
			this.dialogItem = item
			this.activeDialog = 'confirm-delete'
		},
		performDelete() {
			this.deleteItem(this.dialogItem)
			this.activeContentView && this.activeContentView.reload()
			this.activeContentView && this.activeContentView.clearSelection()
			this.activeDialog = null
			this.dialogItem = null
		},
	},
}
</script>

<style lang="scss" scoped>
.files-app {
	display: flex;
	flex-direction: column;
	height: 100%;
	width: 100%;
	overflow: hidden;
	position: relative;
	font-family: $family-sans-serif;
}
.files-body {
	flex: 1 1 auto;
	display: flex;
	min-height: 0;
	position: relative;
}
.sidebar-section-label {
	font-size: 0.65rem;
	font-weight: 700;
	letter-spacing: 0.05em;
	text-transform: uppercase;
	color: rgba(0, 0, 0, 0.35);
	padding: 0.6rem 0.5rem 0.25rem;
	&:first-child {
		padding-top: 0.25rem;
	}
}
</style>
