<!-- Overview header (spec §12.2): All good / Needs attention / Problem /
     No jobs yet, as icon + text + colour, with the job count, the last
     success and the New job button. -->
<template>
	<header class="bk-status-header" :class="'is-' + view.tone">
		<b-icon :icon="view.icon" pack="mdi" custom-size="mdi-28px" class="bk-status-icon" aria-hidden="true"></b-icon>
		<div class="bk-status-text">
			<h2 class="bk-status-title">{{ $t('backup.overview.state.' + overall.state) }}</h2>
			<p v-if="overall.state !== 'empty'" class="bk-status-sub">
				<span>{{ $tc('backup.overview.jobs_count', overall.jobs, { count: fmt.number(overall.jobs) }) }}</span>
				<template v-if="overall.attention">
					<span aria-hidden="true"> · </span>
					<span>{{ $tc('backup.overview.attention_count', overall.attention, { count: fmt.number(overall.attention) }) }}</span>
				</template>
				<span aria-hidden="true"> · </span>
				<span v-if="overall.lastSuccess">{{ $t('backup.overview.last_success', { when: fmt.when(overall.lastSuccess) }) }}</span>
				<span v-else>{{ $t('backup.overview.no_success_yet') }}</span>
			</p>
		</div>
		<button type="button" class="bk-btn is-primary" @click="$emit('new-job')">
			<b-icon icon="plus" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.jobs.new') }}</span>
		</button>
	</header>
</template>

<script>
const VIEWS = {
	ok: { tone: 'ok', icon: 'check-circle-outline' },
	attention: { tone: 'warn', icon: 'alert-outline' },
	problem: { tone: 'danger', icon: 'alert-octagon-outline' },
	empty: { tone: 'muted', icon: 'information-outline' }
}

export default {
	name: 'StatusHeader',
	props: {
		// overallState() from state.js
		overall: { type: Object, required: true },
		fmt: { type: Object, required: true }
	},
	computed: {
		view() {
			return VIEWS[this.overall.state] || VIEWS.empty
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-status-header {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-3);
	padding: var(--space-4);
	border-radius: var(--radius-card);
	border: 1px solid var(--theme-card-border);
	background: var(--theme-card-bg);
	@each $tone in ok, warn, danger, muted {
		&.is-#{$tone} .bk-status-icon {
			color: var(--status-#{$tone}-fg);
		}
	}
}
.bk-status-text {
	flex: 1 1 14rem;
	min-width: 0;
}
.bk-status-title {
	margin: 0;
	font-size: var(--font-xl);
	font-weight: 600;
	color: var(--theme-text-primary);
}
.bk-status-sub {
	margin: 0;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}
</style>
