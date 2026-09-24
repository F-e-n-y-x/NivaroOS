<template>
	<div class="window-manager" @dragover.prevent="onDesktopDragOver" @drop="onDesktopDrop">
		<!-- Desktop/tablet: every open window floats independently and stays
		     mounted even while minimized (a live terminal session, an
		     in-progress file list load, unsaved settings state must survive
		     minimize/restore, not get torn down and recreated - minimized
		     ones are just hidden via CSS, see DesktopWindow's :class
		     binding). Phone: no floating windows at all - see
		     MobileScreenHost/MobileTabBar's own doc comments for why this
		     needs an entirely different presentation, not just smaller
		     versions of the same chrome. -->
		<template v-if="!isMobileShell">
			<desktop-window v-for="win in windows" :key="win.id" :win="win"></desktop-window>
			<dock></dock>
		</template>
		<template v-else>
			<mobile-screen-host></mobile-screen-host>
			<mobile-tab-bar></mobile-tab-bar>
		</template>

		<!-- Desktop System Tray (Notification Center & Clock Pill) -->
		<div class="desktop-status-tray" v-if="!isMobileShell">
			<notification-center></notification-center>
			<date-time-pill></date-time-pill>
		</div>

		<drag-drop-menu></drag-drop-menu>
		<!-- Server notification feed -> Notification Center (renders nothing). -->
		<notification-feed-sync></notification-feed-sync>
		<!-- One bottom-right column for the transient progress cards, so
		     file transfers and app installs stack instead of overlapping
		     in the same corner. -->
		<div class="shell-progress-stack" :class="{ 'is-mobile': isMobileShell }">
			<container-install-status></container-install-status>
			<transfers-panel></transfers-panel>
		</div>
	</div>
</template>

<script>
import DesktopWindow from './DesktopWindow.vue'
import Dock from './Dock.vue'
import MobileScreenHost from './MobileScreenHost.vue'
import MobileTabBar from './MobileTabBar.vue'
import NotificationCenter from './NotificationCenter.vue'
import DateTimePill from './DateTimePill.vue'
import DragDropMenu from './DragDropMenu.vue'
import TransfersPanel from './TransfersPanel.vue'
import ContainerInstallStatus from './ContainerInstallStatus.vue'
import NotificationFeedSync from './NotificationFeedSync.vue'
import { isFilesDragEvent, getFilesDragData } from '@/utils/files/dragDrop'

const WINDOWS_STORAGE_KEY = 'nivaroos_open_windows'
// Real folder (/DATA/Desktop) FolderTree.vue creates automatically if
// missing - dragging a file/folder onto the desktop background (outside
// any window) copies/moves it here, matching a real desktop's icons.
const DESKTOP_PATH = '/DATA/Desktop'

export default {
	name: 'window-manager',
	components: {
		DesktopWindow,
		Dock,
		MobileScreenHost,
		MobileTabBar,
		NotificationCenter,
		DateTimePill,
		DragDropMenu,
		TransfersPanel,
		ContainerInstallStatus,
		NotificationFeedSync
	},
	computed: {
		windows() {
			return this.$store.state.windows
		},
		isMobileShell() {
			return this.$store.state.isMobile || this.$store.state.isTablet
		}
	},
	created() {
		// Re-open whatever system-app windows (Files/Terminal/Settings)
		// were left open last session, at the same position/size - a
		// fresh session for Terminal specifically, since the actual pty
		// process can't survive a page reload either way.
		try {
			const saved = JSON.parse(localStorage.getItem(WINDOWS_STORAGE_KEY) || '[]')
			this.$store.commit('RESTORE_WINDOWS', saved)
		} catch (e) {
			// malformed storage - ignore, nothing to restore
		}
	},
	methods: {
		// Only reached if nothing underneath (a window, the sidebar, a tab)
		// already stopped propagation while handling the drop itself.
		onDesktopDragOver() {},
		onDesktopDrop(event) {
			if (!isFilesDragEvent(event)) return
			const payload = getFilesDragData(event)
			if (!payload || payload.from === DESKTOP_PATH) return
			this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: DESKTOP_PATH })
		}
	}
}
</script>

<style lang="scss" scoped>
.shell-progress-stack {
	position: fixed;
	right: 1.5rem;
	bottom: 4.8rem;
	z-index: 9999;
	display: flex;
	flex-direction: column;
	align-items: flex-end;
	gap: var(--space-3);
	max-width: calc(100vw - 2rem);
	max-height: calc(100vh - 6rem);
	pointer-events: none;

	// Above the phone tab bar, full width minus the gutters.
	&.is-mobile {
		left: var(--space-4);
		right: var(--space-4);
		bottom: calc(4.25rem + env(safe-area-inset-bottom, 0px));
		max-width: none;
		align-items: stretch;
	}

	> * {
		pointer-events: auto;
	}
}

.desktop-status-tray {
	position: fixed;
	right: 1.5rem;
	bottom: 0.9rem;
	display: flex;
	align-items: center;
	gap: var(--space-3);
	z-index: 99995;
	pointer-events: none;

	> * {
		pointer-events: auto;
	}
}
</style>
