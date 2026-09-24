<!-- Settings (spec §12.9): defaults for new jobs, log retention,
     remembered USB drives, the Scheduled Tasks import, and what the
     service runs on. -->
<template>
	<div class="bk-section bk-settings">
		<div class="bk-section-head">
			<h2>{{ $t('backup.nav.settings') }}</h2>
		</div>

		<p v-if="loadError" class="bk-inline-error" role="alert">
			{{ errText(loadError) }}
			<button type="button" class="bk-btn is-small" @click="load">{{ $t('backup.retry') }}</button>
		</p>

		<!-- Defaults -->
		<form v-if="form" class="bk-card bk-settings-form" novalidate @submit.prevent="save">
			<h3 class="bk-card-title">{{ $t('backup.settings.defaults') }}</h3>
			<p v-if="fieldErrorList.length" :id="`${uid}-errors`" class="bk-inline-error" role="alert">{{ $t('backup.settings.fix_fields') }}</p>
			<div class="bk-fields">
				<div v-for="f in numberFields" :key="f.key" class="bk-field">
					<label :for="`${uid}-${f.key}`">{{ $t('backup.settings.' + f.key) }}</label>
					<input
						:id="`${uid}-${f.key}`"
						v-model.number="form[f.key]"
						type="number"
						class="bk-input"
						:min="f.min"
						:max="f.max"
						step="1"
						inputmode="numeric"
						:aria-invalid="fieldError(f.key) ? 'true' : null"
						:aria-describedby="`${uid}-${f.key}-hint` + (fieldError(f.key) ? ` ${uid}-${f.key}-err` : '')"
					/>
					<p :id="`${uid}-${f.key}-hint`" class="bk-field-hint">{{ $t('backup.settings.' + f.key + '_hint') }}</p>
					<p v-if="fieldError(f.key)" :id="`${uid}-${f.key}-err`" class="bk-field-error">{{ fieldError(f.key) }}</p>
				</div>
				<div class="bk-field bk-field-check">
					<input :id="`${uid}-catch_up`" v-model="form.catch_up_default" type="checkbox" :aria-describedby="`${uid}-catch_up-hint`" />
					<label :for="`${uid}-catch_up`">{{ $t('backup.settings.catch_up_default') }}</label>
					<p :id="`${uid}-catch_up-hint`" class="bk-field-hint">{{ $t('backup.settings.catch_up_default_hint') }}</p>
				</div>
			</div>
			<div class="bk-form-actions">
				<button type="submit" class="bk-btn is-primary" :disabled="saving || !dirty">{{ saving ? $t('backup.saving') : $t('backup.save') }}</button>
				<button type="button" class="bk-btn" :disabled="saving || !dirty" @click="resetForm">{{ $t('backup.settings.undo') }}</button>
				<span class="bk-sr-only" aria-live="polite">{{ savedNote }}</span>
			</div>
		</form>

		<!-- Remembered drives -->
		<section class="bk-card" :aria-labelledby="`${uid}-drives`">
			<h3 :id="`${uid}-drives`" class="bk-card-title">{{ $t('backup.settings.drives') }}</h3>
			<p class="bk-secondary bk-card-text">{{ $t('backup.settings.drives_hint') }}</p>
			<p v-if="!drives.length" class="bk-secondary bk-card-text">{{ $t('backup.settings.drives_none') }}</p>
			<ul v-else class="bk-drives">
				<li v-for="d in drives" :key="d.endpoint.ref_id" class="bk-drive">
					<b-icon icon="usb-flash-drive-outline" pack="mdi" custom-size="mdi-20px" aria-hidden="true"></b-icon>
					<div class="bk-drive-main">
						<label class="bk-sr-only" :for="`${uid}-drive-${d.endpoint.ref_id}`">{{ $t('backup.settings.drive_name', { name: d.endpoint.label }) }}</label>
						<input :id="`${uid}-drive-${d.endpoint.ref_id}`" v-model="driveNames[d.endpoint.ref_id]" class="bk-input" type="text" maxlength="64" />
						<span class="bk-drive-meta">{{ driveMeta(d) }}</span>
					</div>
					<button type="button" class="bk-btn is-small" :disabled="driveBusy === d.endpoint.ref_id || !driveRenamed(d)" @click="renameDrive(d)">{{ $t('backup.settings.rename') }}</button>
					<button type="button" class="bk-btn is-small" :disabled="driveBusy === d.endpoint.ref_id" @click="forgetDrive(d)">{{ $t('backup.settings.forget') }}</button>
				</li>
			</ul>
		</section>

		<!-- Import from Scheduled Tasks -->
		<section class="bk-card" :aria-labelledby="`${uid}-import`">
			<h3 :id="`${uid}-import`" class="bk-card-title">{{ $t('backup.settings.import') }}</h3>
			<template v-if="migration">
				<p class="bk-card-text">{{ migrationText }}</p>
				<ul v-if="migration.items && migration.items.length" class="bk-import-list">
					<li v-for="item in migration.items" :key="item.task_id">
						<span class="bk-import-name">{{ item.task_name }}</span>
						<span class="bk-pill" :class="'is-' + importTone(item.result)">{{ $t('backup.settings.import_result.' + item.result) }}</span>
						<ul v-if="item.notes && item.notes.length" class="bk-import-notes">
							<li v-for="(n, i) in item.notes" :key="i">{{ msg(n) }}</li>
						</ul>
						<button v-if="item.job_id && jobExists(item.job_id)" type="button" class="bk-btn is-link is-small" @click="app.showJob(item.job_id)">{{ $t('backup.settings.open_job') }}</button>
					</li>
				</ul>
			</template>
			<div class="bk-form-actions">
				<button type="button" class="bk-btn" :disabled="rerunning" @click="rerunImport">{{ rerunning ? $t('backup.settings.importing') : $t('backup.settings.rerun_import') }}</button>
			</div>
		</section>

		<!-- About -->
		<section class="bk-card" :aria-labelledby="`${uid}-about`">
			<h3 :id="`${uid}-about`" class="bk-card-title">{{ $t('backup.settings.about') }}</h3>
			<dl v-if="bkCaps" class="bk-about">
				<div>
					<dt>{{ $t('backup.settings.version') }}</dt>
					<dd>{{ bkCaps.version }}</dd>
				</div>
				<div>
					<dt>{{ $t('backup.settings.engine') }}</dt>
					<dd>
						<span class="bk-pill" :class="bkCaps.engine.available ? 'is-ok' : 'is-danger'">
							<b-icon :icon="bkCaps.engine.available ? 'check-circle-outline' : 'close-circle-outline'" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
							<span>{{ bkCaps.engine.available ? $t('backup.settings.engine_ready') : $t('backup.settings.engine_unavailable') }}</span>
						</span>
						<span class="bk-secondary"> rclone {{ bkCaps.engine.rclone }}</span>
					</dd>
				</div>
				<div>
					<dt>{{ $t('backup.settings.clock') }}</dt>
					<dd>
						<span class="bk-pill" :class="bkCaps.clock_synced ? 'is-ok' : 'is-warn'">
							<b-icon :icon="bkCaps.clock_synced ? 'check-circle-outline' : 'alert-outline'" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
							<span>{{ bkCaps.clock_synced ? $t('backup.settings.clock_synced') : $t('backup.settings.clock_unsynced') }}</span>
						</span>
						<span class="bk-secondary"> {{ bkCaps.timezone }} (UTC{{ bkCaps.utc_offset }})</span>
					</dd>
				</div>
				<div>
					<dt>{{ $t('backup.settings.concurrency_now') }}</dt>
					<dd>{{ bkCaps.max_concurrent }}</dd>
				</div>
			</dl>
		</section>
	</div>
</template>

<script>
import { backupMixin, loadCapabilities } from '../backupMixin'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { fieldErrorKey } from '../errorCodes'
import { escapeHtml } from '@/utils/escapeHtml'

const NUMBER_FIELDS = [
	{ key: 'max_concurrent', min: 1, max: 4 },
	{ key: 'default_delete_pct', min: 1, max: 100 },
	{ key: 'default_change_pct', min: 1, max: 100 },
	{ key: 'default_versions_days', min: 0, max: 3650 },
	{ key: 'log_retention_days', min: 1, max: 3650 }
]

export default {
	name: 'BackupSettingsSection',
	mixins: [backupMixin, confirmWindowMixin],
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true }
	},
	data() {
		return {
			uid: 'bk-settings',
			numberFields: NUMBER_FIELDS,
			settings: null,
			form: null,
			fieldErrors: {},
			saving: false,
			savedNote: '',
			drives: [],
			driveNames: {},
			driveBusy: '',
			migration: null,
			rerunning: false,
			loadError: null
		}
	},
	computed: {
		dirty() {
			return !!(this.form && this.settings && JSON.stringify(this.form) !== JSON.stringify(this.settings))
		},
		fieldErrorList() {
			return Object.keys(this.fieldErrors)
		},
		migrationText() {
			const m = this.migration
			if (m.state === 'pending') return this.$t('backup.settings.import_pending')
			if (m.state === 'failed') return this.$t('backup.settings.import_failed')
			if (!m.items || !m.items.length) return this.$t('backup.settings.import_nothing')
			return this.$t('backup.settings.import_done', { count: this.fmt.number(m.imported || 0), when: this.fmt.when(m.ran_at) })
		}
	},
	created() {
		this.load()
	},
	methods: {
		async load() {
			this.loadError = null
			try {
				const [settings, drives, migration] = await Promise.all([this.bkApi.getSettings(), this.bkApi.listDrives(), this.bkApi.getMigration()])
				this.settings = settings
				this.resetForm()
				this.setDrives(drives)
				this.migration = migration
			} catch (e) {
				this.loadError = e
			}
		},
		resetForm() {
			this.form = { ...this.settings }
			this.fieldErrors = {}
		},
		fieldError(key) {
			const v = this.fieldErrors[key]
			return v ? this.$t(fieldErrorKey(v)) : ''
		},
		async save() {
			this.saving = true
			this.fieldErrors = {}
			this.savedNote = ''
			try {
				this.settings = await this.bkApi.putSettings(this.form)
				this.resetForm()
				this.savedNote = this.$t('backup.settings.saved')
				this.toast(this.$t('backup.settings.saved'))
				loadCapabilities(true)
					.then(c => (this.bkCaps = c))
					.catch(() => {})
			} catch (e) {
				if (e.code === 'validation' && Object.keys(e.fieldErrors).length) {
					this.fieldErrors = e.fieldErrors
					this.$nextTick(() => {
						const first = this.$el.querySelector('[aria-invalid="true"]')
						if (first) first.focus()
					})
				} else this.toastError(e)
			} finally {
				this.saving = false
			}
		},
		setDrives(drives) {
			this.drives = drives || []
			const names = {}
			for (const d of this.drives) names[d.endpoint.ref_id] = d.label || d.endpoint.label || ''
			this.driveNames = names
		},
		driveRenamed(d) {
			const name = (this.driveNames[d.endpoint.ref_id] || '').trim()
			return !!name && name !== (d.label || d.endpoint.label || '')
		},
		driveMeta(d) {
			const parts = [d.endpoint.label]
			if (d.last_seen) parts.push(this.$t('backup.settings.last_seen', { when: this.fmt.relative(d.last_seen) }))
			return parts.filter(Boolean).join(' · ')
		},
		async renameDrive(d) {
			const id = d.endpoint.ref_id
			this.driveBusy = id
			try {
				const updated = await this.bkApi.renameDrive(id, this.driveNames[id].trim())
				this.setDrives(this.drives.map(x => (x.endpoint.ref_id === id ? updated : x)))
				this.toast(this.$t('backup.settings.renamed'))
			} catch (e) {
				this.toastError(e)
			} finally {
				this.driveBusy = ''
			}
		},
		forgetDrive(d) {
			const id = d.endpoint.ref_id
			const name = d.label || d.endpoint.label || id
			this.confirmWindow({
				title: this.$t('backup.settings.forget_title'),
				message: escapeHtml(this.$t('backup.settings.forget_message', { name })),
				confirmText: this.$t('backup.settings.forget'),
				type: 'is-danger',
				onConfirm: async () => {
					this.driveBusy = id
					try {
						await this.bkApi.forgetDrive(id)
						this.setDrives(this.drives.filter(x => x.endpoint.ref_id !== id))
					} catch (e) {
						this.toast(e.code === 'invalid_state' ? this.$t('backup.settings.forget_in_use') : this.errText(e), 'is-danger')
					} finally {
						this.driveBusy = ''
					}
				}
			})
		},
		async rerunImport() {
			this.rerunning = true
			try {
				this.migration = await this.bkApi.rerunMigration()
				this.app.reload()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.rerunning = false
			}
		},
		importTone(result) {
			if (result === 'imported') return 'ok'
			if (result === 'imported_unresolved') return 'warn'
			return 'muted'
		},
		jobExists(id) {
			return this.jobs.some(j => j.id === id)
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-settings {
	max-width: 52rem;
}
.bk-card {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}
.bk-card-title {
	margin-bottom: 0;
}
.bk-card-text {
	margin: 0;
	font-size: var(--font-sm);
}
.bk-fields {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
	gap: var(--space-3) var(--space-4);
}
.bk-field {
	display: flex;
	flex-direction: column;
	gap: 0.125rem;
	label {
		font-size: var(--font-sm);
		font-weight: 500;
		color: var(--theme-text-primary);
	}
	input[type='number'] {
		max-width: 8rem;
	}
	p {
		margin: 0;
	}
}
.bk-field-check {
	display: grid;
	grid-template-columns: auto minmax(0, 1fr);
	align-items: center;
	column-gap: var(--space-2);
	input {
		width: 1.125rem;
		height: 1.125rem;
	}
	.bk-field-hint {
		grid-column: 2;
	}
}
.bk-field-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
.bk-field-error,
.bk-inline-error {
	margin: 0;
	font-size: var(--font-sm);
	color: var(--color-danger-fg);
}
.bk-inline-error {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
}
.bk-form-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	margin-top: var(--space-2);
}
.bk-drives,
.bk-import-list {
	list-style: none;
	margin: 0;
	padding: 0;
	> li {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		border-top: 1px solid var(--theme-table-divider);
	}
}
.bk-drive-main {
	flex: 1 1 14rem;
	display: flex;
	flex-direction: column;
	gap: 0.125rem;
	min-width: 0;
}
.bk-drive-meta {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
.bk-import-name {
	font-weight: 600;
	color: var(--theme-text-primary);
}
.bk-import-notes {
	flex-basis: 100%;
	margin: 0;
	padding-left: var(--space-5);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}
.bk-about {
	margin: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	> div {
		display: grid;
		grid-template-columns: minmax(7rem, 11rem) minmax(0, 1fr);
		gap: var(--space-3);
		align-items: center;
	}
	dt {
		font-size: var(--font-sm);
		color: var(--theme-text-secondary);
	}
	dd {
		margin: 0;
		font-size: var(--font-sm);
	}
}
.bk-w-phone .bk-about > div {
	grid-template-columns: minmax(0, 1fr);
	gap: 0;
}
</style>
