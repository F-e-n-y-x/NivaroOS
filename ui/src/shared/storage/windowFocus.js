// Focus rules shared by the picker, wizard and preview windows (spec §13):
// a window focuses its first meaningful control when it opens and gives
// focus back to whatever opened it when it closes; Esc closes windows that
// aren't destructive (a component with requestClose() is asked first).
//
// The opener is whatever had focus when the window was created. When a
// window is opened from another window that closes at the same moment
// (storage picker -> folder picker), the caller passes a `returnFocus`
// prop: a function returning the element to focus instead.
function resolveEl(target) {
	const el = target && (target.$el || target)
	return el && typeof el.focus === 'function' ? el : null
}

export const windowFocusMixin = {
	created() {
		this.openerEl = typeof document !== 'undefined' ? document.activeElement : null
	},
	beforeDestroy() {
		if (typeof document === 'undefined') return
		const own = this.$el
		// Only give focus back when it is still ours (or lost to <body>);
		// another window that took it keeps it.
		const ours = () => {
			const a = document.activeElement
			return !a || a === document.body || (own && own.contains(a)) || !document.contains(a)
		}
		if (!ours()) return
		const opener = this.openerEl
		const returnFocus = this.returnFocus
		// Resolved after the re-render, so returnFocus() can name an element
		// the opener only renders once the choice is applied.
		setTimeout(() => {
			if (!ours()) return
			let target = null
			if (typeof returnFocus === 'function') {
				try {
					target = resolveEl(returnFocus())
				} catch (e) {
					target = null
				}
			}
			if (!target || !document.contains(target)) target = resolveEl(opener)
			if (target && document.contains(target)) target.focus()
		}, 0)
	},
	methods: {
		// focusFirst focuses a ref (element or component) on the next tick.
		focusFirst(ref) {
			this.$nextTick(() => {
				const el = resolveEl(typeof ref === 'string' ? this.$refs[ref] : ref)
				if (el) el.focus()
			})
		},
		closeWindow() {
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id && this.$store) this.$store.commit('CLOSE_WINDOW', id)
			this.$emit('close')
		},
		onWindowKeydown(e) {
			if (e.key !== 'Escape' || e.defaultPrevented) return
			if (this.escCloses === false) return
			e.preventDefault()
			if (typeof this.requestClose === 'function') this.requestClose()
			else this.closeWindow()
		}
	}
}

export default windowFocusMixin
