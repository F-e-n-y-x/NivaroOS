<!-- Runs grouped by day (spec §12.8), newest first. Each run opens its
     run window (read-only once final). Skipped runs are grey, with their
     reason. -->
<template>
	<div class="bk-activity">
		<section v-for="group in groups" :key="group.day" class="bk-activity-day" :aria-label="fmt.longDate(group.at)">
			<h4 class="bk-activity-date">{{ fmt.longDate(group.at) }}</h4>
			<ul class="bk-activity-list">
				<li v-for="run in group.runs" :key="run.id" :class="{ 'is-quiet': run.status === 'skipped' || run.status === 'cancelled' }">
					<button type="button" class="bk-activity-row" @click="$emit('open', run)">
						<span class="bk-activity-time">{{ fmt.time(run.ended_at || run.started_at || run.queued_at) }}</span>
						<span class="bk-activity-main">
							<span class="bk-activity-title">
								<status-pill :tone="view(run).tone" :icon="view(run).icon" :label="$t('backup.status.' + run.status)"></status-pill>
								<span v-if="showJob" class="bk-activity-job">{{ run.job_name }}</span>
								<span v-if="run.kind !== 'backup'" class="bk-activity-kind">{{ $t('backup.kind.' + run.kind) }}</span>
							</span>
							<span v-if="summary(run)" class="bk-activity-summary">{{ summary(run) }}</span>
							<span class="bk-activity-meta">{{ $t('backup.trigger.' + run.trigger) }}<template v-if="duration(run)"> · {{ duration(run) }}</template></span>
						</span>
						<b-icon icon="chevron-right" pack="mdi" custom-size="mdi-18px" class="bk-activity-chevron" aria-hidden="true"></b-icon>
					</button>
				</li>
			</ul>
		</section>
	</div>
</template>

<script>
import StatusPill from './StatusPill.vue'
import { STATUS_VIEW, groupRunsByDay } from '../state'
import { renderMessage } from '../messages'

export default {
	name: 'ActivityList',
	components: { StatusPill },
	props: {
		runs: { type: Array, required: true },
		fmt: { type: Object, required: true },
		showJob: { type: Boolean, default: true }
	},
	computed: {
		groups() {
			return groupRunsByDay(this.runs, this.fmt)
		}
	},
	methods: {
		view(run) {
			return STATUS_VIEW[run.status] || STATUS_VIEW.skipped
		},
		summary(run) {
			return renderMessage(this.$t.bind(this), run.summary, this.fmt)
		},
		duration(run) {
			if (!run.started_at || !run.ended_at) return ''
			const sec = (Date.parse(run.ended_at) - Date.parse(run.started_at)) / 1000
			return sec > 0 ? this.fmt.duration(sec) : ''
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-activity {
	display: flex;
	flex-direction: column;
	gap: var(--space-4);
}
.bk-activity-date {
	margin: 0 0 var(--space-2);
	font-size: var(--font-xs);
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	color: var(--theme-text-muted);
}
.bk-activity-list {
	list-style: none;
	margin: 0;
	padding: 0;
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	overflow: hidden;
	li + li {
		border-top: 1px solid var(--theme-table-divider);
	}
}
.bk-activity-row {
	display: flex;
	align-items: flex-start;
	gap: var(--space-3);
	width: 100%;
	min-height: 44px;
	padding: var(--space-3);
	border: none;
	background: transparent;
	color: var(--theme-text-primary);
	font-family: inherit;
	font-size: var(--font-sm);
	text-align: left;
	cursor: pointer;
	&:hover {
		background: var(--theme-card-hover);
	}
	&:focus-visible {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: -2px;
	}
}
.bk-activity-time {
	flex-shrink: 0;
	width: 3.25rem;
	color: var(--theme-text-secondary);
	font-variant-numeric: tabular-nums;
}
.bk-activity-main {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	gap: 0.125rem;
	min-width: 0;
}
.bk-activity-title {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
}
.bk-activity-job {
	font-weight: 600;
	overflow-wrap: anywhere;
}
.bk-activity-kind {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
.bk-activity-summary {
	overflow-wrap: anywhere;
}
.bk-activity-meta {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
.bk-activity-chevron {
	flex-shrink: 0;
	color: var(--theme-text-muted);
	align-self: center;
}
.is-quiet .bk-activity-row {
	color: var(--theme-text-secondary);
}
</style>
