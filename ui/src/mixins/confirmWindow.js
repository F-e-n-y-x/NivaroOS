// Drop-in replacement for $buefy.dialog.confirm() that renders confined to
// the calling app's own window instead of a viewport-wide overlay over the
// whole simulated desktop - see ConfirmWindow.vue's own comment for why.
//
// Usage: mix this in, render `<confirm-window v-bind="confirmWindowProps"
// @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel" />`
// once in the component's template (inside a `position: relative`
// container - the app's own root, matching the existing convention), then
// call `this.confirmWindow({ title, message, confirmText, cancelText,
// type, hasIcon, icon, iconPack, onConfirm, onCancel })` exactly where
// `this.$buefy.dialog.confirm({...})` used to be called.
export const confirmWindowMixin = {
	data() {
		return {
			// NOT "_confirmWindowState" - Vue 2 never proxies a data() key
			// starting with "_" or "$" onto the instance (reserved for its
			// own internals), so `this._confirmWindowState = x` would just
			// set a plain, non-reactive property no computed/template
			// binding would ever see change. That's not a style nitpick,
			// it silently broke every confirm window: the popup never
			// opened, with no warning in a production build.
			confirmWindowState: null,
		}
	},
	computed: {
		confirmWindowProps() {
			const s = this.confirmWindowState
			return {
				active: !!s,
				title: (s && s.title) || '',
				message: (s && s.message) || '',
				confirmText: (s && s.confirmText) || this.$t('Confirm'),
				cancelText: (s && s.cancelText) || this.$t('Cancel'),
				type: (s && s.type) || 'is-primary',
				hasIcon: !!(s && s.hasIcon),
				icon: (s && s.icon) || 'alert',
				iconPack: (s && s.iconPack) || 'mdi',
			}
		},
	},
	methods: {
		confirmWindow(options) {
			if (!options) return
			const dialogId = 'dialog-' + Date.now() + '-' + Math.random().toString(36).substr(2, 5)
			const width = options.width || 420
			const height = options.height || 210
			const x = options.x !== undefined ? options.x : Math.max(16, Math.round((window.innerWidth - width) / 2))
			const y = options.y !== undefined ? options.y : Math.max(40, Math.round((window.innerHeight - height) / 2))

			// Windows need the desktop's window manager; pages without it
			// (the standalone VM console tab) use the inline fallback.
			const hasWindowManager = !this.$route || !!(this.$route.meta && this.$route.meta.showWindows)
			if (this.$store && this.$store.commit && hasWindowManager) {
				this.$store.commit('OPEN_WINDOW', {
					id: dialogId,
					title: options.title || (this.$t ? this.$t('Confirmation') : 'Confirmation'),
					component: 'ConfirmDialogWindow',
					props: {
						id: dialogId,
						isDialog: true,
						title: options.title || '',
						message: options.message || '',
						confirmText: options.confirmText || (this.$t ? this.$t('Confirm') : 'Confirm'),
						cancelText: options.cancelText || (this.$t ? this.$t('Cancel') : 'Cancel'),
						type: options.type || 'is-primary',
						hasIcon: options.hasIcon !== undefined ? options.hasIcon : true,
						icon: options.icon || '',
						iconPack: options.iconPack || 'mdi',
						onConfirm: options.onConfirm,
						onCancel: options.onCancel
					},
					width,
					height,
					x,
					y
				})
			} else {
				// Fallback to local confirmWindowState if store is not available
				this.confirmWindowState = options
			}
		},
		_onConfirmWindowConfirm() {
			const onConfirm = this.confirmWindowState && this.confirmWindowState.onConfirm
			this.confirmWindowState = null
			if (onConfirm) onConfirm()
		},
		_onConfirmWindowCancel() {
			const onCancel = this.confirmWindowState && this.confirmWindowState.onCancel
			this.confirmWindowState = null
			if (onCancel) onCancel()
		},
	},
}

export default confirmWindowMixin
