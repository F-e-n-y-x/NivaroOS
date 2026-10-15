// Shared window metadata, used by both DesktopWindow.vue (the desktop/
// tablet floating-window chrome) and MobileScreenHost.vue (the mobile
// fullscreen "app shell" chrome) - kept in one place so the two chromes
// can never silently drift apart on which components get which treatment.
// Every window loads its own chunk when first opened, keeping the entry small.
import { defineAsyncComponent } from 'vue'

export const COMPONENT_REGISTRY = {
	FilesApp: defineAsyncComponent(() => import('@/apps/files/FilesApp.vue')),
	TerminalPanel: defineAsyncComponent(() => import('@/apps/terminal/TerminalPanel.vue')),
	ContainerConsolePanel: defineAsyncComponent(() => import('@/shell/desktop/ContainerConsolePanel.vue')),
	VmConsolePanel: defineAsyncComponent(() => import('@/apps/vm/VmConsolePanel.vue')),
	HostDesktopPanel: defineAsyncComponent(() => import('@/shell/desktop/HostDesktopPanel.vue')),
	CreateVmModal: defineAsyncComponent(() => import('@/apps/vm/CreateVmModal.vue')),
	EditVmModal: defineAsyncComponent(() => import('@/apps/vm/EditVmModal.vue')),
	SettingsApp: defineAsyncComponent(() => import('@/apps/settings/SettingsApp.vue')),
	AppStoreApp: defineAsyncComponent(() => import('@/apps/app-store/AppStoreApp.vue')),
	LegacyAppEditPanel: defineAsyncComponent(() => import('@/apps/app-store/LegacyAppEditPanel.vue')),
	VmManagerApp: defineAsyncComponent(() => import('@/apps/vm/VmManagerApp.vue')),
	DownloadStationApp: defineAsyncComponent(() => import('@/apps/download-station/DownloadStationApp.vue')),
	DsAddDownloadWindow: defineAsyncComponent(() => import('@/apps/download-station/DsAddDownloadWindow.vue')),
	DsDownloadDetailWindow: defineAsyncComponent(() => import('@/apps/download-station/DsDownloadDetailWindow.vue')),
	DsFolderPickerWindow: defineAsyncComponent(() => import('@/apps/download-station/DsFolderPickerWindow.vue')),
	DsAddTorrentWindow: defineAsyncComponent(() => import('@/apps/download-station/torrent/DsAddTorrentWindow.vue')),
	DsTorrentDetailWindow: defineAsyncComponent(() => import('@/apps/download-station/torrent/DsTorrentDetailWindow.vue')),
	RbSigninWindow: defineAsyncComponent(() => import('@/apps/download-station/rb/RbSigninWindow.vue')),
	// Backup & Sync (optional module; its launcher entries only show when
	// installed - utils/backupInstalled.js). Window ids/sizes: apps/backup/windows.js.
	BackupApp: defineAsyncComponent(() => import('@/apps/backup/BackupApp.vue')),
	BackupRunWindow: defineAsyncComponent(() => import('@/apps/backup/windows/BackupRunWindow.vue')),
	BackupBrowseWindow: defineAsyncComponent(() => import('@/apps/backup/windows/BackupBrowseWindow.vue')),
	BackupRestoreWindow: defineAsyncComponent(() => import('@/apps/backup/windows/BackupRestoreWindow.vue')),
	BackupJobWizardWindow: defineAsyncComponent(() => import('@/apps/backup/windows/BackupJobWizardWindow.vue')),
	BackupPreviewWindow: defineAsyncComponent(() => import('@/apps/backup/windows/BackupPreviewWindow.vue')),
	// Shared storage pickers (GET /v1/backup/locations), usable outside Backup.
	StoragePickerWindow: defineAsyncComponent(() => import('@/shared/storage/StoragePickerWindow.vue')),
	FolderPickerWindow: defineAsyncComponent(() => import('@/shared/storage/FolderPickerWindow.vue')),
	ImageViewer: defineAsyncComponent(() => import('@/apps/files/viewers/ImageViewer.vue')),
	VideoPlayer: defineAsyncComponent(() => import('@/apps/files/viewers/VideoPlayer.vue')),
	CodeEditor: defineAsyncComponent(() => import('@/apps/files/viewers/CodeEditor.vue')),
	DocViewer: defineAsyncComponent(() => import('@/apps/files/viewers/DocViewer.vue')),
	ExcelViewer: defineAsyncComponent(() => import('@/apps/files/viewers/ExcelViewer.vue')),
	PdfViewer: defineAsyncComponent(() => import('@/apps/files/viewers/PdfViewer.vue')),
	MarkdownEditor: defineAsyncComponent(() => import('@/apps/files/viewers/MarkdownEditor.vue')),
	UnsupportedViewer: defineAsyncComponent(() => import('@/apps/files/viewers/UnsupportedViewer.vue')),
	DetailWindow: defineAsyncComponent(() => import('@/apps/files/DetailWindow.vue')),
	FolderWindow: defineAsyncComponent(() => import('@/apps/files/FolderWindow.vue')),
	SystemUpdateWindow: defineAsyncComponent(() => import('@/apps/settings/SystemUpdateWindow.vue')),
	ScheduledTaskWindow: defineAsyncComponent(() => import('@/apps/settings/ScheduledTaskWindow.vue')),
	ScheduledTaskLogWindow: defineAsyncComponent(() => import('@/apps/settings/ScheduledTaskLogWindow.vue')),
	ConfirmDialogWindow: defineAsyncComponent(() => import('@/shared/basicComponents/ConfirmDialogWindow.vue')),
	PortalWindow: defineAsyncComponent(() => import('@/shell/desktop/PortalWindow.vue')),
	PromptDialogWindow: defineAsyncComponent(() => import('@/shared/basicComponents/PromptDialogWindow.vue')),
	AddToFolderPanel: defineAsyncComponent(() => import('@/apps/app-store/AddToFolderPanel.vue')),
	IconEditorModal: defineAsyncComponent(() => import('@/apps/app-store/IconEditorModal.vue')),
	ExternalLinkPanel: defineAsyncComponent(() => import('@/apps/app-store/ExternalLinkPanel.vue')),
	TipEditorModal: defineAsyncComponent(() => import('@/apps/app-store/TipEditorModal.vue')),
	ImportPanel: defineAsyncComponent(() => import('@/shared/forms/ImportPanel.vue')),
	AppTerminalPanel: defineAsyncComponent(() => import('@/apps/app-store/AppTerminalPanel.vue')),
	CreatePanel: defineAsyncComponent(() => import('@/apps/files/fileList/CreatePanel.vue')),
	FeedbackPanel: defineAsyncComponent(() => import('@/shared/feedback/FeedbackPanel.vue')),
	NotificationList: defineAsyncComponent(() => import('@/shell/desktop/NotificationList.vue')),
	FilePanel: defineAsyncComponent(() => import('@/apps/files/fileList/FilePanel.vue')),
	TransferConflictWindow: defineAsyncComponent(() => import('@/apps/files/dialogs/TransferConflictWindow.vue')),
	FreeMemoryWindow: defineAsyncComponent(() => import('@/shell/desktop/FreeMemoryWindow.vue')),
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
export const DARK_WINDOW_COMPONENTS = ['TerminalPanel', 'ContainerConsolePanel', 'SystemUpdateWindow', 'ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer', 'MarkdownEditor', 'UnsupportedViewer', 'VmConsolePanel', 'HostDesktopPanel', 'ScheduledTaskLogWindow']

export const NO_SCROLL_COMPONENTS = ['VideoPlayer', 'ImageViewer', 'VmConsolePanel', 'HostDesktopPanel', 'DownloadStationApp', 'DsAddDownloadWindow', 'DsDownloadDetailWindow', 'DsFolderPickerWindow', 'DsAddTorrentWindow', 'DsTorrentDetailWindow', 'RbSigninWindow', 'BackupBrowseWindow', 'BackupPreviewWindow', 'StoragePickerWindow', 'FolderPickerWindow']

export function resolveComponent(name) {
	return COMPONENT_REGISTRY[name]
}

// Which app a window belongs to, so the dock shows ONE icon per app: an
// app's child windows (Add Download, the backup job wizard, a VM console...)
// group under that app's dock icon instead of adding a second identical
// one. Keys are the dock's built-in app ids (Dock.vue BUILTIN_DEFS). A
// window can name its app itself with OPEN_WINDOW's appId (e.g. a shared
// picker opened from Backup); otherwise its component decides. Windows not
// listed here (viewers, dialogs, app editors) keep their own dock entry.
const APP_OF_COMPONENT = {
	'files': ['FilesApp', 'FolderWindow', 'DetailWindow', 'TransferConflictWindow'],
	'appstore': ['AppStoreApp'],
	'terminal': ['TerminalPanel', 'ContainerConsolePanel', 'AppTerminalPanel'],
	'host-desktop': ['HostDesktopPanel'],
	'download-station': ['DownloadStationApp', 'DsAddDownloadWindow', 'DsDownloadDetailWindow', 'DsFolderPickerWindow', 'DsAddTorrentWindow', 'DsTorrentDetailWindow', 'RbSigninWindow'],
	'backup': ['BackupApp', 'BackupJobWizardWindow', 'BackupRunWindow', 'BackupPreviewWindow', 'BackupBrowseWindow', 'BackupRestoreWindow'],
	'vms': ['VmManagerApp', 'VmConsolePanel', 'CreateVmModal', 'EditVmModal'],
	'settings': ['SettingsApp', 'SystemUpdateWindow', 'ScheduledTaskWindow', 'ScheduledTaskLogWindow']
}
const APP_BY_COMPONENT = Object.fromEntries(Object.entries(APP_OF_COMPONENT).flatMap(([app, comps]) => comps.map(c => [c, app])))

export function appIdOf(win) {
	return (win && (win.appId || APP_BY_COMPONENT[win.component])) || null
}

// The dock key a window groups under: its app, else the window itself.
export const dockKeyOf = win => appIdOf(win) || win.id

// Which window a click on an app's dock icon should act on: the app's
// front window if it isn't already the front-most visible one, else (with
// several) its back-most window, so repeated clicks cycle through them.
// Returns { id, minimize } - minimize when its only window is already in front.
export function dockClickTarget(appWindows, topZ) {
	if (!appWindows.length) return null
	const sorted = [...appWindows].sort((a, b) => a.zIndex - b.zIndex)
	const front = sorted[sorted.length - 1]
	if (front.minimized || front.zIndex !== topZ) return { id: front.id, minimize: false }
	if (sorted.length === 1) return { id: front.id, minimize: true }
	return { id: sorted[0].id, minimize: false }
}
