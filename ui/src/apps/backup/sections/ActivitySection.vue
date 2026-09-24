<!-- Activity (spec §12.8): every run, grouped by day, filtered by job,
     result, kind and date range. The filters are remembered per browser. -->
<template>
	<div class="bk-section">
		<div class="bk-section-head">
			<h2>{{ $t('backup.nav.activity') }}</h2>
			<button type="button" class="bk-btn" :disabled="loading" @click="load(false)">
				<b-icon icon="refresh" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.refresh') }}</span>
			</button>
		</div>

		<div class="bk-activity-filters" role="group" :aria-label="$t('backup.activity.filters')">
			<label>
				<span>{{ $t('backup.activity.job') }}</span>
				<select v-model="filters.jobId" class="bk-select">
					<option value="">{{ $t('backup.activity.all_jobs') }}</option>
					<option v-for="j in sortedJobs" :key="j.id" :value="j.id">{{ j.name }}</option>
				</select>
			</label>
			<label>
				<span>{{ $t('backup.activity.result') }}</span>
				<select v-model="filters.result" class="bk-select">
					<option v-for="r in results" :key="r" :value="r">{{ $t('backup.activity.result_' + r) }}</option>
				</select>
			</label>
			<label>
				<span>{{ $t('backup.activity.kind') }}</span>
				<select v-model="filters.kind" class="bk-select">
					<option v-for="k in kinds" :key="k" :value="k">{{ k === 'all' ? $t('backup.activity.all_kinds') : $t('backup.kind.' + k) }}</option>
				</select>
			</label>
			<label>
				<span>{{ $t('backup.activity.range') }}</span>
				<select v-model="filters.range" class="bk-select">
					<option v-for="r in ranges" :key="r" :value="r">{{ $t('backup.activity.range_' + r) }}</option>
				</select>
			</label>
		</div>

		<p v-if="error" class="bk-inline-error" role="alert">{{ errText(error) }}</p>
		<p v-if="loading && !runs.length" class="bk-secondary">{{ $t('backup.loading') }}</p>
		<div v-else-if="!shown.length && !error" class="bk-empty">
			<b-icon icon="history" pack="mdi" custom-size="mdi-48px" aria-hidden="true"></b-icon>
			<p>{{ jobs.length ? $t('backup.activity.none') : $t('backup.activity.none_no_jobs') }}</p>
		</div>
		<activity-list v-else :runs="shown" :fmt="fmt" @open="r => app.openRun(r.id, { id: r.job_id, name: r.job_name })"></activity-list>
		<button v-if="nextBefore && !reachedRange" type="button" class="bk-btn bk-more" :disabled="loading" @click="load(true)">{{ $t('backup.activity.more') }}</button>
	</div>
</template>

<script>
import ActivityList from '../components/ActivityList.vue'
import { backupMixin } from '../backupMixin'
import { ACTIVITY_RESULTS, ACTIVITY_KINDS, ACTIVITY_RANGES, normalizeActivityFilters, activityQuery, runInRange } from '../state'

const FILTERS_KEY = 'nivaroos_backup_activity_filters'
const PAGE = 50

function readFilters() {
	try {
		return normalizeActivityFilters(JSON.parse(localStorage.getItem(FILTERS_KEY) || 'null'))
	} catch (e) {
		return normalizeActivityFilters(null)
	}
}

export default {
	name: 'ActivitySection',
	components: { ActivityList },
	mixins: [backupMixin],
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true },
		initialJobId: { type: String, default: '' },
		// Bumped by the app on every run event, to refresh the list.
		version: { type: Number, default: 0 }
	},
	data() {
		const filters = readFilters()
		if (this.initialJobId) filters.jobId = this.initialJobId
		return { filters, results: ACTIVITY_RESULTS, kinds: ACTIVITY_KINDS, ranges: ACTIVITY_RANGES, runs: [], nextBefore: '', loading: false, error: null, seq: 0 }
	},
	computed: {
		sortedJobs() {
			return this.jobs.slice().sort((a, b) => String(a.name).localeCompare(String(b.name)))
		},
		shown() {
			return this.runs.filter(r => runInRange(r, this.filters.range))
		},
		// Pages are newest first: once one run is older than the range,
		// every later page is too.
		reachedRange() {
			return this.runs.length > 0 && !runInRange(this.runs[this.runs.length - 1], this.filters.range)
		}
	},
	watch: {
		filters: {
			deep: true,
			handler(f) {
				try {
					localStorage.setItem(FILTERS_KEY, JSON.stringify(f))
				} catch (e) {
					// Not remembered (private mode or full storage) - still applied.
				}
				this.load(false)
			}
		},
		initialJobId(id) {
			if (id) this.filters.jobId = id
		},
		version() {
			this.load(false)
		}
	},
	created() {
		this.load(false)
	},
	methods: {
		async load(more) {
			const seq = ++this.seq
			this.loading = true
			this.error = null
			try {
				const page = await this.bkApi.listRuns({ ...activityQuery(this.filters), limit: PAGE, before: more ? this.nextBefore : undefined })
				if (seq !== this.seq) return
				this.runs = more ? this.runs.concat(page.runs || []) : page.runs || []
				this.nextBefore = page.next_before || ''
			} catch (e) {
				if (seq === this.seq) this.error = e
			} finally {
				if (seq === this.seq) this.loading = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-activity-filters {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2) var(--space-3);
	label {
		display: flex;
		flex-direction: column;
		gap: 0.125rem;
		flex: 1 1 9rem;
		min-width: 0;
		font-size: var(--font-xs);
		color: var(--theme-text-secondary);
	}
}
.bk-more {
	align-self: center;
}
.bk-inline-error {
	margin: 0;
	color: var(--color-danger-fg);
	font-size: var(--font-sm);
}
</style>
