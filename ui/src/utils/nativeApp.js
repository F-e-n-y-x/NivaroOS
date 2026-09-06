// Detects whether this page is running inside the NivaroOS native Android/
// iOS app (see /mobile in the repo root - a Capacitor shell whose only
// bundled content is a small "enter your server address" bootstrap page,
// which then navigates this same webview to the user's own live NivaroOS
// server). Capacitor keeps its native bridge (window.Capacitor) available
// even after that navigation - allowNavigation in mobile/capacitor.config.json
// is what permits this - so this file works whether the current page is
// that bootstrap page or (as in every real case once actually in use) this
// full app itself.
//
// Everything here is a no-op when not running inside that shell (a normal
// browser has no window.Capacitor at all), so importing this anywhere in
// the app carries no risk to the existing web experience.

export function isNativeApp() {
	return !!(window.Capacitor && window.Capacitor.isNativePlatform && window.Capacitor.isNativePlatform())
}

// Clears the saved server address (stored via the native Preferences
// plugin, not localStorage - it has to survive navigating away from the
// bootstrap page's own origin, see mobile/www/index.html) and sends the
// webview back to the bootstrap page so the user can enter a different
// server. There's no way back to "this" server afterwards except
// re-entering its address, same as signing out.
export async function changeServer() {
	if (!isNativeApp()) return
	const prefs = window.Capacitor.Plugins && window.Capacitor.Plugins.Preferences
	if (prefs) {
		await prefs.remove({ key: 'nivaroos_server_url' })
	}
	window.location.href = 'capacitor://localhost/index.html?reset=1'
}
