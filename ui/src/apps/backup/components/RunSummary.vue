<!-- A finished run in numbers (spec §12.5): added, changed, moved to
     recycle, skipped, errors (listed), duration and data. -->
<template>
	<section class="bk-run-summary" :class="'is-' + view.tone" :aria-labelledby="headingId">
		<h3 :id="headingId" class="bk-run-summary-title">
			<b-icon :icon="view.icon" pack="mdi" custom-size="mdi-20px" :class="'bk-tone-' + view.tone" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.status.' + run.status) }}</span>
		</h3>
		<p v-if="summaryText" class="bk-run-summary-text">{{ summaryText }}</p>
		<p v-if="run.error_code && run.status !== 'success'" class="bk-run-summary-text bk-secondary">{{ explain(run.error_code).cause }}</p>
		<dl class="bk-run-counts">
			<div v-for="c in counts" :key="c.key">
				<dt>{{ $t('backup.run.count.' + c.key) }}</dt>
				<dd>{{ c.value }}</dd>
			</div>
		</dl>
		<details v-if="run.file_errors && run.file_errors.length" class="bk-run-errors">
			<summary>{{ $tc('backup.run.file_errors', run.file_errors.length, { count: fmt.number(run.file_errors.length) }) }}</summary>
			<ul>
				<li v-for="(e, i) in run.file_errors" :key="i">
					<span class="bk-run-error-path">{{ e.path }}</span>
					<span class="bk-secondary">{{ explain(e.code).title }}</span>
				</li>
			</ul>
		</details>
	</section>
</template>

<script>
import { backupMixin } from '../backupMixin'

let seq = 0

export default {
	name: 'RunSummary',
	mixins: [backupMixin],
	props: {
		run: { type: Object, required: true }
	},
	data() {
		return { headingId: `bk-run-summary-${++seq}` }
	},
	computed: {
		view() {
			return this.statusView(this.run.status)
		},
		summaryText() {
			return this.msg(this.run.summary)
		},
		counts() {
			const c = this.run.counts || {}
			const n = v => this.fmt.number(v || 0)
			const list = [
				{ key: 'added', value: n(c.added) },
				{ key: 'changed', value: n(c.changed) }
			]
			if (c.deleted) list.push({ key: this.run.kind === 'backup' ? 'recycled' : 'deleted', value: n(c.deleted) })
			if (c.skipped) list.push({ key: 'skipped', value: n(c.skipped) })
			if (c.errored) list.push({ key: 'errored', value: n(c.errored) })
			list.push({ key: 'data', value: this.fmt.bytes(c.bytes_transferred || 0) })
			if (this.run.started_at && this.run.ended_at) {
				const sec = (Date.parse(this.run.ended_at) - Date.parse(this.run.started_at)) / 1000
				if (sec >= 0) list.push({ key: 'duration', value: this.fmt.duration(sec) })
			}
			return list
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-run-summary {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	border-radius: var(--radius-card);
	border: 1px solid var(--theme-card-border);
	background: var(--theme-card-bg);
	@each $tone in ok, warn, danger, info, muted {
		&.is-#{$tone} {
			border-left: 4px solid var(--status-#{$tone}-fg);
		}
	}
	p {
		margin: 0;
	}
}
.bk-run-summary-title {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin: 0;
	font-size: var(--font-md);
	font-weight: 600;
}
.bk-run-summary-text {
	font-size: var(--font-sm);
	overflow-wrap: anywhere;
}
.bk-run-counts {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
	gap: var(--space-2);
	margin: 0;
	dt {
		font-size: var(--font-xs);
		color: var(--theme-text-secondary);
	}
	dd {
		margin: 0;
		font-size: var(--font-md);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
}
.bk-run-errors {
	summary {
		cursor: pointer;
		min-height: 2rem;
		display: flex;
		align-items: center;
		color: var(--color-primary-fg);
		font-size: var(--font-sm);
	}
	ul {
		margin: var(--space-1) 0 0;
		padding-left: var(--space-4);
		max-height: 8rem;
		overflow: auto;
		font-size: var(--font-sm);
	}
	li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
}
.bk-run-error-path {
	overflow-wrap: anywhere;
}
</style>
