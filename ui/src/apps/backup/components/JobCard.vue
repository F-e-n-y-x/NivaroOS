<!-- One job in the Jobs list (spec §12.3): what it does in plain words,
     how it's doing, what's next, and its direct actions. The title button
     (Enter) opens the detail pane. -->
<template>
	<li class="bk-job" :class="{ 'is-selected': selected, 'is-paused': !job.enabled }">
		<div class="bk-job-head">
			<span class="bk-job-type">
				<b-icon :icon="typeIcon" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t(`backup.type.${job.type}.label`) }}</span>
			</span>
			<button type="button" class="bk-job-name" :aria-expanded="selected ? 'true' : 'false'" @click="$emit('select', job)">{{ job.name }}</button>
			<span v-if="imported" class="bk-pill is-info">
				<b-icon icon="import" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.jobs.imported_badge') }}</span>
			</span>
			<span class="bk-job-spacer"></span>
			<status-pill v-if="active" :tone="statusView.tone" :icon="statusView.icon" :label="activeLabel"></status-pill>
			<status-pill v-else-if="job.health !== 'ok'" :tone="healthView.tone" :icon="healthView.icon" :label="$t('backup.health.' + job.health)"></status-pill>
			<button
				v-if="!active"
				type="button"
				role="switch"
				class="bk-switch"
				:aria-checked="job.enabled ? 'true' : 'false'"
				:aria-label="$t('backup.jobs.enabled_named', { name: job.name })"
				:disabled="busy"
				@click="$emit('toggle', job)"
			>
				<span class="bk-switch-track" aria-hidden="true"><span class="bk-switch-thumb"></span></span>
				<span class="bk-switch-text">{{ job.enabled ? $t('backup.jobs.on') : $t('backup.jobs.off') }}</span>
			</button>
		</div>

		<p class="bk-job-line">{{ describeLine }}</p>
		<p class="bk-job-promise bk-secondary">{{ $t(`backup.type.${job.type}.deleted`) }}</p>

		<run-progress v-if="active && active.status === 'running'" :live="live" :fmt="fmt" :label="$t('backup.run.progress_of', { name: job.name })" compact></run-progress>

		<p class="bk-job-last">
			<template v-if="job.last_run">
				<b-icon :icon="lastView.icon" pack="mdi" custom-size="mdi-16px" :class="'bk-tone-' + lastView.tone" aria-hidden="true"></b-icon>
				<span>{{ lastText }}</span>
			</template>
			<span v-else class="bk-secondary">{{ $t('backup.jobs.never_run') }}</span>
			<template v-if="nextText">
				<span aria-hidden="true">·</span>
				<span class="bk-secondary">{{ nextText }}</span>
			</template>
		</p>

		<div class="bk-job-actions">
			<template v-if="active">
				<button type="button" class="bk-btn is-small" @click="$emit('open-run', job)">{{ $t('backup.jobs.open_progress') }}</button>
				<button v-if="active.status === 'waiting_user'" type="button" class="bk-btn is-small is-primary" @click="$emit('action', 'review', job)">{{ $t('backup.action.review') }}</button>
				<button type="button" class="bk-btn is-small" :disabled="busy" @click="$emit('cancel', job)">{{ $t('backup.run.cancel') }}</button>
			</template>
			<template v-else>
				<button type="button" class="bk-btn is-small" :class="{ 'is-primary': !fixAction }" :disabled="busy" @click="$emit('run', job)">
					<b-icon icon="play" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.jobs.run_now') }}</span>
				</button>
				<button v-if="fixAction" type="button" class="bk-btn is-small is-primary" :disabled="busy" @click="$emit('action', fixAction, job)">{{ $t('backup.action.' + fixAction) }}</button>
			</template>
			<span class="bk-job-spacer"></span>
			<job-menu :job="job" @select="a => $emit('menu', a, job)"></job-menu>
		</div>
	</li>
</template>

<script>
import StatusPill from './StatusPill.vue'
import RunProgress from './RunProgress.vue'
import JobMenu from './JobMenu.vue'
import { STATUS_VIEW, HEALTH_VIEW, TYPE_ICON, attentionFor, retentionMessage, jobTriggers, isActiveStatus } from '../state'
import { renderMessage } from '../messages'

// Fixes handled in place elsewhere on the card (Run now, Review) or that
// only make sense next to their explanation (How to run it).
const INLINE_FIXES_SKIPPED = ['retry', 'review', 'how_to_run', 'view_log']

export default {
	name: 'JobCard',
	components: { StatusPill, RunProgress, JobMenu },
	props: {
		job: { type: Object, required: true },
		fmt: { type: Object, required: true },
		// { cron: CronPreview } from /cron/preview, shared by the app
		cronInfo: { type: Object, default: () => ({}) },
		live: { type: Object, default: null },
		selected: { type: Boolean, default: false },
		busy: { type: Boolean, default: false }
	},
	computed: {
		active() {
			const r = this.job.active_run
			return r && isActiveStatus(r.status) ? r : null
		},
		typeIcon() {
			return TYPE_ICON[this.job.type] || 'content-copy'
		},
		imported() {
			return !!this.job.migrated_from && this.job.needs_attention === 'imported'
		},
		statusView() {
			return STATUS_VIEW[this.active.status] || STATUS_VIEW.queued
		},
		activeLabel() {
			const s = this.$t('backup.status.' + this.active.status)
			const p = this.active.status === 'running' && this.live && this.live.total_bytes > 0 ? Math.floor((this.live.bytes / this.live.total_bytes) * 100) : null
			return p === null ? s : `${s} ${this.fmt.percent(p / 100)}`
		},
		healthView() {
			return HEALTH_VIEW[this.job.health] || HEALTH_VIEW.ok
		},
		lastView() {
			return STATUS_VIEW[this.job.last_run.status] || STATUS_VIEW.skipped
		},
		t() {
			return this.$t.bind(this)
		},
		// "Mirror · Keeps deleted files 30 days · Every Sunday at 03:00 · Drive plugged in"
		describeLine() {
			const parts = []
			const keep = retentionMessage(this.job)
			if (keep) parts.push(renderMessage(this.t, keep, this.fmt))
			const triggers = jobTriggers(this.job)
			for (const tr of triggers) {
				if (tr.kind === 'volume_mounted') parts.push(this.$t('backup.jobs.on_plug_in'))
				else parts.push(this.cronText(tr.cron))
			}
			if (!triggers.length) parts.push(this.$t('backup.jobs.manual_only'))
			return parts.filter(Boolean).join(' · ')
		},
		lastText() {
			const r = this.job.last_run
			const when = r.ended_at ? this.fmt.relative(r.ended_at) : ''
			const summary = renderMessage(this.t, r.summary, this.fmt)
			const status = this.$t('backup.status.' + r.status)
			return summary ? this.$t('backup.jobs.last_run_summary', { status, when, summary }) : this.$t('backup.jobs.last_run', { status, when })
		},
		nextText() {
			if (!this.job.enabled || !this.job.next_run || this.active) return ''
			return this.$t('backup.jobs.next_run', { when: this.fmt.when(this.job.next_run) })
		},
		fixAction() {
			const item = attentionFor(this.job)
			if (!item) return ''
			return item.actions.find(a => !INLINE_FIXES_SKIPPED.includes(a)) || ''
		}
	},
	methods: {
		cronText(cron) {
			const info = this.cronInfo[cron]
			if (!info) return ''
			// Still asking the server: say nothing rather than the raw cron.
			if (info.pending) return ''
			if (!info.valid || !info.human_key) return this.$t('backup.cron.custom', { expr: cron })
			return this.$t(info.human_key, info.args || {})
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-job {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-3) var(--space-4);
	border-top: 1px solid var(--theme-table-divider);
	&:first-child {
		border-top: none;
	}
	&.is-selected {
		background: var(--theme-card-selected);
	}
	p {
		margin: 0;
	}
}
.bk-job-head,
.bk-job-actions,
.bk-job-last {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	min-width: 0;
}
.bk-job-spacer {
	flex: 1 1 0;
}
.bk-job-type {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: 0 var(--space-2);
	border-radius: var(--radius-xs);
	background: var(--theme-pill-bg);
	color: var(--theme-pill-color);
	font-size: var(--font-2xs);
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.02em;
}
.bk-job-name {
	border: none;
	background: transparent;
	padding: var(--space-1) 0;
	font-family: inherit;
	font-size: var(--font-md);
	font-weight: 600;
	color: var(--theme-text-primary);
	text-align: left;
	cursor: pointer;
	overflow-wrap: anywhere;
	min-width: 0;
	&:hover {
		color: var(--color-primary-fg);
		text-decoration: underline;
	}
}
.bk-job-line {
	font-size: var(--font-sm);
	color: var(--theme-text-primary);
}
.bk-job-promise,
.bk-job-last {
	font-size: var(--font-sm);
}
.bk-job-last {
	color: var(--theme-text-primary);
	gap: var(--space-1);
}
.bk-job-actions {
	margin-top: var(--space-1);
}
.is-paused .bk-job-name {
	color: var(--theme-text-secondary);
}

.bk-switch {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	min-height: 2rem;
	padding: 0 var(--space-1);
	border: none;
	background: transparent;
	color: var(--theme-text-secondary);
	font-family: inherit;
	font-size: var(--font-xs);
	cursor: pointer;
	&:disabled {
		opacity: 0.5;
		cursor: default;
	}
}
.bk-switch-track {
	position: relative;
	width: 2.25rem;
	height: 1.25rem;
	border-radius: var(--radius-pill);
	background: var(--theme-input-border);
	transition: background 0.15s ease;
}
.bk-switch-thumb {
	position: absolute;
	top: 0.125rem;
	left: 0.125rem;
	width: 1rem;
	height: 1rem;
	border-radius: 50%;
	background: var(--theme-card-bg);
	transition: transform 0.15s ease;
}
.bk-switch[aria-checked='true'] {
	color: var(--theme-text-primary);
	.bk-switch-track {
		background: var(--color-primary);
	}
	.bk-switch-thumb {
		transform: translateX(1rem);
	}
}
@media (pointer: coarse) {
	.bk-switch {
		min-height: 44px;
	}
}
@media (prefers-reduced-motion: reduce) {
	.bk-switch-track,
	.bk-switch-thumb {
		transition: none;
	}
}
</style>
