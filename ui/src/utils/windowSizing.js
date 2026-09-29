// How big a desktop window opens.
//
// Windows used to open at each call site's fixed pixel size (Files
// 960x620, VMs 880x560, ...) at the top-left, whatever the screen - on a
// big monitor every app opened small and the first thing to do was drag
// a corner. Now:
//   - an app window the user has resized opens at that size again
//     (remembered per app, in this browser);
//   - otherwise a main app opens large: about three quarters of the
//     screen, never smaller than its own size, centred;
//   - small tool windows and dialogs keep the size their caller asks for.

const STORAGE_KEY = 'nivaroos_window_sizes'

// The apps you open from the dock or the start menu.
export const MAIN_APP_COMPONENTS = [
	'FilesApp', 'FolderWindow', 'AppStoreApp', 'VmManagerApp', 'DownloadStationApp', 'BackupApp',
	'SettingsApp', 'HostDesktopPanel', 'TerminalPanel', 'VmConsolePanel', 'ContainerConsolePanel'
]

// Room the dock and the top bar take.
const CHROME_H = 88
const MARGIN = 16

function readAll() {
	try {
		const v = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
		return v && typeof v === 'object' ? v : {}
	} catch (e) {
		return {}
	}
}

export function rememberedSize(component) {
	const s = readAll()[component]
	return s && s.width > 0 && s.height > 0 ? s : null
}

// Called when the user finishes resizing a window.
export function rememberSize(component, width, height) {
	if (!component || !(width > 0) || !(height > 0)) return
	try {
		const all = readAll()
		all[component] = { width: Math.round(width), height: Math.round(height) }
		localStorage.setItem(STORAGE_KEY, JSON.stringify(all))
	} catch (e) {
		// Private mode or full storage: the next window just opens at the default.
	}
}

// The rect a new, non-dialog desktop window opens at. [req] is what the
// caller asked for ({width, height, x, y}); [open] is how many windows
// are already open (to stagger); [remembered] the saved size or null.
export function openRect(component, req, viewport, open = 0, remembered = null) {
	const vw = viewport.width, vh = viewport.height
	const maxW = Math.max(280, vw - MARGIN * 2)
	const maxH = Math.max(200, vh - CHROME_H)
	let width = req.width || 900
	let height = req.height || 600
	const main = MAIN_APP_COMPONENTS.includes(component)
	if (remembered) {
		width = remembered.width
		height = remembered.height
	} else if (main) {
		width = Math.max(width, Math.round(vw * 0.74))
		height = Math.max(height, Math.round(vh * 0.8))
		width = Math.min(width, 1480)
		height = Math.min(height, 980)
	}
	width = Math.min(width, maxW)
	height = Math.min(height, maxH)
	const offset = (open % 6) * 24
	let x = req.x
	let y = req.y
	if (x === undefined) x = main || remembered ? Math.round((vw - width) / 2) + offset : 80 + offset
	if (y === undefined) y = main || remembered ? Math.max(12, Math.round((vh - CHROME_H - height) / 2) + 12) + offset : 60 + offset
	x = Math.max(0, Math.min(x, vw - width))
	y = Math.max(0, Math.min(y, vh - height - 40))
	return { x, y, width, height }
}
