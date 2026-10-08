<template>
	<div class="drive-health">
		<h4 class="drive-details-subtitle">{{ $t('Health') }}</h4>
		<p v-if="error" class="error-note" role="alert">{{ error }}</p>
		<template v-if="report">
			<div class="verdict" :class="`is-${report.verdict}`" role="status">
				<span class="verdict-badge">{{ $t(verdictText[report.verdict] || 'Unknown') }}</span>
				<span class="verdict-text">{{ report.summary }}</span>
			</div>
			<ul v-if="report.reasons.length > 1" class="health-list">
				<li v-for="r in report.reasons.slice(1)" :key="r">{{ r }}</li>
			</ul>
			<ul v-if="report.changes.length" class="health-list is-change">
				<li v-for="c in report.changes" :key="c">{{ c }}</li>
			</ul>
			<ul v-if="report.notes.length" class="health-list is-note">
				<li v-for="n in report.notes" :key="n">{{ n }}</li>
			</ul>
			<p v-if="report.stale" class="hint">{{ $t('The drive is asleep - this is its last reading while awake.') }}</p>

			<dl v-if="report.metrics.length" class="metrics">
				<div v-for="m in report.metrics" :key="m.key" class="metric" :class="`is-${m.level}`">
					<dt>{{ m.label }}</dt>
					<dd>{{ metricText(m) }}</dd>
					<p class="metric-explain">{{ m.explain }}</p>
				</div>
			</dl>

			<template v-if="keys.length">
				<div class="history-head">
					<span>{{ $t('History') }}</span>
					<select v-model="chartKey" class="history-select" :aria-label="$t('Value to chart')">
						<option v-for="k in keys" :key="k" :value="k">{{ labelOf(k) }}</option>
					</select>
				</div>
				<svg class="history-chart" viewBox="-4 -4 248 56" preserveAspectRatio="none" role="img"
					:aria-label="$t('{name} over the last {n} days', { name: labelOf(chartKey), n: report.history.length })">
					<polyline :points="points" fill="none" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke"></polyline>
				</svg>
				<div class="history-range">
					<span>{{ report.history[0].date }}</span>
					<span>{{ report.history[report.history.length - 1].date }}</span>
				</div>
			</template>

			<h4 class="drive-details-subtitle">{{ $t('Self-test') }}</h4>
			<div class="setting-row">
				<div class="row-label">{{ $t('Status') }}</div>
				<div class="row-control">{{ selfTestText(report.self_test) }}</div>
			</div>
			<progress v-if="report.self_test.running" class="progress is-small selftest-progress" :value="100 - report.self_test.remaining_percent" max="100"
				:aria-label="$t('Self-test progress')"></progress>
			<div v-if="report.self_test.supported" class="setting-row">
				<div class="row-label">{{ $t('Run test') }}</div>
				<div class="row-control">
					<b-button rounded size="is-small" :loading="testing" :disabled="report.self_test.running" @click="runTest('short')">
						{{ report.self_test.short_minutes ? $t('Short (~{n} min)', { n: report.self_test.short_minutes }) : $t('Short') }}
					</b-button>
					<b-button rounded size="is-small" class="ml-2" :loading="testing" :disabled="report.self_test.running" @click="runTest('long')">
						{{ report.self_test.long_minutes ? $t('Long (~{n} min)', { n: report.self_test.long_minutes }) : $t('Long') }}
					</b-button>
				</div>
			</div>
			<p v-if="report.self_test.supported" class="hint">{{ $t('The drive tests itself in the background and stays usable. A long test reads the whole surface and can take hours.') }}</p>
			<ul v-if="report.self_test.last.length" class="health-list is-note">
				<li v-for="(t, i) in report.self_test.last" :key="i">{{ t.type }} · {{ t.result }} · {{ Number(t.hours).toLocaleString('en-US') }} h</li>
			</ul>
			<p v-if="testError" class="error-note" role="alert">{{ testError }}</p>

			<details v-if="report.raw.length" class="raw">
				<summary>{{ $t('Raw SMART data') }}</summary>
				<div class="raw-scroll">
					<table class="raw-table">
						<thead>
							<tr><th>ID</th><th>{{ $t('Attribute') }}</th><th>{{ $t('Value') }}</th><th>{{ $t('Worst') }}</th><th>{{ $t('Threshold') }}</th><th>{{ $t('Raw') }}</th></tr>
						</thead>
						<tbody>
							<tr v-for="(a, i) in report.raw" :key="i" :class="{ 'is-failed': a.failed && a.failed !== '-' }">
								<td>{{ a.id || '' }}</td><td>{{ a.name }}</td><td>{{ a.value }}</td><td>{{ a.worst }}</td><td>{{ a.thresh }}</td><td>{{ a.raw }}</td>
							</tr>
						</tbody>
					</table>
				</div>
			</details>
		</template>
	</div>
</template>

<script>
import { VERDICT_TEXT, metricText, chartKeys, defaultChartKey, chartPoints, selfTestText } from './driveHealth'

export default {
	name: 'drive-health-section',
	props: {
		path: { type: String, required: true }
	},
	data() {
		return { report: null, error: '', chartKey: '', testing: false, testError: '', timer: 0, verdictText: VERDICT_TEXT }
	},
	computed: {
		keys() {
			return this.report ? chartKeys(this.report.history) : []
		},
		points() {
			return this.report ? chartPoints(this.report.history, this.chartKey) : ''
		}
	},
	created() {
		this.load()
	},
	beforeUnmount() {
		this.closed = true
		clearTimeout(this.timer)
	},
	methods: {
		metricText,
		selfTestText,
		labelOf(k) {
			const m = this.report && this.report.metrics.find(x => x.key === k)
			return m ? m.label : k
		},
		load() {
			// fresh: read the drive now (a sleeping one still isn't woken)
			return this.$api.disks.getHealth(this.path).then(res => {
				if (this.closed) return
				if (res.data.success !== 200) {
					this.error = res.data.message || this.$t('Could not read drive health')
					return
				}
				this.error = ''
				this.report = res.data.data
				if (!this.chartKey || !this.keys.includes(this.chartKey)) this.chartKey = defaultChartKey(this.report)
				// poll while a self-test runs
				if (this.report.self_test && this.report.self_test.running) this.timer = setTimeout(() => this.load(), 30000)
			}).catch(e => {
				if (this.closed) return
				this.error = (e && e.response && e.response.data && e.response.data.message) || this.$t('Could not read drive health')
				this.timer = setTimeout(() => this.load(), 60000)
			})
		},
		runTest(type) {
			this.testError = ''
			this.testing = true
			this.$api.disks.startSmartTest(this.path, type).then(res => {
				if (res.data.success !== 200) {
					this.testError = res.data.message
					return
				}
				clearTimeout(this.timer)
				this.timer = setTimeout(() => this.load(), 3000)
			}).catch(e => {
				this.testError = (e && e.response && e.response.data && e.response.data.message) || this.$t('Failed to start self-test')
			}).finally(() => {
				this.testing = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.drive-details-subtitle {
	margin: var(--space-4) var(--space-5) var(--space-1);
	font-size: var(--font-xs);
	font-weight: 500;
	text-transform: uppercase;
	letter-spacing: 0.02em;
	opacity: 0.5;
}

.verdict {
	display: flex;
	align-items: baseline;
	gap: var(--space-2);
	margin: var(--space-1) var(--space-5) var(--space-2);
	font-size: var(--font-sm, 0.875rem);
}

.verdict-badge {
	flex-shrink: 0;
	padding: 0.1rem 0.55rem;
	border-radius: 999px;
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.12));
	font-size: var(--font-xs);
	font-weight: 600;
}

.verdict.is-good .verdict-badge { color: var(--color-success-fg, hsl(140, 50%, 30%)); border-color: currentColor; }
.verdict.is-watch .verdict-badge { color: var(--color-warning-fg, hsl(35, 80%, 35%)); border-color: currentColor; }
.verdict.is-failing .verdict-badge { color: var(--color-danger-fg, hsl(0, 65%, 45%)); border-color: currentColor; }

.health-list {
	margin: 0 var(--space-5) var(--space-2);
	padding-left: 1rem;
	list-style: disc;
	font-size: var(--font-xs);

	&.is-change { color: var(--color-warning-fg, inherit); }
	&.is-note { opacity: 0.7; }
}

.hint {
	margin: 0 var(--space-5) var(--space-2);
	font-size: var(--font-xs);
	opacity: 0.6;
}

.metrics {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
	gap: var(--space-2);
	margin: var(--space-2) var(--space-5);
}

.metric {
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	border-radius: var(--radius-card, 8px);

	dt { font-size: var(--font-xs); opacity: 0.7; }
	dd { margin: 0; font-weight: 600; font-variant-numeric: tabular-nums; }

	&.is-watch dd { color: var(--color-warning-fg, inherit); }
	&.is-failing dd { color: var(--color-danger-fg, inherit); }
	&.is-note dd { opacity: 0.8; }
}

.metric-explain {
	margin: 0.15rem 0 0;
	font-size: 0.7rem;
	line-height: 1.3;
	opacity: 0.6;
}

.history-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin: var(--space-3) var(--space-5) var(--space-1);
	font-size: var(--font-xs);
	opacity: 0.8;
}

.history-select {
	font: inherit;
	color: inherit;
	background: transparent;
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.12));
	border-radius: 6px;
	padding: 0.1rem 0.3rem;
}

.history-chart {
	display: block;
	width: calc(100% - 2 * var(--space-5));
	height: 3.5rem;
	margin: 0 var(--space-5);
	color: var(--theme-text-primary, currentColor);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
}

.history-range {
	display: flex;
	justify-content: space-between;
	margin: 0.2rem var(--space-5) 0;
	font-size: 0.7rem;
	opacity: 0.5;
}

.selftest-progress {
	width: calc(100% - 2 * var(--space-5));
	margin: 0 var(--space-5) var(--space-2);
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
	margin: var(--space-2) var(--space-5) 0;
}

.raw {
	margin: var(--space-3) var(--space-5) var(--space-2);
	font-size: var(--font-xs);

	summary { cursor: pointer; opacity: 0.7; }
}

.raw-scroll {
	overflow-x: auto;
	margin-top: var(--space-2);
}

.raw-table {
	width: 100%;
	border-collapse: collapse;
	font-variant-numeric: tabular-nums;

	th, td {
		padding: 0.2rem 0.4rem;
		text-align: left;
		border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
		white-space: nowrap;
	}

	th { font-weight: 500; opacity: 0.6; }
	tr.is-failed td { color: var(--color-danger-fg); }
}
</style>
