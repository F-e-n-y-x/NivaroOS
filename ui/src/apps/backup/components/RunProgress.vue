<!-- A run's progress bar with its figures (spec §12.5). role=progressbar
     with a text value; an unknown total is indeterminate (no aria-valuenow). -->
<template>
	<div class="bk-run-progress" :class="{ 'is-compact': compact }">
		<div
			class="bk-progress"
			:class="{ 'is-indeterminate': progress.ratio === null, 'is-large': !compact }"
			role="progressbar"
			:aria-label="label"
			aria-valuemin="0"
			aria-valuemax="100"
			:aria-valuenow="progress.percent === null ? null : progress.percent"
			:aria-valuetext="valueText"
		>
			<span :style="progress.ratio === null ? null : { width: progress.percent + '%' }"></span>
		</div>
		<p class="bk-run-figures">
			<strong v-if="progress.percent !== null">{{ fmt.percent(progress.ratio) }}</strong>
			<span v-if="countsText">{{ countsText }}</span>
		</p>
		<p v-if="!compact && rateText" class="bk-run-figures bk-secondary">{{ rateText }}</p>
	</div>
</template>

<script>
import { runProgress } from '../state'

export default {
	name: 'RunProgress',
	props: {
		live: { type: Object, default: null },
		fmt: { type: Object, required: true },
		label: { type: String, required: true },
		phaseKey: { type: String, default: '' },
		compact: { type: Boolean, default: false }
	},
	computed: {
		progress() {
			return runProgress(this.live)
		},
		countsText() {
			const l = this.live
			if (!l) return this.phaseKey ? this.$t(this.phaseKey) : ''
			const parts = []
			if (this.phaseKey) parts.push(this.$t(this.phaseKey))
			if (l.total_files > 0) parts.push(this.$t('backup.run.files_of', { done: this.fmt.number(l.files), total: this.fmt.number(l.total_files) }))
			if (l.total_bytes > 0) parts.push(this.$t('backup.run.bytes_of', { done: this.fmt.bytes(l.bytes), total: this.fmt.bytes(l.total_bytes) }))
			else if (l.bytes > 0) parts.push(this.fmt.bytes(l.bytes))
			return parts.join(' · ')
		},
		rateText() {
			const l = this.live
			if (!l) return ''
			const parts = []
			if (l.speed_bps > 0) parts.push(this.fmt.speed(l.speed_bps))
			if (l.eta_sec > 0) parts.push(this.$t('backup.run.eta', { time: this.fmt.duration(l.eta_sec) }))
			return parts.join(' · ')
		},
		valueText() {
			if (this.progress.percent === null) return this.$t('backup.run.progress_unknown')
			return [this.fmt.percent(this.progress.ratio), this.countsText].filter(Boolean).join(', ')
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-run-progress {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
	p {
		margin: 0;
	}
}
.bk-run-figures {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	font-size: var(--font-sm);
	color: var(--theme-text-primary);
	font-variant-numeric: tabular-nums;
}
.is-compact .bk-run-figures {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
</style>
