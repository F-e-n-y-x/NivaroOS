<!-- What the server's prechecks say about this job before it is saved
     (spec §12.4 Where: POST /validate): the space estimate ("Needs about
     212 GB. 1.8 TB free, OK.") and every check as icon + text. -->
<template>
	<div class="space-estimate" aria-live="polite" :aria-busy="pending ? 'true' : 'false'">
		<p v-if="pending && !result" class="wz-hint se-pending">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-16px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.wizard.where.checking') }}</span>
		</p>
		<p v-else-if="unavailable" class="wz-note tone-info">
			<b-icon icon="information-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.wizard.where.checks_unavailable') }}</span>
		</p>
		<template v-else-if="result">
			<p v-if="estimateText" class="wz-note" :class="'tone-' + estimateTone">
				<b-icon :icon="estimateTone === 'ok' ? 'check-circle-outline' : 'alert-outline'" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ estimateText }}</span>
			</p>
			<ul class="se-checks" :aria-label="$t('backup.wizard.where.checks_label')">
				<li v-for="c in visibleChecks" :key="c.id" :class="'is-' + c.status">
					<b-icon :icon="checkIcon(c.status)" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span class="se-check-text">
						<span class="sr-only">{{ $t('backup.wizard.check_status.' + c.status) }}:</span>
						{{ checkText(c) }}
						<span v-if="c.code && c.status !== 'pass'" class="se-check-why">{{ $t(errKeys(c.code).cause) }}</span>
					</span>
				</li>
			</ul>
		</template>
	</div>
</template>

<script>
import { errorKeys } from '../errorCodes'
import { renderMessage } from '../messages'

export default {
	name: 'SpaceEstimate',
	props: {
		result: { type: Object, default: null },
		pending: { type: Boolean, default: false },
		unavailable: { type: Boolean, default: false },
		fmt: { type: Object, required: true }
	},
	computed: {
		visibleChecks() {
			// Skipped checks don't apply here; say nothing about them.
			return ((this.result && this.result.checks) || []).filter(c => c.status !== 'skip')
		},
		estimate() {
			return (this.result && this.result.estimate) || null
		},
		estimateTone() {
			const e = this.estimate
			if (!e || typeof e.dest_free !== 'number' || !e.need_bytes) return 'info'
			return e.dest_free >= e.need_bytes ? 'ok' : 'warn'
		},
		estimateText() {
			const e = this.estimate
			if (!e || !e.need_bytes) return ''
			const need = this.fmt.bytes(e.need_bytes)
			if (typeof e.dest_free !== 'number') return this.$t('backup.wizard.where.needs_unknown_free', { need })
			const free = this.fmt.bytes(e.dest_free)
			if (e.dest_free >= e.need_bytes) return this.$t(e.partial ? 'backup.wizard.where.needs_ok_partial' : 'backup.wizard.where.needs_ok', { need, free })
			return this.$t('backup.wizard.where.needs_short', { need, free })
		}
	},
	methods: {
		errKeys(code) {
			return errorKeys(code)
		},
		checkIcon(status) {
			return { pass: 'check-circle-outline', warn: 'alert-outline', fail: 'close-circle-outline' }[status] || 'minus-circle-outline'
		},
		checkText(c) {
			return renderMessage(this.$t.bind(this), { key: c.msg_key || `backup.check.${c.id}`, args: c.args }, this.fmt)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.space-estimate {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.se-pending {
	display: flex;
	align-items: center;
	gap: var(--space-1);
}

.se-checks {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	margin: 0;
	padding: 0;
	list-style: none;
	font-size: var(--font-xs);

	li {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		color: var(--theme-text-secondary);
	}
	.is-pass .icon {
		color: var(--color-success-fg);
	}
	.is-warn {
		color: var(--color-warning-fg);
	}
	.is-fail {
		color: var(--color-danger-fg);
		font-weight: 600;
	}
}

.se-check-why {
	display: block;
	font-weight: 400;
	color: var(--theme-text-secondary);
}
</style>
