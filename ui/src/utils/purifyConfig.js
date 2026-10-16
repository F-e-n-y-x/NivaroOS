// Config for every v-dompurify-html in the app (registered in main.js).
// Only links and classes survive: no src, style or event-handler
// attributes. Also used for toast/snackbar/dialog messages (ui/notices.js).
export const purifyConfig = {
	default: {
		ALLOWED_ATTR: ['target', 'href', 'class'],
	},
}
