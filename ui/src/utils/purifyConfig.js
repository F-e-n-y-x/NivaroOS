// Config for every v-dompurify-html in the app (registered in main.js).
// Only links survive: no src, style or event-handler attributes.
export const purifyConfig = {
	default: {
		ALLOWED_ATTR: ['target', 'href'],
	},
}
