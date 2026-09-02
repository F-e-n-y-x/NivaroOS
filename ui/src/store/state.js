const state = {
	// User
	access_token: "",
	refresh_token: "",
	user: {
		avatar: "",
		created_at: "",
		description: "",
		email: "",
		id: 0,
		nickname: "",
		role: "",
		updated_at: "",
		username: ""
	},
	initKey: "", // Initialization key for reg

	sidebarOpen: false,

	// System Config
	searchEngine: '',
	searchEngineSwitch: true,
	existingAppsSwitch: true,
	recommendSwitch: true,

	siteLoading: true,
	needInitialization: false,
	hardwareInfo: {},
	isMobile: false,

	// Files
	operateObject: null,
	currentPath: "",
	// 'grid' (thumbnails) | 'grid-large' (bigger thumbnails) | 'list' (details)
	viewMode: localStorage.getItem('filesViewMode') || 'grid',
	// Drag-and-drop copy/move menu ("Copy here"/"Move here"), shown wherever
	// a files drag is dropped - the source and target can be in entirely
	// different tabs/windows/the sidebar/the desktop, so this needs to be
	// reachable from (and renderable independent of) any one of them.
	dragDropMenu: {
		visible: false,
		x: 0,
		y: 0,
		payload: null, // { items: [path, ...], from: sourceFolderPath }
		targetPath: null
	},

	// Wallpaper - read the persisted path immediately so the login/
	// welcome screens (which load before any authenticated fetch can
	// happen) show the same background as the desktop, instead of
	// always falling back to the hardcoded default.
	wallpaperObject: {
		path: localStorage.getItem('wallpaper') || require('@/assets/background/default_wallpaper.jpg'),
		from: "Built-in" //Built-in, Upload, Files
	},

	// Samba and nfs data
	networkStorage: JSON.parse(localStorage.getItem('networkStorage')) || [],

	// shortcut data
	shortcutData: [],

	// public params
	device_id: "xxx",
	access_id: "dsdad",
	nivaroos_lang: "zh",
	notImportList: [],

	// Desktop windowing system (Files, Settings, future apps) - shape:
	// { id, title, component, props, x, y, width, height, zIndex, minimized }
	windows: [],
	nextWindowZIndex: 100,

	// Desktop date/time pill - kept in Vuex (not just localStorage) so the
	// pill (DesktopWindow.vue's sibling) and the System settings panel
	// stay in sync live, without a page reload.
	timeFormat: localStorage.getItem('timeFormat') || 'HH:MM', // 'HH:MM' (24h) | 'h:MM TT' (12h)
	dateFormatStyle: localStorage.getItem('dateFormatStyle') || 'long', // 'long' | 'medium' | 'short'
	showSeconds: localStorage.getItem('showSeconds') === 'true',
	// A strftime-style pattern (e.g. "%Y-%m-%d %H:%M:%S") that overrides the
	// preset time/date/seconds controls entirely when non-empty.
	customDateTimeFormat: localStorage.getItem('customDateTimeFormat') || '',
}
export default state
