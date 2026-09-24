<template>
	<div class="desktop-app-section">
		<!-- Skeleton App List on Desktop Canvas -->
		<div v-if="isLoading" class="app-canvas app-canvas-skeleton">
			<div
				v-for="sk in skeletonSlots"
				:key="'sk-' + sk.id"
				class="app-slot skeleton-slot"
				:style="sk.style"
			>
				<app-card-skeleton :delay="sk.delay"></app-card-skeleton>
			</div>
		</div>
		<div v-else ref="canvas" class="app-canvas contextmenu-canvas" :class="{ 'is-dragging': !!draggingName }"
			@mousedown.self="startMarquee">
			<div class="drop-grid"></div>
			<div v-if="dragPreviewStyle" class="drop-preview" :style="dragPreviewStyle"></div>
			<div v-if="marqueeStyle" class="marquee-box" :style="marqueeStyle"></div>
			<!-- @dragstart.prevent: the icon's <img> (via b-image) is natively
			     draggable by default in every browser - without preventing it, a
			     real mouse drag can get hijacked partway through by the browser's
			     own HTML5 image-drag instead of this component's custom
			     mousemove/mouseup tracking, which then stops receiving events
			     until the drag is cancelled - the icon needed a second, separate
			     click to actually complete the drop. -->
			<div v-for="item in positionedAppList" :key="'app-' + item.name" :id="'app-' + item.name"
				:data-app-name="item.name"
				class="app-slot" :class="{ dragging: draggingName === item.name, selected: selectedNames.includes(item.name) }" :style="slotStyle(item)"
				@mousedown="startDrag(item, $event)" @click.capture="swallowClickAfterDrag" @dragstart.prevent>
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
				<div v-else-if="item.app_type === 'installing'"
					class="installing-app-slot common-card is-flex is-align-items-center is-justify-content-center"
					:class="{ 'is-failed': item.failed }"
					:role="item.failed ? 'status' : 'progressbar'"
					:aria-label="item.failed
						? $t('Installing {name} failed: {reason}', { name: item.title || item.name, reason: item.errorMessage })
						: $t('Installing {name}', { name: item.title || item.name })"
					:aria-valuemin="item.failed ? null : 0"
					:aria-valuemax="item.failed ? null : 100"
					:aria-valuenow="item.failed ? null : (item.progress || 0)"
					:title="item.failed ? item.errorMessage : null">
					<div class="cards-content has-text-centered is-flex is-justify-content-center is-flex-direction-column" style="padding:6px 4px 4px;width:100%;height:100%">
						<div class="is-flex is-justify-content-center is-relative">
							<div class="installing-icon-box">
								<img :src="item.icon || defaultAppIcon" class="is-52x52 installing-icon" :alt="item.title || item.name || ''" @error="onIconError" />
								<div class="installing-ring-wrap">
									<svg class="progress-ring-svg" viewBox="0 0 72 72" aria-hidden="true" focusable="false">
										<circle class="ring-track" cx="36" cy="36" r="32"></circle>
										<circle
											class="ring-fill"
											cx="36"
											cy="36"
											r="32"
											:style="{ strokeDashoffset: item.failed ? 0 : 201 - (201 * (item.progress || 10)) / 100 }"
										></circle>
									</svg>
								</div>
							</div>
						</div>
						<p class="app-label one-line" style="margin-top:4px">
							<span class="one-line installing-title">{{ item.title || item.name }}</span>
						</p>
						<span v-if="item.failed" class="installing-badge is-failed">
							<i class="mdi mdi-alert-circle-outline" aria-hidden="true"></i> {{ $t('Install failed') }}
						</span>
						<span v-else class="installing-badge">{{ item.progress ? (item.progress + '%') : $t('Installing...') }}</span>
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

		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import { checkDownloadStationInstalled } from '@/utils/downloadStationInstalled'
import { checkBackupInstalled } from '@/utils/backupInstalled'
import { assetUrl } from '@/utils/assetUrl'
import AppCard from './AppCard.vue'
import AppCardSkeleton from './AppCardSkeleton.vue'
import FolderCard from './FolderCard.vue'
import concat from 'lodash/concat'
import events from '@/events/events'
import last from 'lodash/last'
import business_ShowNewAppTag from '@/mixins/app/Business_ShowNewAppTag'
import business_LinkApp from '@/mixins/app/Business_LinkApp'
import business_Folders, { parseComposeProject } from '@/mixins/app/Business_Folders'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import isEqual from 'lodash/isEqual'
import { ice_i18n } from '@/mixins/base/common-i18n'
import defaultAppIcon from '@/assets/img/app-icons/default.svg'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { apiErrorHtml } from '@/mixins/app/apiError'
import { escapeHtml } from '@/utils/escapeHtml'

// Store ids are compose project names (catalog Apps/Syncthing -> `name: syncthing`).
const SYNCTHING_STORE_ID = 'syncthing'
const LIST_REFRESH_MS = 8000
const INSTALL_FAILED_TILE_MS = 15000

const builtInApplications = [
	{
		id: '1',
		name: 'App Store',
		title: { en_us: 'App Store' },
		icon: assetUrl(require(`@/assets/img/app-icons/appstore.png`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '2',
		name: 'Files',
		title: { en_us: 'Files' },
		icon: assetUrl(require(`@/assets/img/app-icons/files.svg`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '3',
		name: 'Settings',
		title: { en_us: 'Settings' },
		icon: assetUrl(require(`@/assets/img/app-icons/settings.png`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '4',
		name: 'Terminal',
		title: { en_us: 'Terminal' },
		icon: assetUrl(require(`@/assets/img/app-icons/terminal.png`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '5',
		name: 'VMs',
		title: { en_us: 'VMs' },
		icon: assetUrl(require(`@/assets/img/app-icons/vm-manager.png`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '6',
		name: 'Host Desktop',
		title: { en_us: 'Host Desktop' },
		icon: assetUrl(require(`@/assets/img/app-icons/desktop.svg`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '7',
		name: 'Download Station',
		title: { en_us: 'Download Station' },
		icon: assetUrl(require(`@/assets/img/app-icons/download-station.svg`)),
		status: 'running',
		app_type: 'system'
	},
	{
		id: '8',
		name: 'Backup & Sync',
		title: { en_us: 'Backup & Sync' },
		icon: assetUrl(require(`@/assets/img/app-icons/backup.svg`)),
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
		FolderCard
	},
	mixins: [business_ShowNewAppTag, business_LinkApp, business_Folders, business_LegacyAppOverrides, confirmWindowMixin],
	data() {
		return {
			defaultAppIcon,
			appList: [],
			installingApps: [],
			positions: [],
			isLoading: true,
			skCount: 6,
			draggingName: null,
			dragGhost: null,
			dragOverFolderId: null,
			selectedNames: [],
			draggingGroup: null,
			groupGhosts: null,
			marquee: null,
			justDragged: false,
			retryCount: 0,
			appListErrorMessage: '',
			// Real canvas size once it's laid out; `known: false` until then
			// (hidden/zero-size canvas), in which case positions are never
			// saved - only clamped at render.
			canvasSize: { w: 0, h: 0, known: false }
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
			// Saved positions are kept as-is even when the current viewport
			// is smaller than the one they were arranged on; they're only
			// clamped (to the last visible grid cell) for display.
			const { w, h, known } = this.canvasSize
			const ROW_H = CELL_H + GAP
			const maxX = known ? Math.max(0, Math.floor(Math.max(0, w - CELL_W) / SNAP) * SNAP) : Infinity
			const maxY = known ? Math.max(0, Math.floor(Math.max(0, h - CELL_H) / ROW_H) * ROW_H) : Infinity
			return this.positions
				.map(p => {
					const item = this.combinedAppList.find(i => i.name === p.name)
					return item ? { ...item, x: Math.min(p.x, maxX), y: Math.min(p.y, maxY) } : null
				})
				.filter(Boolean)
		},
		skeletonSlots() {
			const canvasHeight = typeof window !== 'undefined' ? (window.innerHeight - 120) : 600
			const maxRows = Math.max(3, Math.floor(canvasHeight / (CELL_H + GAP)))
			const total = Math.min(12, Math.max(6, maxRows * 2))
			const slots = []
			for (let i = 0; i < total; i++) {
				const col = Math.floor(i / maxRows)
				const row = i % maxRows
				const x = col * SNAP
				const y = row * (CELL_H + GAP)
				slots.push({
					id: i,
					delay: (col * maxRows + row) * 0.08,
					style: {
						width: CELL_W + 'px',
						height: CELL_H + 'px',
						transform: `translate3d(${x}px, ${y}px, 0)`
					}
				})
			}
			return slots
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
		// Keep a reference to every handler: `$off(event)` without one would
		// strip every other component's listener for that event too.
		this.busHandlers = {
			[events.OPEN_APP_STORE_AND_GOTO_SYNCTHING]: () => this.showInstall(SYNCTHING_STORE_ID),
			[events.RELOAD_APP_LIST]: () => this.getList(),
			[events.SHOW_CUSTOM_INSTALL]: () => this.showInstall(0, 'custom'),
			[events.SHOW_EXTERNAL_LINK_PANEL]: () => this.showExternalLinkPanel(),
			[events.SHOW_CREATE_FOLDER_PROMPT]: () => this.createFolderPrompt(),
			[events.ARRANGE_APPS]: () => this.arrangeApps(),
			// FolderWindow events (folder open in windowed mode)
			[events.REMOVE_FROM_FOLDER]: (payload) => this.handleRemoveFromFolder(payload),
			[events.REMOVE_MULTIPLE_FROM_FOLDER]: (payload) => this.handleRemoveMultipleFromFolder(payload),
			[events.GET_APP_LIST]: () => this.getList(),
			[events.SHOW_CONFIG_PANEL]: (item) => this.showConfigPanel(item),
			[events.SHOW_CONTAINER_PANEL]: (item) => this.showContainerPanel(item)
		}
		Object.keys(this.busHandlers).forEach(evt => this.$EventBus.$on(evt, this.busHandlers[evt]))

		// Background refresh: skipped while the tab is hidden, and never
		// overlapping a still-running getList (see getList's single-flight).
		this.ListRefreshTimer = setInterval(() => {
			if (typeof document !== 'undefined' && document.hidden) return
			this.getList()
		}, LIST_REFRESH_MS)
		this.onVisibilityChange = () => {
			if (!document.hidden) this.getList()
		}
		document.addEventListener('visibilitychange', this.onVisibilityChange)
	},
	beforeDestroy() {
		Object.keys(this.busHandlers || {}).forEach(evt => this.$EventBus.$off(evt, this.busHandlers[evt]))
		window.removeEventListener('resize', this.getSkCount)
		document.removeEventListener('visibilitychange', this.onVisibilityChange)
		clearInterval(this.ListRefreshTimer)
		if (this.canvasObserver) this.canvasObserver.disconnect()
		Object.values(this.failedInstallTimers || {}).forEach(clearTimeout)
	},
	mounted() {
		window.addEventListener('resize', this.getSkCount)
		this.getSkCount()
	},
	methods: {
		onIconError(e) {
			e.target.src = defaultAppIcon
		},
		clearFailedInstallTimer(name) {
			if (!this.failedInstallTimers) this.failedInstallTimers = {}
			if (this.failedInstallTimers[name]) {
				clearTimeout(this.failedInstallTimers[name])
				delete this.failedInstallTimers[name]
			}
		},
		maxRowsPerCol(height) {
			const canvasHeight = height || (this.$refs.canvas && this.$refs.canvas.clientHeight) || (window.innerHeight - 120)
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

		// Single-flight: a call made while a refresh is running doesn't start
		// a second, overlapping one (whose older response could land last) -
		// it queues exactly one more run after the current one finishes.
		getList() {
			if (this.listInFlight) {
				this.listQueued = true
				return this.listInFlight
			}
			this.listInFlight = this.loadList().finally(() => {
				this.listInFlight = null
				if (this.listQueued) {
					this.listQueued = false
					this.getList()
				}
			})
			return this.listInFlight
		},

		async loadList() {
			try {
				const orgAppList = await this.$openAPI.appGrid.getAppGrid().then(res => res.data.data || [])
				let legacyOverrides
				try {
					legacyOverrides = await this.getLegacyAppOverrides()
					this.lastOverrides = legacyOverrides
				} catch (e) {
					console.error('getLegacyAppOverrides', e)
					legacyOverrides = this.lastOverrides || {}
				}
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
					item.icon = item.icon || assetUrl(require(`@/assets/img/app-icons/default.svg`))
					applyOverride(item)
				})

				// Fresh copies: never mutate the shared module-level
				// definitions (a removed override would otherwise stick).
				// Download Station and Backup & Sync are optional: only listed
				// when their service is installed.
				const [dsInstalled, backupInstalled] = await Promise.all([checkDownloadStationInstalled(), checkBackupInstalled()])
				const builtIns = builtInApplications
					.filter(item => (dsInstalled || item.name !== 'Download Station') && (backupInstalled || item.name !== 'Backup & Sync'))
					.map(item => ({ ...item, title: { ...item.title } }))
				builtIns.forEach(item => {
					applyOverride(item)
				})

				let linkAppList = []
				try {
					linkAppList = await this.getLinkAppList()
				} catch (e) {
					console.error('getLinkAppList', e)
					linkAppList = this.lastLinkAppList || []
				}
				this.lastLinkAppList = linkAppList
				linkAppList.forEach(item => {
					item.icon = item.icon || assetUrl(require(`@/assets/img/app-icons/default.svg`))
					applyOverride(item)
				})

				let allApps = concat(builtIns, orgAppList, linkAppList)

				// A failed folder read must never be followed by a folder
				// write (it would save [] over every folder) - fall back to
				// the last good copy for display only and skip the writes.
				let folders
				let foldersReadable = true
				try {
					folders = await this.getFolders()
				} catch (e) {
					console.error('getFolders', e)
					foldersReadable = false
					folders = JSON.parse(JSON.stringify(this.lastFolders || []))
				}

				// Self-heal: an app can transiently look like a stray "container"
				// for a single poll (e.g. mid-update, while Docker Compose is
				// swapping the old container out for the new one) and get
				// auto-filed into an isAutoContainerFolder bucket by the sweep
				// below. Once it's positively reclassified as a real app (v1/v2/
				// link) on a later poll, it needs to leave that bucket on its own -
				// the sweep below only ever ADDS unfiled containers, it never
				// removes one, so without this an app mis-filed by a single bad
				// poll would stay stuck in "Other Containers" (or its compose-
				// project folder) forever. Only prunes isAutoContainerFolder
				// buckets - never touches a folder the user created/placed apps
				// into themselves.
				const currentAppTypeByName = {}
				allApps.forEach(item => { currentAppTypeByName[item.name] = item.app_type })
				const hasStaleAutoFile = folders.some(f => f.isAutoContainerFolder && f.appNames.some(n => {
					const type = currentAppTypeByName[n]
					return !(type === undefined || type === 'container')
				}))
				if (foldersReadable && hasStaleAutoFile) {
					try {
						folders = await this.pruneStaleAutoFiledApps(currentAppTypeByName)
					} catch (e) {
						console.error('pruneStaleAutoFiledApps', e)
					}
				}

				const initialFolderIdByAppName = {}
				folders.forEach(f => {
					f.appNames.forEach(n => { initialFolderIdByAppName[n] = f.id })
				})

				// Auto-file containers deployed outside the App Store (Portainer/CLI
				// docker-compose, not the app's own custom-install flow) into a
				// dedicated folder per compose project instead of leaving them loose
				// on the desktop or dumped together in one bucket. Skips anything
				// already in a folder - including one the user deliberately moved
				// back out, tracked via getContainerAutoExcludes().
				const unfiledContainers = allApps.filter(item => item.app_type === 'container' && !initialFolderIdByAppName[item.name])
				if (foldersReadable && unfiledContainers.length) {
					try {
						const excludes = await this.getContainerAutoExcludes()
						const toFile = unfiledContainers.filter(item => !excludes.includes(item.name))
						if (toFile.length) {
							const groupsByProject = {}
							toFile.forEach(item => {
								const project = parseComposeProject(ice_i18n(item.title))
								const key = project || ''
								if (!groupsByProject[key]) groupsByProject[key] = { project: project || null, appNames: [] }
								groupsByProject[key].appNames.push(item.name)
							})
							folders = await this.autoFileContainerApps(Object.values(groupsByProject)) || folders
						}
					} catch (e) {
						console.error('autoFileContainerApps', e)
					}
				}
				if (foldersReadable) {
					this.lastFolders = JSON.parse(JSON.stringify(folders))
				}

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

				// Sync any open FolderWindow windows with fresh folder data -
				// only when something actually changed, so an idle poll doesn't
				// re-render (and re-persist) every open folder window.
				folderPseudoItems.forEach(f => {
					const winId = `folder-${f.folderData.id}`
					const win = this.$store.state.windows.find(w => w.id === winId)
					if (!win) return
					const sameTitle = win.title === f.folderData.name
					const sameFolder = win.props && isEqual(win.props.folder, f.folderData)
					if (!sameTitle || !sameFolder) {
						this.$store.commit('UPDATE_WINDOW_PROPS', { id: winId, title: f.folderData.name, props: { folder: f.folderData } })
					}
				})

				this.appList = allApps

				let savedPositions = null
				try {
					savedPositions = await this.$api.users
						.getCustomStorage(orderConfig)
						.then(res => (Array.isArray(res.data.data) ? res.data.data : []))
					this.lastSavedPositions = savedPositions
				} catch (e) {
					console.error('get app_order', e)
				}

				this.isLoading = false
				await this.$nextTick()
				this.syncPositions(savedPositions !== null)

				this.retryCount = 0
				this.appListErrorMessage = ''
			} catch (error) {
				console.error(error)
				this.isLoading = false
			}
		},

		measureCanvas() {
			const el = this.$refs.canvas
			if (el && el.clientWidth > 0 && el.clientHeight > 0) {
				return { w: el.clientWidth, h: el.clientHeight, known: true }
			}
			return { w: Math.max(CELL_W, window.innerWidth - 360), h: Math.max(CELL_H, window.innerHeight - 120), known: false }
		},

		ensureCanvasObserver() {
			const el = this.$refs.canvas
			if (!el || this.canvasObserver || typeof ResizeObserver === 'undefined') return
			this.canvasObserver = new ResizeObserver(() => {
				const wasKnown = this.canvasSize.known
				this.canvasSize = this.measureCanvas()
				// First time the real size is known: place any new icons for
				// the real grid (and persist that, if anything was added).
				if (!wasKnown && this.canvasSize.known) this.syncPositions(true)
			})
			this.canvasObserver.observe(el)
		},

		// Reconciles saved positions against the current app list. Only
		// persists when the set of apps changed (new/removed/overlapping
		// icons) AND the real canvas size is known - never because the
		// current viewport happens to be smaller.
		syncPositions(allowSave) {
			this.ensureCanvasObserver()
			const size = this.measureCanvas()
			this.canvasSize = size
			if (this.draggingName) return
			const saved = this.lastSavedPositions || this.positions || []
			const reconciled = this.reconcileAppPositions(this.combinedAppList, saved, size)
			this.positions = reconciled
			if (allowSave && size.known && !isEqual(saved, reconciled)) {
				this.savePositions()
			}
		},

		reconcileAppPositions(appList, saved, size) {
			const knownNames = appList.map(i => i.name)
			const maxRows = this.maxRowsPerCol((size || this.measureCanvas()).h)

			const isValid = p => (
				p &&
				Number.isInteger(p.x) &&
				Number.isInteger(p.y) &&
				p.x >= 0 &&
				p.y >= 0
			)

			const placedRects = []
			const rectOf = p => ({ left: p.x, top: p.y, right: p.x + CELL_W, bottom: p.y + CELL_H })
			const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top

			const kept = []
			for (const p of saved || []) {
				if (knownNames.includes(p.name) && isValid(p) && !kept.some(k => k.name === p.name)) {
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
			const toSave = this.positions.map(p => ({ ...p }))
			this.lastSavedPositions = toSave
			this.$api.users.setCustomStorage(orderConfig, toSave).then(res => {
				if (res.data.success === 200 && Array.isArray(res.data.data)) {
					this.positions = res.data.data
					this.lastSavedPositions = res.data.data
				}
			}).catch(e => {
				console.error('save app_order', e)
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
			// Displayed (render-clamped) coordinates, so the drag starts where
			// the icon actually is on screen.
			const startItem = this.positionedAppList.find(p => p.name === item.name)
			if (!startItem) return

			const initialOffsets = {}
			groupNames.forEach(name => {
				const p = this.positionedAppList.find(pos => pos.name === name)
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
						this.addAppToFolder(item.name, targetFolderId)
							.catch(err => this.toastFolderError(err))
							.then(() => this.getList())
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

		createFolderPrompt() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'create-folder-prompt',
				title: this.$t('New folder'),
				component: 'PromptDialogWindow',
				props: {
					isDialog: true,
					message: this.$t('New folder name:'),
					placeholder: this.$t('Folder'),
					maxlength: 30,
					confirmText: this.$t('Create'),
					cancelText: this.$t('Cancel'),
					onConfirm: async (name) => {
						const trimmed = String(name || '').trim()
						if (!trimmed) return
						try {
							await this.createFolder(trimmed)
						} catch (e) {
							this.toastFolderError(e)
						}
						this.getList()
					}
				},
				width: 380,
				height: 190
			})
		},

		renameFolderPrompt(folder) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'rename-folder-prompt',
				title: this.$t('Rename folder'),
				component: 'PromptDialogWindow',
				props: {
					isDialog: true,
					message: this.$t('Rename folder:'),
					initialValue: folder.name,
					maxlength: 30,
					confirmText: this.$t('Save'),
					cancelText: this.$t('Cancel'),
					onConfirm: async (name) => {
						const trimmed = String(name || '').trim()
						if (!trimmed) return
						try {
							await this.renameFolder(folder.id, trimmed)
						} catch (e) {
							this.toastFolderError(e)
						}
						this.getList()
					}
				},
				width: 380,
				height: 190
			})
		},

		deleteFolderConfirm(folder) {
			this.confirmWindow({
				title: this.$t('Delete folder'),
				message: this.$t('Delete the folder {name}? Apps inside it are not affected.', { name: `<b>${escapeHtml(folder.name)}</b>` }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				onConfirm: async () => {
					try {
						await this.deleteFolder(folder.id)
					} catch (e) {
						this.toastFolderError(e)
					}
					this.getList()
				}
			})
		},

		toastFolderError(err) {
			console.error(err)
			this.$buefy.toast.open({
				message: this.$t('Folders could not be updated: {reason}', { reason: apiErrorHtml(err, this.$t('Something went wrong')) }),
				type: 'is-danger',
				position: 'is-top',
				duration: 5000
			})
		},

		async addToFolderPrompt(item) {
			let folders
			try {
				folders = await this.getFolders()
			} catch (e) {
				this.toastFolderError(e)
				return
			}
			this.$store.commit('OPEN_WINDOW', {
				id: 'add-to-folder',
				title: this.$t('Add to folder'),
				component: 'AddToFolderPanel',
				props: { folders, itemName: item.name, isDialog: true },
				width: 420,
				height: 210
			})
		},

		openFolderIconEditor(folder) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'folder-icon-editor',
				title: this.$t('Edit icon'),
				component: 'IconEditorModal',
				props: {
					initialRadius: folder.iconRadius || 0,
					src: folder.icon || (folder.apps[0] && folder.apps[0].icon) || defaultAppIcon,
					folderId: folder.id,
					isDialog: true
				},
				width: 420,
				height: 420
			})
		},

		// Places item(s) dragged out of a folder at the actual cursor drop
		// point on the desktop canvas (converted from viewport to canvas-
		// local coordinates), instead of leaving them to reconcileAppPositions'
		// generic "first free cell" fallback - otherwise the app visibly
		// teleports to an unrelated corner of the desktop instead of landing
		// where it was actually dropped. Must run (and be saved) BEFORE the
		// next getList(), since that's what reconcileAppPositions treats as
		// this item's authoritative saved position.
		placeDroppedItems(names, clientX, clientY) {
			if (clientX === undefined || clientY === undefined || !this.$refs.canvas) return
			const canvasRect = this.$refs.canvas.getBoundingClientRect()
			const targetX = clientX - canvasRect.left - CELL_W / 2
			const targetY = clientY - canvasRect.top - CELL_H / 2
			names.forEach(name => this.placeItem(name, targetX, targetY))
			this.savePositions()
		},

		handleRemoveFromFolder({ item, folderId, clientX, clientY }) {
			return this.removeAppFromFolder(item.name, folderId).then(async (folders) => {
				// If this was the auto-filed "Other Containers" folder, remember the
				// user's choice permanently so getList() won't re-file it right back
				// in on its next refresh.
				const folder = folders.find(f => f.id === folderId)
				if (folder && folder.isAutoContainerFolder) {
					await this.addContainerAutoExclude(item.name)
				}
				this.placeDroppedItems([item.name], clientX, clientY)
			}).catch(err => this.toastFolderError(err)).then(() => this.getList())
		},

		handleRemoveMultipleFromFolder({ items, folderId, clientX, clientY }) {
			const appNames = items.map(i => i.name)
			return this.removeAppsFromFolder(appNames, folderId).then(async (folders) => {
				const folder = folders.find(f => f.id === folderId)
				if (folder && folder.isAutoContainerFolder) {
					await this.addContainerAutoExcludes(appNames)
				}
				this.placeDroppedItems(appNames, clientX, clientY)
			}).catch(err => this.toastFolderError(err)).then(() => this.getList())
		},

		async openLegacyEditModal(item) {
			let override
			try {
				override = await this.getLegacyAppOverride(item.name)
			} catch (e) {
				console.error(e)
				this.$buefy.toast.open({
					message: this.$t('Unable to load the app settings: {reason}', { reason: apiErrorHtml(e, this.$t('Something went wrong')) }),
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
				return
			}
			const displayName = (item.title && ice_i18n(item.title)) || item.name
			this.$store.commit('OPEN_WINDOW', {
				id: `edit-app-${item.name}`,
				title: `${this.$t('Edit')} - ${displayName}`,
				component: 'LegacyAppEditPanel',
				props: { item, override },
				width: 720,
				height: 530
			})
		},

		async showInstall(storeId = 0, mode = '') {
			if (mode === 'custom') {
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

		// "Import to NivaroOS": a plain (non-compose) container has no compose
		// app to load via myComposeApp (404), so export its compose first and
		// open the custom installer pre-filled with that YAML. Compose apps
		// keep the regular edit flow.
		async showContainerPanel(item) {
			if (!item || item.app_type !== 'container') {
				await this.showConfigPanel(item)
				return
			}
			const displayName = (item.title && ice_i18n(item.title)) || item.name
			try {
				const res = await this.$api.container.exportAsCompose(item.name)
				const yaml = res && res.data
				if (!yaml || typeof yaml !== 'string') {
					throw new Error(this.$t('The exported compose file is empty'))
				}
				this.$store.commit('OPEN_WINDOW', {
					id: 'appstore',
					title: `${this.$t('App Store')} - ${displayName}`,
					component: 'AppStoreApp',
					width: 1040,
					height: 720,
					props: {
						initialMode: 'custom',
						initialAppName: '',
						initialComposeYaml: yaml,
						importContainerName: item.name,
						// lets an already-open App Store react to a repeat request
						requestedAt: Date.now()
					}
				})
			} catch (e) {
				console.error('Import container failed', e)
				this.$buefy.toast.open({
					message: this.$t('Unable to import {name}: {reason}', { name: escapeHtml(displayName), reason: apiErrorHtml(e, this.$t('Something went wrong')) }),
					type: 'is-danger',
					position: 'is-top',
					duration: 6000
				})
			}
		},

		async showExternalLinkPanel(item = {}) {
			const displayName = item.name ? ` - ${item.name}` : ''
			this.$store.commit('OPEN_WINDOW', {
				id: item.name ? `edit-weblink-${item.name}` : 'add-weblink',
				title: `${this.$t('Add Web Link')}${displayName}`,
				component: 'ExternalLinkPanel',
				width: 520,
				height: 380,
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

			this.clearFailedInstallTimer(name)
			let existing = this.installingApps.find(a => a.name === name)
			if (existing && existing.failed) {
				this.installingApps = this.installingApps.filter(a => a.name !== name)
				existing = null
			}
			if (!existing) {
				existing = {
					name,
					title,
					icon,
					app_type: 'installing',
					progress: 5,
					failed: false,
					errorMessage: ''
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
			if (existing && !existing.failed && !isNaN(num)) {
				existing.progress = Math.min(99, Math.max(existing.progress, num))
			}
		},

		'app:install-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			// install-end is also published (from a defer) after a failure -
			// keep a failed tile visible until its own timer clears it.
			this.installingApps = this.installingApps.filter(a => a.name !== name || a.failed)
			this.getList()
		},

		// Show the failure on the tile itself instead of making it vanish;
		// the reason is also announced by the install-status toast/notice.
		'app:install-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const existing = this.installingApps.find(a => a.name === name)
			if (!existing) {
				this.getList()
				return
			}
			existing.failed = true
			existing.errorMessage = props.message || this.$t('Deployment encountered an error.')
			this.clearFailedInstallTimer(name)
			this.failedInstallTimers[name] = setTimeout(() => {
				delete this.failedInstallTimers[name]
				this.installingApps = this.installingApps.filter(a => a.name !== name)
				this.syncPositions(false)
			}, INSTALL_FAILED_TILE_MS)
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

.app-canvas {
	position: absolute;
	inset: 0;
	width: 100%;
	height: 100%;
	overflow: hidden;
}

.skeleton-slot {
	pointer-events: none;
	cursor: default;
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
	border-radius: var(--radius-card);

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
		border-radius: var(--radius-card);
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
	border-radius: var(--radius-card);
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
	border-radius: var(--radius-card);
	padding: var(--space-1);
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
	font-size: var(--font-sm);
	font-weight: 600;
	color: #ffffff;
	text-shadow: 0 1px 3px rgba(0, 0, 0, 0.8);
}

.installing-app-slot.is-failed {
	opacity: 1;

	.ring-fill {
		stroke: #f87171;
	}
}

.installing-badge.is-failed {
	color: #fecaca;
	border-color: rgba(248, 113, 113, 0.5);
}

.installing-badge {
	display: inline-block;
	font-size: var(--font-2xs);
	font-weight: 700;
	color: #38bdf8;
	background: rgba(15, 23, 42, 0.7);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	margin-top: var(--space-1);
	border: 1px solid rgba(56, 189, 248, 0.3);
}
</style>
