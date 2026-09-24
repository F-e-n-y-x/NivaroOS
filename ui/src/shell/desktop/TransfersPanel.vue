<!-- src/shell/desktop/TransfersPanel.vue -->
<!--
	The one place copy / move / delete jobs are shown (replaces the two
	disagreeing widgets OperationTray.vue and FileOperationStatus.vue).
	Mounted once, globally, because a job's source and destination can be
	in different windows - or no window at all.

	Honest states: a job only ever says "Done" when every file arrived;
	otherwise it says how many failed, lists them with the reason, and
	offers Retry. Clean successes fade away on their own; problems stay
	until dismissed.
-->
<template>
	<div v-if="jobs.length" class="transfers-panel" :class="{ collapsed }" @mouseenter="hovering = true" @mouseleave="onLeave">
		<div class="tp-head">
			<button type="button" class="tp-toggle" :aria-expanded="String(!collapsed)" aria-controls="transfers-panel-list" @click="collapsed = !collapsed">
				<b-icon :icon="headIcon" custom-size="mdi-18px" class="tp-head-icon" :class="headTone"></b-icon>
				<span class="tp-head-text">
					<span class="tp-head-title one-line" aria-live="polite">{{ summaryTitle }}</span>
					<span v-if="collapsed && activeJobs.length" class="tp-mini-track">
						<span class="tp-mini-fill" :style="{ width: overallPercent + '%' }"></span>
					</span>
				</span>
				<b-icon :icon="collapsed ? 'chevron-up' : 'chevron-down'" custom-size="mdi-18px" class="tp-chevron"></b-icon>
			</button>
			<button v-if="finishedJobs.length && !collapsed" type="button" class="tp-link tp-clear" :title="$t('Remove finished transfers from the list')" @click="clearFinished">{{ $t('Clear') }}</button>
		</div>

		<div v-if="!collapsed" id="transfers-panel-list" class="tp-list scrollbars-light">
			<div v-for="job in jobs" :key="job.id" class="tp-job" :class="'is-' + job.state">
				<b-icon :icon="kindIcon(job)" custom-size="mdi-18px" class="tp-kind"></b-icon>
				<div class="tp-body">
					<div class="tp-title one-line" :title="titleTooltip(job)">{{ title(job) }}</div>
					<div v-if="!isTerminal(job)" class="tp-track">
						<div class="tp-fill" :class="{ indeterminate: indeterminate(job) }" :style="{ width: percent(job) + '%' }"></div>
					</div>
					<div class="tp-meta" :class="metaTone(job)">{{ meta(job) }}</div>

					<div v-if="job.failures.length && expanded[job.id]" class="tp-failures">
						<div v-for="(f, i) in job.failures" :key="i" class="tp-failure">
							<span class="tp-fname one-line" :title="f.path">{{ baseName(f.path) }}</span>
							<span class="tp-freason">{{ f.error }}</span>
						</div>
						<div v-if="job.files_failed > job.failures.length" class="tp-failure tp-more">
							{{ $t('…and {n} more', { n: job.files_failed - job.failures.length }) }}
						</div>
					</div>

					<div class="tp-actions">
						<button v-if="job.failures.length" class="tp-link" @click="toggle(job.id)">
							{{ expanded[job.id] ? $t('Hide details') : $t('Show what failed') }}
						</button>
						<button v-if="canRetry(job)" class="tp-link strong" :disabled="busy[job.id]" @click="doRetry(job)">{{ $t('Retry') }}</button>
						<button v-if="job.kind !== 'delete' && isTerminal(job) && job.dest" class="tp-link" @click="showFolder(job)">{{ $t('Show in folder') }}</button>
					</div>
				</div>
				<button v-if="!isTerminal(job)" type="button" class="tp-icon-btn" :title="$t('Cancel')" :aria-label="$t('Cancel {title}', { title: title(job) })" :disabled="busy[job.id]" @click="doCancel(job)">
					<b-icon icon="close" custom-size="mdi-16px"></b-icon>
				</button>
				<button v-else type="button" class="tp-icon-btn" :title="$t('Dismiss')" :aria-label="$t('Dismiss')" @click="hide(job.id)">
					<b-icon icon="close" custom-size="mdi-16px"></b-icon>
				</button>
			</div>
		</div>
	</div>
</template>

<script>
import transfers, { applyEvent, sync, isTerminal, isActive, retry, cancel, hide, clearFinished, bus } from '@/service/transfers'
import { startClipboardSync } from '@/service/filesClipboard'
import { renderSize } from '@/mixins/file_utils'
import { baseName } from '@/utils/files/path'
import { openFolderWindow } from '@/utils/files/openFolder'
import { activityService } from '@/service/activity'
import { escapeHtml } from '@/utils/escapeHtml'

// Resync if a job is active but no event arrived for this long (a dropped
// socket must not freeze the panel).
const QUIET_MS = 2500
const POLL_MS = 1500
// How long a clean success stays visible.
const SUCCESS_LINGER_MS = 6000

export default {
	name: 'transfers-panel',
	data() {
		return {
			// On a phone an expanded list would cover the full-screen app
			// underneath - start as the one-line summary there.
			collapsed: !!(this.$store.state.isMobile || this.$store.state.isTablet),
			hovering: false,
			expanded: {},
			busy: {},
			timers: {},
			poller: null,
		}
	},
	computed: {
		jobs() {
			return transfers.visibleJobs().slice().reverse() // newest first
		},
		activeJobs() {
			return this.jobs.filter(isActive)
		},
		finishedJobs() {
			return this.jobs.filter(isTerminal)
		},
		problemJobs() {
			return this.jobs.filter((j) => ['done_with_errors', 'failed', 'interrupted'].includes(j.state))
		},
		overallPercent() {
			let done = 0
			let total = 0
			for (const j of this.activeJobs) {
				if (j.bytes_total > 0) {
					done += j.bytes_done
					total += j.bytes_total
				}
			}
			return total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0
		},
		summaryTitle() {
			const n = this.activeJobs.length
			if (n) {
				return n === 1 ? `${this.title(this.activeJobs[0])} · ${this.overallPercent}%` : this.$t('{n} transfers in progress · {p}%', { n, p: this.overallPercent })
			}
			if (this.problemJobs.length) {
				return this.problemJobs.length === 1 ? this.$t('A transfer needs attention') : this.$t('{n} transfers need attention', { n: this.problemJobs.length })
			}
			return this.$t('Transfers complete')
		},
		headIcon() {
			if (this.activeJobs.length) return 'swap-vertical'
			if (this.problemJobs.length) return 'alert-circle-outline'
			return 'check-circle-outline'
		},
		headTone() {
			if (this.activeJobs.length) return ''
			return this.problemJobs.length ? 'tone-warn' : 'tone-ok'
		},
	},
	watch: {
		jobs: {
			handler(list) {
				for (const j of list) {
					if (j.state === 'done' && !this.timers[j.id]) this.scheduleHide(j.id)
				}
			},
			immediate: true,
		},
	},
	sockets: {
		'nivaroos:file:operate'(res) {
			applyEvent(res)
		},
		// Reconnected after a drop: anything could have happened meanwhile.
		connect() {
			sync()
		},
	},
	created() {
		startClipboardSync(this.$store)
		sync()
		this.poller = setInterval(() => {
			const active = transfers.state.order.some((id) => isActive(transfers.state.jobs[id]))
			if (active && Date.now() - Math.max(transfers.state.lastEventAt, transfers.state.lastSyncAt) > QUIET_MS) sync()
		}, POLL_MS)
		bus.$on('finished', this.onFinished)
	},
	beforeDestroy() {
		clearInterval(this.poller)
		bus.$off('finished', this.onFinished)
		Object.values(this.timers).forEach(clearTimeout)
	},
	methods: {
		renderSize,
		baseName,
		isTerminal,
		kindIcon(job) {
			return { copy: 'content-copy', move: 'file-move-outline', delete: 'delete-outline' }[job.kind] || 'swap-vertical'
		},
		what(job) {
			const n = job.sources.length
			if (n === 1) return `“${baseName(job.sources[0])}”`
			return this.$t('{n} items', { n })
		},
		destName(job) {
			return baseName(job.dest) || job.dest
		},
		title(job) {
			const what = this.what(job)
			const to = this.destName(job)
			// Past tense only when it actually happened.
			const past = job.state === 'done' || job.state === 'done_with_errors'
			switch (job.kind) {
				case 'move':
					return past ? this.$t('Moved {what} to {to}', { what, to }) : this.$t('Moving {what} to {to}', { what, to })
				case 'delete':
					return past ? this.$t('Deleted {what}', { what }) : this.$t('Deleting {what}', { what })
				default:
					return past ? this.$t('Copied {what} to {to}', { what, to }) : this.$t('Copying {what} to {to}', { what, to })
			}
		},
		titleTooltip(job) {
			return job.kind === 'delete' ? job.sources.join('\n') : `${job.sources.join('\n')}\n→ ${job.dest}`
		},
		indeterminate(job) {
			return job.state === 'queued' || job.state === 'scanning' || job.state === 'syncing' || !(job.bytes_total > 0)
		},
		percent(job) {
			if (this.indeterminate(job)) return 35
			return Math.min(100, Math.round((job.bytes_done / job.bytes_total) * 100))
		},
		eta(job) {
			if (!(job.speed > 0) || !(job.bytes_total > 0)) return ''
			const s = Math.round((job.bytes_total - job.bytes_done) / job.speed)
			if (s < 60) return this.$t('{n}s left', { n: s })
			if (s < 3600) return this.$t('{n} min left', { n: Math.round(s / 60) })
			return this.$t('{h} h {m} min left', { h: Math.floor(s / 3600), m: Math.round((s % 3600) / 60) })
		},
		meta(job) {
			const files = (n) => `${(n || 0).toLocaleString()} ${n === 1 ? this.$t('file') : this.$t('files')}`
			switch (job.state) {
				case 'queued':
					return this.$t('Waiting to start…')
				case 'scanning':
					return job.files_total ? this.$t('Preparing… {n} found', { n: files(job.files_total) }) : this.$t('Preparing…')
				case 'syncing':
					return this.$t('Uploading to cloud storage…')
				case 'running': {
					const parts = [`${job.files_done.toLocaleString()} / ${files(job.files_total)}`]
					if (job.speed > 0) parts.push(`${renderSize(job.speed)}/s`)
					const eta = this.eta(job)
					if (eta) parts.push(eta)
					return parts.join(' · ')
				}
				case 'done': {
					const skipped = job.files_skipped ? ` · ${this.$t('{n} already there', { n: job.files_skipped.toLocaleString() })}` : ''
					// bytes_done includes skipped files (so progress reaches 100%),
					// so only quote a size when everything was actually written.
					const size = job.bytes_done > 0 && job.kind !== 'delete' && !job.files_skipped ? ' · ' + renderSize(job.bytes_done) : ''
					return `${files(job.files_done)}${size}${skipped}`
				}
				case 'done_with_errors':
					return this.$t('{failed} failed · {done} done', { failed: files(job.files_failed), done: job.files_done.toLocaleString() })
				case 'failed':
					return job.error || this.$t('Failed')
				case 'cancelled':
					return this.$t('Cancelled · {n} done before stopping', { n: files(job.files_done) })
				case 'interrupted':
					return this.$t('Stopped when the server restarted · {n} done', { n: files(job.files_done) })
			}
			return ''
		},
		metaTone(job) {
			if (['done_with_errors', 'failed', 'interrupted'].includes(job.state)) return 'tone-warn'
			if (job.state === 'done') return 'tone-ok'
			return ''
		},
		canRetry(job) {
			return ['done_with_errors', 'failed', 'interrupted', 'cancelled'].includes(job.state)
		},
		toggle(id) {
			this.$set(this.expanded, id, !this.expanded[id])
		},
		hide(id) {
			hide(id)
		},
		scheduleHide(id) {
			this.$set(this.timers, id, setTimeout(() => {
				if (this.hovering) {
					this.$delete(this.timers, id)
					return
				}
				hide(id)
			}, SUCCESS_LINGER_MS))
		},
		onLeave() {
			this.hovering = false
			for (const j of this.jobs) {
				if (j.state === 'done' && !this.timers[j.id]) this.scheduleHide(j.id)
			}
		},
		async doRetry(job) {
			this.$set(this.busy, job.id, true)
			try {
				await retry(job.id)
			} catch (e) {
				this.$buefy.toast.open({ message: escapeHtml(e.message), type: 'is-danger' })
			} finally {
				this.$set(this.busy, job.id, false)
			}
		},
		async doCancel(job) {
			this.$set(this.busy, job.id, true)
			try {
				await cancel(job.id)
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t("Couldn't cancel - check the connection"), type: 'is-danger' })
			} finally {
				this.$set(this.busy, job.id, false)
			}
		},
		clearFinished() {
			clearFinished()
		},
		showFolder(job) {
			openFolderWindow(this.$store, job.dest, this.$t('Files'))
		},
		onFinished(job) {
			// A problem should never go unnoticed, even with the panel
			// collapsed or the job hidden.
			if (['done_with_errors', 'failed', 'interrupted'].includes(job.state)) {
				// The summary line already says "needs attention" on phones.
				if (!(this.$store.state.isMobile || this.$store.state.isTablet)) this.collapsed = false
				activityService.add({
					title: job.state === 'failed' ? this.$t('Transfer failed') : this.$t('Transfer finished with problems'),
					message: `${this.title(job)} - ${this.meta(job)}`,
					type: 'system',
					status: 'error',
				})
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.transfers-panel {
	position: relative;
	pointer-events: auto;
	width: 22rem;
	max-width: calc(100vw - 2rem);
	max-height: min(60vh, 32rem);
	display: flex;
	flex-direction: column;
	background: var(--theme-card-bg);
	backdrop-filter: blur(12px);
	-webkit-backdrop-filter: blur(12px);
	color: var(--theme-text-primary);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card, 12px);
	box-shadow: var(--shadow-xl);
	overflow: hidden;
	font-size: var(--font-sm);
}

.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.tp-head {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding-right: var(--space-3);
	user-select: none;
}

.tp-toggle {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: 0.65rem var(--space-3);
	border: none;
	background: transparent;
	color: inherit;
	font: inherit;
	text-align: left;
	cursor: pointer;

	.collapsed & {
		padding-bottom: 0.6rem;
	}
}

.tp-head-text,
.tp-head-title,
.tp-mini-track,
.tp-mini-fill {
	display: block;
}

.tp-head-icon {
	flex-shrink: 0;
	color: var(--color-primary-fg, var(--color-primary));

	&.tone-ok {
		color: var(--color-success-fg);
	}
	&.tone-warn {
		color: var(--color-warning-fg);
	}
}

.tp-head-text {
	flex: 1 1 auto;
	min-width: 0;
}

.tp-head-title {
	font-weight: 600;
	font-size: var(--font-sm);
}

.tp-mini-track {
	margin-top: 0.35rem;
	height: 3px;
	border-radius: 99px;
	background: var(--theme-card-hover);
	overflow: hidden;
}

.tp-mini-fill {
	height: 100%;
	background: var(--color-primary);
	transition: width 0.3s ease;
}

.tp-chevron {
	flex-shrink: 0;
	color: var(--theme-text-secondary);
}

.tp-list {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	border-top: 1px solid var(--theme-card-border);
}

.tp-job {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	padding: 0.7rem var(--space-3);
	border-bottom: 1px solid var(--theme-table-divider, var(--theme-card-border));

	&:last-child {
		border-bottom: none;
	}
}

.tp-kind {
	flex-shrink: 0;
	margin-top: 0.1rem;
	color: var(--theme-text-secondary);

	.is-done & {
		color: var(--color-success-fg);
	}
	.is-done_with_errors &,
	.is-failed &,
	.is-interrupted & {
		color: var(--color-warning-fg);
	}
}

.tp-body {
	flex: 1 1 auto;
	min-width: 0;
}

.tp-title {
	font-size: var(--font-sm);
	font-weight: 500;
}

.tp-track {
	position: relative;
	margin: 0.4rem 0 0.3rem;
	height: 4px;
	border-radius: 99px;
	background: var(--theme-card-hover);
	overflow: hidden;
}

.tp-fill {
	position: absolute;
	inset: 0 auto 0 0;
	border-radius: inherit;
	background: var(--color-primary);
	transition: width 0.3s ease;

	&.indeterminate {
		animation: tp-slide 1.3s ease-in-out infinite;
	}
}

@keyframes tp-slide {
	0% {
		left: -35%;
	}
	100% {
		left: 100%;
	}
}

@media (prefers-reduced-motion: reduce) {
	.tp-fill.indeterminate {
		animation: none;
		left: 0;
		opacity: 0.6;
	}
}

.tp-meta {
	margin-top: 0.15rem;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
	font-variant-numeric: tabular-nums;

	&.tone-ok {
		color: var(--color-success-fg);
	}
	&.tone-warn {
		color: var(--color-warning-fg);
	}
}

.tp-failures {
	margin-top: 0.4rem;
	padding: 0.4rem 0.5rem;
	border-radius: 8px;
	background: var(--theme-card-subtle);
	max-height: 9rem;
	overflow-y: auto;
}

.tp-failure {
	display: flex;
	flex-direction: column;
	padding: 0.2rem 0;
	font-size: var(--font-2xs);

	& + & {
		border-top: 1px solid var(--theme-card-border);
	}
}

.tp-fname {
	color: var(--theme-text-primary);
}

.tp-freason {
	color: var(--color-danger-fg);
}

.tp-more {
	color: var(--theme-text-secondary);
}

.tp-actions {
	display: flex;
	flex-wrap: wrap;
	gap: 0.15rem 0.75rem;
	margin-top: 0.3rem;

	&:empty {
		display: none;
	}
}

.tp-link {
	border: none;
	background: none;
	padding: 0;
	font: inherit;
	font-size: var(--font-xs);
	color: var(--color-primary-fg, var(--color-primary));
	cursor: pointer;

	&.strong {
		font-weight: 600;
	}
	&:hover:not(:disabled) {
		text-decoration: underline;
	}
	&:disabled {
		opacity: 0.5;
		cursor: default;
	}
}

.tp-icon-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.6rem;
	height: 1.6rem;
	border: none;
	border-radius: 6px;
	background: transparent;
	color: var(--theme-text-secondary);
	cursor: pointer;

	&:hover:not(:disabled) {
		background: var(--theme-card-hover);
		color: var(--theme-text-primary);
	}
	&:disabled {
		opacity: 0.4;
	}
}
.tp-toggle,
.tp-link,
.tp-icon-btn {
	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: 2px;
	}
}
</style>
