<!-- Restore (spec §12.7): 1. which job, 2. from when (a version), 3.
     browse it - the Browse window picks files, and its Restore… opens the
     restore window. -->
<template>
	<div class="bk-section bk-restore">
		<div class="bk-section-head">
			<h2>{{ $t('backup.nav.restore') }}</h2>
		</div>

		<div v-if="!jobs.length" class="bk-empty">
			<b-icon icon="backup-restore" pack="mdi" custom-size="mdi-48px" aria-hidden="true"></b-icon>
			<p>{{ $t('backup.restore.no_jobs') }}</p>
			<button type="button" class="bk-btn is-primary" @click="app.openWizard({})">{{ $t('backup.jobs.create_first') }}</button>
		</div>

		<ol v-else class="bk-steps-form">
			<li class="bk-card">
				<label class="bk-step-label" :for="jobSelectId">{{ $t('backup.restore.step_job') }}</label>
				<select :id="jobSelectId" v-model="jobId" class="bk-select bk-restore-job" data-autofocus>
					<option v-for="j in sortedJobs" :key="j.id" :value="j.id">{{ j.name }}</option>
				</select>
			</li>
			<li class="bk-card">
				<p :id="versionsLabelId" class="bk-step-label">{{ $t('backup.restore.step_when') }}</p>
				<p v-if="loading" class="bk-secondary">{{ $t('backup.loading') }}</p>
				<p v-else-if="error" class="bk-inline-error" role="alert">
					{{ errText(error) }}
					<button type="button" class="bk-btn is-small" @click="load">{{ $t('backup.retry') }}</button>
				</p>
				<version-list
					v-else
					v-model="versionId"
					:versions="versions"
					:fmt="fmt"
					:job-type="job ? job.type : ''"
					:current-at="lastSuccess"
					:labelledby="versionsLabelId"
					selectable
				></version-list>
			</li>
			<li class="bk-card">
				<p class="bk-step-label">{{ $t('backup.restore.step_browse') }}</p>
				<p class="bk-secondary bk-step-hint">{{ $t('backup.restore.browse_hint') }}</p>
				<button type="button" class="bk-btn is-primary" :disabled="!versionId || loading" @click="browse">
					<span>{{ $t('backup.restore.browse_version') }}</span>
					<b-icon icon="arrow-right" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				</button>
			</li>
		</ol>
	</div>
</template>

<script>
import VersionList from '../components/VersionList.vue'
import { backupMixin } from '../backupMixin'

export default {
	name: 'RestoreSection',
	components: { VersionList },
	mixins: [backupMixin],
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true },
		initialJobId: { type: String, default: '' }
	},
	data() {
		return {
			jobId: '',
			versionId: '',
			versions: [],
			lastSuccess: '',
			loading: false,
			error: null,
			jobSelectId: 'bk-restore-job',
			versionsLabelId: 'bk-restore-versions'
		}
	},
	computed: {
		sortedJobs() {
			return this.jobs.slice().sort((a, b) => String(a.name).localeCompare(String(b.name)))
		},
		job() {
			return this.jobs.find(j => j.id === this.jobId) || null
		}
	},
	watch: {
		initialJobId: {
			immediate: true,
			handler(id) {
				if (id && this.jobs.some(j => j.id === id)) this.jobId = id
			}
		},
		jobs: {
			immediate: true,
			handler(list) {
				if (!list.some(j => j.id === this.jobId)) this.jobId = this.sortedJobs.length ? this.sortedJobs[0].id : ''
			}
		},
		// Immediate: the two watchers above may already have picked a job.
		jobId: {
			immediate: true,
			handler() {
				this.load()
			}
		}
	},
	methods: {
		async load() {
			const id = this.jobId
			this.versions = []
			this.versionId = ''
			this.error = null
			if (!id) return
			this.loading = true
			try {
				const [versions, detail] = await Promise.all([this.bkApi.listVersions(id), this.bkApi.getJob(id).catch(() => null)])
				if (id !== this.jobId) return
				this.versions = versions || []
				this.lastSuccess = (detail && detail.stats && detail.stats.last_success) || ''
				this.versionId = this.versions.length ? this.versions[0].id : ''
			} catch (e) {
				if (id === this.jobId) this.error = e
			} finally {
				if (id === this.jobId) this.loading = false
			}
		},
		browse() {
			const version = this.versions.find(v => v.id === this.versionId)
			if (this.job && version) this.app.openBrowse(this.job, version)
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-steps-form {
	margin: 0;
	padding: 0;
	list-style: none;
	counter-reset: bk-step;
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	max-width: 44rem;
	> li {
		counter-increment: bk-step;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
	}
}
.bk-step-label {
	margin: 0;
	font-weight: 600;
	color: var(--theme-text-primary);
	&::before {
		content: counter(bk-step) '. ';
	}
}
.bk-step-hint {
	margin: 0;
	font-size: var(--font-sm);
}
.bk-restore-job {
	width: 100%;
	max-width: 28rem;
}
.bk-steps-form .bk-versions {
	width: 100%;
}
.bk-inline-error {
	margin: 0;
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	color: var(--color-danger-fg);
	font-size: var(--font-sm);
}
</style>
