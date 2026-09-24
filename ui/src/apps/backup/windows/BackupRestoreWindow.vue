<!-- Restore… (spec §12.7): where the files go (back where they came from,
     or another folder through the shared pickers) and what happens when a
     file is already there (keep both - the default -, replace or skip).
     The restore runs as a kind=restore run in the usual run window. -->
<template>
	<form class="bk-window bk-restore-win" novalidate @keydown="onWindowKeydown" @submit.prevent="submit">
		<div class="bk-restore-body">
			<section class="bk-restore-what">
				<h2 class="bk-restore-h">{{ $t('backup.restore.what') }}</h2>
				<p>{{ whatText }}</p>
				<ul v-if="paths.length && paths.length <= 5" class="bk-restore-paths">
					<li v-for="p in paths" :key="p">{{ p }}</li>
				</ul>
				<p class="bk-secondary">{{ versionText }}</p>
			</section>

			<fieldset class="bk-restore-set">
				<legend class="bk-restore-h">{{ $t('backup.restore.where') }}</legend>
				<label class="bk-radio">
					<input v-model="mode" type="radio" value="original" name="bk-restore-target" data-autofocus />
					<span>
						<span class="bk-radio-title">{{ $t('backup.restore.to_original') }}</span>
						<span v-if="originalText" class="bk-radio-hint">{{ originalText }}</span>
					</span>
				</label>
				<label class="bk-radio">
					<input v-model="mode" type="radio" value="other" name="bk-restore-target" />
					<span>
						<span class="bk-radio-title">{{ $t('backup.restore.to_other') }}</span>
						<span class="bk-radio-hint">{{ otherEndpoint ? endpointText(otherEndpoint) : $t('backup.restore.no_folder_yet') }}</span>
					</span>
				</label>
				<button v-if="mode === 'other'" type="button" class="bk-btn bk-restore-choose" :aria-describedby="fieldError('target') ? 'bk-restore-target-err' : null" @click="chooseFolder">
					<b-icon icon="folder-search-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ otherEndpoint ? $t('backup.restore.change_folder') : $t('backup.restore.choose_folder') }}</span>
				</button>
				<p v-if="fieldError('target')" id="bk-restore-target-err" class="bk-field-error">{{ fieldError('target') }}</p>
			</fieldset>

			<fieldset class="bk-restore-set">
				<legend class="bk-restore-h">{{ $t('backup.restore.conflict') }}</legend>
				<label v-for="c in conflicts" :key="c" class="bk-radio">
					<input v-model="conflict" type="radio" :value="c" name="bk-restore-conflict" />
					<span>
						<span class="bk-radio-title">{{ $t('backup.restore.conflict_' + c) }}</span>
						<span class="bk-radio-hint">{{ $t('backup.restore.conflict_' + c + '_hint', { date: today }) }}</span>
					</span>
				</label>
				<p v-if="conflict === 'overwrite'" class="bk-restore-warn" role="note">
					<b-icon icon="alert-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.restore.overwrite_warning') }}</span>
				</p>
			</fieldset>

			<div v-if="error" class="bk-restore-error" role="alert">
				<p class="bk-restore-error-title">{{ explain(error.code).title }}</p>
				<p>{{ explain(error.code).fix }}</p>
			</div>
		</div>

		<footer class="bk-restore-foot">
			<button type="button" class="bk-btn" @click="$emit('close')">{{ $t('backup.cancel') }}</button>
			<button type="submit" class="bk-btn is-primary" :disabled="submitting || (mode === 'other' && !otherEndpoint)">
				<b-icon icon="backup-restore" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ submitting ? $t('backup.restore.starting') : $t('backup.restore.start') }}</span>
			</button>
		</footer>
	</form>
</template>

<script>
import { backupMixin, windowBehavior } from '../backupMixin'
import { fieldErrorKey } from '../errorCodes'

const CONFLICTS = ['keep_both', 'overwrite', 'skip']

export default {
	name: 'BackupRestoreWindow',
	mixins: [backupMixin, windowBehavior],
	props: {
		winId: { type: String, default: '' },
		jobId: { type: String, required: true },
		versionId: { type: String, required: true },
		// Relative to the version's root; [] restores everything.
		paths: { type: Array, default: () => [] },
		jobName: { type: String, default: '' }
	},
	data() {
		return { job: null, version: null, mode: 'original', otherEndpoint: null, conflict: 'keep_both', conflicts: CONFLICTS, submitting: false, error: null }
	},
	computed: {
		closeBlocked() {
			return this.submitting
		},
		today() {
			// The date the engine puts in "name (restored <date>)": server-local.
			return this.fmt.dayKey(new Date())
		},
		whatText() {
			if (!this.paths.length) return this.$t('backup.restore.what_all')
			return this.$tc('backup.restore.what_n', this.paths.length, { count: this.fmt.number(this.paths.length) })
		},
		versionText() {
			const v = this.version
			if (!v) return ''
			if (v.kind === 'current') return this.$t('backup.ver.current')
			return this.$t(v.label_key || `backup.ver.${v.kind}`, { at: this.fmt.dateTime(v.time) })
		},
		originalText() {
			if (!this.job) return ''
			return this.fmt.list((this.job.sources || []).map(s => this.endpointText(s)))
		}
	},
	created() {
		this.load()
	},
	methods: {
		async load() {
			try {
				const [job, versions] = await Promise.all([this.bkApi.getJob(this.jobId), this.bkApi.listVersions(this.jobId)])
				this.job = job
				this.version = (versions || []).find(v => v.id === this.versionId) || null
			} catch (e) {
				this.error = e
			}
		},
		endpointText(ep) {
			const where = ep.label || this.$t('backup.ep.' + ep.kind)
			return ep.sub_path ? `${where} › ${ep.sub_path}` : where
		},
		fieldError(field) {
			const fe = (this.error && this.error.fieldErrors) || {}
			const key = Object.keys(fe).find(k => k === field || k.startsWith(field + '.'))
			return key ? this.$t(fieldErrorKey(fe[key])) : ''
		},
		// Location first (StoragePickerWindow), then a folder inside it
		// (FolderPickerWindow) - both shared pickers are separate windows.
		chooseFolder() {
			this.openBackupWindow('storagePicker', {
				role: 'dest',
				selected: this.otherEndpoint,
				sourceEndpoint: null,
				onSelect: location => {
					if (!location) return
					const endpoint = { kind: location.kind, ref_id: location.ref_id, sub_path: '', label: location.label }
					if (location.match) endpoint.match = location.match
					this.openBackupWindow('folderPicker', {
						endpoint,
						startPath: '',
						allowCreate: true,
						onSelect: picked => {
							if (picked) this.otherEndpoint = picked
							this.error = null
						}
					})
				}
			})
		},
		async submit() {
			if (this.submitting) return
			if (this.mode === 'other' && !this.otherEndpoint) return
			this.submitting = true
			this.error = null
			try {
				const target = this.mode === 'other' ? { mode: 'other', endpoint: this.otherEndpoint } : { mode: 'original' }
				const { run_id: runId } = await this.bkApi.restore(this.jobId, { version_id: this.versionId, paths: this.paths, target, conflict: this.conflict, dry_run: false })
				this.openBackupWindow('run', { runId, jobId: this.jobId, jobName: (this.job && this.job.name) || this.jobName })
				this.submitting = false
				this.$emit('close')
			} catch (e) {
				this.error = e
				this.submitting = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-restore-win {
	display: flex;
	flex-direction: column;
	min-height: 100%;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
	font-size: var(--font-base);
	p {
		margin: 0;
	}
}
.bk-restore-body {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	gap: var(--space-4);
	padding: var(--space-4) var(--space-5);
}
.bk-restore-h {
	margin: 0 0 var(--space-2);
	padding: 0;
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-secondary);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}
.bk-restore-what {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.bk-restore-paths {
	margin: 0;
	padding-left: var(--space-5);
	font-size: var(--font-sm);
	overflow-wrap: anywhere;
}
.bk-restore-set {
	margin: 0;
	padding: 0;
	border: none;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.bk-radio {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	min-height: 44px;
	padding: var(--space-2);
	border-radius: var(--radius-sm);
	cursor: pointer;
	&:hover {
		background: var(--theme-card-hover);
	}
	input {
		flex-shrink: 0;
		width: 1.125rem;
		height: 1.125rem;
		margin-top: 0.125rem;
	}
	> span {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
}
.bk-radio-title {
	font-weight: 500;
}
.bk-radio-hint {
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
	overflow-wrap: anywhere;
}
.bk-restore-choose {
	align-self: flex-start;
	margin-left: calc(1.125rem + var(--space-4));
}
.bk-restore-warn {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	background: var(--status-warn-bg);
	color: var(--status-warn-fg);
	font-size: var(--font-sm);
}
.bk-field-error {
	margin-left: calc(1.125rem + var(--space-4));
	font-size: var(--font-sm);
	color: var(--color-danger-fg);
}
.bk-restore-error {
	padding: var(--space-3);
	border-radius: var(--radius-sm);
	background: var(--status-danger-bg);
	color: var(--status-danger-fg);
	font-size: var(--font-sm);
}
.bk-restore-error-title {
	font-weight: 600;
}
.bk-restore-foot {
	position: sticky;
	bottom: 0;
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5) calc(var(--space-3) + env(safe-area-inset-bottom));
	border-top: 1px solid var(--theme-card-border);
	background: var(--theme-bg-window);
}
</style>
