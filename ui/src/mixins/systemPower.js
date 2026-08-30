// Shared restart/shutdown confirm + poll-until-back-up flow, used by both
// the System settings panel and the desktop date/time pill menu - keeping
// the actual power/poll logic in one place avoids the two surfaces
// drifting out of sync with each other.
export default {
	data() {
		return {
			showPowerModal: false,
			powerTitle: '',
			powerMessage: ''
		}
	},
	methods: {
		confirmPower(key, containerSelector) {
			const isRestart = key === 'Restart'
			this.$buefy.dialog.confirm({
				...(containerSelector ? { container: containerSelector } : {}),
				title: this.$t(key),
				message: isRestart ? this.$t('Restart the system now?') : this.$t('Shut down the system now?'),
				type: 'is-danger',
				confirmText: this.$t(key),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.doPower(isRestart)
			})
		},
		doPower(isRestart) {
			this.showPowerModal = true
			this.powerTitle = isRestart ? 'Restarting now' : 'Now shutting down'
			this.powerMessage = isRestart
				? 'Please wait for about 90 seconds.'
				: 'Please wait for about 30 seconds before cutting off the power.'
			let timer
			this.$api.sys.power(isRestart ? 'restart' : 'off').then(res => {
				if (res.data.success === 200) {
					this.powerMessage = res.data.data
					timer = setInterval(() => {
						this.$api.users.getUserStatus().then(statusRes => {
							if (statusRes.data.data.initialized) {
								clearInterval(timer)
								location.reload()
							}
						})
					}, 30000)
				}
			})
		},
		resetPowerModal() {
			this.showPowerModal = false
		}
	}
}
