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
		@contextmenu.self.prevent="openBlankContextMenu"
	>
		<b-loading v-model="loading" :is-full-page="false"></b-loading>
		<error-holder v-if="error" :error="error"></error-holder>
		<div v-else-if="!loading && listing.length === 0" class="empty-state-wrap" @contextmenu.prevent="openBlankContextMenu">
			<empty-folder></empty-folder>
		</div>
		<div
			v-else
			ref="scrollArea"
			class="items-scroll-area scrollbars-light"
			@contextmenu.self.prevent="openBlankContextMenu"
		>
			<div
				ref="itemsEl"
				class="items"
				:class="[viewMode, { 'single-column': filesController.breakpoints.singleColumnGrid }]"
				@mousedown.left.prevent="onDragSelectionStart"
				@contextmenu.self.prevent="openBlankContextMenu"
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
		</div>
		<files-context-menu
			ref="ctxMenu"
			@reload="reload"
			@paste="paste"
			@select-all="selectAll"
			@rename-request="$emit('rename-request', $event)"
			@detail-request="$emit('detail-request', $event)"
			@delete-request="$emit('delete-request', $event)"
			@open-request="openItem"
			@open-new-tab-request="$emit('open-new-tab-request', $event)"
			@compress-request="$emit('compress-request', $event)"
			@extract-request="$emit('extract-request', $event)"
			@copy-selection="$emit('copy-selection')"
			@move-selection="$emit('move-selection')"
			@download-selection="$emit('download-selection')"
			@compress-selection="$emit('compress-selection')"
			@delete-selection="$emit('delete-selection')"
		></files-context-menu>
		<upload-tray ref="uploadTray" :current-path="path" @uploaded="reload"></upload-tray>

		<!-- Status Summary Footer Bar -->
		<footer v-if="!loading && listing.length > 0" class="content-status-bar">
			<div class="status-left">
				<span>{{ folderItemCountLabel }}</span>
			</div>
			<div v-if="selection.length > 0" class="status-center">
				<span class="status-selected-pill">
					<i class="mdi mdi-checkbox-marked-circle-outline mr-1"></i>
					{{ selectionStatusLabel }}
				</span>
			</div>
			<div class="status-right">
				<button class="status-btn" :title="$t('Refresh')" @click="reload">
					<i class="mdi mdi-refresh"></i>
				</button>
			</div>
		</footer>
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
import events from '@/events/events'

// Minimum drag distance (px) before a mousedown+move is treated as a
// selection-rectangle drag rather than a plain click on empty space.
const DRAG_THRESHOLD = 5

export default {
	name: 'files-content-view',
	components: { EmptyFolder, ErrorHolder, GridItem, ListRow, FilesContextMenu, UploadTray },
	inject: ['filesController'],
	props: {
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
		folderItemCountLabel() {
			const total = this.listing.length
			const dirs = this.listing.filter((i) => i.is_dir).length
			const files = total - dirs
			if (dirs && files) {
				return `${total} ${this.$t('items')} (${dirs} ${this.$t('folders')}, ${files} ${this.$t('files')})`
			}
			return `${total} ${this.$t('items')}`
		},
		selectionStatusLabel() {
			const count = this.selection.length
			if (count === 0) return ''
			const selectedItems = this.listing.filter((i) => this.selection.includes(i.path))
			const totalBytes = selectedItems.reduce((acc, item) => acc + (item.size || 0), 0)
			const sizeStr = totalBytes > 0 ? ` • ${this.renderSize(totalBytes)}` : ''
			return `${count} ${count === 1 ? this.$t('item selected') : this.$t('items selected')}${sizeStr}`
		},
		showHidden() {
			return !!this.$store.state.showHidden
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
		showHidden() {
			this.fetchListing(this.path)
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
		this.$EventBus.$on(events.RELOAD_FILE_LIST, this.reload)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.RELOAD_FILE_LIST, this.reload)
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
		'nivaroos:file:operate'(res) {
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
						const visible = this.showHidden ? mapped : mapped.filter((item) => !item.name.startsWith('.'))
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
			if (!this.selection.includes(item.path)) {
				this.selection = [item.path]
				this.lastClickedPath = item.path
			}
			const selectedItems = this.listing.filter((i) => this.selection.includes(i.path))
			this.$refs.ctxMenu.open(event, item, this.$refs.scrollArea || this.$el, selectedItems)
		},
		openBlankContextMenu(event) {
			this.$refs.ctxMenu.open(event, null, this.$refs.scrollArea || this.$el)
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
		renderSize(bytes) {
			if (!bytes || bytes === 0) return '0 B'
			const k = 1024
			const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
			const i = Math.floor(Math.log(bytes) / Math.log(k))
			return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
		},
		onKeyDown(event) {
			const meta = event.ctrlKey || event.metaKey
			const key = event.key.toLowerCase()

			if (meta && key === 'a') {
				event.preventDefault()
				this.selectAll()
				return
			}

			if (meta && key === 'h') {
				event.preventDefault()
				this.$store.commit('SET_SHOW_HIDDEN', !this.showHidden)
				return
			}

			if (event.key === 'Escape') {
				this.clearSelection()
				return
			}

			if (event.key === 'Enter') {
				if (this.selection.length === 1) {
					event.preventDefault()
					const selectedItem = this.listing.find((i) => i.path === this.selection[0])
					if (selectedItem) this.openItem(selectedItem)
					return
				}
			}

			// Arrow keys navigation
			if (['arrowdown', 'arrowup', 'arrowleft', 'arrowright'].includes(key)) {
				if (!this.listing.length) return
				event.preventDefault()
				const currentIndex = this.lastClickedPath
					? this.listing.findIndex((i) => i.path === this.lastClickedPath)
					: -1

				let nextIndex = 0
				if (currentIndex === -1) {
					nextIndex = 0
				} else if (key === 'arrowdown' || key === 'arrowright') {
					nextIndex = Math.min(this.listing.length - 1, currentIndex + 1)
				} else if (key === 'arrowup' || key === 'arrowleft') {
					nextIndex = Math.max(0, currentIndex - 1)
				}

				const targetItem = this.listing[nextIndex]
				if (targetItem) {
					if (event.shiftKey && this.lastClickedPath) {
						this.selection = selectRange(this.listing, this.lastClickedPath, targetItem.path)
					} else {
						this.selection = [targetItem.path]
					}
					this.lastClickedPath = targetItem.path

					// Scroll item into view
					this.$nextTick(() => {
						const itemEls = this.$refs.itemEl
						if (itemEls && itemEls[nextIndex] && itemEls[nextIndex].$el) {
							itemEls[nextIndex].$el.scrollIntoView({ block: 'nearest', inline: 'nearest' })
						}
					})
				}
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
						this.reload()
						this.$EventBus.$emit(events.RELOAD_FILE_LIST)
						setTimeout(() => this.reload(), 400)
						setTimeout(() => this.reload(), 1200)
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
	display: flex;
	flex-direction: column;
	position: relative;
	outline: none;
	overflow: hidden;

	&.is-drag-over {
		outline: 2px dashed rgba(50, 115, 220, 0.6);
		outline-offset: -2px;
	}
}

.empty-state-wrap {
	flex: 1 1 auto;
	min-height: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	overflow-y: auto;
}

.items-scroll-area {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	overflow-x: hidden;
	padding: 0.75rem;
	position: relative;
}

.items {
	position: relative;
	min-height: 100%;
}
.items.grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(4.5rem, 1fr));
	align-content: start;
	gap: 0.6rem 0.85rem;
	justify-items: start;
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

.content-status-bar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.35rem 1rem;
	font-size: 0.75rem;
	color: #64748b;
	border-top: 1px solid rgba(0, 0, 0, 0.06);
	background: #ffffff;
	user-select: none;
	flex-shrink: 0;
	z-index: 15;

	.status-selected-pill {
		display: inline-flex;
		align-items: center;
		padding: 0.15rem 0.65rem;
		background: rgba(37, 99, 235, 0.09);
		color: #2563eb;
		border-radius: 9999px;
		font-weight: 500;
	}

	.status-btn {
		background: transparent;
		border: none;
		color: #64748b;
		cursor: pointer;
		padding: 0.2rem;
		border-radius: 4px;
		font-size: 0.95rem;
		line-height: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: all 0.12s ease;

		&:hover {
			color: #1e293b;
			background: rgba(0, 0, 0, 0.06);
		}
	}
}
</style>
