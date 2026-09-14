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
			_confirmWindowState: null,
		}
	},
	computed: {
		confirmWindowProps() {
			const s = this._confirmWindowState
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
			this._confirmWindowState = options
		},
		_onConfirmWindowConfirm() {
			const onConfirm = this._confirmWindowState && this._confirmWindowState.onConfirm
			this._confirmWindowState = null
			if (onConfirm) onConfirm()
		},
		_onConfirmWindowCancel() {
			const onCancel = this._confirmWindowState && this._confirmWindowState.onCancel
			this._confirmWindowState = null
			if (onCancel) onCancel()
		},
	},
}

export default confirmWindowMixin
