<template>
	<div class="home-section has-text-left">
		<!-- The "Apps" title + "+" button are gone - right-click the
		desktop background for the same actions (see ContextMenu.vue). -->

		<!-- App List Start -->
		<div v-if="isLoading" class="app-list-skeleton">
			<div v-for="index in skCount" :id="'app-' + index" :key="'app-' + index">
				<app-card-skeleton :index="index"></app-card-skeleton>
			</div>
		</div>
		<div v-else ref="canvas" class="app-canvas contextmenu-canvas" :class="{ 'is-dragging': !!draggingName }" :style="{ height: canvasHeight + 'px' }"
			@mousedown.self="startMarquee">
			<div class="drop-grid"></div>
			<div v-if="dragPreviewStyle" class="drop-preview" :style="dragPreviewStyle"></div>
			<div v-if="marqueeStyle" class="marquee-box" :style="marqueeStyle"></div>
			<div v-for="item in positionedAppList" :key="'app-' + item.name" :id="'app-' + item.name"
				:data-app-name="item.name"
				class="app-slot" :class="{ dragging: draggingName === item.name, selected: selectedNames.includes(item.name) }" :style="slotStyle(item)"
				@mousedown="startDrag(item, $event)" @click.capture="swallowClickAfterDrag">
				<folder-card
					v-if="item.app_type === 'folder'"
					:folder="item.folderData"
					:is-drag-target="dragOverFolderId === item.name"
					@open="openFolder"
					@rename="renameFolderPrompt"
					@delete="deleteFolderConfirm"
					@editIcon="openFolderIconEditor"
				></folder-card>
				<app-card
					v-else
					:item="item"
					@configApp="showConfigPanel"
					@importApp="showContainerPanel"
					@updateState="getList"
					@addToFolder="addToFolderPrompt"
					@editLegacyApp="openLegacyEditModal"
				></app-card>
			</div>
		</div>
		<!-- App List End -->

		<b-modal v-model="showFolderModal" :can-cancel="['escape', 'outside']" animation="zoom-in" aria-modal
				 has-modal-card>
			<template #default>
				<folder-modal v-if="activeFolder" :folder="activeFolder" @close="showFolderModal = false"
					@configApp="showConfigPanel" @importApp="showContainerPanel"
					@removeFromFolder="handleRemoveFromFolder" @updateState="getList"
					@editLegacyApp="openLegacyEditModal"></folder-modal>
			</template>
		</b-modal>


		<b-modal v-model="showAddToFolderModal" :can-cancel="['escape', 'outside']" animation="zoom-in" aria-modal
				 has-modal-card>
			<template #default>
				<add-to-folder-panel :folders="addToFolderChoices" @close="showAddToFolderModal = false"
					@confirm="handleAddToFolderConfirm"></add-to-folder-panel>
			</template>
		</b-modal>

		<b-modal v-model="showFolderIconEditor" :can-cancel="['escape', 'outside']" animation="zoom-in" aria-modal
				 has-modal-card>
			<template #default>
				<icon-editor-modal v-if="folderIconEditTarget" :initial-radius="folderIconEditTarget.iconRadius || 0"
					:src="folderIconEditTarget.icon || (folderIconEditTarget.apps[0] && folderIconEditTarget.apps[0].icon) || defaultAppIcon"
					@apply="handleFolderIconEdited" @close="showFolderIconEditor = false"></icon-editor-modal>
			</template>
		</b-modal>
	</div>
</template>

<script>
import AppCard from './AppCard.vue'
import AppCardSkeleton from './AppCardSkeleton.vue'
import AppPanel from './AppPanel.vue'
import ExternalLinkPanel from '@/components/Apps/ExternalLinkPanel'
import FolderCard from './FolderCard.vue'
import FolderModal from './FolderModal.vue'
import AddToFolderPanel from './AddToFolderPanel.vue'
import IconEditorModal from './IconEditorModal.vue'
import concat from 'lodash/concat'
import events from '@/events/events'
import last from 'lodash/last'
import business_ShowNewAppTag from '@/mixins/app/Business_ShowNewAppTag'
import business_LinkApp from '@/mixins/app/Business_LinkApp'
import business_Folders from '@/mixins/app/Business_Folders'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import isEqual from 'lodash/isEqual'
import { ice_i18n } from '@/mixins/base/common-i18n'
import YAML from 'yamljs'

const SYNCTHING_STORE_ID = 74

// meta_data :: build-in app
const builtInApplications = [
	{
		id: '1',
		name: 'App Store',
		title: {
			en_us: 'App Store'
		},
		icon: require(`@/assets/img/app/appstore.svg`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '2',
		name: 'Files',
		title: {
			en_us: 'Files'
		},
		icon: require(`@/assets/img/app/files.svg`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '3',
		name: 'Settings',
		title: {
			en_us: 'Settings'
		},
		icon: require(`@/assets/img/app/settings.png`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '4',
		name: 'Terminal',
		title: {
			en_us: 'Terminal'
		},
		icon: require(`@/assets/img/app/terminal.png`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '5',
		name: 'VMs',
		title: {
			en_us: 'VMs'
		},
		icon: require(`@/assets/img/app/vm-manager.png`),
		status: 'running',
		app_type: 'system'
	}
]

const orderConfig = 'app_order'

// Apps free-roam and snap to this placement grid on drop (same model as
// the widget canvas) - sized to roughly match the icon tile + label
// that used to come from the CSS grid's minmax(96px, 116px) columns.
const CELL_W = 116
const CELL_H = 120
const GAP = 4
const SNAP = 20

export default {
	mixins: [business_ShowNewAppTag, business_LinkApp, business_Folders, business_LegacyAppOverrides],
	data () {
		return {
			user_id: localStorage.getItem('user_id'),
			appList: [],
			positions: [], // [{ name, x, y }] - free-roam, snap-to-grid placement
			draggingName: null,
			dragGhost: null,
			draggingGroup: null, // string[] of names moving together, when dragging a multi-selection
			groupGhosts: null, // { [name]: { left, top } } - live positions of the OTHER selected items during a group drag
			selectedNames: [], // marquee-selected app/folder names
			marquee: null, // { x, y, width, height } - canvas-relative, while rubber-band-selecting
			justDragged: false,
			appConfig: {},
			isLoading: false,
			isShowing: false,
			retryCount: 0,
			appListErrorMessage: '',
			skCount: 0,
			ListRefreshTimer: null,
			showFolderModal: false,
			activeFolder: null,
			dragOverFolderId: null,
			showAddToFolderModal: false,
			addToFolderItem: null,
			addToFolderChoices: [],
			showFolderIconEditor: false,
			folderIconEditTarget: null,
			defaultAppIcon: require('@/assets/img/app/default.svg')
		}
	},
	components: {
		AppCard,
		AppCardSkeleton,
		FolderCard,
		FolderModal,
		AddToFolderPanel,
		IconEditorModal
	},
	provide () {
		return {
			openAppStore: this.showInstall
		}
	},
	computed: {
		exsitingAppsShow () {
			return this.$store.state.existingAppsSwitch
		},
		positionedAppList () {
			return this.positions
				.map(p => {
					const item = this.appList.find(i => i.name === p.name)
					return item ? { ...item, x: p.x, y: p.y } : null
				})
				.filter(Boolean)
		},
		canvasHeight () {
			const maxY = this.positions.reduce((max, p) => Math.max(max, p.y), 0)
			return maxY + CELL_H + 200
		},
		// Snapped landing-spot preview, shown while dragging - same snap
		// math placeItem uses on drop, so the box is exactly where the
		// item will land. Hidden while hovering a folder since the folder
		// itself already highlights via is-drag-target, and while
		// group-dragging a multi-selection (no single meaningful "landing
		// cell" to preview - each item snaps independently).
		dragPreviewStyle () {
			const isGroupDrag = this.draggingGroup && this.draggingGroup.length > 1
			if (!this.draggingName || !this.dragGhost || this.dragOverFolderId || isGroupDrag) return null
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : window.innerWidth
			const maxLeft = Math.max(0, canvasWidth - CELL_W)
			const x = Math.min(maxLeft, Math.max(0, Math.round(this.dragGhost.left / SNAP) * SNAP))
			const y = Math.max(0, Math.round(this.dragGhost.top / SNAP) * SNAP)
			return {
				transform: `translate(${x}px, ${y}px)`,
				width: CELL_W + 'px',
				height: CELL_H + 'px'
			}
		},
		// Rubber-band selection rectangle, canvas-relative.
		marqueeStyle () {
			if (!this.marquee) return null
			return {
				transform: `translate(${this.marquee.x}px, ${this.marquee.y}px)`,
				width: this.marquee.width + 'px',
				height: this.marquee.height + 'px'
			}
		}
	},
	created () {
		this.getList()
		this.$EventBus.$on(events.OPEN_APP_STORE_AND_GOTO_SYNCTHING, () => {
			this.showInstall(SYNCTHING_STORE_ID)
		})

		this.$EventBus.$on(events.RELOAD_APP_LIST, () => {
			this.getList()
		})

		// The "+" button/title moved to the desktop's right-click context
		// menu (ContextMenu.vue) - it emits these instead of calling
		// AppSection's methods directly, since it lives in a different
		// branch of the component tree.
		this.$EventBus.$on(events.SHOW_CUSTOM_INSTALL, () => {
			this.showInstall(0, 'custom')
		})
		this.$EventBus.$on(events.SHOW_EXTERNAL_LINK_PANEL, () => {
			this.showExternalLinkPanel()
		})
		this.$EventBus.$on(events.SHOW_CREATE_FOLDER_PROMPT, () => {
			this.createFolderPrompt()
		})
		this.$EventBus.$on(events.ARRANGE_APPS, () => {
			this.arrangeApps()
		})

		this.ListRefreshTimer = setInterval(() => {
			this.getList()
		}, 5000)
	},
	beforeDestroy () {
		this.$EventBus.$off(events.OPEN_APP_STORE_AND_GOTO_SYNCTHING)
		this.$EventBus.$off(events.SHOW_CUSTOM_INSTALL)
		this.$EventBus.$off(events.SHOW_EXTERNAL_LINK_PANEL)
		this.$EventBus.$off(events.SHOW_CREATE_FOLDER_PROMPT)
		this.$EventBus.$off(events.ARRANGE_APPS)
		window.removeEventListener('resize', this.getSkCount)

		clearInterval(this.ListRefreshTimer)
	},
	mounted () {
		window.addEventListener('resize', this.getSkCount)
		this.getSkCount()
	},
	methods: {
		// Only used when auto-placing apps that have no saved position yet
		// (brand new install, or first load after this feature shipped) -
		// caps how many rows fill before wrapping to the next column, so
		// the fill order actually goes top-to-bottom-then-across like real
		// desktop icons, instead of an arbitrary single column forever.
		// Measures the canvas's real top offset instead of assuming a
		// fixed "reserved space above it" - a hardcoded guess goes stale
		// every time the layout above the grid changes (base-bar removal,
		// dock, wide-screen container, ...) and, being too conservative,
		// silently wraps to a new column before the actual visible height
		// is used up - leaving real usable space at the bottom unfilled.
		maxRowsPerCol () {
			const bottomMargin = 24
			const canvasTop = this.$refs.canvas ? this.$refs.canvas.getBoundingClientRect().top : 130
			const available = window.innerHeight - canvasTop - bottomMargin
			return Math.max(3, Math.floor(available / (CELL_H + GAP)))
		},

		getSkCount () {
			const windowWidth = window.innerWidth
			if (windowWidth < 1024) {
				this.skCount = 4
			} else if (windowWidth < 1216) {
				this.skCount = 6
			} else if (windowWidth < 1408) {
				this.skCount = 8
			} else {
				this.skCount = 10
			}
		},

		/**
		 * @description: Fetch the list of installed apps
		 * @return {*} void
		 */
		async getList () {
			try {
				const orgAppList = await this.$openAPI.appGrid.getAppGrid().then(res => res.data.data || [])
				const legacyOverrides = await this.getLegacyAppOverrides()
				// Rename/icon/roundness overrides apply to every app type
				// the same way - a per-user display-only layer that never
				// touches the app's real config.
				const applyOverride = item => {
					const override = legacyOverrides[item.name]
					if (!override) return
					if (override.icon) item.icon = override.icon
					if (override.url) item.overrideUrl = override.url
					if (override.iconRadius) item.iconRadius = override.iconRadius
					if (override.title) item.title = { ...item.title, custom: override.title }
				}

				orgAppList.forEach(item => {
					item.hostname = item.hostname || this.$baseIp
					// Container app does not have icon.
					item.icon = item.icon || require(`@/assets/img/app/default.svg`)
					applyOverride(item)
				})

				let listLinkApp = await this.getLinkAppList()
				listLinkApp.forEach(item => {
					// linkApp does not have title.
					item.title = {
						en_us: item.name
					}
					applyOverride(item)
				})

				// Clone rather than mutate builtInApplications directly - it's
				// a module-level singleton shared across every getList() call.
				const systemApps = builtInApplications.map(app => ({ ...app, title: { ...app.title } }))
				systemApps.forEach(applyOverride)

				// all app list - legacy apps (v1app/container) are shown
				// unified here rather than in a separate section, and are
				// draggable/sortable the same as any other app.
				let casaAppList = concat(systemApps, orgAppList, listLinkApp)

				// Fold folders in: grouped apps are pulled out of the flat
				// list and replaced by one pseudo-item per folder (rendered
				// as folder-card instead of app-card). Folders participate
				// in the same drag-sort as everything else via their id
				// standing in for `name`.
				const folders = await this.getFolders()
				const folderIdByAppName = {}
				folders.forEach(f => {
					f.appNames.forEach(n => { folderIdByAppName[n] = f.id })
				})
				const ungrouped = casaAppList.filter(item => !folderIdByAppName[item.name])
				const folderPseudoItems = folders.map(f => ({
					name: f.id,
					app_type: 'folder',
					folderData: {
						...f,
						apps: casaAppList.filter(item => folderIdByAppName[item.name] === f.id)
					}
				}))
				casaAppList = concat(folderPseudoItems, ungrouped)

				if (this.activeFolder) {
					const refreshed = folderPseudoItems.find(f => f.name === this.activeFolder.id)
					this.activeFolder = refreshed ? refreshed.folderData : null
				}

				this.appList = casaAppList

				// get app placement info (free-roam grid position, not a
				// simple order list - see reconcileAppPositions).
				const savedPositions = await this.$api.users
					.getCustomStorage(orderConfig)
					.then(res => (Array.isArray(res.data.data) ? res.data.data : []))

				const reconciled = this.reconcileAppPositions(casaAppList, savedPositions)
				this.positions = reconciled
				if (!isEqual(savedPositions, reconciled)) {
					this.savePositions()
				}

				this.isLoading = false
				this.retryCount = 0
				this.appListErrorMessage = ''
			} catch (error) {
				console.error(error)
				this.isLoading = true
				if (this.retryCount < 5) {
					setTimeout(() => {
						this.retryCount++
						this.getList()
					}, 2000)
				} else {
					this.appListErrorMessage = 'Failed to get app list.'
					this.$buefy.toast.open({
						message: this.$t(`Failed to load apps, please refresh later.`),
						type: 'is-danger'
					})
				}
			}
		},

		/**
		 * @description:
		 * @param {Array} oriList
		 * @param {Array} newList
		 * @return {*}
		 */
		// Keeps saved positions for items that still exist and are still
		// within the canvas, drops everything else (including the old
		// col/row-cell format and anything that landed off to the side
		// from a previous bug), and auto-places whatever's left over -
		// column-major, so the fill order goes top-to-bottom then
		// across, like real desktop icons.
		reconcileAppPositions (casaAppList, saved) {
			const knownNames = casaAppList.map(i => i.name)
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : window.innerWidth
			const isOnCanvas = p => p.x >= 0 && p.x <= Math.max(0, canvasWidth - CELL_W)
			const kept = saved.filter(p => knownNames.includes(p.name) && Number.isInteger(p.x) && Number.isInteger(p.y) && isOnCanvas(p))
			const keptNames = kept.map(p => p.name)
			const maxRows = this.maxRowsPerCol()

			// Real rect-overlap against kept AND newly-placed items, not
			// rounded-cell-index equality - a freely-dragged item that
			// isn't sitting exactly on a nominal (col*(CELL_W+GAP),
			// row*(CELL_H+GAP)) multiple could round to a cell index that
			// doesn't match where it visually sits, making a genuinely
			// free cell look "occupied" (leaving a gap) or a genuinely
			// occupied one look free (an overlap). Same fix as placeItem.
			const rectOf = p => ({ left: p.x, top: p.y, right: p.x + CELL_W, bottom: p.y + CELL_H })
			const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top
			const placedRects = kept.map(rectOf)

			const firstFreeCell = () => {
				for (let col = 0; ; col++) {
					for (let row = 0; row < maxRows; row++) {
						const x = col * (CELL_W + GAP)
						const y = row * (CELL_H + GAP)
						const rect = rectOf({ x, y })
						if (!placedRects.some(r => overlaps(rect, r))) {
							placedRects.push(rect)
							return { x, y }
						}
					}
				}
			}

			const newItems = casaAppList
				.filter(i => !keptNames.includes(i.name))
				.map(i => ({ name: i.name, ...firstFreeCell() }))

			return kept.concat(newItems)
		},

		savePositions () {
			this.$api.users.setCustomStorage(orderConfig, this.positions).then(res => {
				if (res.data.success === 200 && Array.isArray(res.data.data)) {
					this.positions = res.data.data
				}
			})
		},

		// "Arrange icons" from the desktop right-click menu - re-flows
		// every current item through the same fresh-auto-placement path a
		// brand-new app gets, discarding wherever they'd been dragged to.
		arrangeApps () {
			this.positions = this.reconcileAppPositions(this.appList, [])
			this.savePositions()
		},

		slotStyle (item) {
			const isDragging = this.draggingName === item.name
			const isGroupMember = !isDragging && this.draggingGroup && this.draggingGroup.includes(item.name)
			let left = item.x
			let top = item.y
			if (isDragging && this.dragGhost) {
				left = this.dragGhost.left
				top = this.dragGhost.top
			} else if (isGroupMember && this.groupGhosts && this.groupGhosts[item.name]) {
				left = this.groupGhosts[item.name].left
				top = this.groupGhosts[item.name].top
			}
			return {
				width: CELL_W + 'px',
				transform: `translate(${left}px, ${top}px)`,
				zIndex: (isDragging || isGroupMember) ? 50 : 1
			}
		},

		// Rubber-band multi-select, like any real desktop icon grid -
		// mousedown on empty canvas (not a tile, via @mousedown.self)
		// grows a selection box; any item whose slot rect intersects it
		// gets selected live as the box grows. A plain click (no drag
		// past the threshold) just clears the current selection.
		startMarquee (e) {
			if (e.button !== 0 || this.$store.state.isMobile) return
			const canvasRect = this.$refs.canvas.getBoundingClientRect()
			const startX = e.clientX - canvasRect.left
			const startY = e.clientY - canvasRect.top
			let moved = false

			const onMove = moveEvent => {
				const curX = moveEvent.clientX - canvasRect.left
				const curY = moveEvent.clientY - canvasRect.top
				if (!moved && Math.hypot(curX - startX, curY - startY) > 4) {
					moved = true
				}
				if (!moved) return
				const x = Math.min(startX, curX)
				const y = Math.min(startY, curY)
				const width = Math.abs(curX - startX)
				const height = Math.abs(curY - startY)
				this.marquee = { x, y, width, height }
				const rect = { left: x, top: y, right: x + width, bottom: y + height }
				this.selectedNames = this.positionedAppList
					.filter(item => {
						const r = { left: item.x, top: item.y, right: item.x + CELL_W, bottom: item.y + CELL_H }
						return r.left < rect.right && r.right > rect.left && r.top < rect.bottom && r.bottom > rect.top
					})
					.map(item => item.name)
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (!moved) {
					this.selectedNames = []
				}
				this.marquee = null
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},

		// A capture-phase click handler on the same element as mousedown -
		// guaranteed to run before any descendant's own click handler
		// (e.g. AppCard's @click.native that opens the app), regardless of
		// browser event-timing specifics. The browser still fires a plain
		// "click" right after mouseup even when the mouse moved a lot in
		// between; without swallowing it, repositioning an app also
		// re-triggers opening it/its webui immediately after every drag.
		swallowClickAfterDrag (e) {
			if (this.justDragged) {
				e.stopPropagation()
				e.preventDefault()
				this.justDragged = false
			}
		},

		// Mousedown-based drag (not native HTML5 DnD) - only actually
		// starts "dragging" once the pointer moves past a small
		// threshold, so a plain click (open the app, right-click menu)
		// passes straight through. While dragging a non-folder item,
		// elementFromPoint checks whether it's currently over a folder
		// card, same detection primitive the old Sortable-based version
		// used, just driven by real mousemove events now instead of the
		// dragover/touchmove native-DnD workaround.
		startDrag (item, e) {
			if (e.button !== 0 || this.$store.state.isMobile) return
			// Images (app icons) are natively draggable in every browser by
			// default - without this, grabbing one kicks off the browser's
			// own native image-drag instead of this handler, which then
			// suppresses mousemove entirely for the rest of the gesture.
			e.preventDefault()

			// Dragging a member of the current multi-selection moves the
			// whole group together, like any real desktop. Starting a drag
			// on something outside the current selection replaces it
			// (matches clicking a single icon deselecting the rest).
			const isGroupDrag = this.selectedNames.includes(item.name) && this.selectedNames.length > 1
			const groupNames = isGroupDrag ? this.selectedNames : [item.name]
			if (!isGroupDrag) this.selectedNames = []

			const startX = e.clientX
			const startY = e.clientY
			const origins = {}
			groupNames.forEach(name => {
				const p = this.positions.find(pp => pp.name === name)
				if (p) origins[name] = { x: p.x, y: p.y }
			})
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : window.innerWidth
			const maxLeft = Math.max(0, canvasWidth - CELL_W)
			let dragging = false

			const onMove = moveEvent => {
				const dx = moveEvent.clientX - startX
				const dy = moveEvent.clientY - startY
				if (!dragging && Math.hypot(dx, dy) > 6) {
					dragging = true
					this.draggingName = item.name
					this.draggingGroup = groupNames
				}
				if (!dragging) return
				this.dragGhost = {
					left: Math.min(maxLeft, Math.max(0, origins[item.name].x + dx)),
					top: Math.max(0, origins[item.name].y + dy)
				}
				if (groupNames.length > 1) {
					const ghosts = {}
					groupNames.forEach(name => {
						if (name === item.name) return
						ghosts[name] = {
							left: Math.max(0, origins[name].x + dx),
							top: Math.max(0, origins[name].y + dy)
						}
					})
					this.groupGhosts = ghosts
				} else if (item.app_type !== 'folder') {
					const el = document.elementFromPoint(moveEvent.clientX, moveEvent.clientY)
					const folderEl = el && el.closest('[data-folder-id]')
					this.dragOverFolderId = folderEl ? folderEl.dataset.folderId : null
				}
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (dragging) {
					this.justDragged = true
					if (groupNames.length > 1) {
						// Group drop: every selected item snaps
						// independently at the same delta - no swap/
						// occupant resolution, which is ambiguous once
						// more than one item is landing at once.
						groupNames.forEach(name => {
							const ghost = name === item.name ? this.dragGhost : this.groupGhosts[name]
							if (!ghost) return
							const x = Math.min(maxLeft, Math.max(0, Math.round(ghost.left / SNAP) * SNAP))
							const y = Math.max(0, Math.round(ghost.top / SNAP) * SNAP)
							this.placeItemNoSwap(name, x, y)
						})
						this.savePositions()
					} else if (this.dragOverFolderId && item.app_type !== 'folder') {
						this.addAppToFolder(item.name, this.dragOverFolderId).then(() => this.getList())
					} else if (this.dragGhost) {
						const x = Math.min(maxLeft, Math.max(0, Math.round(this.dragGhost.left / SNAP) * SNAP))
						const y = Math.max(0, Math.round(this.dragGhost.top / SNAP) * SNAP)
						this.placeItem(item.name, x, y)
					}
				} else if (isGroupDrag) {
					// A plain click (no drag) on an already-selected icon
					// collapses the selection back down, same as a real
					// desktop - the click still opens the app as normal.
					this.selectedNames = []
				}
				this.draggingName = null
				this.dragGhost = null
				this.dragOverFolderId = null
				this.draggingGroup = null
				this.groupGhosts = null
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},

		// Dropping onto a spot that lands in the same grid cell as another
		// item swaps the two rather than stacking them on top of each
		// other. Collision is decided in grid-index space (round each
		// position to its nearest cell, compare indices for exact
		// equality) rather than pixel-precise bounding boxes measured
		// live from the DOM - that approach was unreliable (swaps
		// triggering from far away or not triggering while visually
		// overlapping) because it depended on exactly when the browser
		// had (or hadn't) settled layout. Whole-cell comparison is what
		// grid-layout libraries (react-grid-layout, vue-grid-layout, etc)
		// actually do, and is deterministic regardless of render timing.
		placeItem (name, x, y) {
			const moved = this.positions.find(p => p.name === name)
			if (!moved) return

			// Real rect-overlap, not grid-cell-index equality - every app
			// tile is the same fixed CELL_W x CELL_H, so this is pure
			// arithmetic (no DOM measurement, no render-timing dependency).
			// Only swaps when the dropped tile actually overlaps another
			// one, not just "close enough" on a coarse nominal grid.
			const rectOf = p => ({ left: p.x, top: p.y, right: p.x + CELL_W, bottom: p.y + CELL_H })
			const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top
			const targetRect = rectOf({ x, y })
			const occupant = this.positions.find(p => p.name !== name && overlaps(targetRect, rectOf(p)))

			if (occupant) {
				occupant.x = moved.x
				occupant.y = moved.y
			}
			moved.x = x
			moved.y = y
			this.savePositions()
		},

		// Used for group drags only - just sets the position, no occupant
		// swap (ambiguous once more than one item lands at once) and no
		// save (the caller saves once after placing the whole group).
		placeItemNoSwap (name, x, y) {
			const moved = this.positions.find(p => p.name === name)
			if (!moved) return
			moved.x = x
			moved.y = y
		},

		openFolder (folder) {
			this.activeFolder = folder
			this.showFolderModal = true
		},

		createFolderPrompt () {
			this.$buefy.dialog.prompt({
				message: this.$t('Folder name'),
				inputAttrs: { placeholder: this.$t('Folder name'), required: true },
				trapFocus: true,
				onConfirm: async (name) => {
					await this.createFolder(name)
					this.getList()
				}
			})
		},

		renameFolderPrompt (folder) {
			this.$buefy.dialog.prompt({
				message: this.$t('Folder name'),
				inputAttrs: { placeholder: this.$t('Folder name'), value: folder.name, required: true },
				trapFocus: true,
				onConfirm: async (name) => {
					await this.renameFolder(folder.id, name)
					this.getList()
				}
			})
		},

		deleteFolderConfirm (folder) {
			this.$buefy.dialog.confirm({
				message: this.$t('Delete this folder? Apps inside it are not affected.'),
				onConfirm: async () => {
					await this.deleteFolder(folder.id)
					if (this.activeFolder && this.activeFolder.id === folder.id) {
						this.showFolderModal = false
						this.activeFolder = null
					}
					this.getList()
				}
			})
		},

		async addToFolderPrompt (item) {
			this.addToFolderItem = item
			this.addToFolderChoices = await this.getFolders()
			this.showAddToFolderModal = true
		},

		async handleAddToFolderConfirm (name) {
			let folder = this.addToFolderChoices.find(f => f.name === name)
			if (!folder) {
				folder = await this.createFolder(name)
			}
			await this.addAppToFolder(this.addToFolderItem.name, folder.id)
			this.getList()
		},

		openFolderIconEditor (folder) {
			this.folderIconEditTarget = folder
			this.showFolderIconEditor = true
		},

		handleFolderIconEdited ({ dataUrl, radius }) {
			this.setFolderIcon(this.folderIconEditTarget.id, dataUrl, radius).then(() => this.getList())
		},

		handleRemoveFromFolder ({ item, folderId }) {
			this.removeAppFromFolder(item.name, folderId).then(() => this.getList())
		},

		// Opens as a real desktop window (movable, no dimmed backdrop)
		// rather than a floating modal - LegacyAppEditPanel saves its own
		// changes and emits RELOAD_APP_LIST itself.
		async openLegacyEditModal (item) {
			const override = await this.getLegacyAppOverride(item.name)
			const displayName = (item.title && ice_i18n(item.title)) || item.name
			this.$store.commit('OPEN_WINDOW', {
				id: `edit-app-${item.name}`,
				title: `${this.$t('Edit')} - ${displayName}`,
				component: 'LegacyAppEditPanel',
				props: { item, override },
				width: 640,
				height: 560
			})
		},

		/**
		 * @description: Show Install Panel Programmatic
		 * @return {*} void
		 */
		async showInstall (storeId = 0, mode = '') {
			if (mode === 'custom') {
				this.$messageBus('apps_custominstall')
			}
			this.$store.commit('OPEN_WINDOW', {
				id: 'appstore',
				title: this.$t('App Store'),
				component: 'AppStoreApp',
				width: 1040,
				height: 720,
				props: {
					storeId: storeId,
					initialMode: mode
				}
			})
		},

		/**
		 * @description: Show Settings Panel Programmatic
		 * @param {Object} {id:String,status:String }
		 * @param {Boolean} isCasa
		 * @return {*}
		 */
		async showConfigPanel (item, isCasa) {
			let name = item.name
			this.$messageBus('appsexsiting_open', name)
			try {
				if (item?.app_type === 'LinkApp') {
					await this.showExternalLinkPanel(item)
					return
				}
				const networks = await this.$api.container.getNetworks()
				const memory = this.$store.state.hardwareInfo.mem
				const configData = {
					networks: networks.data.data,
					memory: memory
				}
				const ret = await this.$openAPI.appManagement.compose.myComposeApp(name, {
					headers: {
						'content-type': 'application/yaml',
						accept: 'application/yaml'
					}
				})
				this.$buefy.modal.open({
					parent: this,
					component: AppPanel,
					hasModalCard: true,
					customClass: '',
					trapFocus: true,
					canCancel: [''],
					scroll: 'keep',
					animation: 'zoom-in',
					events: {
						updateState: () => {
							this.getList()
						}
					},
					props: {
						id: name,
						state: 'update',
						isCasa: isCasa,
						// 区分 terminal
						runningStatus: item.status,
						configData: configData,
						// settingData: ret.data,
						settingComposeData: ret.data
					}
				})
			} catch (e) {
				console.error(e)
			}
		},

		async showContainerPanel (item) {
			this.$messageBus('appsexsiting_open', item.name)
			let id = item.name
			const networks = await this.$api.container.getNetworks()
			const memory = this.$store.state.hardwareInfo.mem
			const configData = {
				networks: networks.data.data,
				memory: memory
			}
			const ret = await this.$api.container.getInfo(id)
			this.$buefy.modal.open({
				parent: this,
				component: AppPanel,
				hasModalCard: true,
				customClass: '',
				trapFocus: true,
				canCancel: [''],
				scroll: 'keep',
				animation: 'zoom-in',
				events: {
					updateState: () => {
						this.getList()
					}
				},
				props: {
					id: id,
					state: 'update',
					isCasa: false,
					runningStatus: item.status,
					configData: configData,
					settingData: ret.data.data
				}
			})
		},

		async showExternalLinkPanel (item = {}) {
			this.$buefy.modal.open({
				parent: this,
				component: ExternalLinkPanel,
				hasModalCard: true,
				customClass: '',
				trapFocus: true,
				canCancel: [''],
				scroll: 'keep',
				animation: 'zoom-in',
				events: {
					updateState: () => {
						this.$messageBus('apps_external')
						this.getList().then(() => {
							this.scrollToNewApp()
						})
					}
				},
				props: {
					linkName: item.name,
					linkHost: item.hostname,
					linkIcon: item.icon
				}
			})
		},

		scrollToNewApp () {
			// business :: scroll to last position
			let name = last(this.newAppIds)
			let showEl = document.getElementById('app-' + name)
			showEl?.scrollIntoView({ behavior: 'smooth', block: 'end' })
		},

		messageBusToast (message, type) {
			let duration = 5000
			this.$buefy.toast.open({
				message: message,
				duration,
				type
			})
		}
	},
	sockets: {
		'app:install-end' () {
			this.getList().then(() => {
				this.scrollToNewApp()
			})
		},
		'app:install-error' () {
			this.getList().then(() => {
				this.scrollToNewApp()
			})
		},
		'app:uninstall-end' () {
			this.getList()
		},
		'app:apply-changes-error' (res) {
			// toast info
			this.messageBusToast(res.Properties.message, 'is-danger')
		},
		'app:apply-changes-end' (res) {
			let languages = JSON.parse(res.Properties['app:title'])
			const title = ice_i18n(languages)
			// toast info
			this.messageBusToast(title + ' is OK', 'is-success')

			// business :: Tagging of new app / scrollIntoView
			this.addIdToSessionStorage(res.Properties['app:name'])

			this.getList().then(() => {
				this.scrollToNewApp()
			})
		},
		/**
		 * @description: Update App Version
		 * @param {Object} data
		 * @return {void}
		 */
		'app:update-end' (data) {
			if (data.Properties['docker:image:updated'] === 'true') {
				// business :: Tagging of new app / scrollIntoView
				this.addIdToSessionStorage(data.Properties['app:name'])

				this.$buefy.toast.open({
					message: this.$t(`{name} has been updated to the latest version!`, {
						name: data.Properties.name
					}),
					type: 'is-success'
				})
				this.getList().then(() => {
					this.scrollToNewApp()
				})
			}
		},
		'app:update-error' (data) {
			if (data.Properties.cid === this.item.id) {
				this.isUpdating = false
				this.$buefy.toast.open({
					message: this.$t(data.Properties['error']),
					type: 'is-danger'
				})
			}
		}
	}
}
</script>

<style lang="scss" scoped>
// Loading placeholders only - not draggable, so a plain wrap is fine.
.app-list-skeleton {
	display: flex;
	flex-wrap: wrap;
	gap: 0.5rem;
}

.app-canvas {
	position: relative;
}

.app-slot {
	position: absolute;
	top: 0;
	left: 0;
	cursor: grab;
	transition: transform 0.15s ease;
	user-select: none;
	-webkit-user-drag: none;

	&.dragging {
		cursor: grabbing;
		transition: none;
		opacity: 0.85;
		// Without this, this element (which tracks the cursor 1:1 while
		// dragging) is always what document.elementFromPoint() hits,
		// hiding whatever's actually underneath the cursor - including a
		// folder card, which is how "drag app onto folder to add" detects
		// its target.
		pointer-events: none;
	}

	img {
		-webkit-user-drag: none;
		user-drag: none;
	}
}

// Faint dot grid, visible only while something is being dragged - shows
// the underlying snap grid so drops land predictably.
.drop-grid {
	position: absolute;
	inset: 0;
	pointer-events: none;
	opacity: 0;
	transition: opacity 0.15s ease;
	background-image: radial-gradient(rgba(255, 255, 255, 0.35) 1.5px, transparent 1.5px);
	background-size: 20px 20px; // matches SNAP in the script block
}

.app-canvas.is-dragging .drop-grid {
	opacity: 1;
}

// Snapped landing-spot placeholder - shows exactly where the dragged
// tile will land on drop, separately from the tile itself (which
// follows the raw, unsnapped cursor position).
.drop-preview {
	position: absolute;
	top: 0;
	left: 0;
	border: 2px dashed rgba(255, 255, 255, 0.6);
	border-radius: 12px;
	background: rgba(255, 255, 255, 0.08);
	pointer-events: none;
	transition: transform 0.05s linear;
	z-index: 2;
}

// Rubber-band selection rectangle, like any real desktop icon grid.
.marquee-box {
	position: absolute;
	top: 0;
	left: 0;
	border: 1px solid rgba(80, 160, 255, 0.8);
	background: rgba(80, 160, 255, 0.15);
	pointer-events: none;
	z-index: 40;
}

.app-slot.selected {
	border-radius: 12px;
	background: rgba(80, 160, 255, 0.18);
	box-shadow: 0 0 0 1px rgba(80, 160, 255, 0.6) inset;
}
</style>
