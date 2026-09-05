const WINDOWS_STORAGE_KEY = 'nivaroos_open_windows'

// Only system-app windows persist across sessions - edit-app windows
// carry a live snapshot of an app list item as props, which is
// meaningless to resurrect after a reload (the app list itself will
// have been refetched fresh by then).
const PERSISTABLE_COMPONENTS = ['FilesApp', 'TerminalPanel', 'SettingsApp', 'AppStoreApp']

function persistWindows(state) {
	const toSave = state.windows
		.filter(w => PERSISTABLE_COMPONENTS.includes(w.component))
		.map(({ id, title, component, x, y, width, height, minimized }) => ({ id, title, component, x, y, width, height, minimized }))
	localStorage.setItem(WINDOWS_STORAGE_KEY, JSON.stringify(toSave))
}

const mutations = {
	// User and tokens
	SET_ACCESS_TOKEN(state, token) {
		state.access_token = token
	},

	SET_REFRESH_TOKEN(state, token) {
		state.refresh_token = token
	},

	SET_USER(state, user) {
		state.user = user
	},

	SET_INIT_KEY(state, key) {
		state.initKey = key
	},

	// Site
	SET_SITE_LOADING(state, loading) {
		state.siteLoading = loading
	},

	SET_NEED_INITIALIZATION(state, need) {
		state.needInitialization = need
	},

	SET_SIDEBAR_CLOSE(state) {
		state.sidebarOpen = false
	},

	TOOGLE_SIDEBAR_STATE(state) {
		state.sidebarOpen = !state.sidebarOpen
	},

	SET_WALLPAPER(state, wallpaper) {
		localStorage.setItem('wallpaper', wallpaper.path)
		state.wallpaperObject = wallpaper
	},

	SET_DEFAULT_WALLPAPER(state) {
		state.wallpaperObject = {
			path: require('@/assets/background/default_wallpaper.jpg'),
			from: "Built-in" //Built-in, Upload, Files
		}
	},

	SET_IS_MOBILE(state, val) {
		state.isMobile = val
	},

	SET_SEARCH_ENGINE(state, val) {
		state.searchEngine = val
	},

	SET_SEARCH_ENGINE_SWITCH(state, val) {
		state.searchEngineSwitch = val
	},

	SET_EXISTING_APPS_SWITCH(state, val) {
		state.existingAppsSwitch = val
	},

	SET_RECOMMEND_SWITCH(state, val) {
		state.recommendSwitch = val
	},

	SET_HARDWARE_INFO(state, val) {
		state.hardwareInfo = val
	},

	SET_CURRENT_PATH(state, val) {
		state.currentPath = val
	},

	SET_VIEW_MODE(state, val) {
		localStorage.setItem('filesViewMode', val)
		state.viewMode = val
	},

	SET_OPERATE_OBJECT(state, val) {
		state.operateObject = val
	},

	SHOW_DRAG_DROP_MENU(state, { x, y, payload, targetPath }) {
		state.dragDropMenu = { visible: true, x, y, payload, targetPath }
	},

	HIDE_DRAG_DROP_MENU(state) {
		state.dragDropMenu = { visible: false, x: 0, y: 0, payload: null, targetPath: null }
	},

	SET_NETWORK_STORAGE(state, val) {
		localStorage.setItem('networkStorage', JSON.stringify(val))
		state.networkStorage = val
	},

	// shortcut data mutations
	SET_SHORTCUT_DATA(state, val) {
		state.shortcutData = val
	},
	GET_SHORTCUT_DATA(state) {
		return state.shortcutData
	},

	// public params
	SET_DEVICE_ID(state, val) {
		state.device_id = val
	},

	SET_ACCESS_ID(state, val) {
		state.access_id = val
	},

	SET_LANGUAGE(state, val) {
		state.nivaroos_lang = val
	},

	SET_TIME_FORMAT(state, val) {
		localStorage.setItem('timeFormat', val)
		state.timeFormat = val
	},

	SET_DATE_FORMAT_STYLE(state, val) {
		localStorage.setItem('dateFormatStyle', val)
		state.dateFormatStyle = val
	},

	SET_SHOW_SECONDS(state, val) {
		localStorage.setItem('showSeconds', val)
		state.showSeconds = val
	},

	SET_CUSTOM_DATETIME_FORMAT(state, val) {
		localStorage.setItem('customDateTimeFormat', val)
		state.customDateTimeFormat = val
	},

	SET_SHOW_HIDDEN(state, val) {
		localStorage.setItem('filesShowHidden', val)
		state.showHidden = val
	},

	// TODO v2 does not have.
	SET_NOTIMPORT_LIST(state, val) {
		state.notImportList = val
	},

	// Desktop windowing system
	OPEN_WINDOW(state, { id, title, component, props, width, height, x, y }) {
		const existing = state.windows.find(w => w.id === id)
		if (existing) {
			existing.minimized = false
			existing.zIndex = state.nextWindowZIndex++
			if (props) existing.props = { ...existing.props, ...props }
			persistWindows(state)
			return
		}
		// Stagger new windows so they don't stack exactly on top of each other.
		const offset = (state.windows.length % 6) * 24
		state.windows.push({
			id,
			title,
			component,
			props: props || {},
			x: x !== undefined ? x : 80 + offset,
			y: y !== undefined ? y : 60 + offset,
			width: width || 900,
			height: height || 600,
			zIndex: state.nextWindowZIndex++,
			minimized: false
		})
		persistWindows(state)
	},

	CLOSE_WINDOW(state, id) {
		state.windows = state.windows.filter(w => w.id !== id)
		persistWindows(state)
	},

	FOCUS_WINDOW(state, id) {
		const win = state.windows.find(w => w.id === id)
		if (win) {
			win.minimized = false
			win.zIndex = state.nextWindowZIndex++
			persistWindows(state)
		}
	},

	TOGGLE_MINIMIZE_WINDOW(state, id) {
		const win = state.windows.find(w => w.id === id)
		if (win) {
			win.minimized = !win.minimized
			persistWindows(state)
		}
	},

	UPDATE_WINDOW_RECT(state, { id, x, y, width, height }) {
		const win = state.windows.find(w => w.id === id)
		if (!win) return
		if (x !== undefined) win.x = x
		if (y !== undefined) win.y = y
		if (width !== undefined) win.width = width
		if (height !== undefined) win.height = height
	},

	// Drag/resize call UPDATE_WINDOW_RECT on every mousemove for a smooth
	// visual, but persisting to localStorage on every pixel of movement
	// is real jank - callers commit this once on mouseup instead.
	PERSIST_WINDOWS(state) {
		persistWindows(state)
	},

	// Restores system-app windows (Files/Terminal/Settings) left open
	// from a previous session - called once on load if nothing has
	// opened any windows yet this session.
	RESTORE_WINDOWS(state, savedWindows) {
		if (state.windows.length || !savedWindows.length) return
		let maxZ = state.nextWindowZIndex
		state.windows = savedWindows.map(w => {
			const zIndex = maxZ++
			return { ...w, props: {}, zIndex }
		})
		state.nextWindowZIndex = maxZ
	},

}
export default mutations
