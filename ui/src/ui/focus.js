// Tab / Shift+Tab wrap around inside `container` (dialogs, menus).
export function trapTab(e, container) {
	if (e.key !== 'Tab' || !container) return
	const f = container.querySelectorAll('a[href]:not([tabindex="-1"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])')
	if (!f.length) return
	const first = f[0]
	const last = f[f.length - 1]
	if (e.shiftKey && e.target === first) {
		e.preventDefault()
		last.focus()
	} else if (!e.shiftKey && e.target === last) {
		e.preventDefault()
		first.focus()
	}
}
