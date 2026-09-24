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
import DownloadStationApp from '@/apps/download-station/DownloadStationApp.vue'
import DsAddDownloadWindow from '@/apps/download-station/DsAddDownloadWindow.vue'
import DsDownloadDetailWindow from '@/apps/download-station/DsDownloadDetailWindow.vue'
import DsFolderPickerWindow from '@/apps/download-station/DsFolderPickerWindow.vue'
import ImageViewer from '@/apps/files/viewers/ImageViewer.vue'
import VideoPlayer from '@/apps/files/viewers/VideoPlayer.vue'
import CodeEditor from '@/apps/files/viewers/CodeEditor.vue'
import DocViewer from '@/apps/files/viewers/DocViewer.vue'
import ExcelViewer from '@/apps/files/viewers/ExcelViewer.vue'
import PdfViewer from '@/apps/files/viewers/PdfViewer.vue'
import MarkdownEditor from '@/apps/files/viewers/MarkdownEditor.vue'
import UnsupportedViewer from '@/apps/files/viewers/UnsupportedViewer.vue'
import DetailWindow from '@/apps/files/DetailWindow.vue'
import VmConsolePanel from '@/apps/vm/VmConsolePanel.vue'
import CreateVmModal from '@/apps/vm/CreateVmModal.vue'
import EditVmModal from '@/apps/vm/EditVmModal.vue'
import FolderWindow from '@/apps/files/FolderWindow.vue'
import SystemUpdateWindow from '@/apps/settings/SystemUpdateWindow.vue'
import ContainerConsolePanel from '@/shell/desktop/ContainerConsolePanel.vue'
import ScheduledTaskWindow from '@/apps/settings/ScheduledTaskWindow.vue'
import ScheduledTaskLogWindow from '@/apps/settings/ScheduledTaskLogWindow.vue'
import ConfirmDialogWindow from '@/shared/basicComponents/ConfirmDialogWindow.vue'
import PortalWindow from '@/shell/desktop/PortalWindow.vue'
import PromptDialogWindow from '@/shared/basicComponents/PromptDialogWindow.vue'
import HostDesktopPanel from '@/shell/desktop/HostDesktopPanel.vue'
import AddToFolderPanel from '@/apps/app-store/AddToFolderPanel.vue'
import IconEditorModal from '@/apps/app-store/IconEditorModal.vue'
import ExternalLinkPanel from '@/apps/app-store/ExternalLinkPanel.vue'
import TipEditorModal from '@/apps/app-store/TipEditorModal.vue'
import ImportPanel from '@/shared/forms/ImportPanel.vue'
import AppTerminalPanel from '@/apps/app-store/AppTerminalPanel.vue'
import CreatePanel from '@/apps/files/fileList/CreatePanel.vue'
import FeedbackPanel from '@/shared/feedback/FeedbackPanel.vue'
import NotificationList from '@/shell/desktop/NotificationList.vue'
import FilePanel from '@/apps/files/fileList/FilePanel.vue'
import TransferConflictWindow from '@/apps/files/dialogs/TransferConflictWindow.vue'

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
	DownloadStationApp,
	DsAddDownloadWindow,
	DsDownloadDetailWindow,
	DsFolderPickerWindow,
	ImageViewer,
	VideoPlayer,
	CodeEditor,
	DocViewer,
	ExcelViewer,
	PdfViewer,
	MarkdownEditor,
	UnsupportedViewer,
	DetailWindow,
	FolderWindow,
	SystemUpdateWindow,
	ScheduledTaskWindow,
	ScheduledTaskLogWindow,
	ConfirmDialogWindow,
	PortalWindow,
	PromptDialogWindow,
	AddToFolderPanel,
	IconEditorModal,
	ExternalLinkPanel,
	TipEditorModal,
	ImportPanel,
	AppTerminalPanel,
	CreatePanel,
	FeedbackPanel,
	NotificationList,
	FilePanel,
	TransferConflictWindow
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

export const NO_SCROLL_COMPONENTS = ['VideoPlayer', 'ImageViewer', 'VmConsolePanel', 'HostDesktopPanel', 'DownloadStationApp', 'DsAddDownloadWindow', 'DsDownloadDetailWindow', 'DsFolderPickerWindow']

export function resolveComponent(name) {
	return COMPONENT_REGISTRY[name]
}

