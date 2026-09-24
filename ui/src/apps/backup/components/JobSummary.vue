<!-- A job in plain sentences (spec §12.4 Review), from summaries.js - the
     same keys a job card uses. Optional edit links jump to the step that
     sets each part. -->
<template>
	<ul class="job-summary">
		<li v-for="(line, i) in lines" :key="i">
			<b-icon :icon="line.icon" custom-size="mdi-16px" class="js-icon" aria-hidden="true"></b-icon>
			<span class="js-text">{{ line.text }}</span>
			<button v-if="editable && line.step" type="button" class="wz-link js-edit" :aria-label="$t('backup.wizard.review.edit_step', { step: $t('backup.wizard.step.' + line.step) })"
				@click="$emit('edit', line.step)">{{ $t('Edit') }}</button>
		</li>
	</ul>
</template>

<script>
import { jobSummary } from '../summaries'
import { describeCronText } from '@/shared/scheduling/cronText'
import { formatClock } from '@/shared/scheduling/timeFormat'

// Which step sets each sentence, and its icon.
const LINE_META = {
	'backup.summary.mirror': ['what', 'mirror'],
	'backup.summary.copy': ['what', 'content-copy'],
	'backup.summary.archive': ['what', 'archive-outline'],
	'backup.summary.when_schedule': ['when', 'calendar-clock'],
	'backup.summary.when_plug': ['when', 'usb-flash-drive-outline'],
	'backup.summary.when_plug_gap': ['when', 'usb-flash-drive-outline'],
	'backup.summary.when_manual': ['when', 'hand-back-right-outline'],
	'backup.summary.catch_up': ['when', 'restore'],
	'backup.summary.window': ['when', 'clock-time-four-outline'],
	'backup.summary.unmet_skip': ['when', 'debug-step-over'],
	'backup.summary.unmet_wait': ['when', 'timer-sand'],
	'backup.summary.unmet_fail': ['when', 'alert-circle-outline'],
	'backup.keep.recycle_days': ['keep', 'delete-restore'],
	'backup.keep.recycle_forever': ['keep', 'delete-restore'],
	'backup.keep.last_archives': ['keep', 'archive-clock-outline'],
	'backup.keep.copy_never_deletes': ['keep', 'shield-check-outline'],
	'backup.summary.guards': ['keep', 'shield-alert-outline'],
	'backup.summary.guard_change': ['keep', 'shield-alert-outline'],
	'backup.summary.preview_first': ['keep', 'eye-outline'],
	'backup.summary.verify': ['keep', 'check-decagram-outline'],
	'backup.summary.stop_apps': ['what', 'stop-circle-outline'],
	'backup.summary.stop_apps_each': ['what', 'stop-circle-outline'],
	'backup.summary.shutdown_vm': ['what', 'power'],
	'backup.summary.skips': ['what', 'filter-remove-outline'],
	'backup.summary.retry': ['when', 'refresh'],
	'backup.summary.no_retry': ['when', 'refresh']
}

export default {
	name: 'JobSummary',
	props: {
		job: { type: Object, required: true },
		fmt: { type: Object, required: true },
		editable: { type: Boolean, default: false }
	},
	computed: {
		hour12() {
			const f = this.$store && this.$store.state && this.$store.state.timeFormat
			return typeof f === 'string' ? f.startsWith('h') : undefined
		},
		lines() {
			return jobSummary(this.job).map(m => {
				const [step, icon] = LINE_META[m.key] || ['', 'information-outline']
				return { step, icon, text: this.render(m) }
			})
		}
	},
	methods: {
		render(m) {
			const opts = { locale: this.$i18n && this.$i18n.locale, hour12: this.hour12 }
			const args = {}
			for (const [k, v] of Object.entries(m.args || {})) {
				if (k === 'schedule_cron') {
					// Lower-case the first letter: it sits inside a sentence.
					const s = describeCronText(this.$t.bind(this), v, opts) || v
					args.schedule = s.charAt(0).toLocaleLowerCase() + s.slice(1)
				} else if (v && typeof v === 'object' && !Array.isArray(v) && v.key) {
					args[k] = this.render(v)
				} else if (Array.isArray(v)) {
					args[k] = this.fmt.list(v)
				} else if ((k === 'start' || k === 'end') && /^\d{1,2}:\d{2}$/.test(v)) {
					args[k] = formatClock(v, opts.locale, opts.hour12)
				} else if (typeof v === 'number') {
					args[k] = this.fmt.number(v)
				} else {
					args[k] = v
				}
			}
			return this.$t(m.key, args)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.job-summary {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	margin: 0;
	padding: 0;
	list-style: none;

	li {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		font-size: var(--font-sm);
		line-height: 1.45;
		color: var(--theme-text-primary);
	}
}

.js-icon {
	flex-shrink: 0;
	margin-top: 0.1rem;
	color: var(--color-primary-fg);
}

.js-text {
	flex: 1 1 auto;
	min-width: 0;
}

.js-edit {
	flex-shrink: 0;
	min-height: 1.75rem;
	font-size: var(--font-xs);
}
</style>
