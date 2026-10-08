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
				<drive-health-section :path="disk.path"></drive-health-section>

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
import DriveHealthSection from './DriveHealthSection.vue'

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
	emits: ['close'],
	name: 'drive-details-panel',
	components: { DriveHealthSection },
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
			loadError: '',
			customMode: false,
			customMinutesInput: 0
		}
	},
	computed: {
		isCustomValue() {
			return !this.standbyOptions.some(opt => opt.value === this.standbyMinutes)
		}
	},
	created() {
		this.load()
	},
	beforeUnmount() {
		this.isClosed = true
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
			}).catch(e => {
				if (this.isClosed) return
				this.loadError = this.apiMessage(e, this.$t('Could not read drive information'))
			}).finally(() => {
				this.loading = false
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
