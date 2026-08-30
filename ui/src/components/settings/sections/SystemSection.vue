<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('System') }}</h2>

		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="language-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Language') }}</div>
				<div class="row-control">
					<b-select v-model="barData.lang" class="set-select" size="is-small" @input="saveBarData">
						<option v-for="lang in languages" :key="lang.lang" :value="lang.lang">{{ lang.name }}</option>
					</b-select>
				</div>
			</div>

			<div v-if="hasNotImportedApps" class="setting-row">
				<b-icon class="row-icon" icon="docker-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Show other Docker container app(s)') }}</div>
				<div class="row-control">
					<b-switch v-model="barData.existing_apps_switch" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="saveBarData"></b-switch>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="port-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('WebUI Port') }}</div>
				<div class="row-control">
					<template v-if="!editingPort">
						<span class="mr-2">{{ port }}</span>
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
				<b-icon class="row-icon has-text-red" icon="restart-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label has-text-red">{{ $t('Restart or Shutdown') }}</div>
				<div class="row-control">
					<b-button class="mr-2" rounded size="is-small" @click="confirmPower('Restart', '#window-settings')">{{ $t('Restart') }}</b-button>
					<b-button rounded size="is-small" type="is-danger" @click="confirmPower('Shutdown', '#window-settings')">{{ $t('Shutdown') }}</b-button>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Date & Time') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="time-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Preview') }}</div>
				<div class="row-control">
					<span class="datetime-preview">{{ previewText }}</span>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="time-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Time format') }}</div>
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
				<b-icon class="row-icon" icon="time-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Show seconds') }}</div>
				<div class="row-control">
					<b-switch :value="showSeconds" :disabled="!!customDateTimeFormat" class="is-flex-direction-row-reverse mr-0"
						type="is-dark" @input="setShowSeconds"></b-switch>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="time-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Date format') }}</div>
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
				<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Custom format') }}</div>
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

		<b-modal v-model="showPowerModal" :can-cancel="false" scroll="clip" width="20rem">
			<b-message @close="resetPowerModal">
				<template #header>
					{{ $t(powerTitle) }}
				</template>
				<div>{{ $t(powerMessage) }}</div>
			</b-message>
		</b-modal>
	</section>
</template>

<script>
import AboutPanel from '@/components/settings/AboutPanel.vue'
import { mixin } from '@/mixins/mixin'
import systemPower from '@/mixins/systemPower'
import messages from '@/assets/lang'
import { formatTime, formatDate, formatStrftime, STRFTIME_TOKEN_LIST, STRFTIME_SHORTCUTS } from '@/utils/dateTimeFormat'

export const ROWS = [
	{ label: 'Date & Time' },
	{ label: 'Language' },
	{ label: 'Show other Docker container app(s)' },
	{ label: 'WebUI Port' },
	{ label: 'Restart or Shutdown' },
	{ label: 'About' }
]

const TIME_FORMAT_OPTIONS = [
	{ value: 'HH:MM', label: '24-hour' },
	{ value: 'h:MM TT', label: '12-hour' }
]

const DATE_FORMAT_OPTIONS = [
	{ value: 'long', label: 'Long' },
	{ value: 'medium', label: 'Medium' },
	{ value: 'short', label: 'Short' }
]

export default {
	name: 'system-section',
	mixins: [mixin, systemPower],
	components: { AboutPanel },
	data() {
		return {
			barData: {
				lang: this.getLangFromBrowser ? this.getLangFromBrowser() : 'en_us',
				recommend_switch: true,
				existing_apps_switch: true,
				rss_switch: false
			},
			languages: Object.entries(messages).map(([key, value]) => ({ lang: key, name: value.lang_name })),
			port: '',
			editingPort: false,
			portInput: '',
			savingPort: false,
			portError: '',
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
		this.previewTimer = setInterval(() => {
			this.now = new Date()
		}, 1000)
	},
	beforeDestroy() {
		clearInterval(this.previewTimer)
	},
	methods: {
		setTimeFormat(value) {
			this.$store.commit('SET_TIME_FORMAT', value)
		},
		setShowSeconds(value) {
			this.$store.commit('SET_SHOW_SECONDS', value)
		},
		setDateFormatStyle(value) {
			this.$store.commit('SET_DATE_FORMAT_STYLE', value)
		},
		setCustomFormat(value) {
			this.$store.commit('SET_CUSTOM_DATETIME_FORMAT', value.trim())
		},
		getBarData() {
			this.$api.users.getCustomStorage('system').then(res => {
				if (res.data.success === 200 && res.data.data !== '') {
					this.barData = res.data.data
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
	font-weight: 600;
	color: rgba(44, 62, 80, 0.8);
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
	background: rgba(0, 0, 0, 0.045);
	color: rgba(44, 62, 80, 0.65);
	font-family: $family-monospace;
	font-size: 0.7rem;
	white-space: nowrap;
}
</style>
