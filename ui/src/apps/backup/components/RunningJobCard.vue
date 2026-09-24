<!-- "Running now" row (spec §12.2): the job, its live progress and Open. -->
<template>
	<li class="bk-running">
		<div class="bk-running-head">
			<span class="bk-running-name">{{ job.name }}</span>
			<status-pill :tone="view.tone" :icon="view.icon" :label="$t('backup.status.' + run.status)"></status-pill>
		</div>
		<run-progress v-if="run.status === 'running'" :live="live" :fmt="fmt" :label="$t('backup.run.progress_of', { name: job.name })" compact></run-progress>
		<p v-else-if="run.status === 'waiting_user'" class="bk-secondary bk-running-note">{{ $t('backup.attention.waiting.cause') }}</p>
		<p v-else class="bk-secondary bk-running-note">{{ $t('backup.run.queued_note') }}</p>
		<div class="bk-running-actions">
			<button v-if="run.status === 'waiting_user'" type="button" class="bk-btn is-small is-primary" @click="$emit('review', job)">{{ $t('backup.action.review') }}</button>
			<button type="button" class="bk-btn is-small" :aria-label="$t('backup.run.open_named', { name: job.name })" @click="$emit('open', job)">{{ $t('backup.run.open') }}</button>
		</div>
	</li>
</template>

<script>
import StatusPill from './StatusPill.vue'
import RunProgress from './RunProgress.vue'
import { STATUS_VIEW } from '../state'

export default {
	name: 'RunningJobCard',
	components: { StatusPill, RunProgress },
	props: {
		job: { type: Object, required: true },
		live: { type: Object, default: null },
		fmt: { type: Object, required: true }
	},
	computed: {
		run() {
			return this.job.active_run
		},
		view() {
			return STATUS_VIEW[this.run.status] || STATUS_VIEW.queued
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-running {
	display: grid;
	grid-template-columns: minmax(0, 1fr) auto;
	grid-template-areas: 'head actions' 'body actions';
	gap: var(--space-1) var(--space-3);
	align-items: center;
	padding: var(--space-3) 0;
	border-top: 1px solid var(--theme-table-divider);
	&:first-child {
		border-top: none;
		padding-top: 0;
	}
	> .bk-run-progress,
	> .bk-running-note {
		grid-area: body;
	}
}
.bk-running-head {
	grid-area: head;
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	min-width: 0;
}
.bk-running-name {
	font-weight: 600;
	color: var(--theme-text-primary);
	overflow-wrap: anywhere;
}
.bk-running-note {
	margin: 0;
	font-size: var(--font-sm);
}
.bk-running-actions {
	grid-area: actions;
	display: flex;
	gap: var(--space-2);
}
// BackupApp sets bk-w-phone on its root below 480 px of window width.
.bk-w-phone .bk-running {
	grid-template-columns: minmax(0, 1fr);
	grid-template-areas: 'head' 'body' 'actions';
}
</style>
