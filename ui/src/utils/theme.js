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
let mediaQueryListener = null

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
	return window.matchMedia('(prefers-color-scheme: dark)').matches
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
 * Applies theme to <html> element, updates classes and meta tags
 */
export function applyTheme(mode) {
	if (!mode) mode = getStoredThemeMode()
	try {
		localStorage.setItem(THEME_STORAGE_KEY, mode)
	} catch (e) {}

	const effective = getEffectiveTheme(mode)
	const isDark = effective === 'dark'
	const root = document.documentElement

	root.setAttribute('data-theme', effective)
	root.setAttribute('data-theme-mode', mode)
	root.classList.toggle('is-dark', isDark)
	root.classList.toggle('is-light', !isDark)

	// Update meta theme-color for mobile browser address bar styling
	const metaTheme = document.querySelector('meta[name="theme-color"]')
	if (metaTheme) {
		metaTheme.setAttribute('content', isDark ? '#000000' : '#ffffff')
	}

	// Listen for OS scheme changes when in 'auto' mode
	if (typeof window !== 'undefined' && window.matchMedia) {
		const mql = window.matchMedia('(prefers-color-scheme: dark)')

		if (mediaQueryListener) {
			if (mql.removeEventListener) {
				mql.removeEventListener('change', mediaQueryListener)
			} else if (mql.removeListener) {
				mql.removeListener(mediaQueryListener)
			}
			mediaQueryListener = null
		}

		if (mode === THEME_MODES.AUTO) {
			mediaQueryListener = () => {
				if (getStoredThemeMode() === THEME_MODES.AUTO) {
					applyTheme(THEME_MODES.AUTO)
				}
			}
			if (mql.addEventListener) {
				mql.addEventListener('change', mediaQueryListener)
			} else if (mql.addListener) {
				mql.addListener(mediaQueryListener)
			}
		}
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
 * Initialize theme immediately
 */
export function initTheme() {
	return applyTheme(getStoredThemeMode())
}
