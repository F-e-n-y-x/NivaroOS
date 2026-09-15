// Buefy's toast/snackbar/dialog/notification components all render their
// `message` prop with v-html (see NoticeMixin-based components in
// node_modules/buefy/src/components/{toast,snackbar,dialog,notification}).
// That's relied on elsewhere in this codebase to put a literal, developer-
// authored `<i class="mdi ...">` icon in front of a message - so those
// components can't just be swapped for a safe alternative.
//
// The actual risk is interpolating a value the current user didn't type
// themselves (a LAN peer's device/file name, a container or app name that
// came from a docker-compose manifest someone else authored) into one of
// those messages: unescaped, it's stored/reflected XSS running with this
// session's access - including the JWT sitting in localStorage.
//
// Escape any such value before it goes into a toast/snackbar/dialog message.
export function escapeHtml(value) {
	if (value === null || value === undefined) return ''
	return String(value)
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&#39;')
}

export default escapeHtml
