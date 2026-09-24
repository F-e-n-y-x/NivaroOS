// Shared by the Backup & Sync app and its windows: the API client, the
// server's capabilities (time zone for every date), a formatter bound to
// the viewer's locale, and message rendering. The capabilities request is
// shared by everything open at once and refetched when forced (after the
// service restarts, or on Retry).
import backup from '@/service/backup'
import { createFormatter } from './format'
import { renderMessage, explainError, errorText } from './messages'
import { STATUS_VIEW, HEALTH_VIEW } from './state'
import { backupWindow } from './windows'
import { escapeHtml } from '@/utils/escapeHtml'

let capsPromise = null
export function loadCapabilities(force = false) {
	if (force || !capsPromise) {
		capsPromise = backup.capabilities().catch(err => {
			capsPromise = null
			throw err
		})
	}
	return capsPromise
}

export const backupMixin = {
	data() {
		return { bkCaps: null }
	},
	computed: {
		bkApi() {
			return backup
		},
		fmt() {
			const timeFormat = this.$store && this.$store.state.timeFormat
			return createFormatter({
				locale: this.$i18n && this.$i18n.locale,
				timeZone: this.bkCaps && this.bkCaps.timezone,
				hour12: timeFormat ? timeFormat !== 'HH:MM' : undefined,
				t: this.$t.bind(this)
			})
		}
	},
	created() {
		loadCapabilities()
			.then(c => {
				this.bkCaps = c
			})
			.catch(() => {
				// Dates then show in the browser's zone; the app itself
				// reports the service as unavailable.
			})
	},
	methods: {
		msg(m) {
			return renderMessage(this.$t.bind(this), m, this.fmt)
		},
		explain(code) {
			return explainError(this.$t.bind(this), code)
		},
		errText(err) {
			return errorText(this.$t.bind(this), err)
		},
		statusView(status) {
			return STATUS_VIEW[status] || STATUS_VIEW.skipped
		},
		healthView(health) {
			return HEALTH_VIEW[health] || HEALTH_VIEW.ok
		},
		openBackupWindow(kind, params) {
			this.$store.commit('OPEN_WINDOW', backupWindow(this.$t.bind(this), kind, params))
		},
		// Buefy toasts render HTML: the text (which may hold a job name) is
		// always escaped.
		toast(message, type = 'is-success') {
			if (this.$buefy && this.$buefy.toast) this.$buefy.toast.open({ message: escapeHtml(message), type, duration: type === 'is-danger' ? 5000 : 3000 })
		},
		toastError(err) {
			this.toast(this.errText(err), 'is-danger')
		}
	}
}

// Window behaviour for the app's own windows (spec §13): focus the first
// meaningful control on open (an element marked data-autofocus, else the
// first focusable one), give focus back to the opener on close, and close
// on Esc unless the window says it's busy (closeBlocked).
const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export const windowBehavior = {
	mounted() {
		this.bkOpener = typeof document !== 'undefined' ? document.activeElement : null
		this.$nextTick(() => this.focusFirst())
	},
	beforeDestroy() {
		const o = this.bkOpener
		if (o && typeof o.focus === 'function' && document.contains(o)) setTimeout(() => o.focus(), 0)
	},
	methods: {
		focusFirst() {
			const root = this.$el
			if (!root || !root.querySelector) return
			const el = root.querySelector('[data-autofocus]') || root.querySelector(FOCUSABLE)
			if (el) el.focus()
		},
		onWindowKeydown(e) {
			if (e.key !== 'Escape' || e.defaultPrevented || this.closeBlocked) return
			e.preventDefault()
			e.stopPropagation()
			this.$emit('close')
		}
	}
}

export default backupMixin
