<template>
	<div class="drive-details">
		<div class="drive-details-head">
			<div class="drive-details-title">{{ disk.model || disk.path }}</div>
			<button class="icon-button" type="button" :title="$t('Close')" @click="$emit('close')">
				<b-icon icon="close-outline" pack="casa" size="is-16"></b-icon>
			</button>
		</div>

		<div class="drive-details-body">
			<b-loading v-model="loading" :is-full-page="false"></b-loading>

			<template v-if="!loading">
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
				<p v-if="testError" class="error-note">{{ testError }}</p>

				<h4 class="drive-details-subtitle">{{ $t('Standby') }}</h4>
				<p class="hint">{{ $t('Spin the drive down after it has been idle for this long - useful for drives backing Docker volumes, which otherwise get pinged often enough to never sleep.') }}</p>
				<div class="segmented-control">
					<button v-for="opt in standbyOptions" :key="opt.value" type="button" class="segmented-option"
						:disabled="savingStandby" :class="{ active: !customMode && standbyMinutes === opt.value }" @click="selectPreset(opt.value)">
						{{ $t(opt.label) }}
					</button>
					<button type="button" class="segmented-option" :disabled="savingStandby" :class="{ active: customMode || isCustomValue }"
						@click="openCustom">
						{{ isCustomValue && !customMode ? $t('Custom ({minutes}m)', { minutes: standbyMinutes }) : $t('Custom') }}
					</button>
				</div>
				<div v-if="customMode" class="custom-standby">
					<b-input v-model.number="customMinutesInput" type="number" min="0" size="is-small" class="custom-standby-input"
						:placeholder="$t('Minutes')"></b-input>
					<b-button rounded size="is-small" type="is-dark" :loading="savingStandby" @click="applyCustom">{{ $t('Apply') }}</b-button>
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
		clearTimeout(this.pollTimer)
	},
	methods: {
		formatSize,
		load() {
			return Promise.all([
				this.$api.disks.getSmartInfo(this.disk.path),
				this.$api.disks.getStandby(this.disk.path)
			]).then(([smartRes, standbyRes]) => {
				if (smartRes.data.success === 200) this.smart = smartRes.data.data || {}
				if (standbyRes.data.success === 200) this.standbyMinutes = standbyRes.data.data.minutes || 0
				if (this.selfTestInProgress) this.schedulePoll()
			}).finally(() => {
				this.loading = false
			})
		},
		schedulePoll() {
			clearTimeout(this.pollTimer)
			this.pollTimer = setTimeout(() => {
				this.$api.disks.getSmartInfo(this.disk.path).then(res => {
					if (res.data.success === 200) this.smart = res.data.data || {}
					if (this.selfTestInProgress) this.schedulePoll()
				})
			}, 15000)
		},
		runTest(type) {
			this.testError = ''
			this.testing = true
			this.$api.disks.startSmartTest(this.disk.path, type).then(res => {
				if (res.data.success !== 200) {
					this.testError = res.data.message
					return
				}
				this.schedulePoll()
			}).catch(e => {
				this.testError = e.response && e.response.data ? e.response.data.message : this.$t('Failed to start self-test')
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
			if (!Number.isFinite(minutes) || minutes < 0) {
				this.standbyError = this.$t('Enter a valid number of minutes')
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
				this.standbyError = e.response && e.response.data ? e.response.data.message : this.$t('Failed to set standby timer')
			}).finally(() => {
				this.savingStandby = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.drive-details {
	margin: 0.5rem 0 0.75rem;
	background: rgba(0, 0, 0, 0.02);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 10px;
	overflow: hidden;
}

.drive-details-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
}

.drive-details-title {
	font-weight: 500;
	font-size: 0.85rem;
}

.drive-details-body {
	position: relative;
	min-height: 8rem;
	padding-bottom: 0.25rem;
}

.drive-details-subtitle {
	margin: 1rem 1.25rem 0.25rem;
	font-size: 0.75rem;
	font-weight: 500;
	text-transform: uppercase;
	letter-spacing: 0.02em;
	opacity: 0.5;
}

.hint {
	margin: 0 1.25rem 0.5rem;
	font-size: 0.75rem;
	opacity: 0.6;
}

.segmented-control {
	margin-left: 1.25rem;
	margin-right: 1.25rem;
}

.custom-standby {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin: 0.6rem 1.25rem 0;
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
	color: #d64545;
	font-size: 0.75rem;
	margin: 0.4rem 1.25rem 0;
}

.icon-button {
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.5rem;
	height: 1.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}
</style>
