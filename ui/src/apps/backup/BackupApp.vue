<!-- Backup & Sync (spec §12): the optional backup module's app window.
     Overview · Jobs · Restore · Activity · Settings, fed by the
     nivaroos-backup service through /v1/backup and kept live by its
     message-bus events (polling only while the socket is down). Every
     secondary surface - the job wizard, run progress, preview, browse,
     restore, confirmations - is its own desktop window. -->
<template>
	<div ref="root" class="bk-app bk-scope" :class="widthClass" @keydown="onKeydown">
		<backup-nav v-if="status === 'ready'" :active="activeSection" :layout="navLayout" :badges="badges" @select="goSection"></backup-nav>

		<main ref="main" class="bk-main" :aria-busy="status === 'loading' ? 'true' : null">
			<div v-if="status === 'loading'" class="bk-empty" role="status">
				<b-icon icon="loading" pack="mdi" custom-size="mdi-36px" custom-class="mdi-spin" aria-hidden="true"></b-icon>
				<p>{{ $t('backup.loading') }}</p>
			</div>

			<div v-else-if="status !== 'ready'" class="bk-offline" role="alert">
				<b-icon :icon="offlineIcon" pack="mdi" custom-size="mdi-48px" aria-hidden="true"></b-icon>
				<p class="bk-offline-title">{{ offline.title }}</p>
				<p class="bk-offline-text">{{ offline.cause }}</p>
				<p v-if="offline.fix" class="bk-offline-text">{{ offline.fix }}</p>
				<button type="button" class="bk-btn is-primary" data-autofocus @click="retry">
					<b-icon icon="refresh" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.retry') }}</span>
				</button>
			</div>

			<template v-else>
				<overview-section v-if="activeSection === 'overview'" :jobs="jobs" :fmt="fmt"></overview-section>
				<jobs-section
					v-else-if="activeSection === 'jobs'"
					ref="jobsSection"
					:jobs="jobs"
					:fmt="fmt"
					:cron-info="cronInfo"
					:selected-job-id="selectedJobId"
					:narrow="contentNarrow"
				></jobs-section>
				<restore-section v-else-if="activeSection === 'restore'" :jobs="jobs" :initial-job-id="restoreJobId"></restore-section>
				<activity-section v-else-if="activeSection === 'activity'" :jobs="jobs" :initial-job-id="activityJobId" :version="runVersion"></activity-section>
				<backup-settings-section v-else-if="activeSection === 'settings'" :jobs="jobs"></backup-settings-section>
			</template>

			<section v-if="showShortcuts" class="bk-card bk-shortcuts" aria-labelledby="bk-shortcuts-title">
				<h3 id="bk-shortcuts-title" class="bk-card-title">{{ $t('backup.shortcuts.title') }}</h3>
				<dl>
					<div v-for="s in shortcuts" :key="s.key">
						<dt><kbd>{{ s.key }}</kbd></dt>
						<dd>{{ $t(s.label) }}</dd>
					</div>
				</dl>
				<button ref="shortcutsClose" type="button" class="bk-btn is-small" @click="showShortcuts = false">{{ $t('backup.close') }}</button>
			</section>

			<div class="bk-sr-only" aria-live="polite">{{ announcement }}</div>
		</main>
	</div>
</template>

<script>
import BackupNav from './BackupNav.vue'
import OverviewSection from './sections/OverviewSection.vue'
import JobsSection from './sections/JobsSection.vue'
import RestoreSection from './sections/RestoreSection.vue'
import ActivitySection from './sections/ActivitySection.vue'
import BackupSettingsSection from './sections/BackupSettingsSection.vue'
import { backupMixin, loadCapabilities } from './backupMixin'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { BACKUP_SECTIONS, BACKUP_APP_ID } from './windows'
import { attentionItems, runningItems, distinctCrons, isActiveStatus } from './state'
import { watchJobs } from './liveRun'
import { CLIENT_CODES } from '@/service/backup'
import { escapeHtml } from '@/utils/escapeHtml'

// Window-width tiers (spec §12.1): a bottom tab bar on phones, a rail on
// tablets, a sidebar otherwise. The job detail pane sits beside the list
// only when the content area is wide enough for both.
const PHONE_MAX = 480
const RAIL_MAX = 760
const DETAIL_BESIDE_MIN = 900
const RELOAD_DEBOUNCE_MS = 400
// Safety net even with the socket up (a missed event, a clock tick).
const IDLE_REFRESH_MS = 60000

// Where each fix sends the wizard (spec §12.4 steps).
const WIZARD_STEP_FOR = { change_dest: 'where', edit_filters: 'what', edit_hooks: 'what', switch_archive: 'what', edit_job: 'review' }

const SHORTCUTS = [
	{ key: 'N', label: 'backup.shortcuts.new' },
	{ key: '/', label: 'backup.shortcuts.search' },
	{ key: 'R', label: 'backup.shortcuts.run' },
	{ key: '?', label: 'backup.shortcuts.help' }
]

export default {
	name: 'BackupApp',
	components: { BackupNav, OverviewSection, JobsSection, RestoreSection, ActivitySection, BackupSettingsSection },
	mixins: [backupMixin, confirmWindowMixin],
	provide() {
		return { backupApp: this }
	},
	// Deep-link props (windows.js BACKUP_WINDOWS.app). A second OPEN_WINDOW
	// for 'backup' merges them and stamps requestedAt.
	props: {
		section: { type: String, default: '' },
		jobId: { type: String, default: '' },
		runId: { type: String, default: '' },
		wizard: { type: Boolean, default: false },
		preset: { type: String, default: '' },
		sourcePath: { type: String, default: '' },
		destRef: { type: Object, default: null },
		destPath: { type: String, default: '' },
		requestedAt: { type: Number, default: 0 },
		sectionRequestedAt: { type: Number, default: 0 }
	},
	data() {
		return {
			// loading | ready | error (see errorCode)
			status: 'loading',
			errorCode: '',
			activeSection: 'overview',
			jobs: [],
			caps: null,
			cronInfo: {},
			// Live progress by run id, from bus events or fallback polls.
			live: {},
			selectedJobId: '',
			restoreJobId: '',
			activityJobId: '',
			busyJobId: '',
			// Bumped on changes, so open panes can refetch their own data.
			jobVersion: 0,
			runVersion: 0,
			width: 1040,
			contentWidth: 840,
			showShortcuts: false,
			shortcuts: SHORTCUTS,
			announcement: ''
		}
	},
	computed: {
		navLayout() {
			if (this.width < PHONE_MAX) return 'tabs'
			if (this.width < RAIL_MAX) return 'rail'
			return 'side'
		},
		widthClass() {
			return {
				'bk-w-phone': this.width < PHONE_MAX,
				'bk-w-narrow': this.width < RAIL_MAX,
				'is-tabs': this.navLayout === 'tabs'
			}
		},
		contentNarrow() {
			return this.contentWidth < DETAIL_BESIDE_MIN
		},
		badges() {
			const attention = attentionItems(this.jobs).length
			const running = runningItems(this.jobs).length
			return { overview: attention || 0, activity: running || 0 }
		},
		offline() {
			return this.explain(this.errorCode || CLIENT_CODES.UNAVAILABLE)
		},
		offlineIcon() {
			if (this.errorCode === 'unauthorized' || this.errorCode === 'forbidden') return 'account-lock-outline'
			return this.errorCode === CLIENT_CODES.UNAVAILABLE ? 'lan-disconnect' : 'alert-octagon-outline'
		}
	},
	watch: {
		// Stamped by every OPEN_WINDOW of 'backup' (sectionRequestedAt only
		// with a section, so it isn't watched: it would act twice).
		requestedAt() {
			this.applyDeepLink()
		}
	},
	created() {
		this.load().then(() => this.applyDeepLink())
		this.watcher = watchJobs({
			socket: this.$socket && this.$socket.client,
			isVisible: this.isVisible,
			onChange: this.onBusChange,
			onLive: (runId, live) => this.$set(this.live, runId, live)
		})
		this.idleTimer = setInterval(() => {
			if (this.status === 'ready' && this.isVisible()) this.scheduleReload()
		}, IDLE_REFRESH_MS)
	},
	mounted() {
		this.measure()
		if (typeof ResizeObserver !== 'undefined') {
			this.resizeObserver = new ResizeObserver(() => this.measure())
			this.resizeObserver.observe(this.$refs.root)
		}
	},
	beforeDestroy() {
		if (this.watcher) this.watcher.stop()
		if (this.resizeObserver) this.resizeObserver.disconnect()
		clearInterval(this.idleTimer)
		clearTimeout(this.reloadTimer)
	},
	methods: {
		measure() {
			const root = this.$refs.root
			if (!root) return
			this.width = root.clientWidth || this.width
			const main = this.$refs.main
			this.contentWidth = (main && main.clientWidth) || this.width
		},
		isVisible() {
			if (typeof document !== 'undefined' && document.hidden) return false
			const win = (this.$store.state.windows || []).find(w => w.id === BACKUP_APP_ID)
			return !win || !win.minimized
		},

		// --- data -----------------------------------------------------
		// refreshCaps refetches capabilities too (Retry: the service may have
		// restarted or been updated); event-driven reloads only need jobs.
		async load({ refreshCaps = false } = {}) {
			try {
				const [jobs, caps] = await Promise.all([this.bkApi.listJobs(), loadCapabilities(refreshCaps)])
				this.caps = caps
				this.bkCaps = caps
				this.setJobs(jobs)
				this.status = 'ready'
				this.errorCode = ''
			} catch (e) {
				if (this.status === 'ready' && e.code !== CLIENT_CODES.UNAVAILABLE) {
					// Keep showing what we have; a reload failing once isn't an outage.
					return
				}
				this.status = 'error'
				this.errorCode = e.code || 'internal'
			}
		},
		retry() {
			this.status = 'loading'
			this.load({ refreshCaps: true }).then(() => this.$nextTick(() => this.focusMain()))
		},
		reload() {
			this.scheduleReload()
		},
		scheduleReload() {
			clearTimeout(this.reloadTimer)
			this.reloadTimer = setTimeout(async () => {
				await this.load()
				if (!(this.$socket && this.$socket.client && this.$socket.client.connected)) this.pollLive()
			}, RELOAD_DEBOUNCE_MS)
		},
		setJobs(jobs) {
			this.jobs = jobs || []
			this.jobVersion++
			// Drop live stats of runs that aren't active any more.
			const active = new Set(this.jobs.filter(j => j.active_run && isActiveStatus(j.active_run.status)).map(j => j.active_run.id))
			for (const id of Object.keys(this.live)) if (!active.has(id)) this.$delete(this.live, id)
			this.loadCronInfo()
		},
		// One /cron/preview per distinct expression, cached for the session:
		// the human wording comes from the server (server time, same parser).
		loadCronInfo() {
			for (const cron of distinctCrons(this.jobs)) {
				if (this.cronInfo[cron]) continue
				this.$set(this.cronInfo, cron, { valid: true, human_key: '', args: {}, pending: true })
				this.bkApi
					.cronPreview(cron)
					.then(info => this.$set(this.cronInfo, cron, info))
					.catch(() => this.$delete(this.cronInfo, cron))
			}
		},
		// Socket down: fetch progress of running runs directly.
		async pollLive() {
			for (const job of runningItems(this.jobs)) {
				if (job.active_run.status !== 'running') continue
				try {
					const run = await this.bkApi.getRun(job.active_run.id)
					if (run.live) this.$set(this.live, run.id, run.live)
				} catch (e) {
					// The next poll tries again.
				}
			}
		},
		onBusChange(kind) {
			if (kind === 'run') this.runVersion++
			this.scheduleReload()
		},
		replaceJob(detail) {
			const i = this.jobs.findIndex(j => j.id === detail.id)
			if (i >= 0) this.jobs.splice(i, 1, detail)
			this.jobVersion++
		},

		// --- navigation -----------------------------------------------
		goSection(id) {
			if (!BACKUP_SECTIONS.includes(id)) return
			this.activeSection = id
			this.$nextTick(() => this.focusMain())
		},
		focusMain() {
			const main = this.$refs.main
			if (!main) return
			const target = main.querySelector('[data-autofocus]') || main.querySelector('h2')
			if (target) {
				if (target.tagName === 'H2' && !target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1')
				target.focus()
			}
		},
		showJob(id) {
			this.activeSection = 'jobs'
			this.selectedJobId = id || ''
		},
		async applyDeepLink() {
			// Props are the request; activeSection is what's shown. The
			// request is consumed: OPEN_WINDOW merges props, so a later deep
			// link would otherwise replay this one's wizard or run.
			const p = { ...this.$props }
			if (p.jobId || p.runId || p.wizard || p.section) {
				this.$store.commit('UPDATE_WINDOW_PROPS', { id: BACKUP_APP_ID, props: { section: '', jobId: '', runId: '', wizard: false, preset: '', sourcePath: '', destRef: null, destPath: '' } })
			}
			if (p.section && BACKUP_SECTIONS.includes(p.section)) this.activeSection = p.section
			if (p.jobId) {
				if (this.activeSection === 'restore') this.restoreJobId = p.jobId
				else if (this.activeSection === 'activity') this.activityJobId = p.jobId
				else this.showJob(p.jobId)
			}
			if (p.runId) {
				const job = this.jobs.find(j => j.id === p.jobId)
				this.openRun(p.runId, job || null)
			}
			if (p.wizard) {
				const opts = { preset: p.preset || '' }
				try {
					if (p.sourcePath) {
						const res = await this.bkApi.resolvePath(p.sourcePath)
						if (res && res.ok) opts.sourceEndpoint = res.endpoint
						else this.toast(this.$t('backup.app.source_not_backupable'), 'is-danger')
					}
					if (p.destRef && p.destRef.kind && p.destRef.ref_id) {
						const locations = await this.bkApi.locations('dest')
						const loc = (locations || []).find(l => l.kind === p.destRef.kind && l.ref_id === p.destRef.ref_id)
						if (loc) opts.destEndpoint = { kind: loc.kind, ref_id: loc.ref_id, match: loc.match, sub_path: '', label: loc.label }
					}
					if (p.destPath && !opts.destEndpoint) {
						// A drive's mount point (Storage's "Back up to this
						// drive"): the engine knows its identity, the UI doesn't.
						const res = await this.bkApi.resolvePath(p.destPath)
						if (res && res.ok) opts.destEndpoint = res.endpoint
						else this.toast(this.$t('backup.app.dest_not_usable'), 'is-danger')
					}
				} catch (e) {
					this.toastError(e)
				}
				this.openWizard(opts)
			}
		},

		// --- windows --------------------------------------------------
		openWizard({ jobId = '', preset = '', startStep = '', sourceEndpoint = null, destEndpoint = null } = {}) {
			const job = jobId ? this.jobs.find(j => j.id === jobId) : null
			const props = { jobId: jobId || null, preset: preset || '', sourceEndpoint, destEndpoint }
			if (startStep) props.startStep = startStep
			this.openBackupWindow('wizard', { ...props, jobName: job ? job.name : '' })
		},
		openRun(runId, job) {
			if (!runId) return
			this.openBackupWindow('run', { runId, jobId: (job && job.id) || '', jobName: (job && job.name) || '' })
		},
		openPreview(runId, job) {
			this.openBackupWindow('preview', { runId, jobId: job.id, jobName: job.name })
		},
		openBrowse(job, version) {
			this.openBackupWindow('browse', { jobId: job.id, versionId: version.id, jobName: job.name })
		},

		// --- job actions ----------------------------------------------
		async runNow(job) {
			if (this.busyJobId) return
			this.busyJobId = job.id
			try {
				const { run_id: runId } = await this.bkApi.runJob(job.id)
				this.announce(this.$t('backup.jobs.started', { name: job.name }))
				this.openRun(runId, job)
				this.scheduleReload()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busyJobId = ''
			}
		},
		async previewRun(job) {
			if (this.busyJobId) return
			this.busyJobId = job.id
			try {
				const { run_id: runId } = await this.bkApi.runJob(job.id, { preview: true })
				this.openPreview(runId, job)
				this.scheduleReload()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busyJobId = ''
			}
		},
		cancelRun(runId, job) {
			const apps = []
			for (const h of (job && job.hooks) || []) if (h.phase === 'pre' && h.action === 'stop_apps') apps.push(...(h.apps || []))
			const vms = ((job && job.hooks) || []).filter(h => h.phase === 'pre' && h.action === 'shutdown_vm').map(h => h.vm)
			const lines = [this.$t('backup.run.cancel_confirm')]
			if (apps.length) lines.push(this.$t('backup.run.cancel_confirm_apps', { apps: this.fmt.list(apps) }))
			if (vms.length) lines.push(this.$t('backup.run.cancel_confirm_vms', { vms: this.fmt.list(vms) }))
			this.confirmWindow({
				title: this.$t('backup.run.cancel_title', { name: (job && job.name) || '' }),
				message: lines.map(escapeHtml).join('<br>'),
				confirmText: this.$t('backup.run.cancel'),
				cancelText: this.$t('backup.run.keep_running'),
				type: 'is-danger',
				icon: 'stop-circle-outline',
				onConfirm: async () => {
					try {
						await this.bkApi.cancelRun(runId)
						this.scheduleReload()
					} catch (e) {
						if (e.code !== 'invalid_state') this.toastError(e)
						this.scheduleReload()
					}
				}
			})
		},
		async toggleJob(job) {
			if (this.busyJobId) return
			this.busyJobId = job.id
			try {
				const updated = await this.bkApi.toggleJob(job.id, !job.enabled)
				this.replaceJob(updated)
				this.announce(this.$t(updated.enabled ? 'backup.jobs.resumed' : 'backup.jobs.paused', { name: job.name }))
			} catch (e) {
				this.toastError(e)
			} finally {
				this.busyJobId = ''
			}
		},
		jobMenu(action, job) {
			switch (action) {
				case 'edit':
					return this.openWizard({ jobId: job.id, startStep: 'review' })
				case 'duplicate':
					return this.duplicateJob(job)
				case 'preview':
					return this.previewRun(job)
				case 'pause':
				case 'resume':
					if ((action === 'pause') === job.enabled) return this.toggleJob(job)
					return undefined
				case 'delete':
					return this.deleteJob(job)
			}
			return undefined
		},
		// A duplicate is created paused, so it never runs before the user
		// has looked at it, and opens in the wizard.
		async duplicateJob(job) {
			const copy = JSON.parse(JSON.stringify(job))
			for (const k of ['id', 'revision', 'created_at', 'updated_at', 'health', 'last_run', 'active_run', 'next_run', 'dest_online', 'stats', 'migrated_from', 'needs_attention']) delete copy[k]
			copy.name = this.$t('backup.jobs.copy_name', { name: job.name })
			copy.enabled = false
			copy.needs_attention = ''
			copy.migrated_from = null
			try {
				const created = await this.bkApi.createJob(copy)
				this.jobs.push(created)
				this.openWizard({ jobId: created.id, startStep: 'review' })
				this.scheduleReload()
			} catch (e) {
				this.toastError(e)
			}
		},
		deleteJob(job) {
			this.confirmWindow({
				title: this.$t('backup.jobs.delete_title', { name: job.name }),
				message: escapeHtml(this.$t('backup.jobs.delete_message')),
				confirmText: this.$t('backup.jobs.menu.delete'),
				type: 'is-danger',
				checkbox: {
					label: this.$t('backup.jobs.delete_purge'),
					checked: false,
					checkedHint: this.$t('backup.jobs.delete_purge_on'),
					uncheckedHint: this.$t('backup.jobs.delete_purge_off'),
					checkedIsDanger: true
				},
				onConfirm: async purgeData => {
					try {
						await this.bkApi.deleteJob(job.id, { purgeData: !!purgeData })
						this.jobs = this.jobs.filter(j => j.id !== job.id)
						if (this.selectedJobId === job.id) this.selectedJobId = ''
						this.announce(this.$t('backup.jobs.deleted', { name: job.name }))
						this.scheduleReload()
					} catch (e) {
						this.toast(e.code === 'invalid_state' ? this.$t('backup.jobs.delete_running') : this.errText(e), 'is-danger')
					}
				}
			})
		},
		reconnectDest(job, adoptOtherJob = false) {
			const dest = (job.dest && job.dest.label) || ''
			this.confirmWindow({
				title: this.$t('backup.action.reconnect_dest'),
				message: escapeHtml(this.$t(adoptOtherJob ? 'backup.jobs.reconnect_other_message' : 'backup.jobs.reconnect_message', { dest })),
				confirmText: this.$t(adoptOtherJob ? 'backup.jobs.reconnect_other_confirm' : 'backup.action.reconnect_dest'),
				type: adoptOtherJob ? 'is-danger' : 'is-warning',
				onConfirm: async () => {
					try {
						this.replaceJob(await this.bkApi.reconnectDest(job.id, undefined, { adoptOtherJob }))
						this.scheduleReload()
					} catch (e) {
						// The folder holds another job's backup: ask again, with
						// what taking it over means.
						if (!adoptOtherJob && e.code === 'dest_marker_mismatch') {
							this.reconnectDest(job, true)
							return
						}
						this.toastError(e)
					}
				}
			})
		},
		// runAction carries out one of errorCodes.js ACTIONS for a job.
		runAction(action, job, runId) {
			switch (action) {
				case 'view_log':
					return runId ? this.openRun(runId, job) : this.showJob(job.id)
				case 'retry':
					return this.runNow(job)
				case 'review':
					return runId ? this.openPreview(runId, job) : this.showJob(job.id)
				case 'reconnect_dest':
					return this.reconnectDest(job)
				case 'fix_sign_in':
					return this.$store.commit('OPEN_WINDOW', { id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', props: { section: 'cloud' }, width: 760, height: 540 })
				case 'open_storage':
					return this.$store.commit('OPEN_WINDOW', { id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', props: { section: 'storage' }, width: 760, height: 540 })
				case 'sign_in':
					// The session is gone: the app's own logout flow signs in again.
					return this.$router.replace({ path: '/logout' }).catch(() => {})
				default:
					if (WIZARD_STEP_FOR[action]) return this.openWizard({ jobId: job.id, startStep: WIZARD_STEP_FOR[action] })
			}
			return undefined
		},
		announce(text) {
			this.announcement = ''
			this.$nextTick(() => (this.announcement = text))
		},

		// --- keyboard (spec §13): N, /, R, ? - never while typing ---------
		onKeydown(e) {
			if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return
			const t = e.target
			if (t && (t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName))) return
			if (this.status !== 'ready') return
			if (e.key === 'n' || e.key === 'N') {
				e.preventDefault()
				this.openWizard({})
			} else if (e.key === '/') {
				e.preventDefault()
				this.activeSection = 'jobs'
				this.$nextTick(() => this.$refs.jobsSection && this.$refs.jobsSection.focusSearch())
			} else if (e.key === 'r' || e.key === 'R') {
				const job = this.jobs.find(j => j.id === this.selectedJobId)
				if (job && this.activeSection === 'jobs' && !(job.active_run && isActiveStatus(job.active_run.status))) {
					e.preventDefault()
					this.runNow(job)
				}
			} else if (e.key === '?') {
				e.preventDefault()
				this.showShortcuts = !this.showShortcuts
				if (this.showShortcuts) this.$nextTick(() => this.$refs.shortcutsClose && this.$refs.shortcutsClose.focus())
			} else if (e.key === 'Escape' && this.showShortcuts) {
				e.preventDefault()
				this.showShortcuts = false
			}
		}
	}
}
</script>

<style lang="scss" src="./backup-common.scss"></style>

<style lang="scss" scoped>
.bk-app {
	display: flex;
	width: 100%;
	height: 100%;
	min-height: 0;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
	font-size: var(--font-base);
	&.is-tabs {
		flex-direction: column;
	}
}
.bk-main {
	position: relative;
	flex: 1 1 auto;
	min-width: 0;
	min-height: 0;
	overflow-y: auto;
	overflow-x: hidden;
}
.bk-w-phone ::v-deep .bk-section {
	padding: var(--space-3);
}
.bk-offline {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	min-height: 100%;
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-secondary);
	p {
		margin: 0;
		max-width: 32rem;
	}
	.icon {
		color: var(--theme-text-muted);
	}
}
.bk-offline-title {
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-primary);
}
.bk-offline-text {
	font-size: var(--font-sm);
}
.bk-shortcuts {
	position: absolute;
	right: var(--space-4);
	bottom: var(--space-4);
	z-index: 30;
	max-width: calc(100% - 2 * var(--space-4));
	box-shadow: var(--shadow-md);
	dl {
		margin: 0 0 var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		> div {
			display: flex;
			align-items: center;
			gap: var(--space-3);
		}
	}
	dt {
		min-width: 2rem;
	}
	dd {
		margin: 0;
		font-size: var(--font-sm);
	}
	kbd {
		display: inline-block;
		min-width: 1.5rem;
		padding: 0 var(--space-1);
		border: 1px solid var(--theme-card-border);
		border-radius: var(--radius-xs);
		background: var(--theme-bg-subtle);
		font-family: inherit;
		font-size: var(--font-xs);
		text-align: center;
	}
}
</style>
