<!-- src/apps/files/ContentView.vue -->
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
		<!-- Companion Device Banner -->
		<div v-if="companionDeviceInfo" class="companion-device-banner">
			<div class="banner-avatar">
				<b-icon :icon="companionDeviceInfo.icon" pack="mdi" class="casa-color-blue" custom-size="casa-24px"></b-icon>
			</div>
			<div class="banner-info">
				<div class="banner-header-row">
					<span class="banner-device-name">{{ companionDeviceInfo.name }}</span>
					<span class="banner-pill" :class="companionDeviceInfo.isOnline ? 'is-online' : 'is-offline'">
						{{ companionDeviceInfo.isOnline ? $t('Online') : $t('Offline') }}
					</span>
					<span v-if="companionDeviceInfo.batteryLevel > 0" class="banner-battery">
						<i class="mdi mdi-battery mr-1"></i>{{ companionDeviceInfo.batteryLevel }}%
					</span>
					<span class="banner-subtext">{{ companionDeviceInfo.model }} · {{ companionDeviceInfo.platform }} · {{ $t('Companion Sync Folder') }}</span>
				</div>
				<div v-if="companionDeviceInfo.storageTotal > 0" class="banner-storage-row">
					<span class="banner-storage-label">{{ $t('Device Internal Storage') }}:</span>
					<span class="banner-storage-text">{{ companionDeviceInfo.storageText }}</span>
					<div class="banner-storage-track">
						<div class="banner-storage-bar" :style="{ width: companionDeviceInfo.storagePercent + '%' }"></div>
					</div>
					<span class="banner-storage-pct">{{ companionDeviceInfo.storagePercent }}%</span>
				</div>
			</div>
		</div>

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
			@paste-into="paste"
			@select-all="selectAll"
			@rename-request="$emit('rename-request', $event)"
			@share-request="$emit('share-request', $event)"
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
import { pasteClipboard } from '@/utils/files/paste'
import { bus as transferBus } from '@/service/transfers'
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
			companionDeviceList: [],
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
		companionDeviceInfo() {
			if (!this.path || !this.path.startsWith('/DATA/Companion/')) return null
			const pathLower = this.path.toLowerCase()
			for (const dev of this.companionDeviceList) {
				const name = dev.name || dev.device_name || 'Companion'
				const cleanName = name.toLowerCase().replace(/[^a-z0-9]/g, '_')
				if (
					(dev.storage_path && this.path.startsWith(dev.storage_path)) ||
					pathLower.includes(cleanName) ||
					pathLower.includes(name.toLowerCase())
				) {
					const storageUsed = dev.storage_used || 0
					const storageTotal = dev.storage_total || 0
					const storagePercent = storageTotal > 0 ? Math.min(100, Math.round((storageUsed / storageTotal) * 100)) : 0
					const storageText = storageTotal > 0 ? `${this.renderSize(storageUsed)} / ${this.renderSize(storageTotal)}` : ''

					let icon = 'cellphone'
					const model = (dev.model || '').toLowerCase()
					const platform = (dev.platform || '').toLowerCase()
					const nameLower = name.toLowerCase()
					if (
						model.includes('tablet') || model.includes('tab') || model.includes('pad') || model.includes('ruan') ||
						nameLower.includes('tablet') || nameLower.includes('tab') || nameLower.includes('pad')
					) {
						icon = 'tablet-cellphone'
					} else if (platform.includes('ios') || model.includes('iphone') || model.includes('ipad')) {
						icon = 'cellphone-apple'
					}

					return {
						name,
						model: dev.model || '',
						platform: dev.platform || '',
						icon,
						isOnline: !!dev.is_online,
						batteryLevel: dev.battery_level || 0,
						storageUsed,
						storageTotal,
						storagePercent,
						storageText,
					}
				}
			}
			return null
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
				// Exact '/DATA/Companion' is the per-device folder listing itself
				// (needs the device list to classify each row mobile/tablet for
				// its icon - see fetchCompanionFolderTypes()); the '/...' prefix
				// case is being *inside* one device's own folder (needs it for
				// the companionDeviceInfo banner).
				if (path && (path === '/DATA/Companion' || path.startsWith('/DATA/Companion/'))) {
					this.fetchCompanionDevices()
				}
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
		this.fetchCompanionDevices()
		this.$EventBus.$on(events.RELOAD_FILE_LIST, this.reload)
		transferBus.$on('finished', this.onTransferFinished)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.RELOAD_FILE_LIST, this.reload)
		transferBus.$off('finished', this.onTransferFinished)
		window.removeEventListener('mousemove', this.onDragSelectionMove)
		window.removeEventListener('mouseup', this.onDragSelectionEnd)
	},
	sockets: {
		// Live updates for changes made outside this tab entirely - another
		// device over Samba, a scheduled task, a companion sync, a second
		// browser tab - see service/file_watch.go on the backend. Path-scoped
		// (unlike the file:operate handler above): an inotify watch only
		// exists for directories someone is actually viewing, so this only
		// ever fires for a path some open window cares about, and reloading
		// every such window instead of just the matching one would be wasted
		// work. The `this.loading` guard avoids piling up overlapping
		// requests if changes keep landing faster than one reload completes.
		'nivaroos:file:changed'(res) {
			// SendNotify (backend) JSON-encodes each Properties value
			// individually (see e.g. Cpu.vue's sys_cpu handling) - a plain
			// string comparison against the raw Properties.path would never
			// match since it's still wrapped in an extra pair of quotes.
			if (!res.Properties || this.loading) return
			let changedPath
			try {
				changedPath = JSON.parse(res.Properties.path)
			} catch (e) {
				return
			}
			if (changedPath === this.path) this.reload()
		},
		// A USB/HDD plug/eject changes which mount_point -> type entries
		// fetchMountTypes() knows about, but that map is only ever fetched
		// once (mounted() above) - without this, a drive plugged in after
		// this pane was opened shows up in the /DATA listing (via the
		// nivaroos:file:changed watch) with the generic folder icon instead
		// of folder-usb/folder-usb3/folder-hdd until a full page reload
		// re-runs fetchMountTypes() from scratch. Re-fetching the map here
		// (rather than only patching in place) also picks up a removed
		// drive's mount_point falling out of the map.
		'local-storage:disk:added'() {
			setTimeout(() => this.fetchMountTypes(), 500)
		},
		'local-storage:disk:removed'() {
			setTimeout(() => this.fetchMountTypes(), 500)
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
							// mountTypes/companion-devices patch reassigns an
							// existing reactive property rather than adding a new
							// one - Vue 2 can't track newly-added properties.
							type:
								this.mountTypes[item.path] ||
								(path === '/DATA/Companion' ? this.classifyCompanionFolderType(item.path, item.name) : undefined),
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
		async fetchCompanionDevices() {
			try {
				const res = await this.$api.companion.getDevices()
				this.companionDeviceList = (res.data?.data || []).filter((dev) => dev.is_online)
				// Same reasoning as fetchMountTypes()'s post-load patch: the
				// '/DATA/Companion' listing itself can render before this
				// call resolves, so already-built rows need their `type`
				// reassigned once the device list is actually in.
				if (this.path === '/DATA/Companion') {
					this.listing.forEach((item) => {
						const type = this.classifyCompanionFolderType(item.path, item.name)
						if (type) item.type = type
					})
				}
			} catch (_) {
				// No companion devices paired (or the call failed) - folders
				// just fall back to the generic companion icon set below.
			}
		},
		// Matches a '/DATA/Companion' child folder back to the paired device
		// it belongs to and classifies it mobile vs. tablet, for the
		// folder-mobile_companion / folder-tablet_companion icons. Mirrors
		// the matching + tablet heuristic in the companionDeviceInfo
		// computed prop above (path/name against dev.storage_path or a
		// sanitized device name) - kept as a separate method rather than a
		// shared helper since that prop also needs an MDI icon/banner data,
		// not just a two-way mobile/tablet split; if the matching rule ever
		// changes, update both.
		classifyCompanionFolderType(path, name) {
			if (!path || !this.companionDeviceList.length) return null
			const pathLower = path.toLowerCase()
			const dev = this.companionDeviceList.find((d) => {
				const devName = d.name || d.device_name || 'Companion'
				const cleanName = devName.toLowerCase().replace(/[^a-z0-9]/g, '_')
				return (
					(d.storage_path && path.startsWith(d.storage_path)) ||
					pathLower.includes(cleanName) ||
					pathLower.includes(devName.toLowerCase())
				)
			})
			if (!dev) return null
			const model = (dev.model || '').toLowerCase()
			const devName = (dev.name || '').toLowerCase()
			const nameLower = (name || '').toLowerCase()
			const isTablet =
				model.includes('tablet') || model.includes('tab') || model.includes('pad') || model.includes('ruan') ||
				devName.includes('tablet') || devName.includes('tab') || devName.includes('pad') ||
				nameLower.includes('tablet') || nameLower.includes('tab') || nameLower.includes('pad')
			return isTablet ? 'companion-tablet' : 'companion-mobile'
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
		// Every paste route (toolbar, Ctrl+V, context menu, paste-into)
		// goes through utils/files/paste.js; the listing refreshes when the
		// job finishes (see onTransferFinished), not on a guess.
		paste(targetPath) {
			const dest = typeof targetPath === 'string' && targetPath ? targetPath : this.path
			return pasteClipboard(this, dest)
		},
		// Reload exactly when a finished copy/move/delete touched the folder
		// this view shows (source or destination).
		onTransferFinished(job) {
			const norm = (p) => (p || '').replace(/\/+$/, '') || '/'
			const here = norm(this.path)
			// The folders the job changed, plus anything inside the
			// destination (a copied folder's contents land below it).
			const hit = (job.affected_dirs || []).some((d) => norm(d) === here) || (job.dest && here.startsWith(norm(job.dest) + '/'))
			if (hit) this.reload()
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
	padding: var(--space-3);
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
	gap: var(--space-2) var(--space-3);
	justify-items: start;
	&.single-column { grid-template-columns: 1fr; justify-items: stretch; }
}
.items.grid-large {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7.5rem, 1fr));
	align-content: start;
	gap: var(--space-3);
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
	padding: var(--space-1) var(--space-4);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	border-top: 1px solid rgba(0, 0, 0, 0.06);
	background: var(--theme-titlebar-bg, #ffffff); border-top: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06)); user-select: none; flex-shrink: 0;
	z-index: 15;

	.status-selected-pill {
		display: inline-flex;
		align-items: center;
		padding: var(--space-1) var(--space-3);
		background: rgba(37, 99, 235, 0.09);
		color: #2563eb;
		border-radius: var(--radius-pill);
		font-weight: 500;
	}

	.status-btn {
		background: transparent;
		border: none;
		color: var(--theme-text-muted, #64748b);
		cursor: pointer;
		padding: var(--space-1);
		border-radius: var(--radius-xs);
		font-size: var(--font-md);
		line-height: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: all 0.12s ease;

		&:hover {
			color: var(--theme-text-primary, #1e293b);
			background: rgba(0, 0, 0, 0.06);
		}
	}
}

.companion-device-banner {
	flex-shrink: 0;
	margin: var(--space-3) var(--space-3) var(--space-1) var(--space-3);
	padding: var(--space-3) var(--space-4);
	background: var(--theme-card-bg, #ffffff); border: 1px solid var(--theme-card-border, rgb(228 233 237)); color: var(--theme-text-primary, #0f172a);
	border-radius: var(--radius-card);
	display: flex;
	align-items: center;
	gap: var(--space-4);
	box-shadow: var(--shadow-sm);
}
.banner-avatar {
	width: 42px;
	height: 42px;
	border-radius: var(--radius-control);
	background: rgba(59, 130, 246, 0.1);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}
.banner-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.banner-header-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	flex-wrap: wrap;
}
.banner-device-name {
	font-weight: 700;
	font-size: var(--font-md);
	color: var(--theme-text-primary, #1e293b);
}
.banner-pill {
	font-size: var(--font-2xs);
	font-weight: 700;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-sm);
	text-transform: uppercase;
	&.is-online {
		background: rgba(34, 197, 94, 0.12);
		color: #16a34a;
	}
	&.is-offline {
		background: rgba(148, 163, 184, 0.15);
		color: var(--theme-text-muted, #64748b);
	}
}
.banner-battery {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	display: flex;
	align-items: center;
}
.banner-subtext {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #94a3b8);
}
.banner-storage-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}
.banner-storage-label {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	font-weight: 500;
	white-space: nowrap;
}
.banner-storage-text {
	font-size: var(--font-xs);
	color: #3b82f6;
	font-weight: 700;
	white-space: nowrap;
}
.banner-storage-track {
	flex: 1 1 auto;
	max-width: 140px;
	height: 6px;
	background: var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-pill);
	overflow: hidden;
}
.banner-storage-bar {
	height: 100%;
	background: #3b82f6;
	border-radius: var(--radius-pill);
	transition: width 0.3s ease;
}
.banner-storage-pct {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	font-weight: 600;
}
</style>
