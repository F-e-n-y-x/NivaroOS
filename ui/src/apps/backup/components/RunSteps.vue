<!-- The run's steps (spec §12.5): what's done, what's happening and
     what's next. Each state is an icon plus a spoken label. -->
<template>
	<ol class="bk-steps">
		<li v-for="(s, i) in steps" :key="i" class="bk-step" :class="'is-' + s.state">
			<b-icon :icon="icons[s.state] || icons.pending" pack="mdi" custom-size="mdi-18px" :class="'bk-tone-' + (tones[s.state] || 'muted')" aria-hidden="true"></b-icon>
			<span class="bk-sr-only">{{ $t('backup.run.step_state.' + (icons[s.state] ? s.state : 'pending')) }}:</span>
			<span class="bk-step-text">{{ msg({ key: s.key, args: s.args }) }}</span>
		</li>
	</ol>
</template>

<script>
import { backupMixin } from '../backupMixin'

export default {
	name: 'RunSteps',
	mixins: [backupMixin],
	props: {
		steps: { type: Array, required: true }
	},
	data() {
		return {
			icons: { done: 'check-circle', active: 'play-circle', pending: 'circle-outline', failed: 'close-circle', skipped: 'minus-circle-outline' },
			tones: { done: 'ok', active: 'info', pending: 'muted', failed: 'danger', skipped: 'muted' }
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-steps {
	list-style: none;
	margin: 0;
	padding: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.bk-step {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	font-size: var(--font-sm);
	color: var(--theme-text-primary);
	.icon {
		flex-shrink: 0;
	}
	&.is-active {
		font-weight: 600;
	}
	&.is-pending,
	&.is-skipped {
		color: var(--theme-text-secondary);
	}
	&.is-skipped .bk-step-text {
		text-decoration: line-through;
	}
}
</style>
