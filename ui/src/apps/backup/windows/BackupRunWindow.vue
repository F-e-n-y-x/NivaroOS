<!-- Run window (spec §12.5): live progress, the steps, the log and Cancel
     while a run is going; its summary once it's done (read-only for past
     runs). Follows the run over the message bus, polling only while the
     socket is down and this window is visible. A screen reader hears only
     step changes and the result (aria-live polite), never the counters. -->
<template>
	<div class="bk-window bk-run-window" @keydown="onWindowKeydown">
		<div v-if="!run && error" class="bk-empty" role="alert">
			<b-icon icon="alert-octagon-outline" pack="mdi" custom-size="mdi-36px" aria-hidden="true"></b-icon>
			<p>{{ errText(error) }}</p>
			<button type="button" class="bk-btn" data-autofocus @click="refresh">{{ $t('backup.retry') }}</button>
		</div>
		<div v-else-if="!run" class="bk-empty" role="status">
			<b-icon icon="loading" pack="mdi" custom-size="mdi-36px" custom-class="mdi-spin" aria-hidden="true"></b-icon>
			<p>{{ $t('backup.loading') }}</p>
		</div>

		<template v-else>
			<header class="bk-run-head">
				<div class="bk-run-head-text">
					<h2 class="bk-run-title">{{ run.job_name || jobName }}</h2>
					<p class="bk-secondary bk-run-sub">{{ subline }}</p>
				</div>
				<status-pill :tone="view.tone" :icon="view.icon" :label="$t('backup.status.' + run.status)"></status-pill>
			</header>

			<div class="bk-run-body">
				<!-- Running -->
				<template v-if="run.status === 'running'">
					<run-progress :live="live" :fmt="fmt" :label="$t('backup.run.progress_of', { name: run.job_name })" :phase-key="'backup.phase.' + run.phase"></run-progress>
					<p v-if="live && live.current_file" class="bk-run-now">
						<span class="bk-secondary">{{ $t('backup.run.now') }}</span>
						<span class="bk-run-file">{{ live.current_file }}</span>
					</p>
				</template>
				<p v-else-if="run.status === 'queued'" class="bk-run-note">{{ $t('backup.run.queued_note') }}</p>

				<!-- Waiting for the user -->
				<div v-else-if="run.status === 'waiting_user'" class="bk-run-waiting" role="group" :aria-label="$t('backup.status.waiting_user')">
					<p>{{ msg(run.summary) || $t('backup.attention.waiting.cause') }}</p>
					<div class="bk-run-actions-inline">
						<button type="button" class="bk-btn is-primary" data-autofocus @click="review">{{ $t('backup.action.review') }}</button>
					</div>
				</div>

				<!-- Finished -->
				<run-summary v-if="isFinal" :run="run"></run-summary>

				<section v-if="run.steps && run.steps.length" class="bk-run-steps" :aria-labelledby="stepsId">
					<h3 :id="stepsId" class="bk-run-h3">{{ $t('backup.run.steps') }}</h3>
					<run-steps :steps="run.steps"></run-steps>
				</section>

				<details v-if="run.has_log" class="bk-run-details" @toggle="logOpen = $event.target.open">
					<summary>{{ isFinal ? $t('backup.run.details') : $t('backup.run.details_live') }}</summary>
					<run-log v-if="logOpen" :run-id="runId" :active="!isFinal" :visible="visible"></run-log>
				</details>
			</div>

			<footer class="bk-run-foot">
				<button v-if="openFolderPath" type="button" class="bk-btn" @click="openFolder">
					<b-icon icon="folder-open-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.run.open_folder') }}</span>
				</button>
				<button v-if="isFinal && run.kind === 'backup' && jobKnown" type="button" class="bk-btn" :disabled="busy" @click="runAgain">{{ $t('backup.action.retry') }}</button>
				<span class="bk-run-spacer"></span>
				<button v-if="!isFinal" type="button" class="bk-btn is-danger" :disabled="busy || cancelling" :data-autofocus="run.status === 'running' ? '' : null" @click="confirmCancel">
					<b-icon icon="stop-circle-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ cancelling ? $t('backup.run.cancelling') : $t('backup.run.cancel') }}</span>
				</button>
				<button type="button" class="bk-btn" :data-autofocus="isFinal ? '' : null" @click="$emit('close')">{{ $t('backup.close') }}</button>
			</footer>
		</template>

		<div class="bk-sr-only" aria-live="polite">{{ announcement }}</div>
	</div>
</template>

<script>
import StatusPill from '../components/StatusPill.vue'
import RunProgress from '../components/RunProgress.vue'
import RunSteps from '../components/RunSteps.vue'
import RunSummary from '../components/RunSummary.vue'
import RunLog from '../components/RunLog.vue'
import { backupMixin, windowBehavior } from '../backupMixin'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { watchRun } from '../liveRun'
import { isFinalStatus, localPath } from '../state'
import { escapeHtml } from '@/utils/escapeHtml'
import { openFolderWindow } from '@/utils/files/openFolder'

export default {
	name: 'BackupRunWindow',
	components: { StatusPill, RunProgress, RunSteps, RunSummary, RunLog },
	mixins: [backupMixin, windowBehavior, confirmWindowMixin],
	props: {
		winId: { type: String, default: '' },
		runId: { type: String, required: true },
		jobId: { type: String, default: '' },
		jobName: { type: String, default: '' }
	},
	data() {
		return {
			run: null,
			live: null,
			job: null,
			error: null,
			busy: false,
			cancelling: false,
			logOpen: false,
			locations: [],
			announcement: '',
			stepsId: `bk-run-steps-${this.runId}`
		}
	},
	computed: {
		view() {
			return this.statusView(this.run.status)
		},
		isFinal() {
			return !!this.run && isFinalStatus(this.run.status)
		},
		jobKnown() {
			return !!(this.job || this.jobId || this.run.job_id)
		},
		visible() {
			const win = (this.$store.state.windows || []).find(w => w.id === this.winId)
			return !win || !win.minimized
		},
		subline() {
			const r = this.run
			const parts = [this.$t('backup.kind.' + r.kind), this.$t('backup.trigger.' + r.trigger)]
			if (r.started_at) parts.push(this.$t('backup.run.started', { when: this.fmt.when(r.started_at) }))
			else if (r.queued_at) parts.push(this.$t('backup.run.queued_at', { when: this.fmt.when(r.queued_at) }))
			return parts.join(' · ')
		},
		// A finished restore offers its folder in Files, when it's local.
		openFolderPath() {
			const r = this.run
			if (!r || r.kind !== 'restore' || !this.isFinal || !['success', 'partial'].includes(r.status) || !r.restore) return ''
			return localPath(r.restore.target, this.locations)
		},
		activeStepKey() {
			const s = ((this.run && this.run.steps) || []).find(x => x.state === 'active')
			return s ? this.msg({ key: s.key, args: s.args }) : ''
		}
	},
	watch: {
		activeStepKey(text) {
			if (text && !this.isFinal) this.say(this.$t('backup.run.now_step', { step: text }))
		},
		isFinal(final) {
			// Announce the result of a run watched here, not of a past one.
			if (!final || !this.sawActive) return
			const summary = this.msg(this.run.summary)
			this.say([this.$t('backup.status.' + this.run.status), summary].filter(Boolean).join('. '))
			if (this.run.kind === 'restore') this.loadLocations()
		}
	},
	created() {
		this.watcher = watchRun({
			runId: this.runId,
			client: this.bkApi,
			socket: this.$socket && this.$socket.client,
			isVisible: () => this.visible,
			onRun: run => {
				const first = !this.run
				if (!isFinalStatus(run.status)) this.sawActive = true
				this.run = run
				this.error = null
				if (first) {
					if (!this.jobId) this.loadJob()
					this.$nextTick(() => this.focusFirst())
				}
				// A full detail replaces socket figures; null once it's final.
				this.live = run.live || (isFinalStatus(run.status) ? null : this.live)
				if (isFinalStatus(run.status) && run.kind === 'restore') this.loadLocations()
			},
			onLive: live => {
				this.live = live
			},
			onError: err => {
				this.error = err
			}
		})
		if (this.jobId) this.loadJob()
	},
	beforeDestroy() {
		if (this.watcher) this.watcher.stop()
	},
	methods: {
		refresh() {
			this.error = null
			return this.watcher.refresh()
		},
		say(text) {
			this.announcement = ''
			this.$nextTick(() => (this.announcement = text))
		},
		async loadJob() {
			const id = this.jobId || (this.run && this.run.job_id)
			if (!id) return
			try {
				this.job = await this.bkApi.getJob(id)
			} catch (e) {
				// A deleted job still has its runs; only hook names are missing.
			}
		},
		async loadLocations() {
			if (this.locations.length) return
			try {
				this.locations = (await this.bkApi.locations()) || []
			} catch (e) {
				this.locations = []
			}
		},
		confirmCancel() {
			const hooks = (this.job && this.job.hooks) || []
			const apps = []
			for (const h of hooks) if (h.phase === 'pre' && h.action === 'stop_apps') apps.push(...(h.apps || []))
			const vms = hooks.filter(h => h.phase === 'pre' && h.action === 'shutdown_vm').map(h => h.vm)
			const lines = [this.run.kind === 'restore' ? this.$t('backup.run.cancel_confirm_restore') : this.$t('backup.run.cancel_confirm')]
			if (apps.length) lines.push(this.$t('backup.run.cancel_confirm_apps', { apps: this.fmt.list(apps) }))
			if (vms.length) lines.push(this.$t('backup.run.cancel_confirm_vms', { vms: this.fmt.list(vms) }))
			this.confirmWindow({
				title: this.$t('backup.run.cancel_title', { name: this.run.job_name }),
				message: lines.map(escapeHtml).join('<br>'),
				confirmText: this.$t('backup.run.cancel'),
				cancelText: this.$t('backup.run.keep_running'),
				type: 'is-danger',
				icon: 'stop-circle-outline',
				onConfirm: () => this.cancel()
			})
		},
		async cancel() {
			this.cancelling = true
			try {
				this.run = await this.bkApi.cancelRun(this.runId)
			} catch (e) {
				// Already final: just show how it ended.
				if (e.code !== 'invalid_state') this.toastError(e)
			} finally {
				this.cancelling = false
				this.refresh()
			}
		},
		review() {
			this.openBackupWindow('preview', { runId: this.runId, jobId: this.run.job_id, jobName: this.run.job_name })
		},
		async runAgain() {
			const jobId = this.jobId || this.run.job_id
			this.busy = true
			try {
				const { run_id: runId } = await this.bkApi.runJob(jobId)
				this.openBackupWindow('run', { runId, jobId, jobName: this.run.job_name })
				this.$emit('close')
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busy = false
			}
		},
		openFolder() {
			openFolderWindow(this.$store, this.openFolderPath, this.$t('Files'))
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-run-window {
	display: flex;
	flex-direction: column;
	min-height: 100%;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
	font-size: var(--font-base);
	p {
		margin: 0;
	}
}
.bk-run-head {
	display: flex;
	align-items: flex-start;
	gap: var(--space-3);
	padding: var(--space-4) var(--space-5) var(--space-2);
}
.bk-run-head-text {
	flex: 1 1 auto;
	min-width: 0;
}
.bk-run-title {
	margin: 0;
	font-size: var(--font-lg);
	font-weight: 600;
	overflow-wrap: anywhere;
}
.bk-run-sub {
	font-size: var(--font-sm);
}
.bk-run-body {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	gap: var(--space-4);
	padding: var(--space-2) var(--space-5) var(--space-4);
	min-width: 0;
}
.bk-run-now {
	display: flex;
	gap: var(--space-2);
	font-size: var(--font-sm);
	min-width: 0;
}
.bk-run-file {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	min-width: 0;
}
.bk-run-note {
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}
.bk-run-waiting {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	border-radius: var(--radius-card);
	background: var(--status-warn-bg);
	color: var(--status-warn-fg);
}
.bk-run-actions-inline {
	display: flex;
	gap: var(--space-2);
}
.bk-run-h3 {
	margin: 0 0 var(--space-2);
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-secondary);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}
.bk-run-details > summary {
	display: flex;
	align-items: center;
	min-height: 2.5rem;
	cursor: pointer;
	font-weight: 500;
	color: var(--color-primary-fg);
	border-radius: var(--radius-sm);
}
.bk-run-foot {
	position: sticky;
	bottom: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5) calc(var(--space-3) + env(safe-area-inset-bottom));
	border-top: 1px solid var(--theme-card-border);
	background: var(--theme-bg-window);
}
.bk-run-spacer {
	flex: 1 1 0;
}
</style>
