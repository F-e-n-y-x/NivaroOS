/**
 * NivaroOS Theme Manager
 * Supports: 'auto' (System / OS mode), 'dark', 'light'
 */

export const THEME_MODES = {
	AUTO: 'auto',
	DARK: 'dark',
	LIGHT: 'light'
}

const THEME_STORAGE_KEY = 'uiThemeMode'
let mql = null
let mqlListenerAttached = false

/**
 * Returns stored theme mode preference: 'auto' | 'dark' | 'light'
 */
export function getStoredThemeMode() {
	return localStorage.getItem(THEME_STORAGE_KEY) || THEME_MODES.AUTO
}

/**
 * Checks if the user's OS currently prefers dark mode
 */
export function getSystemIsDark() {
	if (typeof window === 'undefined' || !window.matchMedia) return true
	if (!mql) {
		mql = window.matchMedia('(prefers-color-scheme: dark)')
	}
	return mql ? mql.matches : true
}

/**
 * Resolves effective theme ('dark' | 'light') based on the given mode
 */
export function getEffectiveTheme(mode) {
	if (mode === THEME_MODES.DARK) return 'dark'
	if (mode === THEME_MODES.LIGHT) return 'light'
	return getSystemIsDark() ? 'dark' : 'light'
}

/**
 * Updates DOM attributes and dispatches theme change event
 */
function updateThemeDOM(mode) {
	const effective = getEffectiveTheme(mode)
	const isDark = effective === 'dark'
	const root = document.documentElement

	if (root.getAttribute('data-theme') === effective && root.getAttribute('data-theme-mode') === mode) {
		return { mode, effective, isDark }
	}

	root.setAttribute('data-theme', effective)
	root.setAttribute('data-theme-mode', mode)
	root.classList.toggle('is-dark', isDark)
	root.classList.toggle('is-light', !isDark)

	// Update meta theme-color for mobile browser address bar styling
	const metaTheme = document.querySelector('meta[name="theme-color"]')
	if (metaTheme) {
		metaTheme.setAttribute('content', isDark ? '#000000' : '#ffffff')
	}

	// Broadcast event for components that need to respond
	if (typeof window !== 'undefined') {
		window.dispatchEvent(new CustomEvent('nivaroos:theme-change', {
			detail: { mode, effective, isDark }
		}))
	}

	return { mode, effective, isDark }
}

/**
 * Ensures system media query listener is attached once and retained in module scope
 */
function ensureSystemThemeListener() {
	if (typeof window === 'undefined' || !window.matchMedia) return
	if (!mql) {
		mql = window.matchMedia('(prefers-color-scheme: dark)')
	}
	if (!mqlListenerAttached && mql) {
		const handler = () => {
			if (getStoredThemeMode() === THEME_MODES.AUTO) {
				updateThemeDOM(THEME_MODES.AUTO)
			}
		}
		if (mql.addEventListener) {
			mql.addEventListener('change', handler)
		} else if (mql.addListener) {
			mql.addListener(handler)
		}
		mqlListenerAttached = true
	}
}

/**
 * Applies theme to <html> element, updates classes and meta tags
 */
export function applyTheme(mode) {
	if (!mode || !Object.values(THEME_MODES).includes(mode)) {
		mode = getStoredThemeMode()
	}
	try {
		localStorage.setItem(THEME_STORAGE_KEY, mode)
	} catch (e) {}

	ensureSystemThemeListener()

	return updateThemeDOM(mode)
}

/**
 * Initialize theme immediately
 */
export function initTheme() {
	ensureSystemThemeListener()
	return applyTheme(getStoredThemeMode())
}
