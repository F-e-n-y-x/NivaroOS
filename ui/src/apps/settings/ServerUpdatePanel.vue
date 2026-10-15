<template>
	<!-- Server releases (server-vX.Y.Z), installed by nivaroos-update.service:
		status, what's new, Update now, progress, result, schedule, history. -->
	<div v-if="status">
		<h3 class="setting-card-title">{{ $t('NivaroOS server releases') }}</h3>
		<div class="setting-card" data-test="server-update">
			<div class="setting-row">
				<b-icon class="row-icon" :icon="busy ? 'progress-download' : available ? 'arrow-up-bold-circle-outline' : 'check-circle-outline'" pack="mdi" size="is-20" :class="{ 'has-text-primary': busy || available, 'has-text-success': !busy && !available }"></b-icon>
				<div class="row-label">
					<div class="setting-title" data-test="server-update-title">{{ title }}</div>
					<div class="setting-desc">
						<span>{{ current ? $t('Installed: {v}', { v: current }) : $t('Built from source (installer)') }}</span>
						<span class="mx-2">&middot;</span>
						<span>{{ settings.channel === 'beta' ? $t('Beta channel') : $t('Stable channel') }}</span>
						<template v-if="st.last_check">
							<span class="mx-2">&middot;</span>
							<span>{{ $t('Checked') }}: {{ formatDate(st.last_check) }}</span>
						</template>
					</div>
				</div>
				<div class="row-control">
					<div class="buttons are-small mb-0">
						<b-button rounded size="is-small" :disabled="busy" :loading="requesting === 'check'" @click="request('check')">
							<i class="mdi mdi-refresh mr-1"></i>
							{{ $t('Check now') }}
						</b-button>
						<b-button v-if="available && !st.unsupported && !busy" data-test="server-update-now" rounded size="is-small" type="is-primary" :loading="requesting === 'apply'" @click="updateNow">
							<i class="mdi mdi-download mr-1"></i>
							{{ current ? $t('Update now') : $t('Install {v}', { v: available.version }) }}
						</b-button>
					</div>
				</div>
			</div>

			<div v-if="busy" class="update-inset" data-test="server-update-step">
				<b-progress v-if="st.state === 'downloading' && st.progress" :value="st.progress" size="is-small" class="mb-2"></b-progress>
				<b-progress v-else size="is-small" class="mb-2"></b-progress>
				<div class="is-size-7">{{ stepLabel || $t('Starting...') }}</div>
				<div class="is-size-7 text-muted">{{ $t('The dashboard may reload once while services restart.') }}</div>
			</div>

			<div v-for="line in notes" :key="line" class="update-inset is-size-7">{{ line }}</div>

			<div v-if="available && !busy" class="update-inset">
				<div class="is-flex is-justify-content-between is-size-7 mb-1">
					<span>{{ $t('NivaroOS {v}', { v: available.version }) }}<template v-if="available.channel === 'beta'"> ({{ $t('beta') }})</template> &middot; {{ size }}</span>
					<span class="text-muted">{{ formatDate(available.published_at) }}</span>
				</div>
				<pre v-if="available.notes_md" class="notes-pre">{{ available.notes_md }}</pre>
				<div v-if="!current" class="is-size-7 text-muted mt-1">{{ $t('This server was built from source. Installing a release moves it to releases; the installer moves it back.') }}</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="clock-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Automatic updates') }}</div>
					<div class="setting-desc">{{ $t('Installed at night only when no backup, file transfer, VM console or Host Desktop session is running; otherwise the next night.') }}</div>
				</div>
				<div class="row-control is-flex is-align-items-center">
					<b-select v-model="form.auto" size="is-small" data-test="server-update-auto" @update:modelValue="save">
						<option value="install">{{ $t('Install automatically') }}</option>
						<option value="notify">{{ $t('Only tell me') }}</option>
					</b-select>
					<input v-model="form.window" type="time" class="input is-small ml-2 time-input" :aria-label="$t('Time')" :disabled="form.auto !== 'install'" @change="save" />
					<b-select v-model="form.channel" size="is-small" class="ml-2" :aria-label="$t('Channel')" @update:modelValue="save">
						<option value="stable">{{ $t('Stable') }}</option>
						<option value="beta">{{ $t('Beta') }}</option>
					</b-select>
				</div>
			</div>

			<div v-if="history.length" class="setting-row history-row">
				<b-icon class="row-icon" icon="history" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('History') }}</div>
					<div v-for="(h, i) in history.slice(0, 5)" :key="i" class="setting-desc">{{ formatDate(h.at) }} &middot; {{ resultLabel(h) }}</div>
				</div>
				<div class="row-control">
					<b-button v-if="status.can_rollback && !busy" rounded size="is-small" :loading="requesting === 'rollback'" @click="rollback">
						<i class="mdi mdi-undo mr-1"></i>
						{{ $t('Roll back to {v}', { v: history[0].from === 'source' ? $t('the previous build') : history[0].from }) }}
					</b-button>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { apiError } from '@/utils/apiError'
import { isBusy, stepLabel, resultLabel, formatSize } from './serverUpdate'

export default {
	name: 'server-update-panel',
	data() {
		return { status: null, requesting: '', timer: null, form: { auto: 'install', window: '04:00', channel: 'stable' } }
	},
	computed: {
		st() {
			return (this.status && this.status.state) || {}
		},
		settings() {
			return (this.status && this.status.settings) || this.form
		},
		history() {
			return (this.status && this.status.history) || []
		},
		current() {
			return this.st.current || ''
		},
		available() {
			return this.st.available || null
		},
		busy() {
			return isBusy(this.status)
		},
		stepLabel() {
			return stepLabel(this.$t.bind(this), this.st)
		},
		size() {
			return formatSize(this.available && this.available.size)
		},
		title() {
			if (this.busy) return this.$t('Updating NivaroOS')
			if (this.available) return this.$t('NivaroOS {v} is available', { v: this.available.version })
			return this.$t('No newer server release')
		},
		// Why tonight's install was skipped, why this box uses the
		// installer, the last error, and the last update's outcome.
		notes() {
			const r = this.st.last_result && !this.busy ? resultLabel(this.$t.bind(this), this.st.last_result) : ''
			return [this.st.unsupported, this.st.skipped, this.st.last_error, r].filter(Boolean)
		}
	},
	created() {
		this.load()
	},
	beforeUnmount() {
		clearTimeout(this.timer)
	},
	methods: {
		resultLabel(r) {
			return resultLabel(this.$t.bind(this), r)
		},
		formatDate(s) {
			const d = s ? new Date(s) : null
			return d && !isNaN(d) ? d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : ''
		},
		// Polls every 2 s while an update runs; the gateway restarting in the
		// middle is just a failed poll, retried.
		async load() {
			clearTimeout(this.timer)
			try {
				const res = await this.$api.sys.getServerUpdate()
				this.status = res.data.data
				this.form = { ...this.status.settings }
			} catch {
				if (!this.status) return
			}
			if (this.busy || this.requesting) this.timer = setTimeout(this.load, 2000)
		},
		async request(action, body = {}) {
			this.requesting = action
			try {
				await this.$api.sys.requestServerUpdate(action, body)
				// The service picks the request up; keep polling until it's done.
				setTimeout(() => {
					this.requesting = ''
				}, 4000)
				this.load()
			} catch (e) {
				this.requesting = ''
				this.$buefy.toast.open({ message: apiError(e, this.$t('The update could not be started')), type: 'is-danger' })
			}
		},
		async updateNow() {
			let running = []
			try {
				const { default: backup } = await import('@/service/backup.js')
				const page = await backup.listRuns({ status: 'running' })
				running = (page && page.runs) || []
			} catch {
				running = []
			}
			if (!running.length) {
				this.request('apply', { mode: 'wait' })
				return
			}
			this.$buefy.dialog.confirm({
				title: this.$t('Backups are running'),
				message: running.map((r) => r.job_name || r.id).join(', '),
				confirmText: this.$t('Update when they finish'),
				cancelText: this.$t('Cancel them and update'),
				canCancel: ['button'],
				onConfirm: () => this.request('apply', { mode: 'wait' }),
				onCancel: () => this.request('apply', { mode: 'cancel_backups' })
			})
		},
		rollback() {
			const last = this.history[0] || {}
			this.$buefy.dialog.confirm({
				title: this.$t('Roll back {v}?', { v: last.to }),
				message: this.$t('Every file the update changed goes back. Databases stay as they are now.'),
				confirmText: this.$t('Roll back'),
				onConfirm: () => this.request('rollback', { restore_data: false })
			})
		},
		async save() {
			try {
				const res = await this.$api.sys.setServerUpdateSettings(this.form)
				this.status = { ...this.status, settings: res.data.data }
			} catch (e) {
				this.$buefy.toast.open({ message: apiError(e, this.$t('Could not save the update settings')), type: 'is-danger' })
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.update-inset {
	margin: 0 var(--space-5) var(--space-3);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-control);
	padding: var(--space-3) var(--space-4);
}

.notes-pre {
	white-space: pre-wrap;
	font-size: var(--font-xs);
	max-height: 240px;
	overflow: auto;
	background: transparent;
	padding: 0;
}

.time-input {
	width: 7.5rem;
}

.history-row .setting-desc {
	overflow-wrap: anywhere;
}
</style>
