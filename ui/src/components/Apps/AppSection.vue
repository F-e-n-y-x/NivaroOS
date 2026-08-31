<template>
	<div class="desktop-app-section">
		<!-- App List Start -->
		<div v-if="isLoading" class="app-list-skeleton">
			<div v-for="index in skCount" :id="'app-' + index" :key="'app-' + index">
				<app-card-skeleton :index="index"></app-card-skeleton>
			</div>
		</div>
		<div v-else ref="canvas" class="app-canvas contextmenu-canvas" :class="{ 'is-dragging': !!draggingName }"
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
				
				<!-- Live Downloading / Installing App Tile -->
				<div v-else-if="item.app_type === 'installing'" class="installing-app-slot common-card is-flex is-align-items-center is-justify-content-center">
					<div class="cards-content has-text-centered is-flex is-justify-content-center is-flex-direction-column" style="padding:6px 4px 4px;width:100%;height:100%">
						<div class="is-flex is-justify-content-center is-relative">
							<div class="installing-icon-box">
								<img :src="item.icon || defaultAppIcon" class="is-52x52 installing-icon" @error="onIconError" />
								<div class="installing-ring-wrap">
									<svg class="progress-ring-svg" viewBox="0 0 72 72">
										<circle class="ring-track" cx="36" cy="36" r="32"></circle>
										<circle
											class="ring-fill"
											cx="36"
											cy="36"
											r="32"
											:style="{ strokeDashoffset: 201 - (201 * (item.progress || 10)) / 100 }"
										></circle>
									</svg>
								</div>
							</div>
						</div>
						<p class="app-label one-line" style="margin-top:4px">
							<span class="one-line installing-title">{{ item.title || item.name }}</span>
						</p>
						<span class="installing-badge">{{ item.progress ? (item.progress + '%') : $t('Installing...') }}</span>
					</div>
				</div>

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
import defaultAppIcon from '@/assets/img/app/default.svg'

const SYNCTHING_STORE_ID = 74

const builtInApplications = [
	{
		id: '1',
		name: 'App Store',
		title: { en_us: 'App Store' },
		icon: require(`@/assets/img/app/appstore.png`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '2',
		name: 'Files',
		title: { en_us: 'Files' },
		icon: require(`@/assets/img/app/files.svg`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '3',
		name: 'Settings',
		title: { en_us: 'Settings' },
		icon: require(`@/assets/img/app/settings.png`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '4',
		name: 'Terminal',
		title: { en_us: 'Terminal' },
		icon: require(`@/assets/img/app/terminal.png`),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '5',
		name: 'VMs',
		title: { en_us: 'VMs' },
		icon: require(`@/assets/img/app/vm-manager.png`),
		status: 'running',
		app_type: 'system'
	}
]

const orderConfig = 'app_order'
const CELL_W = 88
const CELL_H = 96
const GAP = 8
const SNAP = CELL_W + GAP

export default {
	name: 'app-section',
	components: {
		AppCard,
		AppCardSkeleton,
		FolderCard,
		FolderModal,
		AddToFolderPanel,
		IconEditorModal
	},
	mixins: [business_ShowNewAppTag, business_LinkApp, business_Folders, business_LegacyAppOverrides],
	data() {
		return {
			defaultAppIcon,
			appList: [],
			installingApps: [],
			positions: [],
			isLoading: true,
			skCount: 6,
			activeFolder: null,
			showFolderModal: false,
			showAddToFolderModal: false,
			addToFolderItem: null,
			addToFolderChoices: [],
			showFolderIconEditor: false,
			folderIconEditTarget: null,
			draggingName: null,
			dragGhost: null,
			dragOverFolderId: null,
			selectedNames: [],
			draggingGroup: null,
			groupGhosts: null,
			marquee: null,
			justDragged: false,
			retryCount: 0,
			appListErrorMessage: ''
		}
	},
	provide() {
		return {
			openAppStore: this.showInstall
		}
	},
	computed: {
		exsitingAppsShow() {
			return this.$store.state.existingAppsSwitch
		},
		combinedAppList() {
			const list = [...this.appList]
			this.installingApps.forEach(inst => {
				if (!list.some(a => a.name === inst.name)) {
					list.push(inst)
				}
			})
			return list
		},
		positionedAppList() {
			return this.positions
				.map(p => {
					const item = this.combinedAppList.find(i => i.name === p.name)
					return item ? { ...item, x: p.x, y: p.y } : null
				})
				.filter(Boolean)
		},
		canvasHeight() {
			const maxY = this.positions.reduce((max, p) => Math.max(max, p.y), 0)
			return maxY + CELL_H + 200
		},
		dragPreviewStyle() {
			const isGroupDrag = this.draggingGroup && this.draggingGroup.length > 1
			if (!this.draggingName || !this.dragGhost || this.dragOverFolderId || isGroupDrag) return null
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : window.innerWidth
			const canvasHeight = this.$refs.canvas ? this.$refs.canvas.clientHeight : window.innerHeight
			const maxLeft = Math.max(0, canvasWidth - CELL_W)
			const maxTop = Math.max(0, canvasHeight - CELL_H)
			const ROW_H = CELL_H + GAP
			const x = Math.min(maxLeft, Math.max(0, Math.round(this.dragGhost.left / SNAP) * SNAP))
			const y = Math.min(maxTop, Math.max(0, Math.round(this.dragGhost.top / ROW_H) * ROW_H))
			return {
				transform: `translate(${x}px, ${y}px)`,
				width: CELL_W + 'px',
				height: CELL_H + 'px'
			}
		},
		marqueeStyle() {
			if (!this.marquee) return null
			return {
				transform: `translate(${this.marquee.x}px, ${this.marquee.y}px)`,
				width: this.marquee.width + 'px',
				height: this.marquee.height + 'px'
			}
		}
	},
	created() {
		this.getList()
		this.$EventBus.$on(events.OPEN_APP_STORE_AND_GOTO_SYNCTHING, () => {
			this.showInstall(SYNCTHING_STORE_ID)
		})

		this.$EventBus.$on(events.RELOAD_APP_LIST, () => {
			this.getList()
		})

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

		// FolderWindow events (folder open in windowed mode)
		this.$EventBus.$on(events.REMOVE_FROM_FOLDER, ({ item, folderId }) => {
			this.handleRemoveFromFolder({ item, folderId })
		})
		this.$EventBus.$on(events.GET_APP_LIST, () => {
			this.getList()
		})
		this.$EventBus.$on(events.SHOW_CONFIG_PANEL, (item) => {
			this.showConfigPanel(item)
		})
		this.$EventBus.$on(events.SHOW_CONTAINER_PANEL, (item) => {
			this.showContainerPanel(item)
		})

		this.ListRefreshTimer = setInterval(() => {
			this.getList()
		}, 8000)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.OPEN_APP_STORE_AND_GOTO_SYNCTHING)
		this.$EventBus.$off(events.SHOW_CUSTOM_INSTALL)
		this.$EventBus.$off(events.SHOW_EXTERNAL_LINK_PANEL)
		this.$EventBus.$off(events.SHOW_CREATE_FOLDER_PROMPT)
		this.$EventBus.$off(events.ARRANGE_APPS)
		this.$EventBus.$off(events.REMOVE_FROM_FOLDER)
		this.$EventBus.$off(events.GET_APP_LIST)
		this.$EventBus.$off(events.SHOW_CONFIG_PANEL)
		this.$EventBus.$off(events.SHOW_CONTAINER_PANEL)
		window.removeEventListener('resize', this.getSkCount)
		clearInterval(this.ListRefreshTimer)
	},
	mounted() {
		window.addEventListener('resize', this.getSkCount)
		this.getSkCount()
	},
	methods: {
		onIconError(e) {
			e.target.src = defaultAppIcon
		},
		maxRowsPerCol() {
			const canvasHeight = this.$refs.canvas ? this.$refs.canvas.clientHeight : (window.innerHeight - 120)
			const available = Math.max(CELL_H, canvasHeight - 16)
			return Math.max(1, Math.floor(available / (CELL_H + GAP)))
		},

		getSkCount() {
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

		async getList() {
			try {
				const orgAppList = await this.$openAPI.appGrid.getAppGrid().then(res => res.data.data || [])
				const legacyOverrides = await this.getLegacyAppOverrides()
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
					item.icon = item.icon || require(`@/assets/img/app/default.svg`)
					applyOverride(item)
				})

				builtInApplications.forEach(item => {
					applyOverride(item)
				})

				let linkAppList = await this.getLinkAppList()
				linkAppList.forEach(item => {
					item.icon = item.icon || require(`@/assets/img/app/default.svg`)
					applyOverride(item)
				})

				let allApps = concat(builtInApplications, orgAppList, linkAppList)

				const folders = await this.getFolders()
				const folderIdByAppName = {}
				folders.forEach(f => {
					f.appNames.forEach(n => { folderIdByAppName[n] = f.id })
				})
				const ungrouped = allApps.filter(item => !folderIdByAppName[item.name])
				const folderPseudoItems = folders.map(f => ({
					name: f.id,
					app_type: 'folder',
					folderData: {
						...f,
						apps: allApps.filter(item => folderIdByAppName[item.name] === f.id)
					}
				}))
				allApps = concat(folderPseudoItems, ungrouped)

				if (this.activeFolder) {
					const refreshed = folderPseudoItems.find(f => f.name === this.activeFolder.id)
					this.activeFolder = refreshed ? refreshed.folderData : null
				}

				// Sync any open FolderWindow windows with fresh folder data
				folderPseudoItems.forEach(f => {
					const winId = `folder-${f.folderData.id}`
					const win = this.$store.state.windows.find(w => w.id === winId)
					if (win) {
						this.$store.commit('OPEN_WINDOW', { id: winId, title: f.folderData.name, component: 'FolderWindow', props: { folder: f.folderData } })
					}
				})

				this.appList = allApps

				const savedPositions = await this.$api.users
					.getCustomStorage(orderConfig)
					.then(res => (Array.isArray(res.data.data) ? res.data.data : []))

				const reconciled = this.reconcileAppPositions(this.combinedAppList, savedPositions)
				this.positions = reconciled
				if (!isEqual(savedPositions, reconciled)) {
					this.savePositions()
				}

				this.isLoading = false
				this.retryCount = 0
				this.appListErrorMessage = ''
			} catch (error) {
				console.error(error)
				this.isLoading = false
			}
		},

		reconcileAppPositions(appList, saved) {
			const knownNames = appList.map(i => i.name)
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : (window.innerWidth - 360)
			const canvasHeight = this.$refs.canvas ? this.$refs.canvas.clientHeight : (window.innerHeight - 120)
			const maxRows = this.maxRowsPerCol()

			const isOnCanvas = p => (
				Number.isInteger(p.x) &&
				Number.isInteger(p.y) &&
				p.x >= 0 &&
				p.x <= Math.max(0, canvasWidth - CELL_W) &&
				p.y >= 0 &&
				p.y <= Math.max(0, canvasHeight - CELL_H)
			)

			const placedRects = []
			const rectOf = p => ({ left: p.x, top: p.y, right: p.x + CELL_W, bottom: p.y + CELL_H })
			const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top

			const kept = []
			for (const p of saved) {
				if (knownNames.includes(p.name) && isOnCanvas(p) && !kept.some(k => k.name === p.name)) {
					const rect = rectOf(p)
					if (!placedRects.some(r => overlaps(rect, r))) {
						placedRects.push(rect)
						kept.push(p)
					}
				}
			}

			const keptNames = kept.map(p => p.name)

			const firstFreeCell = () => {
				for (let col = 0; ; col++) {
					for (let row = 0; row < maxRows; row++) {
						const x = col * SNAP
						const y = row * (CELL_H + GAP)
						const rect = rectOf({ x, y })
						if (!placedRects.some(r => overlaps(rect, r))) {
							placedRects.push(rect)
							return { x, y }
						}
					}
				}
			}

			const newItems = appList
				.filter(i => !keptNames.includes(i.name))
				.map(i => ({ name: i.name, ...firstFreeCell() }))

			return kept.concat(newItems)
		},

		savePositions() {
			this.$api.users.setCustomStorage(orderConfig, this.positions).then(res => {
				if (res.data.success === 200 && Array.isArray(res.data.data)) {
					this.positions = res.data.data
				}
			})
		},

		slotStyle(item) {
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
				height: CELL_H + 'px',
				transform: `translate3d(${left}px, ${top}px, 0)`,
				zIndex: (isDragging || isGroupMember) ? 50 : 1
			}
		},

		startMarquee(e) {
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

		swallowClickAfterDrag(e) {
			if (this.justDragged) {
				e.stopPropagation()
				e.preventDefault()
				this.justDragged = false
			}
		},

		startDrag(item, e) {
			if (e.button !== 0 || this.$store.state.isMobile || item.app_type === 'installing') return

			const isGroupDrag = this.selectedNames.includes(item.name) && this.selectedNames.length > 1
			const groupNames = isGroupDrag ? this.selectedNames : [item.name]
			if (!isGroupDrag) {
				this.selectedNames = [item.name]
			}

			const startMouseX = e.clientX
			const startMouseY = e.clientY
			const startItem = this.positions.find(p => p.name === item.name)
			if (!startItem) return

			const initialOffsets = {}
			groupNames.forEach(name => {
				const p = this.positions.find(pos => pos.name === name)
				if (p) initialOffsets[name] = { dx: p.x - startItem.x, dy: p.y - startItem.y }
			})

			let dragged = false

			const onMove = moveEvent => {
				const deltaX = moveEvent.clientX - startMouseX
				const deltaY = moveEvent.clientY - startMouseY
				if (!dragged && Math.hypot(deltaX, deltaY) > 4) {
					dragged = true
					this.draggingName = item.name
					this.draggingGroup = isGroupDrag ? groupNames : null
				}
				if (!dragged) return
				moveEvent.preventDefault()

				const curLeft = Math.max(0, startItem.x + deltaX)
				const curTop = Math.max(0, startItem.y + deltaY)
				this.dragGhost = { left: curLeft, top: curTop }

				if (isGroupDrag) {
					const ghosts = {}
					groupNames.forEach(name => {
						const off = initialOffsets[name] || { dx: 0, dy: 0 }
						ghosts[name] = { left: Math.max(0, curLeft + off.dx), top: Math.max(0, curTop + off.dy) }
					})
					this.groupGhosts = ghosts
				}

				if (item.app_type !== 'folder' && !isGroupDrag) {
					const el = document.elementFromPoint(moveEvent.clientX, moveEvent.clientY)
					const folderEl = el && el.closest('.folder-card')
					if (folderEl) {
						const slotEl = folderEl.closest('[data-app-name]')
						this.dragOverFolderId = slotEl ? slotEl.getAttribute('data-app-name') : null
					} else {
						this.dragOverFolderId = null
					}
				}
			}

			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (dragged) {
					this.justDragged = true
					if (this.dragOverFolderId && item.app_type !== 'folder') {
						const targetFolderId = this.dragOverFolderId
						this.draggingName = null
						this.dragGhost = null
						this.draggingGroup = null
						this.groupGhosts = null
						this.dragOverFolderId = null
						this.addAppToFolder(item.name, targetFolderId).then(() => this.getList())
						return
					}
					if (isGroupDrag) {
						groupNames.forEach(name => {
							const ghost = this.groupGhosts && this.groupGhosts[name]
							if (ghost) this.placeItem(name, ghost.left, ghost.top)
						})
					} else {
						this.placeItem(item.name, this.dragGhost.left, this.dragGhost.top)
					}
					this.savePositions()
				}
				this.draggingName = null
				this.dragGhost = null
				this.draggingGroup = null
				this.groupGhosts = null
				this.dragOverFolderId = null
			}

			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},

		placeItem(name, rawX, rawY) {
			const canvasWidth = this.$refs.canvas ? this.$refs.canvas.clientWidth : window.innerWidth
			const canvasHeight = this.$refs.canvas ? this.$refs.canvas.clientHeight : window.innerHeight
			const maxLeft = Math.max(0, canvasWidth - CELL_W)
			const maxTop = Math.max(0, canvasHeight - CELL_H)
			const ROW_H = CELL_H + GAP
			const targetX = Math.min(maxLeft, Math.max(0, Math.round(rawX / SNAP) * SNAP))
			const targetY = Math.min(maxTop, Math.max(0, Math.round(rawY / ROW_H) * ROW_H))

			const others = this.positions.filter(p => p.name !== name)
			const rectOf = p => ({ left: p.x, top: p.y, right: p.x + CELL_W, bottom: p.y + CELL_H })
			const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top
			const targetRect = rectOf({ x: targetX, y: targetY })

			const occupied = others.some(p => overlaps(targetRect, rectOf(p)))
			let finalX = targetX
			let finalY = targetY

			if (occupied) {
				const maxRows = this.maxRowsPerCol()
				const targetCol = Math.round(targetX / SNAP)
				let placed = false
				for (let dist = 1; dist < 50 && !placed; dist++) {
					for (let dc = -dist; dc <= dist && !placed; dc++) {
						const col = targetCol + dc
						if (col < 0) continue
						const testX = col * SNAP
						if (testX > maxLeft) continue
						for (let row = 0; row < maxRows; row++) {
							const testY = row * ROW_H
							const testRect = rectOf({ x: testX, y: testY })
							if (!others.some(p => overlaps(testRect, rectOf(p)))) {
								finalX = testX
								finalY = testY
								placed = true
								break
							}
						}
					}
				}
			}

			this.positions = others.concat([{ name, x: finalX, y: finalY }])
		},

		arrangeApps() {
			const maxRows = this.maxRowsPerCol()
			const order = this.positions.map(p => p.name)
			const arranged = []
			let col = 0
			let row = 0
			order.forEach(name => {
				arranged.push({ name, x: col * SNAP, y: row * (CELL_H + GAP) })
				row++
				if (row >= maxRows) {
					row = 0
					col++
				}
			})
			this.positions = arranged
			this.savePositions()
		},

		openFolder(folder) {
			// Open each folder as its own resizable window rather than a
			// full-page modal overlay. Each folder gets a stable window ID
			// (folder-<id>) so re-clicking the same folder just focuses/
			// un-minimizes its existing window instead of opening a duplicate.
			this.$store.commit('OPEN_WINDOW', {
				id: `folder-${folder.id}`,
				title: folder.name,
				component: 'FolderWindow',
				props: { folder },
				width: 520,
				height: 380
			})
		},

		async createFolderPrompt() {
			this.$buefy.dialog.prompt({
				message: this.$t('New folder name:'),
				inputAttrs: { placeholder: this.$t('Folder'), maxlength: 30 },
				trapFocus: true,
				confirmText: this.$t('Create'),
				cancelText: this.$t('Cancel'),
				onConfirm: async (name) => {
					await this.createFolder(name)
					this.getList()
				}
			})
		},

		renameFolderPrompt(folder) {
			this.$buefy.dialog.prompt({
				message: this.$t('Rename folder:'),
				inputAttrs: { value: folder.name, maxlength: 30 },
				trapFocus: true,
				confirmText: this.$t('Save'),
				cancelText: this.$t('Cancel'),
				onConfirm: async (name) => {
					await this.renameFolder(folder.id, name)
					this.getList()
				}
			})
		},

		deleteFolderConfirm(folder) {
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

		async addToFolderPrompt(item) {
			this.addToFolderItem = item
			this.addToFolderChoices = await this.getFolders()
			this.showAddToFolderModal = true
		},

		async handleAddToFolderConfirm(name) {
			let folder = this.addToFolderChoices.find(f => f.name === name)
			if (!folder) {
				folder = await this.createFolder(name)
			}
			await this.addAppToFolder(this.addToFolderItem.name, folder.id)
			this.getList()
		},

		openFolderIconEditor(folder) {
			this.folderIconEditTarget = folder
			this.showFolderIconEditor = true
		},

		handleFolderIconEdited({ dataUrl, radius }) {
			this.setFolderIcon(this.folderIconEditTarget.id, dataUrl, radius).then(() => this.getList())
		},

		handleRemoveFromFolder({ item, folderId }) {
			this.removeAppFromFolder(item.name, folderId).then(() => this.getList())
		},

		async openLegacyEditModal(item) {
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

		async showInstall(storeId = 0, mode = '') {
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

		/* Windowed Container Customizer / Config */
		async showConfigPanel(item) {
			const name = item.name
			this.$messageBus('appsexsiting_open', name)
			try {
				if (item?.app_type === 'LinkApp') {
					await this.showExternalLinkPanel(item)
					return
				}
				const displayName = (item.title && ice_i18n(item.title)) || name
				this.$store.commit('OPEN_WINDOW', {
					id: 'appstore',
					title: `${this.$t('App Store')} - ${displayName}`,
					component: 'AppStoreApp',
					width: 1040,
					height: 720,
					props: {
						initialAppName: name,
						initialMode: 'edit'
					}
				})
			} catch (e) {
				console.error('Failed to open app config', e)
			}
		},

		async showContainerPanel(item) {
			await this.showConfigPanel(item)
		},

		async showExternalLinkPanel(item = {}) {
			this.$buefy.modal.open({
				parent: this,
				component: ExternalLinkPanel,
				hasModalCard: true,
				trapFocus: true,
				canCancel: ['escape', 'x', 'outside'],
				scroll: 'keep',
				animation: 'zoom-in',
				events: {
					updateState: () => {
						this.$messageBus('apps_external')
						this.getList()
					}
				},
				props: {
					linkName: item.name,
					linkHost: item.hostname,
					linkIcon: item.icon
				}
			})
		},

		parseTitle(raw) {
			if (!raw) return ''
			try {
				if (typeof raw === 'string' && (raw.startsWith('{') || raw.startsWith('['))) {
					return ice_i18n(JSON.parse(raw))
				}
				return ice_i18n(raw)
			} catch (e) {
				return String(raw)
			}
		}
	},
	sockets: {
		'app:install-begin'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const title = this.parseTitle(props['app:title']) || name
			const icon = props['app:icon'] || defaultAppIcon

			let existing = this.installingApps.find(a => a.name === name)
			if (!existing) {
				existing = {
					name,
					title,
					icon,
					app_type: 'installing',
					progress: 5
				}
				this.installingApps.push(existing)
			}
			const reconciled = this.reconcileAppPositions(this.combinedAppList, this.positions)
			this.positions = reconciled
		},

		'app:install-progress'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const rawProgress = props['app:progress'] || props.progress || '0'
			const num = parseInt(rawProgress, 10)

			let existing = this.installingApps.find(a => a.name === name)
			if (existing && !isNaN(num)) {
				existing.progress = Math.min(99, Math.max(existing.progress, num))
			}
		},

		'app:install-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			this.installingApps = this.installingApps.filter(a => a.name !== name)
			this.getList()
		},

		'app:install-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			this.installingApps = this.installingApps.filter(a => a.name !== name)
			this.getList()
		},

		'app:uninstall-end'() {
			this.getList()
		},

		'app:apply-changes-end'() {
			this.getList()
		}
	}
}
</script>

<style lang="scss" scoped>
.desktop-app-section {
	width: 100%;
	height: 100%;
	position: relative;
	overflow: hidden;
}

.app-list-skeleton {
	display: flex;
	flex-wrap: wrap;
	gap: 1rem;
	padding: 0.5rem;
}

.app-canvas {
	position: absolute;
	inset: 0;
	width: 100%;
	height: 100%;
	overflow: hidden;
}

.app-slot {
	position: absolute;
	top: 0;
	left: 0;
	width: 88px;
	height: 96px;
	cursor: pointer;
	user-select: none;
	-webkit-user-select: none;
	-webkit-user-drag: none;
	transition: transform 0.15s cubic-bezier(0.2, 0, 0, 1);
	display: flex;
	align-items: center;
	justify-content: center;
	border-radius: 12px;

	&:hover {
		background: rgba(255, 255, 255, 0.08);
	}

	&.dragging {
		cursor: grabbing;
		transition: none;
		opacity: 0.85;
		pointer-events: none;
	}

	&.selected {
		border-radius: 12px;
		background: rgba(59, 130, 246, 0.25);
		box-shadow: 0 0 0 1px rgba(147, 197, 253, 0.6) inset;
	}

	img {
		-webkit-user-drag: none;
		user-drag: none;
	}
}

.drop-grid {
	position: absolute;
	inset: 0;
	pointer-events: none;
	opacity: 0;
	transition: opacity 0.15s ease;
	background-image: radial-gradient(rgba(255, 255, 255, 0.35) 1.5px, transparent 1.5px);
	background-size: 20px 20px;
}

.app-canvas.is-dragging .drop-grid {
	opacity: 1;
}

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

.marquee-box {
	position: absolute;
	top: 0;
	left: 0;
	border: 1px solid rgba(80, 160, 255, 0.8);
	background: rgba(80, 160, 255, 0.15);
	pointer-events: none;
	z-index: 40;
}

.installing-app-slot {
	pointer-events: none;
	opacity: 0.85;
	width: 100%;
	height: 100%;
}

.installing-icon-box {
	position: relative;
	width: 52px;
	height: 52px;
	display: flex;
	align-items: center;
	justify-content: center;
}

.installing-icon {
	border-radius: 0.75rem;
	padding: 3px;
	background: rgba(255, 255, 255, 0.2);
	backdrop-filter: blur(8px);
	border: 1px solid rgba(255, 255, 255, 0.3);
	filter: grayscale(40%);
}

.installing-ring-wrap {
	position: absolute;
	inset: -4px;
	width: 60px;
	height: 60px;
	pointer-events: none;
}

.progress-ring-svg {
	width: 100%;
	height: 100%;
	transform: rotate(-90deg);
}

.ring-track {
	fill: none;
	stroke: rgba(255, 255, 255, 0.2);
	stroke-width: 3;
}

.ring-fill {
	fill: none;
	stroke: #38bdf8;
	stroke-width: 3;
	stroke-linecap: round;
	stroke-dasharray: 201;
	transition: stroke-dashoffset 0.3s ease;
}

.installing-title {
	font-size: 0.8125rem;
	font-weight: 600;
	color: #ffffff;
	text-shadow: 0 1px 3px rgba(0, 0, 0, 0.8);
}

.installing-badge {
	display: inline-block;
	font-size: 0.6875rem;
	font-weight: 700;
	color: #38bdf8;
	background: rgba(15, 23, 42, 0.7);
	padding: 0.1rem 0.4rem;
	border-radius: 9999px;
	margin-top: 0.25rem;
	border: 1px solid rgba(56, 189, 248, 0.3);
}
</style>
