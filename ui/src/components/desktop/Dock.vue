<template>
	<div class="dock-wrapper">
		<div class="dock" @contextmenu.prevent.stop="openDockContextMenu($event, { type: 'dock' })">
			<!-- Pinned Apps (Built-ins and User Apps with smooth drag-and-drop reordering) -->
			<draggable
				v-model="dockItems"
				class="dock-pinned-list"
				tag="div"
				:animation="220"
				:delay="120"
				:delay-on-touch-only="false"
				:touch-start-threshold="4"
				ghost-class="dock-item-ghost"
				chosen-class="dock-item-chosen"
				drag-class="dock-item-drag"
				@end="onDockDragEnd"
			>
				<button
					v-for="item in dockItems"
					:key="'dock-' + item.name"
					class="dock-item"
					:title="displayName(item)"
					@click="launchItem(item)"
					@contextmenu.prevent.stop="openDockContextMenu($event, { type: 'item', data: item })"
				>
					<img
						:src="item.icon"
						class="dock-icon"
						:style="{ borderRadius: item.iconRadius ? item.iconRadius + '%' : '12px' }"
						:alt="displayName(item)"
						draggable="false"
					/>
					<span class="dock-dot" v-if="isItemOpen(item)" :class="{ minimized: isItemMinimized(item) }"></span>
				</button>
			</draggable>

			<div v-if="extraWindows.length && dockItems.length" class="dock-sep"></div>

			<!-- Active Unpinned Windows -->
			<button
				v-for="win in extraWindows"
				:key="win.id"
				class="dock-item"
				:title="win.title"
				@click="toggleWindow(win)"
				@contextmenu.prevent.stop="openDockContextMenu($event, { type: 'window', data: win })"
			>
				<img v-if="isViewerWindow(win)" :src="viewerIconUrl" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'FilesApp' || win.component === 'FolderWindow'" :src="getBuiltinIcon('Files')" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'AppStoreApp'" :src="getBuiltinIcon('App Store')" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'TerminalPanel' || win.component === 'SystemUpdateWindow'" :src="getBuiltinIcon('Terminal')" class="dock-icon" :alt="win.title" />
				<img v-else-if="isVmWindow(win)" :src="vmConsoleIconUrl" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'LegacyAppEditPanel' && win.props && win.props.item" :src="(win.props.override && win.props.override.icon) || win.props.item.icon || require('@/assets/img/app/default.svg')" class="dock-icon" :alt="win.title" />
				<div v-else class="dock-icon dock-icon-generic">
					<b-icon icon="display-applications-outline" pack="casa" size="is-20"></b-icon>
				</div>
				<span class="dock-dot" :class="{ minimized: win.minimized }"></span>
			</button>
		</div>

		<!-- Taskbar Right-Click Context Menu -->
		<div
			v-if="ctxMenu.visible"
			ref="dockCtxMenu"
			class="dock-context-menu"
			:style="{ bottom: ctxMenu.bottom + 'px', left: ctxMenu.left + 'px' }"
			@contextmenu.prevent.stop
		>
			<!-- Header with App info -->
			<div v-if="ctxMenu.title" class="ctx-header is-flex is-align-items-center">
				<img
					v-if="ctxMenu.icon"
					:src="ctxMenu.icon"
					:style="{ borderRadius: ctxMenu.iconRadius ? ctxMenu.iconRadius + '%' : '5px' }"
					class="ctx-header-icon mr-2"
				/>
				<div class="ctx-header-info">
					<div class="ctx-header-title font-semibold">{{ ctxMenu.title }}</div>
					<div v-if="ctxMenu.status" class="ctx-header-sub text-muted is-size-7">{{ ctxMenu.status }}</div>
				</div>
			</div>
			<div v-if="ctxMenu.title" class="ctx-divider"></div>

			<!-- 1. Pinned Item Actions -->
			<template v-if="ctxMenu.target && ctxMenu.target.type === 'item'">
				<!-- Built-in system desktop window actions -->
				<template v-if="isBuiltinApp(ctxMenu.target.data)">
					<button class="ctx-item" @click="handleItemAction('toggle')">
						<i class="mdi mdi-open-in-app ctx-icon"></i>
						<span class="ctx-label">{{ isItemOpen(ctxMenu.target.data) ? (isItemMinimized(ctxMenu.target.data) ? $t('Restore') : $t('Bring to Front')) : $t('Open') }}</span>
					</button>

					<button v-if="isMultiWindowApp(ctxMenu.target.data)" class="ctx-item" @click="handleItemAction('newWindow')">
						<i class="mdi mdi-plus-box-multiple-outline ctx-icon"></i>
						<span class="ctx-label">{{ $t('New Window') }}</span>
					</button>

					<div class="ctx-divider"></div>

					<button class="ctx-item" @click="handleItemAction('unpin')">
						<i class="mdi mdi-pin-off-outline ctx-icon"></i>
						<span class="ctx-label">{{ $t('Unpin from Taskbar') }}</span>
					</button>

					<div v-if="isItemOpen(ctxMenu.target.data)" class="ctx-divider"></div>

					<button v-if="isItemOpen(ctxMenu.target.data)" class="ctx-item is-danger" @click="handleItemAction('close')">
						<i class="mdi mdi-close ctx-icon"></i>
						<span class="ctx-label">{{ $t('Close Window') }}</span>
					</button>
				</template>

				<!-- Docker / Web App Container Actions (External Web Services) -->
				<template v-else>
					<button class="ctx-item" @click="handleItemAction('toggle')">
						<i class="mdi mdi-open-in-new ctx-icon"></i>
						<span class="ctx-label">{{ $t('Open') }}</span>
					</button>

					<button v-if="ctxMenu.target.data.app_type === 'container'" class="ctx-item" @click="handleItemAction('import')">
						<i class="mdi mdi-download-box-outline ctx-icon"></i>
						<span class="ctx-label">{{ $t('Import to NivaroOS') }}</span>
					</button>

					<button class="ctx-item" @click="handleItemAction('edit')">
						<i class="mdi mdi-pencil-outline ctx-icon"></i>
						<span class="ctx-label">{{ $t('Edit Settings') }}</span>
					</button>

					<button v-if="ctxMenu.target.data.app_type !== 'LinkApp'" class="ctx-item" @click="handleItemAction('restart')">
						<i class="mdi mdi-restart ctx-icon"></i>
						<span class="ctx-label">{{ $t('Restart App') }}</span>
					</button>

					<button v-if="ctxMenu.target.data.app_type === 'container'" class="ctx-item" @click="handleItemAction('toggleContainer')">
						<i :class="ctxMenu.target.data.status === 'running' ? 'mdi mdi-stop-circle-outline' : 'mdi mdi-play-circle-outline'" class="ctx-icon"></i>
						<span class="ctx-label">{{ ctxMenu.target.data.status === 'running' ? $t('Stop Container') : $t('Start Container') }}</span>
					</button>

					<div class="ctx-divider"></div>

					<button class="ctx-item" @click="handleItemAction('unpin')">
						<i class="mdi mdi-pin-off-outline ctx-icon"></i>
						<span class="ctx-label">{{ $t('Unpin from Taskbar') }}</span>
					</button>
				</template>
			</template>

			<!-- 2. Running Extra Window Actions -->
			<template v-else-if="ctxMenu.target && ctxMenu.target.type === 'window'">
				<button class="ctx-item" @click="handleWindowAction('toggle')">
					<i :class="ctxMenu.target.data.minimized ? 'mdi mdi-window-maximize' : 'mdi mdi-window-minimize'" class="ctx-icon"></i>
					<span class="ctx-label">{{ ctxMenu.target.data.minimized ? $t('Restore Window') : $t('Minimize Window') }}</span>
				</button>

				<button class="ctx-item" @click="handleWindowAction('pin')">
					<i class="mdi mdi-pin-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Pin to Taskbar') }}</span>
				</button>

				<div class="ctx-divider"></div>

				<button class="ctx-item is-danger" @click="handleWindowAction('close')">
					<i class="mdi mdi-close ctx-icon"></i>
					<span class="ctx-label">{{ $t('Close Window') }}</span>
				</button>
			</template>

			<!-- 3. Dock Background Actions -->
			<template v-else>
				<button class="ctx-item" @click="openAppearanceSettings">
					<i class="mdi mdi-palette-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Taskbar & Appearance') }}</span>
				</button>
				<button class="ctx-item" @click="openSystemSettings">
					<i class="mdi mdi-cog-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('System Settings') }}</span>
				</button>
				<button class="ctx-item" @click="restoreDefaultPins">
					<i class="mdi mdi-restore ctx-icon"></i>
					<span class="ctx-label">{{ $t('Reset Pinned Apps') }}</span>
				</button>
			</template>
		</div>
	</div>
</template>

<script>
import events from '@/events/events'
import filesIcon from '@/assets/img/app/files.svg'
import appStoreIcon from '@/assets/img/app/appstore.png'
import settingsIcon from '@/assets/img/app/settings.png'
import terminalIcon from '@/assets/img/app/terminal.png'
import vmManagerIcon from '@/assets/img/app/vm-manager.png'
import business_ShowNewAppTag from '@/mixins/app/Business_ShowNewAppTag'
import business_OpenThirdApp from '@/mixins/app/Business_OpenThirdApp'
import business_LinkApp from '@/mixins/app/Business_LinkApp'
import business_LegacyAppOverrides from '@/mixins/app/Business_LegacyAppOverrides'
import business_DockPins, { DEFAULT_PINS, SYSTEM_NAME_MAP } from '@/mixins/app/Business_DockPins'
import { ice_i18n } from '@/mixins/base/common-i18n'

import viewerIcon from '@/assets/img/app/viewer.png'
import vmIcon from '@/assets/img/app/vm.png'
import draggable from 'vuedraggable'

const BUILTIN_DEFS = {
	Files: { id: 'files', name: 'Files', label: 'Files', defaultIcon: filesIcon, component: 'FilesApp', width: 960, height: 620 },
	'App Store': { id: 'appstore', name: 'App Store', label: 'App Store', defaultIcon: appStoreIcon, component: 'AppStoreApp', width: 1040, height: 720 },
	Terminal: { id: 'terminal', name: 'Terminal', label: 'Terminal', defaultIcon: terminalIcon, component: 'TerminalPanel', width: 720, height: 480 },
	VMs: { id: 'vms', name: 'VMs', label: 'VMs', defaultIcon: vmManagerIcon, component: 'VmManagerApp', width: 880, height: 560 },
	Settings: { id: 'settings', name: 'Settings', label: 'Settings', defaultIcon: settingsIcon, component: 'SettingsApp', width: 760, height: 540 }
}

const VIEWER_COMPONENTS = ['ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer']
const VM_ICON_COMPONENTS = ['VmConsolePanel', 'CreateVmModal', 'EditVmModal']

export default {
	name: 'dock',
	components: {
		draggable
	},
	mixins: [business_ShowNewAppTag, business_OpenThirdApp, business_LinkApp, business_LegacyAppOverrides, business_DockPins],
	data() {
		return {
			dockItems: [],
			overridesMap: {},
			ctxMenu: {
				visible: false,
				bottom: 80,
				left: 0,
				title: '',
				status: '',
				icon: null,
				iconRadius: 0,
				target: null
			}
		}
	},
	computed: {
		windows() {
			return this.$store.state.windows
		},
		extraWindows() {
			const pinnedNames = this.dockItems.map(item => item.name)
			const pinnedIds = this.dockItems.map(item => item.id).filter(Boolean)
			return this.windows.filter(w => !pinnedIds.includes(w.id) && !pinnedNames.includes(w.title))
		},
		viewerIconUrl() {
			return viewerIcon
		},
		vmConsoleIconUrl() {
			return vmIcon
		}
	},
	created() {
		this.loadDockItems()
		this.$EventBus.$on(events.RELOAD_APP_LIST, this.loadDockItems)
	},
	mounted() {
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('blur', this.closeCtxMenu)
		window.addEventListener('resize', this.closeCtxMenu)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.RELOAD_APP_LIST, this.loadDockItems)
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('blur', this.closeCtxMenu)
		window.removeEventListener('resize', this.closeCtxMenu)
	},
	methods: {
		// Save new pinned order on drag end
		async onDockDragEnd() {
			const names = this.dockItems.map(item => item.name)
			try {
				await this.$api.users.setCustomStorage('dock_pinned_apps', names)
			} catch (e) {
				console.error('Failed to save dock pin order', e)
			}
		},

		getBuiltinIcon(name) {
			const override = this.overridesMap[name]
			if (override && override.icon) return override.icon
			return (BUILTIN_DEFS[name] && BUILTIN_DEFS[name].defaultIcon) || require('@/assets/img/app/default.svg')
		},
		onOutsideClick(e) {
			if (this.ctxMenu.visible && this.$refs.dockCtxMenu && !this.$refs.dockCtxMenu.contains(e.target)) {
				this.closeCtxMenu()
			}
		},
		closeCtxMenu() {
			this.ctxMenu.visible = false
		},
		openDockContextMenu(event, target) {
			const menuWidth = 215
			const maxLeft = Math.max(12, window.innerWidth - menuWidth - 16)
			const left = Math.max(12, Math.min(maxLeft, event.clientX - menuWidth / 2))
			const bottom = Math.max(76, window.innerHeight - event.clientY + 12)

			let title = ''
			let status = ''
			let icon = null
			let iconRadius = 0

			if (target.type === 'item') {
				const item = target.data
				title = this.displayName(item)
				status = this.isItemOpen(item) ? this.$t('Running') : this.$t('Ready')
				icon = item.icon
				iconRadius = item.iconRadius || 0
			} else if (target.type === 'window') {
				const win = target.data
				title = win.title
				status = win.minimized ? this.$t('Minimized') : this.$t('Active')
				icon = this.isViewerWindow(win) ? this.viewerIconUrl : (win.component === 'FilesApp' ? this.getBuiltinIcon('Files') : null)
			}

			this.ctxMenu = {
				visible: true,
				bottom,
				left,
				title,
				status,
				icon,
				iconRadius,
				target
			}
		},
		async loadDockItems() {
			const [pins, orgAppList, linkAppList, overrides] = await Promise.all([
				this.getDockPins(),
				this.$openAPI.appGrid.getAppGrid().then(res => res.data.data || []).catch(() => []),
				this.getLinkAppList().catch(() => []),
				this.getLegacyAppOverrides().catch(() => ({}))
			])
			this.overridesMap = overrides || {}

			const allApps = orgAppList.concat(linkAppList)
			const items = []

			pins.forEach(pinName => {
				const norm = SYSTEM_NAME_MAP[pinName] || pinName
				if (BUILTIN_DEFS[norm]) {
					const def = BUILTIN_DEFS[norm]
					const override = overrides[def.name] || overrides[def.id]
					const icon = (override && override.icon) || def.defaultIcon
					const iconRadius = (override && override.iconRadius) || 0
					const title = (override && override.title) || def.label
					items.push({
						...def,
						icon,
						iconRadius,
						title,
						app_type: 'system',
						status: 'running'
					})
				} else {
					const app = allApps.find(a => a.name === pinName)
					if (app) {
						const override = overrides[app.name]
						const icon = (override && override.icon) || app.icon || require('@/assets/img/app/default.svg')
						const iconRadius = (override && override.iconRadius) || 0
						const title = override && override.title ? { ...app.title, custom: override.title } : app.title
						const overrideUrl = override && override.url
						items.push({
							...app,
							icon,
							iconRadius,
							title,
							overrideUrl
						})
					}
				}
			})

			this.dockItems = items
		},
		displayName(item) {
			if (typeof item.title === 'string') return item.title
			return (item.title && ice_i18n(item.title)) || item.name || item.label
		},
		isBuiltinApp(item) {
			return item.app_type === 'system' || !!BUILTIN_DEFS[item.name]
		},
		isMultiWindowApp(item) {
			return item.id === 'files' || item.name === 'Files' || item.id === 'terminal' || item.name === 'Terminal'
		},
		findWindow(idOrTitle) {
			return this.windows.find(w => w.id === idOrTitle || w.title === idOrTitle)
		},
		isViewerWindow(win) {
			return VIEWER_COMPONENTS.includes(win.component)
		},
		isVmWindow(win) {
			return VM_ICON_COMPONENTS.includes(win.component)
		},
		isItemOpen(item) {
			if (item.id && this.findWindow(item.id)) return true
			if (this.findWindow(item.name)) return true
			return false
		},
		isItemMinimized(item) {
			const win = this.findWindow(item.id || item.name)
			return !!(win && win.minimized)
		},
		isTopmost(win) {
			return win.zIndex === Math.max(...this.windows.map(w => w.zIndex))
		},
		launchItem(item) {
			if (this.isBuiltinApp(item)) {
				const def = BUILTIN_DEFS[SYSTEM_NAME_MAP[item.name] || item.name] || item
				const win = this.findWindow(def.id)
				if (!win) {
					this.$store.commit('OPEN_WINDOW', {
						id: def.id,
						title: this.$t(def.label),
						component: def.component,
						width: def.width,
						height: def.height
					})
					return
				}
				this.toggleWindow(win)
				return
			}

			// User Container / Link App
			if (item.app_type === 'container') {
				if (item.overrideUrl) window.open(item.overrideUrl, '_blank')
				return
			}
			if (item.app_type === 'LinkApp') {
				window.open(item.hostname, '_blank')
				return
			}
			if (item.status === 'running') {
				this.openAppToNewWindow(item)
				return
			}
			const request = item.app_type === 'v2app'
				? this.$openAPI.appManagement.compose.setComposeAppStatus(item.name, 'start')
				: this.$api.container.updateState(item.name, 'start')
			request.then(() => this.firstOpenThirdApp(item))
		},
		toggleWindow(win) {
			if (win.minimized) {
				this.$store.commit('FOCUS_WINDOW', win.id)
			} else if (this.isTopmost(win)) {
				this.$store.commit('TOGGLE_MINIMIZE_WINDOW', win.id)
			} else {
				this.$store.commit('FOCUS_WINDOW', win.id)
			}
		},
		handleItemAction(action) {
			const item = this.ctxMenu.target.data
			this.closeCtxMenu()

			if (action === 'toggle') {
				this.launchItem(item)
			} else if (action === 'newWindow') {
				if (item.id === 'files' || item.name === 'Files') {
					this.$store.commit('OPEN_WINDOW', {
						id: 'files-' + Date.now(),
						title: this.$t('Files'),
						component: 'FilesApp',
						width: 960,
						height: 620
					})
				} else if (item.id === 'terminal' || item.name === 'Terminal') {
					this.$store.commit('OPEN_WINDOW', {
						id: 'terminal-' + Date.now(),
						title: this.$t('Terminal'),
						component: 'TerminalPanel',
						width: 720,
						height: 480
					})
				}
			} else if (action === 'edit') {
				this.$EventBus.$emit(events.SHOW_CONFIG_PANEL, item)
			} else if (action === 'import') {
				this.$EventBus.$emit(events.SHOW_CONTAINER_PANEL, item)
			} else if (action === 'restart') {
				this.$messageBus('apps_restart', item.name)
				const req = item.app_type === 'v2app'
					? this.$openAPI.appManagement.compose.setComposeAppStatus(item.name, 'restart')
					: this.$api.container.updateState(item.name, 'restart')
				req.then(() => {
					this.$buefy.toast.open({
						message: this.$t('Restarting app...'),
						type: 'is-info',
						position: 'is-top',
						duration: 2000
					})
				})
			} else if (action === 'toggleContainer') {
				const newState = item.status === 'running' ? 'stop' : 'start'
				this.$api.container.updateState(item.name, newState).then(() => {
					item.status = newState === 'stop' ? 'exited' : 'running'
					this.$buefy.toast.open({
						message: newState === 'stop' ? this.$t('Container stopped') : this.$t('Container started'),
						type: 'is-info',
						position: 'is-top',
						duration: 2000
					})
					this.$EventBus.$emit(events.UPDATE_SYNC_STATUS)
				}).catch((err) => {
					this.$buefy.toast.open({
						message: err?.response?.data?.message || this.$t('Failed to change container state'),
						type: 'is-danger',
						position: 'is-top',
						duration: 3000
					})
				})
			} else if (action === 'unpin') {
				this.setDockPinned(item.name, false).then(() => {
					this.loadDockItems()
					this.$EventBus.$emit(events.RELOAD_APP_LIST)
					this.$buefy.toast.open({
						message: `<i class="mdi mdi-pin-off-outline mr-1"></i> ${this.$t('Removed from taskbar')}`,
						type: 'is-dark',
						position: 'is-top',
						duration: 2000,
						queue: false
					})
				})
			} else if (action === 'close') {
				const win = this.findWindow(item.id || item.name)
				if (win) this.$store.commit('CLOSE_WINDOW', win.id)
			}
		},
		handleWindowAction(action) {
			const win = this.ctxMenu.target.data
			this.closeCtxMenu()
			if (action === 'toggle') {
				this.toggleWindow(win)
			} else if (action === 'pin') {
				this.setDockPinned(win.title, true).then(() => {
					this.loadDockItems()
					this.$EventBus.$emit(events.RELOAD_APP_LIST)
					this.$buefy.toast.open({
						message: `<i class="mdi mdi-pin-outline mr-1"></i> ${this.$t('Pinned to taskbar')}`,
						type: 'is-dark',
						position: 'is-top',
						duration: 2000,
						queue: false
					})
				})
			} else if (action === 'close') {
				this.$store.commit('CLOSE_WINDOW', win.id)
			}
		},
		openAppearanceSettings() {
			this.closeCtxMenu()
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'appearance' }
			})
		},
		openSystemSettings() {
			this.closeCtxMenu()
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540
			})
		},
		restoreDefaultPins() {
			this.closeCtxMenu()
			this.$api.users.setCustomStorage('dock_pinned_apps', [...DEFAULT_PINS]).then(() => {
				this.loadDockItems()
				this.$EventBus.$emit(events.RELOAD_APP_LIST)
				this.$buefy.toast.open({
					message: `<i class="mdi mdi-restore mr-1"></i> ${this.$t('Taskbar reset to default')}`,
					type: 'is-dark',
					position: 'is-top',
					duration: 2000,
					queue: false
				})
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.dock-wrapper {
	display: contents;
}

.dock {
	position: fixed;
	left: 50%;
	bottom: 0.9rem;
	transform: translateX(-50%);
	display: flex;
	align-items: flex-end;
	gap: 0.65rem;
	padding: 0.5rem 0.75rem 0.4rem;
	background: $backDropColor;
	backdrop-filter: $backDropBlur;
	-webkit-backdrop-filter: $backDropBlur;
	border: $backDropBorder;
	border-radius: 22px;
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25), $backDropShadow;
	z-index: 99990;
}

.dock-sep {
	align-self: stretch;
	width: 1px;
	background: rgba(255, 255, 255, 0.15);
	margin: 0.3rem 0.15rem;
}

.dock-item {
	position: relative;
	border: none;
	background: transparent;
	padding: 0;
	cursor: pointer;
	display: flex;
	flex-direction: column;
	align-items: center;
	transition: transform 0.15s ease;
	transform-origin: bottom center;

	&:hover {
		transform: translateY(-8px) scale(1.18);
	}

	&:active {
		transform: translateY(-2px) scale(1.05);
	}
}

.dock-icon {
	width: 3rem;
	height: 3rem;
	border-radius: 12px;
	box-shadow: 0 3px 8px rgba(0, 0, 0, 0.25);
	object-fit: cover;
}

.dock-icon-generic {
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.4);
	color: #fff;
}

.dock-dot {
	position: absolute;
	bottom: -0.5rem;
	left: 50%;
	transform: translateX(-50%);
	width: 5px;
	height: 5px;
	border-radius: 50%;
	background: $white;

	&.minimized {
		opacity: 0.4;
	}
}

.dock-pinned-list {
	display: flex;
	align-items: flex-end;
	gap: 0.65rem;
}

/* SortableJS drag & drop states */
.dock-item-ghost {
	opacity: 0.25 !important;
	transform: scale(0.9) !important;
}

.dock-item-chosen {
	transform: translateY(-8px) scale(1.15) !important;
	.dock-icon {
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4), 0 0 0 2px rgba(37, 99, 235, 0.7) !important;
	}
}

.dock-item-drag {
	opacity: 0.95 !important;
	cursor: grabbing !important;
}

/* Dock Right Click Menu */
.dock-context-menu {
	position: fixed;
	width: 215px;
	background: rgba(255, 255, 255, 0.95);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid rgba(255, 255, 255, 0.65);
	border-radius: 12px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.22), 0 2px 8px rgba(0, 0, 0, 0.1);
	padding: 0.35rem;
	user-select: none;
	animation: dockCtxFade 0.12s cubic-bezier(0.16, 1, 0.3, 1);
	z-index: 100000;
}

@keyframes dockCtxFade {
	from {
		opacity: 0;
		transform: scale(0.96) translateY(4px);
	}
	to {
		opacity: 1;
		transform: scale(1) translateY(0);
	}
}

.ctx-header {
	padding: 0.35rem 0.65rem 0.25rem;
}

.ctx-header-icon {
	width: 22px;
	height: 22px;
	border-radius: 5px;
	object-fit: cover;
}

.ctx-header-title {
	font-size: 0.8125rem;
	color: #0f172a;
	line-height: 1.2;
}

.ctx-header-sub {
	color: #64748b;
	line-height: 1;
}

.ctx-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	padding: 0.44rem 0.7rem;
	border-radius: 7px;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 500;
	color: #1e293b;
	transition: all 0.12s ease;
	cursor: pointer;
	border: none;
	background: transparent;
	text-align: left;

	.ctx-icon {
		font-size: 1.15rem;
		width: 1.25rem;
		text-align: center;
		color: #475569;
		flex-shrink: 0;
		line-height: 1;
		transition: color 0.12s ease;
	}

	.ctx-label {
		flex: 1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	&:hover {
		background: #2563eb;
		color: #ffffff;

		.ctx-icon {
			color: #ffffff;
		}
	}

	&.is-danger {
		color: #dc2626;

		.ctx-icon {
			color: #dc2626;
		}

		&:hover {
			background: #dc2626;
			color: #ffffff;

			.ctx-icon {
				color: #ffffff;
			}
		}
	}

	&:active {
		transform: scale(0.98);
	}
}

.ctx-divider {
	height: 1px;
	margin: 0.35rem 0.4rem;
	background: rgba(0, 0, 0, 0.08);
}
</style>
