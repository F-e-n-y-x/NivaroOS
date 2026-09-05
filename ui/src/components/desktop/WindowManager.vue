<template>
	<div class="window-manager" @dragover.prevent="onDesktopDragOver" @drop="onDesktopDrop">
		<!-- Every open window stays mounted even while minimized - a
		window's content (a live terminal session, an in-progress file
		list load, unsaved settings state) must survive minimize/restore,
		not get torn down and recreated. Minimized ones are just hidden
		via CSS (see DesktopWindow's :class binding). -->
		<desktop-window v-for="win in windows" :key="win.id" :win="win"></desktop-window>
		<dock></dock>
		<notification-center></notification-center>
		<date-time-pill></date-time-pill>
		<drag-drop-menu></drag-drop-menu>
		<file-operation-status></file-operation-status>
		<container-install-status></container-install-status>
	</div>
</template>

<script>
import DesktopWindow from './DesktopWindow.vue'
import Dock from './Dock.vue'
import NotificationCenter from './NotificationCenter.vue'
import DateTimePill from './DateTimePill.vue'
import DragDropMenu from './DragDropMenu.vue'
import FileOperationStatus from './FileOperationStatus.vue'
import ContainerInstallStatus from './ContainerInstallStatus.vue'
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
		NotificationCenter,
		DateTimePill,
		DragDropMenu,
		FileOperationStatus,
		ContainerInstallStatus
	},
	computed: {
		windows() {
			return this.$store.state.windows
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
