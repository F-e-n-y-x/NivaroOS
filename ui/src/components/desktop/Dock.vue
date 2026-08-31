<template>
	<div class="dock-container" @contextmenu.prevent.stop="openDockContextMenu($event, { type: 'dock' })">
		<div class="dock">
			<!-- 1. Built-in Pinned Apps -->
			<button
				v-for="p in visiblePinned"
				:key="p.id"
				class="dock-item"
				:title="p.label"
				@click="launch(p)"
				@contextmenu.prevent.stop="openDockContextMenu($event, { type: 'pinned', data: p })"
			>
				<img :src="p.icon" class="dock-icon" :alt="p.label" />
				<span class="dock-dot" v-if="isOpen(p.id)" :class="{ minimized: isMinimized(p.id) }"></span>
			</button>

			<div v-if="pinnedApps.length && visiblePinned.length" class="dock-sep"></div>

			<!-- 2. User Pinned Apps -->
			<button
				v-for="app in pinnedApps"
				:key="'pinned-' + app.name"
				class="dock-item"
				:title="displayName(app)"
				@click="launchApp(app)"
				@contextmenu.prevent.stop="openDockContextMenu($event, { type: 'pinnedApp', data: app })"
			>
				<img :src="app.icon" class="dock-icon" :alt="displayName(app)" />
				<span class="dock-dot" v-if="isAppRunning(app)" :class="{ minimized: false }"></span>
			</button>

			<div v-if="extraWindows.length" class="dock-sep"></div>

			<!-- 3. Active Unpinned Windows -->
			<button
				v-for="win in extraWindows"
				:key="win.id"
				class="dock-item"
				:title="win.title"
				@click="toggleWindow(win)"
				@contextmenu.prevent.stop="openDockContextMenu($event, { type: 'window', data: win })"
			>
				<img v-if="isViewerWindow(win)" :src="viewerIconUrl" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'FilesApp' || win.component === 'FolderWindow'" :src="filesIcon" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'AppStoreApp'" :src="appStoreIcon" class="dock-icon" :alt="win.title" />
				<img v-else-if="win.component === 'TerminalPanel' || win.component === 'SystemUpdateWindow'" :src="terminalIcon" class="dock-icon" :alt="win.title" />
				<img v-else-if="isVmWindow(win)" :src="vmConsoleIconUrl" class="dock-icon" :alt="win.title" />
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
				<img v-if="ctxMenu.icon" :src="ctxMenu.icon" class="ctx-header-icon mr-2" />
				<div class="ctx-header-info">
					<div class="ctx-header-title font-semibold">{{ ctxMenu.title }}</div>
					<div v-if="ctxMenu.status" class="ctx-header-sub text-muted is-size-7">{{ ctxMenu.status }}</div>
				</div>
			</div>
			<div v-if="ctxMenu.title" class="ctx-divider"></div>

			<!-- 1. Pinned System App actions -->
			<template v-if="ctxMenu.target && ctxMenu.target.type === 'pinned'">
				<button class="ctx-item" @click="handlePinnedAction('toggle')">
					<i class="mdi mdi-open-in-app ctx-icon"></i>
					<span class="ctx-label">{{ isOpen(ctxMenu.target.data.id) ? (isMinimized(ctxMenu.target.data.id) ? $t('Restore') : $t('Bring to Front')) : $t('Open') }}</span>
				</button>
				<button v-if="ctxMenu.target.data.id === 'files' || ctxMenu.target.data.id === 'terminal'" class="ctx-item" @click="handlePinnedAction('newWindow')">
					<i class="mdi mdi-plus-box-multiple-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('New Window') }}</span>
				</button>
				<button class="ctx-item" @click="handlePinnedAction('unpin')">
					<i class="mdi mdi-pin-off-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Unpin from Taskbar') }}</span>
				</button>
				<div v-if="isOpen(ctxMenu.target.data.id)" class="ctx-divider"></div>
				<button v-if="isOpen(ctxMenu.target.data.id)" class="ctx-item is-danger" @click="handlePinnedAction('close')">
					<i class="mdi mdi-close ctx-icon"></i>
					<span class="ctx-label">{{ $t('Close Window') }}</span>
				</button>
			</template>

			<!-- 2. Pinned User App actions -->
			<template v-else-if="ctxMenu.target && ctxMenu.target.type === 'pinnedApp'">
				<button class="ctx-item" @click="handleUserAppAction('open')">
					<i class="mdi mdi-open-in-app ctx-icon"></i>
					<span class="ctx-label">{{ $t('Open App') }}</span>
				</button>
				<button class="ctx-item" @click="handleUserAppAction('edit')">
					<i class="mdi mdi-pencil-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Edit Settings') }}</span>
				</button>
				<button class="ctx-item" @click="handleUserAppAction('restart')">
					<i class="mdi mdi-restart ctx-icon"></i>
					<span class="ctx-label">{{ $t('Restart App') }}</span>
				</button>
				<div class="ctx-divider"></div>
				<button class="ctx-item" @click="handleUserAppAction('unpin')">
					<i class="mdi mdi-pin-off-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Unpin from Taskbar') }}</span>
				</button>
			</template>

			<!-- 3. Running Window actions -->
			<template v-else-if="ctxMenu.target && ctxMenu.target.type === 'window'">
				<button class="ctx-item" @click="handleWindowAction('toggle')">
					<i :class="ctxMenu.target.data.minimized ? 'mdi mdi-window-maximize' : 'mdi mdi-window-minimize'" class="ctx-icon"></i>
					<span class="ctx-label">{{ ctxMenu.target.data.minimized ? $t('Restore Window') : $t('Minimize Window') }}</span>
				</button>
				<div class="ctx-divider"></div>
				<button class="ctx-item is-danger" @click="handleWindowAction('close')">
					<i class="mdi mdi-close ctx-icon"></i>
					<span class="ctx-label">{{ $t('Close Window') }}</span>
				</button>
			</template>

			<!-- 4. Dock background actions -->
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
import business_DockPins from '@/mixins/app/Business_DockPins'
import { ice_i18n } from '@/mixins/base/common-i18n'

import viewerIcon from '@/assets/img/app/viewer.png'
import vmIcon from '@/assets/img/app/vm.png'

const PINNED = [
	{ id: 'files', label: 'Files', icon: filesIcon },
	{ id: 'appstore', label: 'App Store', icon: appStoreIcon },
	{ id: 'terminal', label: 'Terminal', icon: terminalIcon },
	{ id: 'vms', label: 'VMs', icon: vmManagerIcon },
	{ id: 'settings', label: 'Settings', icon: settingsIcon }
]

const VIEWER_COMPONENTS = ['ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer']
const VM_ICON_COMPONENTS = ['VmConsolePanel', 'CreateVmModal', 'EditVmModal']

export default {
	name: 'dock',
	mixins: [business_ShowNewAppTag, business_OpenThirdApp, business_LinkApp, business_LegacyAppOverrides, business_DockPins],
	data() {
		return {
			pinned: PINNED,
			unpinnedSystemIds: JSON.parse(localStorage.getItem('unpinned_system_dock') || '[]'),
			pinnedApps: [],
			filesIcon,
			terminalIcon,
			appStoreIcon,
			ctxMenu: {
				visible: false,
				bottom: 80,
				left: 0,
				title: '',
				status: '',
				icon: null,
				target: null
			}
		}
	},
	computed: {
		visiblePinned() {
			return this.pinned.filter(p => !this.unpinnedSystemIds.includes(p.id))
		},
		windows() {
			return this.$store.state.windows
		},
		extraWindows() {
			const pinnedIds = this.visiblePinned.map(p => p.id)
			return this.windows.filter(w => !pinnedIds.includes(w.id))
		},
		viewerIconUrl() {
			return viewerIcon
		},
		vmConsoleIconUrl() {
			return vmIcon
		}
	},
	created() {
		this.loadPinnedApps()
		this.$EventBus.$on(events.RELOAD_APP_LIST, this.loadPinnedApps)
	},
	mounted() {
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('blur', this.closeCtxMenu)
		window.addEventListener('resize', this.closeCtxMenu)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.RELOAD_APP_LIST, this.loadPinnedApps)
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('blur', this.closeCtxMenu)
		window.removeEventListener('resize', this.closeCtxMenu)
	},
	methods: {
		onOutsideClick(e) {
			if (this.ctxMenu.visible && this.$refs.dockCtxMenu && !this.$refs.dockCtxMenu.contains(e.target)) {
				this.closeCtxMenu()
			}
		},
		closeCtxMenu() {
			this.ctxMenu.visible = false
		},
		openDockContextMenu(event, target) {
			const menuWidth = 210
			const maxLeft = Math.max(12, window.innerWidth - menuWidth - 16)
			const left = Math.max(12, Math.min(maxLeft, event.clientX - menuWidth / 2))
			const bottom = Math.max(76, window.innerHeight - event.clientY + 12)

			let title = ''
			let status = ''
			let icon = null

			if (target.type === 'pinned') {
				const p = target.data
				title = this.$t(p.label)
				status = this.isOpen(p.id) ? this.$t('Running') : this.$t('Ready')
				icon = p.icon
			} else if (target.type === 'pinnedApp') {
				const app = target.data
				title = this.displayName(app)
				status = app.status === 'running' ? this.$t('Running') : this.$t('Stopped')
				icon = app.icon
			} else if (target.type === 'window') {
				const win = target.data
				title = win.title
				status = win.minimized ? this.$t('Minimized') : this.$t('Active')
				icon = this.isViewerWindow(win) ? this.viewerIconUrl : (win.component === 'FilesApp' ? this.filesIcon : null)
			}

			this.ctxMenu = {
				visible: true,
				bottom,
				left,
				title,
				status,
				icon,
				target
			}
		},
		handlePinnedAction(action) {
			const p = this.ctxMenu.target.data
			this.closeCtxMenu()
			if (action === 'toggle') {
				this.launch(p)
			} else if (action === 'newWindow') {
				if (p.id === 'files') {
					this.$store.commit('OPEN_WINDOW', {
						id: 'files-' + Date.now(),
						title: this.$t('Files'),
						component: 'FilesApp',
						width: 960,
						height: 620
					})
				} else if (p.id === 'terminal') {
					this.$store.commit('OPEN_WINDOW', {
						id: 'terminal-' + Date.now(),
						title: this.$t('Terminal'),
						component: 'TerminalPanel',
						width: 720,
						height: 480
					})
				}
			} else if (action === 'unpin') {
				if (!this.unpinnedSystemIds.includes(p.id)) {
					this.unpinnedSystemIds.push(p.id)
					localStorage.setItem('unpinned_system_dock', JSON.stringify(this.unpinnedSystemIds))
					this.$buefy.toast.open({
						message: this.$t('Removed from taskbar'),
						type: 'is-info',
						duration: 2000
					})
				}
			} else if (action === 'close') {
				const win = this.findWindow(p.id)
				if (win) this.$store.commit('CLOSE_WINDOW', win.id)
			}
		},
		handleUserAppAction(action) {
			const app = this.ctxMenu.target.data
			this.closeCtxMenu()
			if (action === 'open') {
				this.launchApp(app)
			} else if (action === 'edit') {
				this.$EventBus.$emit(events.SHOW_CONFIG_PANEL, app)
			} else if (action === 'restart') {
				this.$messageBus('apps_restart', app.name)
				const req = app.app_type === 'v2app'
					? this.$openAPI.appManagement.compose.setComposeAppStatus(app.name, 'restart')
					: this.$api.container.updateState(app.name, 'restart')
				req.then(() => {
					this.$buefy.toast.open({ message: this.$t('Restarting app...'), type: 'is-info' })
				})
			} else if (action === 'unpin') {
				this.setDockPinned(app.name, false).then(() => {
					this.loadPinnedApps()
					this.$buefy.toast.open({ message: this.$t('Removed from taskbar'), type: 'is-info' })
				})
			}
		},
		handleWindowAction(action) {
			const win = this.ctxMenu.target.data
			this.closeCtxMenu()
			if (action === 'toggle') {
				this.toggleWindow(win)
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
			this.unpinnedSystemIds = []
			localStorage.removeItem('unpinned_system_dock')
			this.$buefy.toast.open({ message: this.$t('Taskbar reset to default'), type: 'is-success' })
		},
		isAppRunning(app) {
			return app.status === 'running' || this.isOpen(app.name)
		},
		async loadPinnedApps() {
			const pins = await this.getDockPins()
			if (!pins.length) {
				this.pinnedApps = []
				return
			}
			const [orgAppList, linkAppList, overrides] = await Promise.all([
				this.$openAPI.appGrid.getAppGrid().then(res => res.data.data || []),
				this.getLinkAppList(),
				this.getLegacyAppOverrides()
			])
			const all = orgAppList.concat(linkAppList)
			this.pinnedApps = pins
				.map(name => all.find(a => a.name === name))
				.filter(Boolean)
				.map(app => {
					const override = overrides[app.name]
					const icon = (override && override.icon) || app.icon || require('@/assets/img/app/default.svg')
					const title = override && override.title ? { ...app.title, custom: override.title } : app.title
					const overrideUrl = override && override.url
					return { ...app, icon, title, overrideUrl }
				})
		},
		displayName(app) {
			return (app.title && ice_i18n(app.title)) || app.name
		},
		launchApp(app) {
			if (app.app_type === 'container') {
				if (app.overrideUrl) window.open(app.overrideUrl, '_blank')
				return
			}
			if (app.app_type === 'LinkApp') {
				window.open(app.hostname, '_blank')
				return
			}
			if (app.status === 'running') {
				this.openAppToNewWindow(app)
				return
			}
			const request = app.app_type === 'v2app'
				? this.$openAPI.appManagement.compose.setComposeAppStatus(app.name, 'start')
				: this.$api.container.updateState(app.name, 'start')
			request.then(() => this.firstOpenThirdApp(app))
		},
		findWindow(id) {
			return this.windows.find(w => w.id === id)
		},
		isViewerWindow(win) {
			return VIEWER_COMPONENTS.includes(win.component)
		},
		isVmWindow(win) {
			return VM_ICON_COMPONENTS.includes(win.component)
		},
		isOpen(id) {
			return !!this.findWindow(id)
		},
		isMinimized(id) {
			const win = this.findWindow(id)
			return !!(win && win.minimized)
		},
		isTopmost(win) {
			return win.zIndex === Math.max(...this.windows.map(w => w.zIndex))
		},
		launch(p) {
			const win = this.findWindow(p.id)
			if (!win) {
				this.open(p.id)
				return
			}
			this.toggleWindow(win)
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
		open(id) {
			if (id === 'files') {
				this.$store.commit('OPEN_WINDOW', {
					id: 'files', title: this.$t('Files'), component: 'FilesApp', width: 960, height: 620
				})
			} else if (id === 'appstore') {
				this.$store.commit('OPEN_WINDOW', {
					id: 'appstore', title: this.$t('App Store'), component: 'AppStoreApp', width: 1040, height: 720
				})
			} else if (id === 'terminal') {
				this.$store.commit('OPEN_WINDOW', {
					id: 'terminal', title: this.$t('Terminal'), component: 'TerminalPanel', width: 720, height: 480
				})
			} else if (id === 'settings') {
				this.$store.commit('OPEN_WINDOW', {
					id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540
				})
			} else if (id === 'vms') {
				this.$store.commit('OPEN_WINDOW', {
					id: 'vms', title: this.$t('VMs'), component: 'VmManagerApp', width: 880, height: 560
				})
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.dock-container {
	position: fixed;
	left: 50%;
	bottom: 0.9rem;
	transform: translateX(-50%);
	z-index: 500;
}

.dock {
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

/* Dock Right Click Menu */
.dock-context-menu {
	position: fixed;
	width: 210px;
	background: rgba(255, 255, 255, 0.92);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid rgba(255, 255, 255, 0.65);
	border-radius: 12px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.18), 0 2px 8px rgba(0, 0, 0, 0.08);
	padding: 0.35rem;
	user-select: none;
	animation: dockCtxFade 0.12s cubic-bezier(0.16, 1, 0.3, 1);
	z-index: 10000;
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
	padding: 0.42rem 0.65rem;
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
		font-size: 1.05rem;
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
