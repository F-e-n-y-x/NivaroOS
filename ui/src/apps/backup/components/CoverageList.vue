<!-- 3-2-1 coverage (Overview): per protected folder, its copies, the
     drives/locations they're on and whether one is off-site, with a gap's
     one-click fix (a new job for that folder, on the "Where" step). -->
<template>
	<section v-if="rows.length" class="bk-card" :aria-labelledby="headingId">
		<h3 :id="headingId" class="bk-card-title">
			<b-icon icon="shield-check-outline" pack="mdi" custom-size="mdi-20px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.coverage.title') }}</span>
		</h3>
		<p class="bk-secondary bk-coverage-intro">{{ $t('backup.coverage.intro') }}</p>
		<ul class="bk-coverage">
			<li v-for="row in rows" :key="row.key" class="bk-coverage-row">
				<div class="bk-coverage-text">
					<span class="bk-coverage-name">{{ row.label }}</span>
					<span class="bk-secondary">{{ factsText(row) }}</span>
					<span v-if="row.pending" class="bk-muted bk-coverage-pending">{{ $t('backup.coverage.pending', { count: row.pending }) }}</span>
				</div>
				<status-pill v-if="!row.gaps.length" tone="ok" icon="check-circle-outline" :label="$t('backup.coverage.met')"></status-pill>
				<template v-else>
					<status-pill tone="warn" icon="alert-outline" :label="gapText(row)"></status-pill>
					<button type="button" class="bk-btn is-small" :aria-label="$t('backup.coverage.add_job_for', { name: row.label })" @click="app.openWizard({ sourceEndpoint: row.source, startStep: 'where' })">
						<b-icon icon="plus" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
						<span>{{ $t('backup.coverage.add_job') }}</span>
					</button>
				</template>
			</li>
		</ul>
	</section>
</template>

<script>
import StatusPill from './StatusPill.vue'
import { coverage, needsLastSuccess } from '../coverage'

let seq = 0

export default {
	name: 'CoverageList',
	components: { StatusPill },
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true }
	},
	data() {
		return { headingId: `bk-coverage-${++seq}`, lastSuccess: {}, asked: {} }
	},
	computed: {
		rows() {
			return coverage(this.jobs, this.lastSuccess)
		}
	},
	watch: {
		jobs: { immediate: true, handler: 'fetchLastSuccess' }
	},
	methods: {
		// A job whose latest run failed may still hold an older copy: only
		// its detail (stats.last_success) says, once per failed run.
		fetchLastSuccess() {
			for (const job of needsLastSuccess(this.jobs)) {
				const k = job.id + ':' + job.last_run.id
				if (this.asked[job.id] === k) continue
				this.asked[job.id] = k
				this.app.bkApi.getJob(job.id).then(d => {
					this.lastSuccess = { ...this.lastSuccess, [job.id]: (d && d.stats && d.stats.last_success) || '' }
				}).catch(() => { delete this.asked[job.id] })
			}
		},
		factsText(row) {
			const copies = this.$t('backup.coverage.copies', { count: row.copies })
			const media = this.$t('backup.coverage.media', { count: row.media })
			return `${copies} ${media}`
		},
		gapText(row) {
			return row.gaps.map(g => (g === 'copies' ? this.$t('backup.coverage.gap.copies', { count: 3 - row.copies }) : this.$t('backup.coverage.gap.' + g))).join(' · ')
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-coverage-intro {
	margin: 0 0 var(--space-2);
	font-size: var(--font-xs);
}
.bk-coverage {
	list-style: none;
	margin: 0;
	padding: 0;
}
.bk-coverage-row {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2) var(--space-3);
	padding: var(--space-2) 0;
	border-top: 1px solid var(--theme-table-divider);
	&:first-child {
		border-top: 0;
	}
}
.bk-coverage-text {
	display: flex;
	flex-direction: column;
	flex: 1 1 12rem;
	min-width: 0;
	font-size: var(--font-sm);
}
.bk-coverage-name {
	font-weight: 600;
	overflow-wrap: anywhere;
}
.bk-coverage-pending {
	font-size: var(--font-xs);
}
</style>
