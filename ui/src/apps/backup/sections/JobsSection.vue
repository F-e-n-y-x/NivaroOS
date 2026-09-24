<!-- Jobs (spec §12.3): search, filters and sort over the job list; the
     selected job's details beside it (wide) or instead of it (narrow). -->
<template>
	<div class="bk-section bk-jobs" :class="{ 'has-detail': selectedJob && !narrow }">
		<template v-if="!(selectedJob && narrow)">
			<div class="bk-section-head">
				<h2>{{ $t('backup.nav.jobs') }}</h2>
				<button type="button" class="bk-btn is-primary" @click="app.openWizard({})">
					<b-icon icon="plus" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.jobs.new') }}</span>
				</button>
			</div>

			<div v-if="jobs.length" class="bk-jobs-toolbar">
				<label class="bk-search">
					<span class="bk-sr-only">{{ $t('backup.jobs.search') }}</span>
					<b-icon icon="magnify" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<input ref="search" v-model="query" type="search" class="bk-input" :placeholder="$t('backup.jobs.search')" />
				</label>
				<div class="bk-chips" role="group" :aria-label="$t('backup.jobs.filter')">
					<button v-for="f in filters" :key="f" type="button" class="bk-chip" :aria-pressed="filter === f ? 'true' : 'false'" @click="filter = f">
						{{ $t('backup.jobs.filter_' + f) }}<template v-if="f !== 'all' && counts[f]"> ({{ counts[f] }})</template>
					</button>
				</div>
				<label class="bk-sort">
					<span>{{ $t('backup.jobs.sort') }}</span>
					<select v-model="sort" class="bk-select">
						<option v-for="s in sorts" :key="s" :value="s">{{ $t('backup.jobs.sort_' + s) }}</option>
					</select>
				</label>
			</div>
		</template>

		<div class="bk-jobs-body">
			<div v-if="!(selectedJob && narrow)" class="bk-jobs-list-wrap">
				<div v-if="!jobs.length" class="bk-empty">
					<b-icon icon="format-list-checks" pack="mdi" custom-size="mdi-48px" aria-hidden="true"></b-icon>
					<p>{{ $t('backup.jobs.none') }}</p>
					<button type="button" class="bk-btn is-primary" @click="app.openWizard({})">{{ $t('backup.jobs.create_first') }}</button>
				</div>
				<div v-else-if="!visible.length" class="bk-empty">
					<p>{{ $t('backup.jobs.no_match') }}</p>
					<button type="button" class="bk-btn" @click="clearFilters">{{ $t('backup.jobs.clear_filters') }}</button>
				</div>
				<ul v-else class="bk-jobs-list" :aria-label="$t('backup.nav.jobs')">
					<job-card
						v-for="job in visible"
						:key="job.id"
						:job="job"
						:fmt="fmt"
						:cron-info="cronInfo"
						:live="job.active_run ? app.live[job.active_run.id] || null : null"
						:selected="job.id === selectedJobId"
						:busy="app.busyJobId === job.id"
						@select="j => app.showJob(j.id === selectedJobId ? '' : j.id)"
						@run="app.runNow"
						@cancel="j => app.cancelRun(j.active_run.id, j)"
						@open-run="j => app.openRun(j.active_run.id, j)"
						@toggle="app.toggleJob"
						@menu="app.jobMenu"
						@action="(a, j) => app.runAction(a, j, j.last_run ? j.last_run.id : '')"
					></job-card>
				</ul>
			</div>

			<job-details-pane
				v-if="selectedJob"
				:key="selectedJob.id"
				:job="selectedJob"
				:cron-info="cronInfo"
				:narrow="narrow"
				:version="app.jobVersion"
				class="bk-jobs-detail"
				@close="app.showJob('')"
				@run="app.runNow"
				@open-run="j => app.openRun(j.active_run.id, j)"
				@open-run-id="r => app.openRun(r.id, selectedJob)"
				@menu="app.jobMenu"
				@browse="(j, v) => app.openBrowse(j, v)"
			></job-details-pane>
		</div>
	</div>
</template>

<script>
import JobCard from '../components/JobCard.vue'
import JobDetailsPane from '../components/JobDetailsPane.vue'
import { JOB_FILTERS, JOB_SORTS, filterJobs, sortJobs } from '../state'

const PREFS_KEY = 'nivaroos_backup_jobs_view'

function readPrefs() {
	try {
		const p = JSON.parse(localStorage.getItem(PREFS_KEY) || '{}')
		return {
			filter: JOB_FILTERS.includes(p.filter) ? p.filter : 'all',
			sort: JOB_SORTS.includes(p.sort) ? p.sort : 'next_run'
		}
	} catch (e) {
		return { filter: 'all', sort: 'next_run' }
	}
}

export default {
	name: 'JobsSection',
	components: { JobCard, JobDetailsPane },
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true },
		fmt: { type: Object, required: true },
		cronInfo: { type: Object, default: () => ({}) },
		selectedJobId: { type: String, default: '' },
		narrow: { type: Boolean, default: false }
	},
	data() {
		const p = readPrefs()
		return { query: '', filter: p.filter, sort: p.sort, filters: JOB_FILTERS, sorts: JOB_SORTS }
	},
	computed: {
		visible() {
			return sortJobs(filterJobs(this.jobs, { filter: this.filter, query: this.query }), this.sort)
		},
		counts() {
			const out = {}
			for (const f of JOB_FILTERS) out[f] = filterJobs(this.jobs, { filter: f }).length
			return out
		},
		selectedJob() {
			return this.jobs.find(j => j.id === this.selectedJobId) || null
		}
	},
	watch: {
		filter: 'savePrefs',
		sort: 'savePrefs'
	},
	methods: {
		savePrefs() {
			try {
				localStorage.setItem(PREFS_KEY, JSON.stringify({ filter: this.filter, sort: this.sort }))
			} catch (e) {
				// Private mode or full storage: the view just isn't remembered.
			}
		},
		clearFilters() {
			this.query = ''
			this.filter = 'all'
		},
		focusSearch() {
			if (this.$refs.search) this.$refs.search.focus()
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-jobs-toolbar {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2) var(--space-3);
}
.bk-search {
	position: relative;
	display: flex;
	align-items: center;
	flex: 1 1 12rem;
	min-width: 0;
	.icon {
		position: absolute;
		left: var(--space-2);
		color: var(--theme-text-muted);
		pointer-events: none;
	}
	input {
		width: 100%;
		padding-left: 2rem;
	}
}
.bk-chips {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
}
.bk-sort {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}
.bk-jobs-body {
	display: flex;
	gap: var(--space-4);
	align-items: flex-start;
	min-width: 0;
}
.bk-jobs-list-wrap {
	flex: 1 1 0;
	min-width: 0;
}
.bk-jobs-list {
	list-style: none;
	margin: 0;
	padding: 0;
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
}
.has-detail .bk-jobs-list-wrap {
	flex-basis: 55%;
}
.bk-jobs-detail {
	flex: 1 1 45%;
	position: sticky;
	top: 0;
}
</style>
