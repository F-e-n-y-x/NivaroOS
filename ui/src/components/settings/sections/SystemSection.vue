<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('System') }}</h2>

		<h3 class="setting-card-title">{{ $t('General') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="language-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Language') }}</div>
					<div class="setting-desc">{{ $t('Display language for NivaroOS') }}</div>
				</div>
				<div class="row-control">
					<b-select v-model="barData.lang" class="set-select" size="is-small" @input="saveBarData">
						<option v-for="(lang, key) in languages" :key="key" :value="key">{{ lang.lang_name }}</option>
					</b-select>
				</div>
			</div>

			<div v-if="hasNotImportedApps" class="setting-row">
				<b-icon class="row-icon" icon="docker-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Show other Docker container app(s)') }}</div>
					<div class="setting-desc">{{ $t('Display unmanaged containers on the home grid') }}</div>
				</div>
				<div class="row-control">
					<b-switch v-model="barData.existing_apps_switch" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="saveBarData"></b-switch>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="port-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('WebUI Port') }}</div>
					<div class="setting-desc">{{ $t('HTTP port used to access NivaroOS') }}</div>
				</div>
				<div class="row-control">
					<template v-if="!editingPort">
						<span class="port-badge mr-2">{{ port }}</span>
						<b-button rounded size="is-small" @click="startEditPort">{{ $t('Change') }}</b-button>
					</template>
					<template v-else>
						<b-input v-model="portInput" type="number" size="is-small" class="port-input"
							@keyup.enter.native="savePort"></b-input>
						<b-button class="ml-2" rounded size="is-small" @click="editingPort = false">{{ $t('Cancel') }}</b-button>
						<b-button class="ml-2" rounded size="is-small" type="is-dark" :loading="savingPort" @click="savePort">
							{{ $t('Save') }}
						</b-button>
					</template>
				</div>
			</div>
			<p v-if="portError" class="error-note">{{ portError }}</p>

			<div class="setting-row">
				<b-icon class="row-icon has-text-danger" icon="restart-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label has-text-danger">
					<div class="setting-title">{{ $t('Power Management') }}</div>
					<div class="setting-desc">{{ $t('Reboot or gracefully power off the host system') }}</div>
				</div>
				<div class="row-control">
					<b-button class="mr-2" rounded size="is-small" @click="confirmPower('Restart', '#window-settings')">
						<i class="mdi mdi-restart mr-1"></i>{{ $t('Restart') }}
					</b-button>
					<b-button rounded size="is-small" type="is-danger" @click="confirmPower('Shutdown', '#window-settings')">
						<i class="mdi mdi-power mr-1"></i>{{ $t('Shutdown') }}
					</b-button>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Date & Time') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="clock-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Live Preview') }}</div>
					<div class="setting-desc">{{ $t('Appearance on top right taskbar pill') }}</div>
				</div>
				<div class="row-control">
					<span class="datetime-preview-pill">
						<i class="mdi mdi-clock-check-outline mr-1"></i>
						{{ previewText }}
					</span>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="time-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Time format') }}</div>
					<div class="setting-desc">{{ $t('Choose between 12-hour AM/PM and 24-hour clock') }}</div>
				</div>
				<div class="row-control">
					<div class="segmented-control">
						<button v-for="opt in timeFormatOptions" :key="opt.value" type="button" class="segmented-option"
							:disabled="!!customDateTimeFormat" :class="{ active: timeFormat === opt.value }" @click="setTimeFormat(opt.value)">
							{{ $t(opt.label) }}
						</button>
					</div>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="timer-sand" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Show seconds') }}</div>
					<div class="setting-desc">{{ $t('Display real-time seconds ticking') }}</div>
				</div>
				<div class="row-control">
					<b-switch :value="showSeconds" :disabled="!!customDateTimeFormat" class="is-flex-direction-row-reverse mr-0"
						type="is-dark" @input="setShowSeconds"></b-switch>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="calendar-month-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Date format') }}</div>
					<div class="setting-desc">{{ $t('Choose date verbosity level') }}</div>
				</div>
				<div class="row-control">
					<div class="segmented-control">
						<button v-for="opt in dateFormatOptions" :key="opt.value" type="button" class="segmented-option"
							:disabled="!!customDateTimeFormat" :class="{ active: dateFormatStyle === opt.value }" @click="setDateFormatStyle(opt.value)">
							{{ $t(opt.label) }}
						</button>
					</div>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="code-braces" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Custom format pattern') }}</div>
					<div class="setting-desc">{{ $t('Advanced strftime pattern (overrides presets)') }}</div>
				</div>
				<div class="row-control">
					<b-input v-model="customFormatInput" size="is-small" class="custom-format-input"
						:placeholder="$t('e.g. %F %H:%M:%S')" @input="setCustomFormat"></b-input>
				</div>
			</div>
			<div class="format-hint">
				<div class="format-hint-row">
					<span v-for="tok in strftimeTokens" :key="tok" class="format-chip">{{ tok }}</span>
				</div>
				<div class="format-hint-row">
					<span v-for="s in strftimeShortcuts" :key="s.token" class="format-chip">{{ s.token }} = {{ s.meaning }}</span>
				</div>
			</div>
		</div>

		<about-panel></about-panel>

		<settings-overlay
			:active="showPowerModal"
			:title="$t(powerTitle)"
			width="22rem"
			@close="resetPowerModal"
		>
			<div class="p-2">{{ $t(powerMessage) }}</div>
			<template #footer>
				<b-button rounded size="is-small" type="is-primary" @click="resetPowerModal">{{ $t('OK') }}</b-button>
			</template>
		</settings-overlay>
	</section>
</template>

<script>
import AboutPanel from '@/components/settings/AboutPanel.vue'
import SettingsOverlay from '@/components/settings/SettingsOverlay.vue'
import { mixin } from '@/mixins/mixin'
import systemPower from '@/mixins/systemPower'
import messages from '@/assets/lang'
import { formatTime, formatDate, formatStrftime, STRFTIME_TOKEN_LIST, STRFTIME_SHORTCUTS } from '@/utils/dateTimeFormat'

export const ROWS = [
	{ label: 'Date & Time' },
	{ label: 'Language' },
	{ label: 'WebUI Port' },
	{ label: 'System Restart / Shutdown' },
	{ label: 'About NivaraOS' }
]

const TIME_FORMAT_OPTIONS = [
	{ value: 'HH:MM', label: '24-hour (14:30)' },
	{ value: 'h:MM TT', label: '12-hour (2:30 PM)' }
]

const DATE_FORMAT_OPTIONS = [
	{ value: 'long', label: 'Long' },
	{ value: 'medium', label: 'Medium' },
	{ value: 'short', label: 'Short' }
]

export default {
	name: 'system-section',
	components: { AboutPanel, SettingsOverlay },
	mixins: [mixin, systemPower],
	data() {
		return {
			barData: {
				lang: 'en_us',
				existing_apps_switch: true,
				recommend_switch: true,
				rss_switch: false,
				search_engine: 'https://duckduckgo.com/?q='
			},
			port: '',
			portInput: '',
			editingPort: false,
			savingPort: false,
			portError: '',
			languages: messages,
			timeFormatOptions: TIME_FORMAT_OPTIONS,
			dateFormatOptions: DATE_FORMAT_OPTIONS,
			customFormatInput: this.$store.state.customDateTimeFormat,
			strftimeTokens: STRFTIME_TOKEN_LIST,
			strftimeShortcuts: STRFTIME_SHORTCUTS,
			now: new Date(),
			previewTimer: 0
		}
	},
	computed: {
		hasNotImportedApps() {
			return this.$store.state.notImportList.length > 0
		},
		timeFormat() {
			return this.$store.state.timeFormat
		},
		dateFormatStyle() {
			return this.$store.state.dateFormatStyle
		},
		showSeconds() {
			return this.$store.state.showSeconds
		},
		customDateTimeFormat() {
			return this.$store.state.customDateTimeFormat
		},
		lang() {
			return this.$i18n.locale.replace('_', '-')
		},
		previewText() {
			if (this.customDateTimeFormat) return formatStrftime(this.now, this.customDateTimeFormat)
			return `${formatTime(this.now, this.timeFormat, this.showSeconds)} · ${formatDate(this.now, this.lang, this.dateFormatStyle)}`
		}
	},
	created() {
		this.getPort()
		this.getBarData()
		this.loadDateTimeSettings()
		this.previewTimer = setInterval(() => {
			this.now = new Date()
		}, 1000)
	},
	beforeDestroy() {
		clearInterval(this.previewTimer)
	},
	methods: {
		async loadDateTimeSettings() {
			try {
				const res = await this.$api.users.getCustomStorage('datetime_format')
				if (res.data && res.data.success === 200 && res.data.data) {
					const data = res.data.data
					if (data.timeFormat) this.$store.commit('SET_TIME_FORMAT', data.timeFormat)
					if (data.dateFormatStyle) this.$store.commit('SET_DATE_FORMAT_STYLE', data.dateFormatStyle)
					if (data.showSeconds !== undefined) this.$store.commit('SET_SHOW_SECONDS', data.showSeconds)
					if (data.customDateTimeFormat !== undefined) {
						this.$store.commit('SET_CUSTOM_DATETIME_FORMAT', data.customDateTimeFormat)
						this.customFormatInput = data.customDateTimeFormat
					}
				}
			} catch (e) {
				// Fallback to existing store
			}
		},
		async persistDateTimeSettings() {
			const payload = {
				timeFormat: this.timeFormat,
				dateFormatStyle: this.dateFormatStyle,
				showSeconds: this.showSeconds,
				customDateTimeFormat: this.customDateTimeFormat
			}
			try {
				await this.$api.users.setCustomStorage('datetime_format', payload)
			} catch (e) {
				console.error('Failed to save datetime settings', e)
			}
		},
		setTimeFormat(value) {
			this.$store.commit('SET_TIME_FORMAT', value)
			this.persistDateTimeSettings()
		},
		setShowSeconds(value) {
			this.$store.commit('SET_SHOW_SECONDS', value)
			this.persistDateTimeSettings()
		},
		setDateFormatStyle(value) {
			this.$store.commit('SET_DATE_FORMAT_STYLE', value)
			this.persistDateTimeSettings()
		},
		setCustomFormat(value) {
			this.$store.commit('SET_CUSTOM_DATETIME_FORMAT', value.trim())
			this.persistDateTimeSettings()
		},
		getBarData() {
			this.$api.users.getCustomStorage('system').then(res => {
				if (res.data.success === 200 && res.data.data !== '') {
					this.barData = Object.assign({}, this.barData, res.data.data)
				}
			})
		},
		saveBarData() {
			this.$api.users.setCustomStorage('system', this.barData).then(res => {
				if (res.data.success === 200) {
					this.barData = res.data.data
					if (this.barData.lang) {
						const lang = this.barData.lang.includes('_') ? this.barData.lang : 'en_us'
						this.setLang(lang)
					}
				}
			})
		},
		getPort() {
			this.$api.sys.getServerPort().then(res => {
				if (res.data.success === 200) this.port = res.data.data
			})
		},
		startEditPort() {
			this.portInput = this.port
			this.portError = ''
			this.editingPort = true
		},
		savePort() {
			const port = Number(this.portInput)
			if (!port || port < 80 || port > 65535) {
				this.portError = this.$t('Port range is 80-65535')
				return
			}
			this.portError = ''
			this.savingPort = true
			this.$messageBus('dashboardsetting_webuiport', String(port))
			this.$api.sys.editServerPort({ port }).then(res => {
				if (res.data.success === 200) {
					this.pollNewPort(port)
				} else {
					this.savingPort = false
					this.portError = res.data.message
				}
			}).catch(err => {
				this.savingPort = false
				this.portError = err.response && err.response.data ? err.response.data.message : this.$t('Failed to change port')
			})
		},
		pollNewPort(port) {
			const timer = setInterval(() => {
				const checkUrl = `${this.$protocol}//${this.$baseIp}:${port}`
				this.$api.sys.checkUiPort(`${checkUrl}/v1/gateway/port`).then(res => {
					if (res.data.success === 200) {
						clearInterval(timer)
						window.open(`${this.$protocol}//${this.$baseIp}:${res.data.data}`, '_self')
					}
				})
			}, 1000)
		}
	}
}
</script>

<style lang="scss" scoped>
// The base .segmented-control (see _settings.scss) is meant to stand alone
// as a tab bar (with its own bottom margin) - inline inside a .row-control
// it just needs to sit level with the label, no extra spacing.
.setting-row .segmented-control {
	margin-bottom: 0;
}

.datetime-preview {
	font-size: 0.85rem;
	font-weight: 500;
	color: #1e293b;
}

.custom-format-input {
	width: 12rem;
}

.format-hint {
	padding: 0 1.25rem 1.1rem;
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}

.format-hint-row {
	display: flex;
	flex-wrap: wrap;
	gap: 0.4rem;
}

.format-chip {
	padding: 0.15rem 0.5rem;
	border-radius: 6px;
	background: #f1f5f9;
	color: #475569;
	font-family: $family-monospace;
	font-size: 0.725rem;
	white-space: nowrap;
}
</style>
