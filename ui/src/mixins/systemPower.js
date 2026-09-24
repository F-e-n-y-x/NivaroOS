// Shared restart/shutdown confirm + poll-until-back-up flow, used by both
// the System settings panel and the desktop date/time pill menu - keeping
// the actual power/poll logic in one place avoids the two surfaces
// drifting out of sync with each other.
//
// The status (showPowerModal/powerTitle/powerMessage/powerState) is shown
// by each surface in a non-blocking way (a SettingsOverlay inside the
// Settings window, a dismissible status card by the tray pill); nothing
// here blocks the whole screen.
//
// Restart: once the request is accepted, the server is polled (every 5s,
// backing off to 15s) until it has gone away and come back, then the page
// reloads. Shutdown: polled only until the server stops answering, then
// polling stops for good. Either way polling gives up after a time limit
// and is cleared when the component is destroyed.
import { confirmWindowMixin } from '@/mixins/confirmWindow'

const POLL_START_MS = 5000
const POLL_MAX_MS = 15000
const RESTART_LIMIT_MS = 10 * 60 * 1000
const SHUTDOWN_LIMIT_MS = 3 * 60 * 1000

export default {
	mixins: [confirmWindowMixin],
	data() {
		return {
			showPowerModal: false,
			powerTitle: '',
			powerMessage: '',
			// '' | 'pending' | 'restarting' | 'off' | 'error'
			powerState: ''
		}
	},
	beforeDestroy() {
		this.stopPowerPolling()
	},
	methods: {
		confirmPower(key) {
			const isRestart = key === 'Restart'
			this.confirmWindow({
				title: this.$t(key),
				message: isRestart ? this.$t('Restart the system now?') : this.$t('Shut down the system now?'),
				type: 'is-danger',
				confirmText: this.$t(key),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.doPower(isRestart)
			})
		},
		stopPowerPolling() {
			clearTimeout(this.powerPollTimer)
			this.powerPollTimer = 0
		},
		setPowerStatus(state, title, message) {
			this.powerState = state
			this.powerTitle = title
			this.powerMessage = message
			this.showPowerModal = true
		},
		doPower(isRestart) {
			this.stopPowerPolling()
			this.setPowerStatus('pending',
				isRestart ? 'Restarting now' : 'Now shutting down',
				isRestart ? 'Please wait for about 90 seconds.' : 'Please wait for about 30 seconds before cutting off the power.')
			this.$api.sys.power(isRestart ? 'restart' : 'off').then(res => {
				if (!res.data || res.data.success !== 200) {
					this.setPowerStatus('error', isRestart ? 'Restart failed' : 'Shutdown failed',
						(res.data && res.data.message) || 'The server refused the request.')
					return
				}
				if (typeof res.data.data === 'string' && res.data.data) this.powerMessage = res.data.data
				this.powerState = isRestart ? 'restarting' : 'pending'
				this.pollPower(isRestart, Date.now(), false, POLL_START_MS)
			}).catch(e => {
				const msg = e && e.response && e.response.data && e.response.data.message
				// No response at all usually means the server already went
				// down mid-request - carry on as if it was accepted.
				if (!e || !e.response) {
					this.powerState = isRestart ? 'restarting' : 'pending'
					this.pollPower(isRestart, Date.now(), true, POLL_START_MS)
					return
				}
				this.setPowerStatus('error', isRestart ? 'Restart failed' : 'Shutdown failed', msg || 'The server refused the request.')
			})
		},
		pollPower(isRestart, startedAt, wentDown, delay) {
			this.stopPowerPolling()
			if (this._isDestroyed) return
			const limit = isRestart ? RESTART_LIMIT_MS : SHUTDOWN_LIMIT_MS
			if (Date.now() - startedAt > limit) {
				if (isRestart) {
					this.setPowerStatus('error', 'Restart is taking longer than expected', 'The server has not come back yet. Reload this page once it is running again.')
				} else {
					this.setPowerStatus('off', 'Now shutting down', 'It is now safe to cut off the power.')
				}
				return
			}
			this.powerPollTimer = setTimeout(() => {
				this.$api.users.getUserStatus().then(statusRes => {
					const up = !!(statusRes && statusRes.data && statusRes.data.data && statusRes.data.data.initialized)
					// Only a server that went away and answered again has
					// actually restarted.
					if (isRestart && up && (wentDown || Date.now() - startedAt > 90000)) {
						location.reload()
						return
					}
					this.pollPower(isRestart, startedAt, wentDown, Math.min(delay * 1.5, POLL_MAX_MS))
				}).catch(() => {
					if (!isRestart) {
						this.stopPowerPolling()
						this.setPowerStatus('off', 'Now shutting down', 'It is now safe to cut off the power.')
						return
					}
					this.pollPower(isRestart, startedAt, true, Math.min(delay * 1.5, POLL_MAX_MS))
				})
			}, delay)
		},
		resetPowerModal() {
			this.showPowerModal = false
			// Once it is off/failed, nothing is left to wait for.
			if (this.powerState === 'off' || this.powerState === 'error') {
				this.stopPowerPolling()
				this.powerState = ''
			}
		}
	}
}
