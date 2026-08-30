<!-- src/components/files/ContentView.vue -->
<template>
	<section
		ref="root"
		class="content-view"
		:class="{ 'is-drag-over': isDragOver }"
		tabindex="-1"
		v-show="filesController.activeSection === 'browser'"
		@dragover.prevent="onDragOver"
		@dragleave.prevent="onDragLeave"
		@drop.prevent="onDrop"
		@paste="paste"
		@keydown="onKeyDown"
		@mousedown.capture="focusRoot"
	>
		<b-loading v-model="loading" :is-full-page="false"></b-loading>
		<error-holder v-if="error" :error="error"></error-holder>
		<empty-folder v-else-if="!loading && listing.length === 0"></empty-folder>
		<div
			v-else
			ref="itemsEl"
			class="items"
			:class="[viewMode, { 'single-column': filesController.breakpoints.singleColumnGrid }]"
			@mousedown.left.prevent="onDragSelectionStart"
		>
			<template v-if="viewMode === 'grid' || viewMode === 'grid-large'">
				<grid-item
					v-for="item in listing"
					:key="item.path"
					ref="itemEl"
					:item="item"
					:large="viewMode === 'grid-large'"
					:single-column="filesController.breakpoints.singleColumnGrid"
					:selected="selection.includes(item.path)"
					@open="openItem"
					@select="onItemClick(item, $event)"
					@contextmenu="openContextMenu(item, $event)"
					@dragstart="onItemDragStart"
					@drop-item="onDropOnItem"
				></grid-item>
			</template>
			<template v-else>
				<list-row
					v-for="item in listing"
					:key="item.path"
					ref="itemEl"
					:item="item"
					:selected="selection.includes(item.path)"
					@open="openItem"
					@select="onItemClick(item, $event)"
					@contextmenu="openContextMenu(item, $event)"
					@dragstart="onItemDragStart"
					@drop-item="onDropOnItem"
				></list-row>
			</template>
			<div v-if="dragBox" class="drag-select-box" :style="dragBoxStyle"></div>
		</div>
		<files-context-menu
			ref="ctxMenu"
			@reload="reload"
			@rename-request="$emit('rename-request', $event)"
			@detail-request="$emit('detail-request', $event)"
			@delete-request="$emit('delete-request', $event)"
			@open-request="openItem"
			@open-new-tab-request="$emit('open-new-tab-request', $event)"
			@compress-request="$emit('compress-request', $event)"
			@extract-request="$emit('extract-request', $event)"
		></files-context-menu>
		<upload-tray ref="uploadTray" :current-path="path" @uploaded="reload"></upload-tray>
	</section>
</template>

<script>
import orderBy from 'lodash/orderBy'
import EmptyFolder from './EmptyFolder.vue'
import ErrorHolder from './ErrorHolder.vue'
import GridItem from './GridItem.vue'
import ListRow from './ListRow.vue'
import FilesContextMenu from './ContextMenu.vue'
import UploadTray from './UploadTray.vue'
import { toggleSelect, selectRange, summarize } from '@/utils/files/selection'
import { isFilesDragEvent, getFilesDragData, setFilesDragData } from '@/utils/files/dragDrop'

// Minimum drag distance (px) before a mousedown+move is treated as a
// selection-rectangle drag rather than a plain click on empty space.
const DRAG_THRESHOLD = 3

export default {
	name: 'files-content-view',
	inject: ['filesController'],
	components: { EmptyFolder, ErrorHolder, GridItem, ListRow, FilesContextMenu, UploadTray },
	props: {
		// The path THIS instance shows - one per open tab (see FilesApp.vue).
		// Deliberately a prop rather than reading filesController.currentPath
		// directly: with multiple tabs, each tab needs its own ContentView
		// instance showing its own folder independently, while
		// filesController.currentPath/navigate() remain the single shared
		// "whichever tab is active" concept the toolbar/sidebar/breadcrumb
		// already key off unchanged.
		path: { type: String, required: true },
	},
	data() {
		return {
			listing: [],
			loading: true,
			error: '',
			selection: [],
			lastClickedPath: null,
			dragOrigin: null,
			dragBaseSelection: [],
			dragBox: null,
			// Maps a real mounted disk's path (e.g. "/DATA/tower") to its disk
			// type ("usb"/"sata"/"nvme"/...), so items in the listing that are
			// actually separate mounted drives can get the distinct
			// folder-hdd/folder-usb icon getIconFile() (mixins/mixin.js) already
			// knows how to render - the folder-listing API's own `item.type`
			// field is an unrelated raw filesystem code (always 0 for
			// directories), not the disk-type string this mixin expects, so
			// that data has to come from the separate storage API instead
			// (the same one MountList.vue's sidebar entries already use).
			mountTypes: {},
			isDragOver: false,
		}
	},
	computed: {
		viewMode() {
			return this.$store.state.viewMode
		},
		summary() {
			return summarize(this.listing, this.selection)
		},
		dragBoxStyle() {
			if (!this.dragBox) return {}
			return {
				left: this.dragBox.left + 'px',
				top: this.dragBox.top + 'px',
				width: this.dragBox.width + 'px',
				height: this.dragBox.height + 'px',
			}
		},
	},
	watch: {
		path: {
			immediate: true,
			handler(path) {
				this.clearSelection()
				this.fetchListing(path)
			},
		},
	},
	mounted() {
		// Focused on mount (and refocused on click via @mousedown.capture below)
		// so the scoped `@paste` listener on this component's root actually
		// receives native paste events - a plain, non-input element only gets
		// `paste` events while it (or a descendant) holds focus. Deliberately
		// click-driven only, not hover-driven: this desktop shell's other
		// windows (src/components/desktop/DesktopWindow.vue) only ever take
		// focus on mousedown, never on mouseenter - a hover-triggered
		// `focusRoot()` here would silently steal keyboard focus away from
		// whatever window the user is actually typing in whenever their mouse
		// merely crosses this pane (e.g. dragging across it to reach another
		// window), which was a real regression caught in review.
		this.focusRoot()
		this.fetchMountTypes()
	},
	beforeDestroy() {
		window.removeEventListener('mousemove', this.onDragSelectionMove)
		window.removeEventListener('mouseup', this.onDragSelectionEnd)
	},
	// PostOperateFileOrDir (the /v1/batch/task backend handler a copy/move/
	// paste/drag-drop ultimately calls) only enqueues the job and returns
	// success immediately - the actual file operation runs in a background
	// goroutine. This is the real completion signal (the same message-bus
	// event the legacy OperationStatusBar.vue already listens to), and
	// every open ContentView showing the destination folder reacts to it
	// independently - reloading right after the initial HTTP response (as
	// paste() used to) meant the listing didn't yet reflect what had
	// actually landed on disk.
	sockets: {
		'casaos:file:operate'(res) {
			let fileOperate
			try {
				fileOperate = JSON.parse(res.Properties.file_operate)
			} catch (e) {
				return
			}
			// The finished-task payload only ever carries `to` (the
			// destination), never the source item paths - fine for a copy,
			// but a move also empties out the SOURCE folder, which this
			// ContentView could be showing with no way to know that from
			// `to` alone. Reloading on any finished task, regardless of
			// path, is the only way to reliably catch that case too - file
			// operations aren't frequent enough for the extra refreshes
			// elsewhere to matter.
			const anyFinished = (fileOperate.data || []).some((task) => task.finished)
			// eslint-disable-next-line no-console
			console.log('[DEBUG socket file:operate]', { path: this.path, anyFinished, data: fileOperate.data })
			if (anyFinished) this.reload()
		},
	},
	methods: {
		focusRoot() {
			this.$refs.root && this.$refs.root.focus()
		},
		fetchListing(path) {
			this.loading = true
			// eslint-disable-next-line no-console
			console.log('[DEBUG fetchListing] requesting', path)
			this.$api.folder
				.getList(path)
				.then((res) => {
					this.loading = false
					if (res.data.success === 200) {
						const mapped = res.data.data.content.map((item) => ({
							date: item.date,
							is_dir: item.is_dir,
							name: item.name,
							path: item.path,
							size: item.size,
							write: item.write,
							extensions: item.extensions,
							// Present from the start (even as undefined) so a later
							// mountTypes patch in fetchMountTypes() reassigns an
							// existing reactive property rather than adding a new
							// one - Vue 2 can't track newly-added properties.
							type: this.mountTypes[item.path],
						}))
						const visible = mapped.filter((item) => !item.name.startsWith('.'))
						this.listing = orderBy(visible, ['is_dir'], ['desc'])
						this.error = ''
						// eslint-disable-next-line no-console
						console.log('[DEBUG fetchListing] got', path, 'names:', this.listing.map((i) => i.name))
					}
				})
				.catch((err) => {
					this.loading = false
					this.listing = []
					this.error = err.response ? err.response.data.data : String(err)
				})
		},
		reload() {
			// eslint-disable-next-line no-console
			console.log('[DEBUG reload] called, this.path =', this.path)
			this.fetchListing(this.path)
		},
		// Best-effort: this needs an authenticated call, so a failure here
		// (or simply no real disk mounts existing) just means items fall
		// back to their normal name/extension-based icon - never a crash.
		async fetchMountTypes() {
			try {
				const res = await this.$api.storage.list()
				const map = {}
				;(res.data.data || []).forEach((disk) => {
					;(disk.children || []).forEach((part) => {
						map[part.mount_point] = disk.type
					})
				})
				this.mountTypes = map
				// Patch already-loaded items in place (the `type` key already
				// exists on each from fetchListing()'s map, so this reassignment
				// is reactive - see the comment there).
				this.listing.forEach((item) => {
					if (map[item.path] !== undefined) item.type = map[item.path]
				})
			} catch (e) {
				// No storage access in this session (or none configured) - fine.
			}
		},
		openItem(item) {
			if (item.is_dir) {
				this.filesController.navigate(item.path)
			} else {
				this.$emit('open-file', item)
			}
		},
		// `this.$el` here is ContentView's own root (`section.content-view`) -
		// the scrollable clipping container the menu must stay inside, per the
		// task-12 brief's positioning fix. Wired via an emitted 'contextmenu'
		// event from GridItem/ListRow (same pattern as their existing
		// 'select'/'open' emits) rather than those children reaching directly
		// into `$refs.ctxMenu`, since that ref only exists on ContentView's own
		// instance, not on the item components.
		openContextMenu(item, event) {
			this.$refs.ctxMenu.open(event, item, this.$el)
		},
		onItemClick(item, event) {
			if (event.shiftKey && this.lastClickedPath) {
				this.selection = selectRange(this.listing, this.lastClickedPath, item.path)
			} else if (event.ctrlKey || event.metaKey) {
				this.selection = toggleSelect(this.selection, item.path)
			} else {
				this.selection = [item.path]
			}
			this.lastClickedPath = item.path
		},
		selectAll() {
			this.selection = this.listing.map((item) => item.path)
		},
		// Copy/Cut/Select All/Delete previously had no keyboard shortcuts at
		// all - the toolbar buttons and right-click menu were the only way
		// in, so pressing Ctrl+C then Ctrl+V (the native `paste` handler
		// above, which already worked) did nothing, reading as "copy and
		// paste isn't working." Re-emits the exact same events the toolbar
		// itself emits (copy-selection/move-selection/delete-selection),
		// so FilesApp's existing handlers for those need no changes.
		onKeyDown(event) {
			const meta = event.ctrlKey || event.metaKey
			const key = event.key.toLowerCase()
			if (meta && key === 'a') {
				event.preventDefault()
				this.selectAll()
				return
			}
			if (!this.selection.length) return
			if (meta && key === 'c') {
				event.preventDefault()
				this.$emit('copy-selection')
			} else if (meta && key === 'x') {
				event.preventDefault()
				this.$emit('move-selection')
			} else if (event.key === 'Delete' || event.key === 'Backspace') {
				event.preventDefault()
				this.$emit('delete-selection')
			}
		},
		clearSelection() {
			this.selection = []
			this.lastClickedPath = null
		},
		// Drag-select rectangle. Ported (conceptually) from the legacy
		// `onDragSelectionStart`/mousemove/mouseup trio in
		// src/mixins/ListViewMixin.js (used by the old GirdView.vue) - see the
		// discrepancy note in the task-11 report re: the legacy implementation
		// actually delegating its rectangle-intersection to the `hitbox-js`
		// library rather than containing that math inline. This version does
		// the intersection math directly against each rendered item's
		// getBoundingClientRect(), and builds `this.selection` as a plain
		// array of paths instead of mutating `item.isSelected` flags.
		onDragSelectionStart(event) {
			const containerEl = this.$refs.itemsEl
			if (!containerEl) return
			const rect = containerEl.getBoundingClientRect()
			this.dragOrigin = { x: event.clientX - rect.left, y: event.clientY - rect.top }
			this.dragBaseSelection = event.ctrlKey || event.metaKey ? this.selection.slice() : []
			this.dragBox = { left: this.dragOrigin.x, top: this.dragOrigin.y, width: 0, height: 0 }
			window.addEventListener('mousemove', this.onDragSelectionMove)
			window.addEventListener('mouseup', this.onDragSelectionEnd)
		},
		onDragSelectionMove(event) {
			const containerEl = this.$refs.itemsEl
			if (!containerEl || !this.dragOrigin) return
			const rect = containerEl.getBoundingClientRect()
			const x = event.clientX - rect.left
			const y = event.clientY - rect.top
			this.dragBox = {
				left: Math.min(this.dragOrigin.x, x),
				top: Math.min(this.dragOrigin.y, y),
				width: Math.abs(x - this.dragOrigin.x),
				height: Math.abs(y - this.dragOrigin.y),
			}
			this.updateDragSelection()
		},
		updateDragSelection() {
			const box = this.dragBox
			const containerEl = this.$refs.itemsEl
			if (!box || !containerEl || (box.width < DRAG_THRESHOLD && box.height < DRAG_THRESHOLD)) return
			const rect = containerEl.getBoundingClientRect()
			const boxRect = {
				left: rect.left + box.left,
				top: rect.top + box.top,
				right: rect.left + box.left + box.width,
				bottom: rect.top + box.top + box.height,
			}
			const itemEls = this.$refs.itemEl || []
			const hits = itemEls
				.filter((vm) => {
					const r = vm.$el.getBoundingClientRect()
					return !(r.right < boxRect.left || r.left > boxRect.right || r.bottom < boxRect.top || r.top > boxRect.bottom)
				})
				.map((vm) => vm.item.path)
			this.selection = Array.from(new Set([...this.dragBaseSelection, ...hits]))
		},
		onDragSelectionEnd() {
			window.removeEventListener('mousemove', this.onDragSelectionMove)
			window.removeEventListener('mouseup', this.onDragSelectionEnd)
			const wasDragging = this.dragBox && (this.dragBox.width >= DRAG_THRESHOLD || this.dragBox.height >= DRAG_THRESHOLD)
			if (!wasDragging) {
				// A plain mousedown+mouseup on empty space with no real drag: clear the selection.
				this.clearSelection()
			}
			this.dragOrigin = null
			this.dragBox = null
		},
		// Drag-drop upload. Per the task-15 brief, files dropped on ContentView
		// are handed straight to the UploadTray's live uploader instance
		// (`uploaderInstance.addFiles`) rather than using simple-uploader.js's
		// own `assignDrop()` DOM-binding helper - ContentView already owns the
		// dragover/drop DOM listeners here, so there's no need for a second,
		// independent set of listeners bound by the uploader itself.
		onDragOver() {
			this.isDragOver = true
		},
		onDragLeave() {
			this.isDragOver = false
		},
		onDrop(event) {
			this.isDragOver = false
			// An internal files drag landing on empty space (not on a
			// specific folder row, which stops propagation and handles
			// itself via onDropOnItem) - copy/move into the folder this
			// ContentView is currently showing. Stopped here regardless so
			// it doesn't also bubble up to the desktop background's own
			// drop handler (WindowManager.vue).
			if (isFilesDragEvent(event)) {
				event.stopPropagation()
				const payload = getFilesDragData(event)
				if (payload && payload.from !== this.path) {
					this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: this.path })
				}
				return
			}
			const files = event.dataTransfer && event.dataTransfer.files
			if (files && files.length && this.$refs.uploadTray) {
				this.$refs.uploadTray.addFiles(files)
			}
		},
		// Dragging a row: the whole current selection if the dragged item is
		// part of it (matches Explorer/Finder - dragging any selected item
		// carries the whole selection), otherwise just that one item.
		onItemDragStart(item, event) {
			const items = this.selection.includes(item.path) && this.selection.length > 1 ? this.selection.slice() : [item.path]
			setFilesDragData(event, { items, from: this.path })
		},
		// Dropped directly on a folder row within this listing.
		onDropOnItem(targetItem, event) {
			const payload = getFilesDragData(event)
			if (!payload) return
			if (payload.from === targetItem.path) return
			if (payload.items.includes(targetItem.path)) return
			this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: targetItem.path })
		},
		// Backs the toolbar's "Upload" button.
		triggerUpload() {
			this.$refs.uploadTray && this.$refs.uploadTray.browse()
		},
		// Ported from src/components/filebrowser/FilePanel.vue:694-712 (`paste`).
		// Unlike the legacy version - which binds `document.onpaste` globally
		// for the lifetime of the whole file panel - this is a native `paste`
		// DOM event scoped to ContentView's own root element (see the `tabindex`
		// + `@paste` wiring on the template's root `<section>`), so it only
		// fires while focus is within this Files window's content area.
		// No reload() on success - see the `sockets` block above for why an
		// immediate reload here used to show a stale listing.
		paste() {
			if (this.$store.state.operateObject == null) return
			const operateObject = this.$store.state.operateObject
			this.$api.batch
				.task({ ...operateObject, to: this.path, style: 'overwrite' })
				.then((res) => {
					if (res.data.success === 200) {
						this.$store.commit('SET_OPERATE_OBJECT', null)
					} else {
						this.$buefy.toast.open({
							message: res.data.message,
							type: 'is-danger',
						})
					}
				})
		},
	},
}
</script>

<style lang="scss" scoped>
.content-view {
	flex: 1 1 auto;
	min-width: 0;
	min-height: 0;
	overflow: auto;
	position: relative;
	padding: 0.75rem;
	outline: none;
	&.is-drag-over {
		outline: 2px dashed rgba(50, 115, 220, 0.6);
		outline-offset: -2px;
	}
}
.items {
	position: relative;
	// .items only sizes itself to wrap its own rows by default - the space
	// below the last row belongs to .content-view's own background instead,
	// which has no drag-select mousedown handler at all, making that area
	// (very commonly clicked, especially in a sparsely-filled folder) do
	// nothing. Filling the visible height guarantees drag-select actually
	// owns the whole pane, not just the rows themselves.
	min-height: 100%;
}
.items.grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(4.5rem, 1fr));
	// min-height: 100% (above) makes this container taller than its rows
	// need - grid's default align-content stretches row tracks (and then
	// align-items stretches each item) to fill that leftover height,
	// bloating icons to fill the whole pane. Pin rows to the top instead.
	align-content: start;
	// Grid items default to stretching to fill their whole cell, including
	// whatever leftover space 1fr columns distribute beyond an item's own
	// content width - since grid-item stops mousedown propagation, that
	// stretched-but-visually-empty area would swallow rubber-band-select
	// drags started anywhere except past the very last item.
	// justify-items: start (below) fixes that, but the leftover-space
	// margin it opens up is window-width-dependent - at some widths 1fr
	// hands a column zero extra space, leaving only this explicit gap as
	// guaranteed real empty ground. It has to be wide enough on its own
	// (not the old 0.25rem/0.5rem) to reliably catch a drag regardless of
	// window size.
	gap: 0.6rem 0.85rem;
	justify-items: start;
	// A single column has no horizontal leftover-space problem to guard
	// against (there's only one item per row), and it should still look
	// like a full-width row rather than a small left-aligned box.
	&.single-column { grid-template-columns: 1fr; justify-items: stretch; }
}
.items.grid-large {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7.5rem, 1fr));
	align-content: start;
	gap: 0.85rem;
	justify-items: start;
	&.single-column { grid-template-columns: 1fr; justify-items: stretch; }
}
.items.list {
	display: flex;
	flex-direction: column;
}
.drag-select-box {
	position: absolute;
	background: rgba(50, 115, 220, 0.15);
	border: 1px solid rgba(50, 115, 220, 0.6);
	pointer-events: none;
	z-index: 10;
}
</style>
