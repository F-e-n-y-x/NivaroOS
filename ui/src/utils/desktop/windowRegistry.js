// Shared window metadata, used by both DesktopWindow.vue (the desktop/
// tablet floating-window chrome) and MobileScreenHost.vue (the mobile
// fullscreen "app shell" chrome) - kept in one place so the two chromes
// can never silently drift apart on which components get which treatment.
import FilesApp from '@/apps/files/FilesApp.vue'
import TerminalPanel from '@/apps/terminal/TerminalPanel.vue'
import SettingsApp from '@/apps/settings/SettingsApp.vue'
import AppStoreApp from '@/apps/app-store/AppStoreApp.vue'
import LegacyAppEditPanel from '@/apps/app-store/LegacyAppEditPanel.vue'
import VmManagerApp from '@/apps/vm/VmManagerApp.vue'
import ImageViewer from '@/apps/files/viewers/ImageViewer.vue'
import VideoPlayer from '@/apps/files/viewers/VideoPlayer.vue'
import CodeEditor from '@/apps/files/viewers/CodeEditor.vue'
import DocViewer from '@/apps/files/viewers/DocViewer.vue'
import ExcelViewer from '@/apps/files/viewers/ExcelViewer.vue'
import PdfViewer from '@/apps/files/viewers/PdfViewer.vue'
import VmConsolePanel from '@/apps/vm/VmConsolePanel.vue'
import CreateVmModal from '@/apps/vm/CreateVmModal.vue'
import EditVmModal from '@/apps/vm/EditVmModal.vue'
import FolderWindow from '@/apps/files/FolderWindow.vue'
import SystemUpdateWindow from '@/apps/settings/SystemUpdateWindow.vue'
import ContainerConsolePanel from '@/shell/desktop/ContainerConsolePanel.vue'
import ScheduledTaskWindow from '@/apps/settings/ScheduledTaskWindow.vue'
import HostDesktopPanel from '@/shell/desktop/HostDesktopPanel.vue'

export const COMPONENT_REGISTRY = {
	FilesApp,
	TerminalPanel,
	ContainerConsolePanel,
	VmConsolePanel,
	HostDesktopPanel,
	CreateVmModal,
	EditVmModal,
	SettingsApp,
	AppStoreApp,
	LegacyAppEditPanel,
	VmManagerApp,
	ImageViewer,
	VideoPlayer,
	CodeEditor,
	DocViewer,
	ExcelViewer,
	PdfViewer,
	FolderWindow,
	SystemUpdateWindow,
	ScheduledTaskWindow
}

// These components' own top row IS the window's titlebar (draggable, with
// their own minimize/close controls, no maximize by design) - the shared
// chrome (desktop titlebar, or mobile back-bar) would just be a redundant
// second bar on top of it.
export const OWN_TITLEBAR_COMPONENTS = ['FilesApp', 'TerminalPanel', 'ContainerConsolePanel']

// Every viewer's own ViewerChrome toolbar is dark (#262626) - a white
// window titlebar sitting directly above that read as a visibly mismatched
// seam, so these windows get the same dark titlebar treatment TerminalPanel
// already uses.
export const DARK_WINDOW_COMPONENTS = ['TerminalPanel', 'ContainerConsolePanel', 'SystemUpdateWindow', 'ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer', 'VmConsolePanel', 'HostDesktopPanel']

export const NO_SCROLL_COMPONENTS = ['VideoPlayer', 'ImageViewer', 'VmConsolePanel', 'HostDesktopPanel']

export function resolveComponent(name) {
	return COMPONENT_REGISTRY[name]
}

