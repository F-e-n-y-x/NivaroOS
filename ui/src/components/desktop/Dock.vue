<template>
	<div class="dock">
		<button v-for="p in pinned" :key="p.id" class="dock-item" :title="p.label" @click="launch(p)">
			<img :src="p.icon" class="dock-icon" :alt="p.label" />
			<span class="dock-dot" v-if="isOpen(p.id)" :class="{ minimized: isMinimized(p.id) }"></span>
		</button>

		<div v-if="pinnedApps.length" class="dock-sep"></div>

		<button v-for="app in pinnedApps" :key="'pinned-' + app.name" class="dock-item" :title="displayName(app)"
			@click="launchApp(app)">
			<img :src="app.icon" class="dock-icon" :alt="displayName(app)" />
		</button>

		<div v-if="extraWindows.length" class="dock-sep"></div>

		<button v-for="win in extraWindows" :key="win.id" class="dock-item" :title="win.title" @click="toggleWindow(win)">
			<img v-if="isViewerWindow(win)" :src="viewerIconUrl" class="dock-icon" :alt="win.title" />
			<img v-else-if="win.component === 'FilesApp'" :src="filesIcon" class="dock-icon" :alt="win.title" />
			<img v-else-if="win.component === 'FolderWindow'" :src="filesIcon" class="dock-icon" :alt="win.title" />
			<img v-else-if="win.component === 'AppStoreApp'" :src="appStoreIcon" class="dock-icon" :alt="win.title" />
			<img v-else-if="win.component === 'TerminalPanel' || win.component === 'SystemUpdateWindow'" :src="terminalIcon" class="dock-icon" :alt="win.title" />
			<img v-else-if="isVmWindow(win)" :src="vmConsoleIconUrl" class="dock-icon" :alt="win.title" />
			<div v-else class="dock-icon dock-icon-generic">
				<b-icon icon="display-applications-outline" pack="casa" size="is-20"></b-icon>
			</div>
			<span class="dock-dot" :class="{ minimized: win.minimized }"></span>
		</button>
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

// Files' own file viewers (src/components/files/viewers/) - each opens as
// its own extra (unpinned) window, see DesktopWindow.vue's COMPONENT_REGISTRY.
const VIEWER_COMPONENTS = ['ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer']

// All VM Manager windows share the same VM icon in
// the taskbar - the console, and the Create/Edit VM windows alike.
const VM_ICON_COMPONENTS = ['VmConsolePanel', 'CreateVmModal', 'EditVmModal']

export default {
	name: 'dock',
	mixins: [business_ShowNewAppTag, business_OpenThirdApp, business_LinkApp, business_LegacyAppOverrides, business_DockPins],
	data() {
		return { pinned: PINNED, pinnedApps: [], filesIcon, terminalIcon }
	},
	computed: {
		windows() {
			return this.$store.state.windows
		},
		extraWindows() {
			const pinnedIds = this.pinned.map(p => p.id)
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
	beforeDestroy() {
		this.$EventBus.$off(events.RELOAD_APP_LIST, this.loadPinnedApps)
	},
	methods: {
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
	border: $backDropBorder;
	border-radius: 22px;
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25), $backDropShadow;
	z-index: 500;
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
</style>
