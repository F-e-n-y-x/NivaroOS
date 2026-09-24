<!-- src/shared/scheduling/ScheduleBuilder.vue -->
<!-- Shared schedule editor (spec §12.4 "When", §12.10): every hour, every N
     hours, every day, some weekdays, every week, every month, or Custom
     (a cron field). v-model is the cron expression, byte-for-byte: a
     schedule the builder can't show exactly opens as Custom and is never
     rewritten, and switching patterns keeps the time of day.
     The sentence and the next runs come from `previewFn` (the server's
     /cron/preview, in server time with the zone named) when given, and
     from this device's clock otherwise (Scheduled Tasks without Backup
     installed). Emits `validity` (true/false) so the parent can hold Next. -->
<template>
	<fieldset class="schedule-builder" :disabled="disabled">
		<legend class="sb-legend" :class="{ 'sr-only': !showLegend }">{{ legend || $t('schedule.builder.legend') }}</legend>

		<div class="sb-controls">
			<div class="sb-field">
				<label :for="uid + '-kind'" class="sb-label">{{ $t('schedule.builder.repeat') }}</label>
				<select :id="uid + '-kind'" ref="kind" class="sb-select" :value="pattern.kind" @change="setKind($event.target.value)">
					<option v-for="k in kinds" :key="k" :value="k">{{ $t('schedule.builder.kind.' + k) }}</option>
				</select>
			</div>

			<div v-if="pattern.kind === 'every_n_minutes'" class="sb-field">
				<label :for="uid + '-n'" class="sb-label">{{ $t('schedule.builder.interval') }}</label>
				<select :id="uid + '-n'" class="sb-select" :value="pattern.n" @change="update({ n: +$event.target.value })">
					<option v-for="n in minuteIntervals" :key="n" :value="n">{{ $t('schedule.builder.n_minutes', { n }) }}</option>
				</select>
			</div>

			<div v-if="pattern.kind === 'every_n_hours'" class="sb-field">
				<label :for="uid + '-n'" class="sb-label">{{ $t('schedule.builder.interval') }}</label>
				<select :id="uid + '-n'" class="sb-select" :value="pattern.n" @change="update({ n: +$event.target.value })">
					<option v-for="n in hourIntervals" :key="n" :value="n">{{ $t('schedule.builder.n_hours', { n }) }}</option>
				</select>
			</div>

			<div v-if="pattern.kind === 'hourly' || pattern.kind === 'every_n_hours'" class="sb-field">
				<label :for="uid + '-minute'" class="sb-label">{{ $t('schedule.builder.at_minute') }}</label>
				<select :id="uid + '-minute'" class="sb-select sb-narrow" :value="pattern.minute" @change="update({ minute: +$event.target.value })">
					<option v-for="m in minuteChoices" :key="m" :value="m">{{ String(m).padStart(2, '0') }}</option>
				</select>
			</div>

			<div v-if="pattern.kind === 'weekly'" class="sb-field">
				<label :for="uid + '-day'" class="sb-label">{{ $t('schedule.builder.on_day') }}</label>
				<select :id="uid + '-day'" class="sb-select" :value="pattern.day" @change="update({ day: +$event.target.value })">
					<option v-for="d in weekOrder" :key="d" :value="d">{{ dayName(d, 'long') }}</option>
				</select>
			</div>

			<div v-if="pattern.kind === 'monthly'" class="sb-field">
				<label :for="uid + '-dom'" class="sb-label">{{ $t('schedule.builder.on_date') }}</label>
				<select :id="uid + '-dom'" class="sb-select sb-narrow" :value="pattern.dom" @change="update({ dom: +$event.target.value })">
					<option v-for="d in 31" :key="d" :value="d">{{ d }}</option>
				</select>
			</div>

			<div v-if="hasTime" class="sb-field">
				<label :for="uid + '-time'" class="sb-label">{{ $t('schedule.builder.at_time') }}</label>
				<input :id="uid + '-time'" class="sb-input sb-time" type="time" step="60" required :value="timeValue" @change="setTime($event.target.value)" />
			</div>
		</div>

		<div v-if="pattern.kind === 'weekdays'" class="sb-days" role="group" :aria-label="$t('schedule.builder.on_days')">
			<span class="sb-label" aria-hidden="true">{{ $t('schedule.builder.on_days') }}</span>
			<button v-for="d in weekOrder" :key="d" type="button" class="sb-chip" :aria-pressed="pattern.days.includes(d) ? 'true' : 'false'"
				:aria-label="dayName(d, 'long')" :title="dayName(d, 'long')" @click="toggleDay(d)">
				<b-icon v-if="pattern.days.includes(d)" icon="check" custom-size="mdi-14px" aria-hidden="true"></b-icon>
				<span aria-hidden="true">{{ dayName(d, 'short') }}</span>
			</button>
			<span class="sb-day-shortcuts">
				<button type="button" class="sb-link" @click="update({ days: [1, 2, 3, 4, 5] })">{{ $t('schedule.builder.weekdays_only') }}</button>
				<button type="button" class="sb-link" @click="update({ days: [0, 6] })">{{ $t('schedule.builder.weekends_only') }}</button>
			</span>
		</div>
		<p v-if="pattern.kind === 'weekdays' && pattern.days.length < 2" class="sb-hint">{{ $t('schedule.builder.pick_two_days') }}</p>
		<p v-if="pattern.kind === 'monthly' && pattern.dom > 28" class="sb-hint">{{ $t('schedule.builder.short_months', { dom: pattern.dom }) }}</p>

		<div v-if="pattern.kind === 'custom'" class="sb-custom">
			<label :for="uid + '-cron'" class="sb-label">{{ $t('schedule.builder.cron_label') }}</label>
			<input :id="uid + '-cron'" ref="cron" class="sb-input sb-cron" type="text" spellcheck="false" autocomplete="off" autocapitalize="off"
				:value="pattern.expr" :aria-invalid="invalid ? 'true' : 'false'" :aria-describedby="uid + '-cron-hint ' + uid + '-status'"
				placeholder="0 3 * * *" @input="setExpr($event.target.value)" />
			<p :id="uid + '-cron-hint'" class="sb-hint">{{ $t('schedule.builder.cron_hint') }}</p>
		</div>

		<div :id="uid + '-status'" class="sb-status" aria-live="polite">
			<p v-if="invalid" class="sb-error" role="alert">
				<b-icon icon="alert-circle-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ errorText }}</span>
			</p>
			<template v-else>
				<p class="sb-summary">
					<b-icon icon="calendar-clock" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ sentence }}</span>
					<span v-if="zoneText" class="sb-zone">· {{ zoneText }}</span>
				</p>
				<p v-if="nextTimes.length" class="sb-next">
					<span class="sb-next-label">{{ $t('schedule.builder.next') }}</span>
					<span v-for="(n, i) in nextTimes" :key="i" class="sb-next-item">{{ n.text }}<span v-if="n.local" class="sb-local"> ({{ $t('schedule.builder.your_time', { time: n.local }) }})</span></span>
				</p>
			</template>
		</div>
	</fieldset>
</template>

<script>
import { parsePattern, buildCron, defaultPattern, validateCron, nextRuns, PATTERN_KINDS, HOUR_INTERVALS, MINUTE_INTERVALS, formatHHMM, CRON_FIELDS } from './cronPatterns'
import { cronSentence, describeCronText } from './cronText'
import { weekdayName, formatRunTime, browserTimeZone, zoneOffsetMinutes, formatUtcOffset, parseUtcOffset, safeTimeZone } from './timeFormat'

let seq = 0
const PREVIEW_DELAY_MS = 350

export default {
	name: 'ScheduleBuilder',
	props: {
		// The cron expression (v-model).
		value: { type: String, default: '' },
		// async (cron) => CronPreview ({ valid, human_key, args, next[],
		// timezone, utc_offset, error }), or null for device-time estimates.
		previewFn: { type: Function, default: null },
		// Offer "every N minutes" (Scheduled Tasks). Backup starts hourly.
		allowMinutes: { type: Boolean, default: false },
		legend: { type: String, default: '' },
		showLegend: { type: Boolean, default: false },
		disabled: { type: Boolean, default: false },
		// How many upcoming runs to list.
		nextCount: { type: Number, default: 3 }
	},
	data() {
		return {
			uid: `sb-${++seq}`,
			pattern: this.patternFor(this.value),
			lastEmitted: this.value,
			preview: null,
			previewFailed: false,
			previewSeq: 0,
			now: new Date()
		}
	},
	computed: {
		kinds() {
			return PATTERN_KINDS.filter(k => k !== 'every_n_minutes' || this.allowMinutes || this.pattern.kind === 'every_n_minutes')
		},
		hourIntervals() {
			return HOUR_INTERVALS
		},
		minuteIntervals() {
			return MINUTE_INTERVALS
		},
		minuteChoices() {
			const base = Array.from({ length: 12 }, (_, i) => i * 5)
			const m = this.pattern.minute
			return Number.isInteger(m) && !base.includes(m) ? [...base, m].sort((a, b) => a - b) : base
		},
		// Monday first; values stay cron's (0 = Sunday).
		weekOrder() {
			return [1, 2, 3, 4, 5, 6, 0]
		},
		hasTime() {
			return ['daily', 'weekdays', 'weekly', 'monthly'].includes(this.pattern.kind)
		},
		timeValue() {
			return formatHHMM(this.pattern.hour || 0, this.pattern.minute || 0)
		},
		cron() {
			return this.pattern.kind === 'custom' ? this.pattern.expr : buildCron(this.pattern)
		},
		localError() {
			if (this.pattern.kind === 'weekdays' && this.pattern.days.length < 1) return { key: 'schedule.builder.pick_a_day', args: {} }
			return validateCron(this.cron)
		},
		serverPreview() {
			const p = this.preview
			return p && p.cron === this.cron ? p.result : null
		},
		invalid() {
			if (this.localError) return true
			return !!(this.serverPreview && this.serverPreview.valid === false)
		},
		errorText() {
			if (this.localError) {
				const args = { ...this.localError.args }
				if (args.field) {
					const f = CRON_FIELDS.find(x => x.name === args.field)
					if (f) args.field = this.$t('schedule.cron_field.' + f.name)
				}
				return this.$t(this.localError.key, args)
			}
			const p = this.serverPreview
			return p && p.error ? this.$t('schedule.builder.server_rejected', { error: p.error }) : this.$t('backup.field.invalid_cron')
		},
		locale() {
			return this.$i18n && this.$i18n.locale
		},
		hour12() {
			const f = this.$store && this.$store.state && this.$store.state.timeFormat
			return typeof f === 'string' ? f.startsWith('h') : undefined
		},
		sentence() {
			const opts = { locale: this.locale, hour12: this.hour12 }
			const p = this.serverPreview
			if (p && p.valid && p.human_key) return cronSentence(this.$t.bind(this), { key: p.human_key, args: p.args }, opts)
			return describeCronText(this.$t.bind(this), this.cron, opts)
		},
		serverZone() {
			const p = this.serverPreview
			return p && p.timezone ? safeTimeZone(p.timezone) : undefined
		},
		zoneText() {
			const p = this.serverPreview
			if (p && p.valid) {
				const off = parseUtcOffset(p.utc_offset)
				const offText = off === null ? p.utc_offset || '' : formatUtcOffset(off)
				return p.timezone ? this.$t('schedule.builder.server_time_zone', { zone: p.timezone, offset: offText }) : this.$t('schedule.builder.server_time')
			}
			const tz = browserTimeZone()
			return tz ? this.$t('schedule.builder.device_time_zone', { zone: tz }) : this.$t('schedule.builder.device_time')
		},
		nextTimes() {
			const opts = { locale: this.locale, hour12: this.hour12 }
			const p = this.serverPreview
			if (p && p.valid && Array.isArray(p.next)) {
				const browser = browserTimeZone()
				return p.next.slice(0, this.nextCount).map((iso, i) => {
					const d = new Date(iso)
					const text = formatRunTime(d, { ...opts, timeZone: this.serverZone })
					// "9:00 PM your time" when the browser's zone differs.
					let local = ''
					if (i === 0 && browser && zoneOffsetMinutes(d, this.serverZone) !== zoneOffsetMinutes(d, browser)) local = formatRunTime(d, { ...opts, timeZone: browser })
					return { text, local }
				})
			}
			return nextRuns(this.cron, this.now, this.nextCount).map(d => ({ text: formatRunTime(d, opts), local: '' }))
		}
	},
	watch: {
		value(v) {
			if (v === this.lastEmitted) return
			this.lastEmitted = v
			this.pattern = this.patternFor(v)
		},
		cron: {
			immediate: true,
			handler() {
				this.schedulePreview()
			}
		},
		invalid: {
			immediate: true,
			handler(v) {
				this.$emit('validity', !v)
			}
		}
	},
	beforeDestroy() {
		clearTimeout(this.previewTimer)
	},
	methods: {
		patternFor(expr) {
			const p = parsePattern(expr || '')
			if (p.kind === 'every_n_minutes' && !this.allowMinutes) return { kind: 'custom', expr: expr || '' }
			return p
		},
		emitCron() {
			const c = this.cron
			if (c === this.lastEmitted) return
			this.lastEmitted = c
			this.$emit('input', c)
		},
		setKind(kind) {
			if (kind === this.pattern.kind) return
			// Custom starts from the current expression, so nothing changes
			// until the user edits it.
			this.pattern = kind === 'custom' ? { kind: 'custom', expr: this.cron } : defaultPattern(kind, this.pattern)
			this.emitCron()
			if (kind === 'custom') this.$nextTick(() => this.$refs.cron && this.$refs.cron.focus())
		},
		update(patch) {
			this.pattern = { ...this.pattern, ...patch }
			this.emitCron()
		},
		setTime(v) {
			const m = /^(\d{2}):(\d{2})/.exec(v || '')
			if (!m) return
			this.update({ hour: parseInt(m[1], 10), minute: parseInt(m[2], 10) })
		},
		toggleDay(d) {
			const days = this.pattern.days.includes(d) ? this.pattern.days.filter(x => x !== d) : [...this.pattern.days, d]
			this.update({ days: days.sort((a, b) => a - b) })
		},
		setExpr(v) {
			this.pattern = { kind: 'custom', expr: v }
			this.emitCron()
		},
		dayName(d, style) {
			return weekdayName(d, this.locale, style)
		},
		schedulePreview() {
			clearTimeout(this.previewTimer)
			this.now = new Date()
			if (!this.previewFn || validateCron(this.cron)) return
			const cron = this.cron
			const seqNo = ++this.previewSeq
			this.previewTimer = setTimeout(async () => {
				try {
					const result = await this.previewFn(cron)
					if (seqNo !== this.previewSeq) return
					this.preview = result ? { cron, result } : null
					this.previewFailed = !result
				} catch (e) {
					if (seqNo !== this.previewSeq) return
					// Offline or not installed: device-time estimates.
					this.preview = null
					this.previewFailed = true
				}
			}, PREVIEW_DELAY_MS)
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../storage/picker-common.scss';

.schedule-builder {
	min-width: 0;
	margin: 0;
	padding: 0;
	border: none;
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

.sb-legend {
	margin-bottom: var(--space-2);
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.sb-controls {
	display: flex;
	flex-wrap: wrap;
	align-items: flex-end;
	gap: var(--space-3);
}

.sb-field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.sb-label {
	font-size: var(--font-xs);
	font-weight: 600;
	color: var(--theme-text-secondary);
}

.sb-select,
.sb-input {
	@include picker-input;
	min-width: 0;
	max-width: 100%;
}

.sb-select {
	padding-right: var(--space-6);
}

.sb-narrow {
	width: 5.5rem;
}

.sb-time {
	width: 8.5rem;
}

.sb-days {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-1);
	margin-top: var(--space-3);

	> .sb-label {
		width: 100%;
	}
}

.sb-chip {
	display: inline-flex;
	align-items: center;
	gap: 0.15rem;
	min-width: 3.25rem;
	min-height: 2.25rem;
	padding: 0 var(--space-2);
	justify-content: center;
	border: 1px solid var(--theme-input-border);
	border-radius: var(--radius-pill);
	background: var(--theme-card-bg);
	color: var(--theme-text-primary);
	font-family: inherit;
	font-size: var(--font-sm);
	cursor: pointer;

	&[aria-pressed='true'] {
		border-color: var(--color-primary);
		background: var(--color-primary);
		color: var(--color-primary-text);
		font-weight: 600;
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
	@media (pointer: coarse) {
		min-height: 44px;
		min-width: 44px;
	}
}

.sb-day-shortcuts {
	display: inline-flex;
	gap: var(--space-1);
	margin-left: var(--space-2);
}

.sb-link {
	@include picker-link-button;
	min-height: 2rem;
	font-size: var(--font-xs);
}

.sb-custom {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	margin-top: var(--space-3);
}

.sb-cron {
	width: 100%;
	max-width: 22rem;
	font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.sb-hint {
	margin: var(--space-1) 0 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.sb-status {
	margin-top: var(--space-3);
	font-size: var(--font-sm);
}

.sb-summary,
.sb-error {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-1);
	margin: 0;
}

.sb-summary {
	color: var(--theme-text-primary);
	font-weight: 500;

	.icon {
		color: var(--color-primary-fg);
	}
}

.sb-zone {
	font-weight: 400;
	color: var(--theme-text-secondary);
}

.sb-error {
	color: var(--color-danger-fg);
	font-weight: 500;
}

.sb-next {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1) var(--space-3);
	margin: var(--space-1) 0 0;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}

.sb-next-label {
	font-weight: 600;
}

.sb-local {
	color: var(--theme-text-muted);
}
</style>
