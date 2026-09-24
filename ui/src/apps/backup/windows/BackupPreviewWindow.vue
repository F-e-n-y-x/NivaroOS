<!-- src/apps/backup/windows/BackupPreviewWindow.vue -->
<!-- Preview and guard window (spec §12.6), a real desktop window (NO_SCROLL:
     the file list scrolls inside it). Opened with
     backupWindow(t, 'preview', { runId, jobId }) for a preview run, a first
     run with "preview first", or a run a guard paused (waiting_user) -
     also from the notification.
     While the plan is worked out it polls the run; then it shows what the
     run will add, update and delete (paged from GET /runs/:id/preview, a
     windowed list so 100 000 paths stay fast) and, for a paused run, asks:
     run it as shown, or once as "Copy new files" (no deletes).
     Continue / Cancel run call POST /runs/:id/decide. -->
<template>
	<div class="backup-preview" @keydown="onWindowKeydown">
		<header class="bp-head">
			<h2 ref="heading" class="bp-title" tabindex="-1">{{ title }}</h2>
			<p v-if="run && run.job_name" class="bp-job one-line">{{ run.job_name }}</p>
		</header>

		<div v-if="loadError" class="bp-state" role="alert">
			<p class="bp-state-title">{{ loadError.title }}</p>
			<p>{{ loadError.cause }}</p>
			<button type="button" class="wz-secondary" @click="loadRun">{{ $t('backup.action.try_again') }}</button>
		</div>

		<div v-else-if="!run || isWorking" class="bp-state" role="status" aria-live="polite">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-24px" aria-hidden="true"></b-icon>
			<p class="bp-state-title">{{ $t('backup.preview.working') }}</p>
			<p>{{ $t('backup.preview.working_hint') }}</p>
			<button v-if="run" type="button" class="wz-secondary" @click="openProgress">{{ $t('backup.preview.show_progress') }}</button>
		</div>

		<div v-else-if="isFailed" class="bp-state" role="alert">
			<b-icon :icon="statusIcon" custom-size="mdi-24px" aria-hidden="true"></b-icon>
			<p class="bp-state-title">{{ failure.title }}</p>
			<p>{{ failure.cause }}</p>
			<p>{{ failure.fix }}</p>
			<p v-if="summaryText" class="bp-muted">{{ summaryText }}</p>
		</div>

		<template v-else>
			<div class="bp-counts" role="group" :aria-label="$t('backup.preview.counts_label')">
				<span class="bp-count is-add"><b-icon icon="plus" custom-size="mdi-16px" aria-hidden="true"></b-icon>{{ $t('backup.preview.count_add', { n: fmt.number(counts.add), bytes: fmt.bytes(counts.bytes_add || 0) }) }}</span>
				<span class="bp-count is-update"><b-icon icon="tilde" custom-size="mdi-16px" aria-hidden="true"></b-icon>{{ $t('backup.preview.count_update', { n: fmt.number(counts.update) }) }}</span>
				<span class="bp-count is-delete"><b-icon icon="minus" custom-size="mdi-16px" aria-hidden="true"></b-icon>{{ $t('backup.preview.count_delete', { n: fmt.number(counts.delete) }) }}</span>
			</div>

			<p v-if="guardText" class="wz-note tone-warn bp-note" role="alert">
				<b-icon icon="shield-alert-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon><span>{{ guardText }}</span>
			</p>
			<p v-else-if="counts.delete && jobType === 'mirror'" class="wz-note tone-warn bp-note">
				<b-icon icon="alert-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon><span>{{ deleteNote }}</span>
			</p>
			<p v-else-if="!counts.add && !counts.update && !counts.delete" class="wz-note tone-ok bp-note">
				<b-icon icon="check-circle-outline" custom-size="mdi-18px" aria-hidden="true"></b-icon><span>{{ $t('backup.preview.nothing') }}</span>
			</p>

			<div class="bp-toolbar">
				<div class="bp-tabs" role="tablist" :aria-label="$t('backup.preview.tabs_label')" @keydown="onTabKeydown">
					<button v-for="t in tabs" :id="idp + '-tab-' + t" :key="t" :ref="'tab-' + t" type="button" role="tab" class="bp-tab"
						:aria-selected="t === tab ? 'true' : 'false'" :aria-controls="idp + '-panel'" :tabindex="t === tab ? 0 : -1" @click="setTab(t)">
						{{ $t('backup.preview.tab.' + t, { n: fmt.number(counts[t] || 0) }) }}
					</button>
				</div>
				<label :for="idp + '-search'" class="sr-only">{{ $t('backup.preview.search') }}</label>
				<input :id="idp + '-search'" v-model="query" class="wz-input bp-search" type="search" autocomplete="off" spellcheck="false" :placeholder="$t('backup.preview.search')" @keydown.esc="clearSearch" />
			</div>

			<div :id="idp + '-panel'" ref="scroller" class="bp-list" role="tabpanel" :aria-labelledby="idp + '-tab-' + tab" tabindex="0" @scroll="onScroll">
				<p v-if="listError" class="bp-list-status" role="alert">{{ listError }}</p>
				<p v-else-if="!items.length && !loadingItems" class="bp-list-status">{{ query ? $t('backup.preview.no_match') : $t('backup.preview.empty_tab') }}</p>
				<div v-else class="bp-spacer" :style="{ height: items.length * rowHeight + 'px' }">
					<ul class="bp-rows" :style="{ transform: 'translateY(' + windowStart * rowHeight + 'px)' }" :aria-label="$t('backup.preview.tab.' + tab, { n: fmt.number(total) })">
						<li v-for="(it, i) in visibleItems" :key="windowStart + i" class="bp-row" :class="'is-' + it.op" :style="{ height: rowHeight + 'px' }">
							<span class="bp-op" aria-hidden="true">{{ opSign[it.op] }}</span>
							<span class="sr-only">{{ $t('backup.preview.op.' + it.op) }}:</span>
							<span class="bp-path one-line" :title="it.path">{{ it.path }}</span>
							<span v-if="it.size" class="bp-size">{{ fmt.bytes(it.size) }}</span>
						</li>
					</ul>
				</div>
				<p v-if="loadingItems" class="bp-list-status" role="status">{{ $t('backup.loc.loading') }}</p>
			</div>

			<fieldset v-if="isWaiting && offerCopyOnce" class="bp-decision" :aria-describedby="idp + '-mode-hint'">
				<legend class="wz-label">{{ $t('backup.preview.decision') }}</legend>
				<label class="wz-check">
					<input v-model="mode" type="radio" :name="idp + '-mode'" value="as_shown" />
					<span>{{ $t('backup.preview.mode_as_shown') }}</span>
				</label>
				<label class="wz-check">
					<input v-model="mode" type="radio" :name="idp + '-mode'" value="copy_once" />
					<span>{{ $t('backup.preview.mode_copy_once') }}</span>
				</label>
				<p v-if="!decisionReady" :id="idp + '-mode-hint'" class="bp-mode-hint">{{ $t('backup.preview.mode_required') }}</p>
			</fieldset>
		</template>

		<footer class="bp-foot">
			<p v-if="decisionError" class="wz-field-error bp-foot-error" role="alert">{{ decisionError }}</p>
			<template v-if="isWaiting">
				<button type="button" class="wz-secondary" :disabled="deciding" @click="decide(false)">{{ $t('backup.preview.cancel_run') }}</button>
				<button type="button" class="wz-primary" :disabled="deciding || !decisionReady"
					:aria-describedby="decisionReady ? null : idp + '-mode-hint'" @click="decide(true)">
					<b-icon v-if="deciding" icon="loading" custom-class="mdi-spin" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.preview.continue') }}</span>
				</button>
			</template>
			<template v-else>
				<button type="button" class="wz-secondary" @click="closeWindow">{{ $t('Close') }}</button>
				<button v-if="isPreviewDone" type="button" class="wz-primary" :disabled="deciding" @click="runNow">
					<b-icon icon="play" custom-size="mdi-16px" aria-hidden="true"></b-icon><span>{{ $t('backup.preview.run_now') }}</span>
				</button>
			</template>
		</footer>
	</div>
</template>

<script>
import { backupMixin } from '../backupMixin'
import { backupWindow } from '../windows'
import { explainError } from '../messages'
import { decisionPayload } from '../state'
import { windowFocusMixin } from '@/shared/storage/windowFocus'

const POLL_MS = 2000
const PAGE = 200
const ROW_HEIGHT = 32
const OVERSCAN = 10
const SEARCH_DELAY_MS = 300

let seq = 0

export default {
	name: 'BackupPreviewWindow',
	mixins: [backupMixin, windowFocusMixin],
	props: {
		winId: { type: String, default: '' },
		runId: { type: String, required: true },
		jobId: { type: String, default: '' }
	},
	data() {
		return {
			idp: `bp${++seq}`,
			run: null,
			job: null,
			loadError: null,
			counts: { add: 0, update: 0, delete: 0, bytes_add: 0 },
			tab: 'add',
			tabChosen: false,
			tabs: ['add', 'update', 'delete'],
			query: '',
			items: [],
			total: 0,
			nextOffset: null,
			loadingItems: false,
			listError: '',
			listSeq: 0,
			scrollTop: 0,
			viewport: 400,
			rowHeight: ROW_HEIGHT,
			// Nothing preselected: see decisionPayload.
			mode: '',
			deciding: false,
			decisionError: '',
			opSign: { add: '+', update: '~', delete: '−' }
		}
	},
	computed: {
		status() {
			return this.run ? this.run.status : ''
		},
		isWaiting() {
			return this.status === 'waiting_user'
		},
		isWorking() {
			return this.status === 'queued' || this.status === 'running'
		},
		isPreviewDone() {
			return !!this.run && this.run.kind === 'preview' && (this.status === 'success' || this.status === 'partial')
		},
		isFailed() {
			return !!this.run && !this.isWaiting && !this.isWorking && !this.isPreviewDone
		},
		failure() {
			return explainError(this.$t.bind(this), (this.run && this.run.error_code) || (this.status === 'cancelled' ? 'cancelled_by_user' : 'internal'))
		},
		statusIcon() {
			return this.statusView(this.status).icon
		},
		summaryText() {
			return this.run && this.run.summary ? this.msg(this.run.summary) : ''
		},
		jobType() {
			return this.job ? this.job.type : ''
		},
		title() {
			if (this.isWaiting && this.run && this.run.guard) return this.$t('backup.preview.title_guard')
			return this.jobType ? this.$t('backup.preview.title_' + this.jobType) : this.$t('backup.preview.title')
		},
		guard() {
			return (this.run && this.run.guard) || null
		},
		guardText() {
			const g = this.guard
			if (!g) return ''
			const args = { count: this.fmt.number(g.count || 0), total: this.fmt.number(g.total || 0), pct: g.pct, limit: g.limit }
			return this.$t('backup.preview.guard.' + (['delete', 'change', 'empty_source'].includes(g.guard) ? g.guard : 'delete'), args)
		},
		deleteNote() {
			const days = this.job && this.job.retention ? this.job.retention.versions_days : 0
			const n = this.fmt.number(this.counts.delete)
			return days ? this.$t('backup.preview.delete_note', { n, days }) : this.$t('backup.preview.delete_note_forever', { n })
		},
		// "Run once as Copy new files" only makes sense when there is
		// something the run would delete.
		offerCopyOnce() {
			return this.jobType === 'mirror' && (this.counts.delete > 0 || (this.guard && this.guard.guard === 'delete'))
		},
		decisionReady() {
			return !!decisionPayload(this.offerCopyOnce, this.mode)
		},
		windowStart() {
			return Math.max(0, Math.floor(this.scrollTop / this.rowHeight) - OVERSCAN)
		},
		visibleItems() {
			const count = Math.ceil(this.viewport / this.rowHeight) + OVERSCAN * 2
			return this.items.slice(this.windowStart, this.windowStart + count)
		}
	},
	watch: {
		query() {
			clearTimeout(this.searchTimer)
			this.searchTimer = setTimeout(() => this.reloadItems(), SEARCH_DELAY_MS)
		}
	},
	created() {
		this.loadRun()
		if (this.jobId) {
			this.bkApi.getJob(this.jobId).then(j => {
				this.job = j
			}).catch(() => {
				// The type and recycle days are only used for wording.
			})
		}
	},
	mounted() {
		this.focusFirst('heading')
		this.onResize = () => this.measure()
		window.addEventListener('resize', this.onResize)
	},
	beforeDestroy() {
		clearTimeout(this.pollTimer)
		clearTimeout(this.searchTimer)
		window.removeEventListener('resize', this.onResize)
	},
	methods: {
		async loadRun() {
			clearTimeout(this.pollTimer)
			this.loadError = null
			try {
				const run = await this.bkApi.getRun(this.runId)
				const wasWorking = !this.run || this.isWorking
				const first = !this.run
				this.run = run
				// Opened from a notification the title has no job name yet.
				if (first && run && run.job_name) this.setTitle(run.job_name)
				if (this.isWorking) {
					this.pollTimer = setTimeout(() => this.loadRun(), POLL_MS)
				} else if (wasWorking && (this.isWaiting || this.isPreviewDone)) {
					if (!this.tabChosen) this.tab = this.guard && this.guard.guard === 'delete' ? 'delete' : this.guard && this.guard.guard === 'change' ? 'update' : 'add'
					await this.reloadItems()
					this.focusFirst('heading')
				}
			} catch (e) {
				this.loadError = explainError(this.$t.bind(this), e.code)
			}
		},
		setTitle(name) {
			if (this.winId && this.$store) this.$store.commit('UPDATE_WINDOW_PROPS', { id: this.winId, title: this.$t('backup.window.preview', { name }) })
		},
		async reloadItems() {
			this.items = []
			this.total = 0
			this.nextOffset = 0
			this.scrollTop = 0
			if (this.$refs.scroller) this.$refs.scroller.scrollTop = 0
			await this.loadMore()
			this.measure()
		},
		async loadMore() {
			if (this.nextOffset === null || this.loadingItems) return
			const seqNo = ++this.listSeq
			this.loadingItems = true
			this.listError = ''
			try {
				const page = await this.bkApi.getRunPreview(this.runId, { op: this.tab, q: this.query.trim(), offset: this.nextOffset, limit: PAGE })
				if (seqNo !== this.listSeq) return
				if (page.counts) this.counts = { ...this.counts, ...page.counts }
				// The server filters by op; keep only this tab's rows anyway.
				this.items = this.items.concat((page.items || []).filter(it => it.op === this.tab))
				this.total = typeof page.total === 'number' ? page.total : this.items.length
				this.nextOffset = typeof page.next_offset === 'number' && page.next_offset > 0 && (page.items || []).length ? page.next_offset : null
			} catch (e) {
				if (seqNo !== this.listSeq) return
				this.nextOffset = null
				this.listError = e.code === 'not_found' ? this.$t('backup.preview.no_plan') : this.errText(e)
			} finally {
				if (seqNo === this.listSeq) this.loadingItems = false
			}
		},
		measure() {
			this.$nextTick(() => {
				const el = this.$refs.scroller
				if (el) this.viewport = el.clientHeight || 400
			})
		},
		onScroll(e) {
			const el = e.target
			this.scrollTop = el.scrollTop
			if (el.scrollTop + el.clientHeight > el.scrollHeight - this.rowHeight * 20) this.loadMore()
		},
		// Esc in a non-empty search clears it; an empty one closes as usual.
		clearSearch(e) {
			if (!this.query) return
			e.preventDefault()
			e.stopPropagation()
			this.query = ''
		},
		setTab(t) {
			if (t === this.tab) return
			this.tab = t
			this.tabChosen = true
			this.reloadItems()
		},
		onTabKeydown(e) {
			const i = this.tabs.indexOf(this.tab)
			let next = -1
			if (e.key === 'ArrowRight') next = (i + 1) % this.tabs.length
			else if (e.key === 'ArrowLeft') next = (i + this.tabs.length - 1) % this.tabs.length
			else if (e.key === 'Home') next = 0
			else if (e.key === 'End') next = this.tabs.length - 1
			if (next < 0) return
			e.preventDefault()
			this.setTab(this.tabs[next])
			this.$nextTick(() => {
				const r = this.$refs['tab-' + this.tabs[next]]
				const el = Array.isArray(r) ? r[0] : r
				if (el) el.focus()
			})
		},
		async decide(proceed) {
			if (this.deciding) return
			this.deciding = true
			this.decisionError = ''
			try {
				const body = proceed ? decisionPayload(this.offerCopyOnce, this.mode) : { proceed: false }
				if (!body) return
				const run = await this.bkApi.decideRun(this.runId, body)
				this.run = run
				if (proceed) {
					this.openRunWindow(this.runId)
				} else {
					this.toast(this.$t('backup.preview.cancelled'))
				}
				this.closeWindow()
			} catch (e) {
				if (e.code === 'invalid_state') {
					// Decided elsewhere (another window, the notification).
					this.decisionError = this.$t('backup.preview.already_decided')
					this.loadRun()
				} else {
					this.decisionError = this.errText(e)
				}
			} finally {
				this.deciding = false
			}
		},
		async runNow() {
			const jobId = (this.run && this.run.job_id) || this.jobId
			if (!jobId || this.deciding) return
			this.deciding = true
			try {
				const res = await this.bkApi.runJob(jobId, { preview: false })
				this.openRunWindow(res.run_id)
				this.closeWindow()
			} catch (e) {
				this.decisionError = this.errText(e)
			} finally {
				this.deciding = false
			}
		},
		openProgress() {
			this.openRunWindow(this.runId)
		},
		openRunWindow(runId) {
			const jobId = (this.run && this.run.job_id) || this.jobId
			const jobName = (this.run && this.run.job_name) || (this.job && this.job.name) || ''
			this.$store.commit('OPEN_WINDOW', backupWindow(this.$t.bind(this), 'run', { runId, jobId, jobName }))
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../components/wizard-form.scss';

.backup-preview {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-width: 0;
	min-height: 0;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
}

.bp-head {
	flex-shrink: 0;
	padding: var(--space-3) var(--space-4) 0;
}

.bp-title {
	margin: 0;
	font-size: var(--font-md);
	font-weight: 700;

	&:focus {
		outline: none;
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
}

.bp-job {
	margin: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.bp-state {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	padding: var(--space-6);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);

	p {
		margin: 0;
		max-width: 32rem;
	}
}

.bp-state-title {
	font-weight: 600;
	color: var(--theme-text-primary);
}

.bp-muted {
	color: var(--theme-text-muted);
}

.bp-counts {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2) var(--space-4);
	padding: var(--space-3) var(--space-4) 0;
	font-size: var(--font-sm);
	font-weight: 600;
}

.bp-count {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);

	&.is-add {
		color: var(--color-success-fg);
	}
	&.is-update {
		color: var(--color-info-fg);
	}
	&.is-delete {
		color: var(--color-danger-fg);
	}
}

.bp-note {
	flex-shrink: 0;
	margin: var(--space-3) var(--space-4) 0;
}

.bp-toolbar {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4) var(--space-2);
}

.bp-tabs {
	display: inline-flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	padding: 0.2rem;
	border-radius: var(--radius-control);
	background: var(--theme-pill-bg);
}

.bp-tab {
	min-height: 2rem;
	padding: 0 var(--space-3);
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: var(--theme-text-secondary);
	font-family: inherit;
	font-size: var(--font-xs);
	font-weight: 600;
	cursor: pointer;

	&[aria-selected='true'] {
		background: var(--theme-card-bg);
		color: var(--theme-text-primary);
		box-shadow: var(--shadow-sm);
	}
	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: 0;
	}
	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.bp-search {
	flex: 1 1 10rem;
	min-width: 0;
}

.bp-list {
	position: relative;
	flex: 1 1 auto;
	min-height: 6rem;
	overflow-y: auto;
	overflow-x: hidden;
	margin: 0 var(--space-4);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);
	background: var(--theme-card-bg);

	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: 0;
	}
}

.bp-list-status {
	margin: 0;
	padding: var(--space-4);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}

.bp-spacer {
	position: relative;
}

.bp-rows {
	margin: 0;
	padding: 0;
	list-style: none;
	will-change: transform;
}

.bp-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: 0 var(--space-3);
	font-size: var(--font-xs);
	border-bottom: 1px solid var(--theme-table-divider);

	&.is-add .bp-op {
		color: var(--color-success-fg);
	}
	&.is-update .bp-op {
		color: var(--color-info-fg);
	}
	&.is-delete .bp-op {
		color: var(--color-danger-fg);
	}
}

.bp-op {
	flex-shrink: 0;
	width: 1rem;
	font-weight: 700;
	text-align: center;
}

.bp-path {
	flex: 1 1 auto;
	min-width: 0;
	font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
	color: var(--theme-text-primary);
}

.bp-size {
	flex-shrink: 0;
	color: var(--theme-text-muted);
}

.bp-decision {
	flex-shrink: 0;
	margin: var(--space-3) var(--space-4) 0;
	padding: 0;
	border: none;
}

.bp-mode-hint {
	margin: var(--space-1) 0 0;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}

.bp-foot {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: flex-end;
	gap: var(--space-2);
	margin-top: var(--space-3);
	padding: var(--space-3) var(--space-4);
	padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
	border-top: 1px solid var(--theme-card-border);
}

.bp-foot-error {
	flex: 1 1 100%;
}
</style>
