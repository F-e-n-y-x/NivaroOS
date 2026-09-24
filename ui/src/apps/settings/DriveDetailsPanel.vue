<template>
	<div class="drive-details">
		<div class="drive-details-head">
			<div class="drive-details-title">{{ disk.model || disk.path }}</div>
			<button class="icon-button" type="button" :title="$t('Close')" :aria-label="$t('Close')" @click="$emit('close')">
				<b-icon icon="close-outline" pack="casa" size="is-16"></b-icon>
			</button>
		</div>

		<div class="drive-details-body">
			<b-loading v-model="loading" :is-full-page="false"></b-loading>

			<div v-if="!loading && loadError" class="load-error" role="alert">
				<span>{{ loadError }}</span>
				<b-button rounded size="is-small" @click="reload">{{ $t('Retry') }}</b-button>
			</div>

			<template v-if="!loading && !loadError">
				<div class="setting-row">
					<div class="row-label">{{ $t('Model') }}</div>
					<div class="row-control">{{ smart.model_name || disk.model || $t('Unknown') }}</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Serial number') }}</div>
					<div class="row-control">{{ smart.serial_number || disk.serial || $t('Unknown') }}</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Firmware') }}</div>
					<div class="row-control">{{ smart.firmware_version || $t('Unknown') }}</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Capacity') }}</div>
					<div class="row-control">{{ formatSize(smart.user_capacity && smart.user_capacity.bytes) }}</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('SMART health') }}</div>
					<div class="row-control">
						<span class="setting-chip" :class="{ 'is-good': smart.smart_status && smart.smart_status.passed }">
							{{ smart.smart_status && smart.smart_status.passed ? $t('Passed') : $t('Failed') }}
						</span>
					</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Temperature') }}</div>
					<div class="row-control">
						{{ smart.temperature && smart.temperature.current ? `${smart.temperature.current} °C` : $t('Unknown') }}
					</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Power-on time') }}</div>
					<div class="row-control">
						{{ smart.power_on_time && smart.power_on_time.hours ? $t('{hours} hours', { hours: smart.power_on_time.hours }) : $t('Unknown') }}
					</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Power cycle count') }}</div>
					<div class="row-control">{{ smart.power_cycle_count || $t('Unknown') }}</div>
				</div>

				<h4 class="drive-details-subtitle">{{ $t('Self-test') }}</h4>
				<div class="setting-row">
					<div class="row-label">{{ $t('Status') }}</div>
					<div class="row-control">{{ selfTestStatusText }}</div>
				</div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Run test') }}</div>
					<div class="row-control">
						<b-button rounded size="is-small" :loading="testing" :disabled="!selfTestsSupported" @click="runTest('short')">
							{{ $t('Short') }}
						</b-button>
						<b-button rounded size="is-small" class="ml-2" :loading="testing" :disabled="!selfTestsSupported" @click="runTest('long')">
							{{ $t('Long') }}
						</b-button>
					</div>
				</div>
				<p v-if="!selfTestsSupported" class="hint">{{ $t('This drive does not support self-tests.') }}</p>
				<p v-if="testError" class="error-note" role="alert">{{ testError }}</p>
				<p v-if="pollError" class="error-note" role="alert">{{ pollError }}</p>

				<h4 class="drive-details-subtitle">{{ $t('Standby') }}</h4>
				<p class="hint">{{ $t('Spin the drive down after it has been idle for this long - useful for drives backing Docker volumes, which otherwise get pinged often enough to never sleep.') }}</p>
				<div class="segmented-control">
					<button v-for="opt in standbyOptions" :key="opt.value" type="button" class="segmented-option"
						:disabled="savingStandby" :class="{ active: !customMode && standbyMinutes === opt.value }"
						:aria-pressed="String(!customMode && standbyMinutes === opt.value)" @click="selectPreset(opt.value)">
						{{ $t(opt.label) }}
					</button>
					<button type="button" class="segmented-option" :disabled="savingStandby" :class="{ active: customMode || isCustomValue }"
						@click="openCustom">
						{{ isCustomValue && !customMode ? $t('Custom ({minutes}m)', { minutes: standbyMinutes }) : $t('Custom') }}
					</button>
				</div>
				<div v-if="customMode" class="custom-standby">
					<b-input v-model.number="customMinutesInput" type="number" min="0" max="330" size="is-small" class="custom-standby-input"
						:placeholder="$t('Minutes')"></b-input>
					<b-button rounded size="is-small" type="is-primary" :loading="savingStandby" @click="applyCustom">{{ $t('Apply') }}</b-button>
					<b-button rounded size="is-small" @click="customMode = false">{{ $t('Cancel') }}</b-button>
				</div>
				<p v-if="standbyError" class="error-note">{{ standbyError }}</p>
			</template>
		</div>
	</div>
</template>

<script>
import { formatSize } from '@/utils/formatSize'

const STANDBY_OPTIONS = [
	{ value: 0, label: 'Never' },
	{ value: 5, label: '5 min' },
	{ value: 10, label: '10 min' },
	{ value: 20, label: '20 min' },
	{ value: 30, label: '30 min' },
	{ value: 60, label: '1 hour' },
	{ value: 120, label: '2 hours' },
	{ value: 180, label: '3 hours' }
]

export default {
	name: 'drive-details-panel',
	props: {
		disk: { type: Object, required: true }
	},
	data() {
		return {
			loading: true,
			smart: {},
			standbyMinutes: 0,
			standbyOptions: STANDBY_OPTIONS,
			savingStandby: false,
			standbyError: '',
			testing: false,
			testError: '',
			pollTimer: 0,
			pollFailures: 0,
			pollError: '',
			loadError: '',
			customMode: false,
			customMinutesInput: 0
		}
	},
	computed: {
		isCustomValue() {
			return !this.standbyOptions.some(opt => opt.value === this.standbyMinutes)
		},
		selfTestsSupported() {
			return !!(this.smart.ata_smart_data && this.smart.ata_smart_data.capabilities && this.smart.ata_smart_data.capabilities.self_tests_supported)
		},
		selfTestInProgress() {
			const status = this.smart.ata_smart_data && this.smart.ata_smart_data.self_test && this.smart.ata_smart_data.self_test.status
			return !!(status && /progress/i.test(status.string || ''))
		},
		selfTestStatusText() {
			const status = this.smart.ata_smart_data && this.smart.ata_smart_data.self_test && this.smart.ata_smart_data.self_test.status
			return (status && status.string) || this.$t('Never run')
		}
	},
	created() {
		this.load()
	},
	beforeDestroy() {
		// A response still in flight when the panel closes must not
		// schedule another poll on a component that no longer exists.
		this.isClosed = true
		clearTimeout(this.pollTimer)
	},
	methods: {
		formatSize,
		apiMessage(e, fallback) {
			return (e && e.response && e.response.data && e.response.data.message) || fallback
		},
		reload() {
			this.loading = true
			this.load()
		},
		load() {
			this.loadError = ''
			// Standby is optional (not every drive/controller reports it);
			// only SMART failing means there's nothing to show.
			return Promise.all([
				this.$api.disks.getSmartInfo(this.disk.path),
				this.$api.disks.getStandby(this.disk.path).catch(() => null)
			]).then(([smartRes, standbyRes]) => {
				if (this.isClosed) return
				if (smartRes.data.success === 200) this.smart = smartRes.data.data || {}
				else this.loadError = smartRes.data.message || this.$t('Could not read drive information')
				if (standbyRes && standbyRes.data.success === 200) this.standbyMinutes = (standbyRes.data.data && standbyRes.data.data.minutes) || 0
				if (this.selfTestInProgress) this.schedulePoll()
			}).catch(e => {
				if (this.isClosed) return
				this.loadError = this.apiMessage(e, this.$t('Could not read drive information'))
			}).finally(() => {
				this.loading = false
			})
		},
		// Polls while a self-test runs. A failed request retries with
		// backoff (15s, 30s, 60s, then every 2 min) and says so, instead of
		// silently stopping after the first error.
		schedulePoll() {
			if (this.isClosed) return
			clearTimeout(this.pollTimer)
			const delay = Math.min(15000 * Math.pow(2, this.pollFailures), 120000)
			this.pollTimer = setTimeout(() => {
				this.$api.disks.getSmartInfo(this.disk.path).then(res => {
					if (this.isClosed) return
					if (res.data.success === 200) {
						this.smart = res.data.data || {}
						this.pollFailures = 0
						this.pollError = ''
					} else {
						this.pollFailures++
						this.pollError = this.$t('Could not refresh self-test status - retrying.')
					}
					if (this.selfTestInProgress || this.pollFailures) this.schedulePoll()
				}).catch(() => {
					if (this.isClosed) return
					this.pollFailures++
					this.pollError = this.$t('Could not refresh self-test status - retrying.')
					this.schedulePoll()
				})
			}, delay)
		},
		runTest(type) {
			this.testError = ''
			this.testing = true
			this.$api.disks.startSmartTest(this.disk.path, type).then(res => {
				if (res.data.success !== 200) {
					this.testError = res.data.message
					return
				}
				this.pollFailures = 0
				this.pollError = ''
				this.schedulePoll()
			}).catch(e => {
				this.testError = this.apiMessage(e, this.$t('Failed to start self-test'))
			}).finally(() => {
				this.testing = false
			})
		},
		selectPreset(minutes) {
			this.customMode = false
			this.setStandby(minutes)
		},
		openCustom() {
			this.customMinutesInput = this.standbyMinutes
			this.customMode = true
		},
		applyCustom() {
			const minutes = Number(this.customMinutesInput)
			// hdparm's standby timer tops out at 330 minutes (5.5 h).
			if (!Number.isFinite(minutes) || minutes < 0 || minutes > 330) {
				this.standbyError = this.$t('Enter a number of minutes from 0 to 330')
				return
			}
			this.setStandby(minutes).then(() => {
				this.customMode = false
			})
		},
		setStandby(minutes) {
			this.standbyError = ''
			this.savingStandby = true
			return this.$api.disks.setStandby(this.disk.path, minutes).then(res => {
				if (res.data.success === 200) {
					this.standbyMinutes = minutes
				} else {
					this.standbyError = res.data.message
				}
			}).catch(e => {
				this.standbyError = this.apiMessage(e, this.$t('Failed to set standby timer'))
			}).finally(() => {
				this.savingStandby = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.load-error {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
}

.drive-details {
	margin: var(--space-2) 0 var(--space-3);
	background: var(--theme-card-subtle, rgba(0, 0, 0, 0.02));
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	border-radius: var(--radius-card);
	overflow: hidden;
}

.drive-details-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
}

.drive-details-title {
	font-weight: 500;
	font-size: var(--font-base);
}

.drive-details-body {
	position: relative;
	min-height: 8rem;
	padding-bottom: var(--space-1);
}

.drive-details-subtitle {
	margin: var(--space-4) var(--space-5) var(--space-1);
	font-size: var(--font-xs);
	font-weight: 500;
	text-transform: uppercase;
	letter-spacing: 0.02em;
	opacity: 0.5;
}

.hint {
	margin: 0 var(--space-5) var(--space-2);
	font-size: var(--font-xs);
	opacity: 0.6;
}

.segmented-control {
	margin-left: var(--space-5);
	margin-right: var(--space-5);
}

.custom-standby {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin: var(--space-2) var(--space-5) 0;
}

.custom-standby-input {
	width: 6rem;
}

.setting-chip.is-good {
	background: hsla(140, 60%, 45%, 0.12);
	border-color: hsla(140, 60%, 45%, 0.3);
	color: hsla(140, 60%, 28%, 1);
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
	margin: var(--space-2) var(--space-5) 0;
}

.icon-button {
	border: none;
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	width: 1.5rem;
	height: 1.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: var(--theme-text-muted);

	&:hover {
		color: var(--theme-text-primary);
	}

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: 2px;
	}
}
</style>
