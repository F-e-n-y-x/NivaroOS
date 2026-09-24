<!-- A job's detail pane (spec §12.3): Summary, History, Versions and
     Settings tabs. Beside the list when the window is wide; a full view
     with a Back button when it's narrow. -->
<template>
	<section class="bk-detail" :aria-labelledby="titleId">
		<header class="bk-detail-head">
			<button v-if="narrow" type="button" class="bk-btn is-link bk-detail-back" data-autofocus @click="$emit('close')">
				<b-icon icon="arrow-left" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.jobs.back') }}</span>
			</button>
			<h3 :id="titleId" ref="title" tabindex="-1" class="bk-detail-title">{{ job.name }}</h3>
			<button v-if="!narrow" type="button" class="bk-icon-btn" :aria-label="$t('backup.jobs.close_details')" :title="$t('backup.jobs.close_details')" @click="$emit('close')">
				<b-icon icon="close" pack="mdi" custom-size="mdi-20px"></b-icon>
			</button>
		</header>

		<div class="bk-tabs" role="tablist" :aria-label="$t('backup.jobs.details_tabs')" @keydown="onTabKeydown">
			<button
				v-for="tab in tabs"
				:id="`${uid}-tab-${tab}`"
				:key="tab"
				type="button"
				role="tab"
				class="bk-tab"
				:aria-selected="active === tab ? 'true' : 'false'"
				:aria-controls="`${uid}-panel`"
				:tabindex="active === tab ? 0 : -1"
				:data-tab="tab"
				@click="active = tab"
			>
				{{ $t('backup.jobs.tab.' + tab) }}
			</button>
		</div>

		<div :id="`${uid}-panel`" class="bk-detail-body" role="tabpanel" :aria-labelledby="`${uid}-tab-${active}`">
			<p v-if="loadError" class="bk-inline-error" role="alert">{{ errText(loadError) }}</p>

			<!-- Summary -->
			<div v-if="active === 'summary'" class="bk-detail-summary">
				<dl class="bk-facts">
					<div>
						<dt>{{ $t('backup.jobs.fact.what') }}</dt>
						<dd>{{ sourcesText }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.where') }}</dt>
						<dd>{{ endpointText(job.dest) }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.how') }}</dt>
						<dd>{{ $t(`backup.type.${job.type}.label`) }}: {{ $t(`backup.type.${job.type}.promise`) }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.when') }}</dt>
						<dd>{{ whenText }}</dd>
					</div>
					<div v-if="keepText">
						<dt>{{ $t('backup.jobs.fact.keep') }}</dt>
						<dd>{{ keepText }}</dd>
					</div>
					<div v-if="detail">
						<dt>{{ $t('backup.jobs.fact.size') }}</dt>
						<dd>{{ sizeText }}</dd>
					</div>
					<div v-if="detail">
						<dt>{{ $t('backup.jobs.fact.last_success') }}</dt>
						<dd>{{ detail.stats.last_success ? fmt.when(detail.stats.last_success) : $t('backup.overview.no_success_yet') }}</dd>
					</div>
				</dl>
				<div class="bk-detail-actions">
					<button type="button" class="bk-btn is-primary" :disabled="!!activeRun" @click="$emit('run', job)">
						<b-icon icon="play" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
						<span>{{ $t('backup.jobs.run_now') }}</span>
					</button>
					<button v-if="activeRun" type="button" class="bk-btn" @click="$emit('open-run', job)">{{ $t('backup.jobs.open_progress') }}</button>
					<button type="button" class="bk-btn" :disabled="!!activeRun" @click="$emit('menu', 'preview', job)">{{ $t('backup.jobs.menu.preview') }}</button>
					<button type="button" class="bk-btn" @click="$emit('menu', 'edit', job)">{{ $t('backup.jobs.menu.edit') }}</button>
				</div>
			</div>

			<!-- History -->
			<div v-else-if="active === 'history'">
				<p v-if="runsLoading" class="bk-secondary">{{ $t('backup.loading') }}</p>
				<p v-else-if="!runs.length" class="bk-secondary">{{ $t('backup.jobs.no_history') }}</p>
				<activity-list v-else :runs="runs" :fmt="fmt" :show-job="false" @open="r => $emit('open-run-id', r)"></activity-list>
				<button v-if="nextBefore" type="button" class="bk-btn bk-more" :disabled="runsLoading" @click="loadRuns(true)">{{ $t('backup.activity.more') }}</button>
			</div>

			<!-- Versions -->
			<div v-else-if="active === 'versions'">
				<p v-if="versionsLoading" class="bk-secondary">{{ $t('backup.loading') }}</p>
				<version-list
					v-else
					:versions="versions"
					:fmt="fmt"
					:job-type="job.type"
					:current-at="(detail && detail.stats.last_success) || ''"
					@browse="v => $emit('browse', job, v)"
				></version-list>
			</div>

			<!-- Settings -->
			<div v-else-if="active === 'settings'" class="bk-detail-settings">
				<dl class="bk-facts">
					<div>
						<dt>{{ $t('backup.jobs.fact.skip') }}</dt>
						<dd>{{ skipText }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.only_when') }}</dt>
						<dd>{{ conditionsText }}</dd>
					</div>
					<div v-if="job.type !== 'copy'">
						<dt>{{ $t('backup.jobs.fact.safety') }}</dt>
						<dd>{{ $t('backup.jobs.safety', { delete: job.guards.delete_pct, change: job.guards.change_pct }) }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.apps') }}</dt>
						<dd>{{ hooksText }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.retry') }}</dt>
						<dd>{{ $tc('backup.jobs.retry', job.retry.max, { count: job.retry.max }) }}</dd>
					</div>
					<div>
						<dt>{{ $t('backup.jobs.fact.verify') }}</dt>
						<dd>{{ job.options.verify ? $t('backup.jobs.yes') : $t('backup.jobs.no') }}</dd>
					</div>
				</dl>
				<div class="bk-detail-actions">
					<button type="button" class="bk-btn is-primary" @click="$emit('menu', 'edit', job)">{{ $t('backup.jobs.menu.edit') }}</button>
					<button type="button" class="bk-btn" @click="$emit('menu', job.enabled ? 'pause' : 'resume', job)">{{ job.enabled ? $t('backup.jobs.menu.pause') : $t('backup.jobs.menu.resume') }}</button>
					<button type="button" class="bk-btn is-danger" @click="$emit('menu', 'delete', job)">{{ $t('backup.jobs.menu.delete') }}</button>
				</div>
			</div>
		</div>
	</section>
</template>

<script>
import ActivityList from './ActivityList.vue'
import VersionList from './VersionList.vue'
import { backupMixin } from '../backupMixin'
import { retentionMessage, jobTriggers, isActiveStatus } from '../state'

let seq = 0
const TABS = ['summary', 'history', 'versions', 'settings']

export default {
	name: 'JobDetailsPane',
	components: { ActivityList, VersionList },
	mixins: [backupMixin],
	props: {
		job: { type: Object, required: true },
		cronInfo: { type: Object, default: () => ({}) },
		narrow: { type: Boolean, default: false },
		// Bumped by the app when this job changed (event or action).
		version: { type: Number, default: 0 }
	},
	data() {
		const uid = `bk-detail-${++seq}`
		return {
			uid,
			titleId: `${uid}-title`,
			tabs: TABS,
			active: 'summary',
			detail: null,
			loadError: null,
			runs: [],
			nextBefore: '',
			runsLoading: false,
			versions: [],
			versionsLoading: false
		}
	},
	computed: {
		activeRun() {
			const r = this.job.active_run
			return r && isActiveStatus(r.status) ? r : null
		},
		sourcesText() {
			return this.fmt.list((this.job.sources || []).map(s => this.endpointText(s)))
		},
		whenText() {
			const parts = jobTriggers(this.job).map(t => (t.kind === 'volume_mounted' ? this.$t('backup.jobs.on_plug_in') : this.cronText(t.cron)))
			if (!parts.length) return this.$t('backup.jobs.manual_only')
			let text = parts.filter(Boolean).join(' · ')
			if (this.job.enabled && this.job.next_run) text += ` · ${this.$t('backup.jobs.next_run', { when: this.fmt.when(this.job.next_run) })}`
			if (!this.job.enabled) text += ` · ${this.$t('backup.health.disabled')}`
			return text
		},
		keepText() {
			const m = retentionMessage(this.job)
			return m ? this.msg(m) : this.job.type === 'copy' ? this.$t('backup.keep.copy_never_deletes') : ''
		},
		sizeText() {
			const s = this.detail.stats
			const parts = [s.size_bytes ? this.fmt.bytes(s.size_bytes) : this.$t('backup.jobs.size_unknown')]
			if (s.versions_count) parts.push(this.$tc('backup.jobs.versions_count', s.versions_count, { count: this.fmt.number(s.versions_count) }))
			return parts.join(' · ')
		},
		skipText() {
			const f = this.job.filters || {}
			const items = (f.exclude_presets || []).map(p => this.$t('backup.exclude.' + p)).concat(f.exclude || [])
			if (f.max_size_bytes) items.push(this.$t('backup.jobs.skip_larger', { size: this.fmt.bytes(f.max_size_bytes) }))
			return items.length ? this.fmt.list(items) : this.$t('backup.jobs.skip_nothing')
		},
		conditionsText() {
			const c = this.job.conditions || {}
			const parts = []
			if (c.dest_available) parts.push(this.$t('backup.jobs.cond_dest'))
			if (c.window && c.window.start && c.window.end) parts.push(this.$t('backup.jobs.cond_window', { start: c.window.start, end: c.window.end }))
			if (!parts.length) return this.$t('backup.jobs.cond_none')
			if (c.when_unmet) parts.push(this.$t('backup.jobs.when_unmet.' + c.when_unmet))
			return parts.join(' · ')
		},
		hooksText() {
			const hooks = (this.job.hooks || []).filter(h => h.phase === 'pre')
			if (!hooks.length) return this.$t('backup.jobs.hooks_none')
			return hooks
				.map(h => (h.action === 'stop_apps' ? this.$t('backup.jobs.hook_stop_apps', { apps: this.fmt.list(h.apps || []) }) : this.$t('backup.jobs.hook_shutdown_vm', { vm: h.vm })))
				.join(' · ')
		}
	},
	watch: {
		'job.id': {
			immediate: true,
			handler() {
				this.active = 'summary'
				this.detail = null
				this.runs = []
				this.versions = []
				this.reload()
			}
		},
		version() {
			this.reload()
		},
		active() {
			this.loadTab()
		}
	},
	// Focus lands in the pane when it opens: its title beside the list,
	// its Back button when it replaces the list.
	mounted() {
		const back = this.narrow && this.$el.querySelector('.bk-detail-back')
		if (back) back.focus()
		else if (this.$refs.title) this.$refs.title.focus()
	},
	methods: {
		endpointText(ep) {
			if (!ep) return ''
			const where = ep.label || this.$t('backup.ep.' + ep.kind)
			return ep.sub_path ? `${where} › ${ep.sub_path}` : where
		},
		cronText(cron) {
			const info = this.cronInfo[cron]
			if (!info) return cron
			// Still asking the server: say nothing rather than the raw cron.
			if (info.pending) return ''
			if (!info.valid || !info.human_key) return this.$t('backup.cron.custom', { expr: cron })
			return this.$t(info.human_key, info.args || {})
		},
		async reload() {
			const id = this.job.id
			this.loadError = null
			try {
				const d = await this.bkApi.getJob(id)
				if (id === this.job.id) this.detail = d
			} catch (e) {
				if (id === this.job.id) this.loadError = e
			}
			this.loadTab()
		},
		loadTab() {
			if (this.active === 'history') this.loadRuns(false)
			else if (this.active === 'versions') this.loadVersions()
		},
		async loadRuns(more) {
			const id = this.job.id
			this.runsLoading = true
			try {
				const page = await this.bkApi.listRuns({ jobId: id, limit: 20, before: more ? this.nextBefore : undefined })
				if (id !== this.job.id) return
				this.runs = more ? this.runs.concat(page.runs || []) : page.runs || []
				this.nextBefore = page.next_before || ''
			} catch (e) {
				if (id === this.job.id) this.loadError = e
			} finally {
				this.runsLoading = false
			}
		},
		async loadVersions() {
			const id = this.job.id
			this.versionsLoading = true
			try {
				const v = await this.bkApi.listVersions(id)
				if (id === this.job.id) this.versions = v || []
			} catch (e) {
				if (id === this.job.id) this.loadError = e
			} finally {
				this.versionsLoading = false
			}
		},
		onTabKeydown(e) {
			const i = TABS.indexOf(this.active)
			let next = -1
			if (e.key === 'ArrowRight') next = (i + 1) % TABS.length
			else if (e.key === 'ArrowLeft') next = (i - 1 + TABS.length) % TABS.length
			else if (e.key === 'Home') next = 0
			else if (e.key === 'End') next = TABS.length - 1
			if (next < 0) return
			e.preventDefault()
			this.active = TABS[next]
			this.$nextTick(() => {
				const el = this.$el.querySelector(`[data-tab="${TABS[next]}"]`)
				if (el) el.focus()
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-detail {
	display: flex;
	flex-direction: column;
	min-width: 0;
	background: var(--theme-card-bg);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
}
.bk-detail-head {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4) 0;
}
.bk-detail-back {
	padding-left: 0;
}
.bk-detail-title {
	flex: 1 1 12rem;
	margin: 0;
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-primary);
	overflow-wrap: anywhere;
	&:focus {
		outline: none;
	}
}
.bk-tabs {
	display: flex;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3) 0;
	border-bottom: 1px solid var(--theme-card-border);
	overflow-x: auto;
}
.bk-tab {
	flex-shrink: 0;
	min-height: 2.5rem;
	padding: 0 var(--space-3);
	border: none;
	border-bottom: 2px solid transparent;
	background: transparent;
	color: var(--theme-text-secondary);
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 500;
	cursor: pointer;
	&[aria-selected='true'] {
		color: var(--color-primary-fg);
		border-bottom-color: var(--color-primary-fg);
	}
	&:focus-visible {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: -2px;
	}
}
.bk-detail-body {
	padding: var(--space-4);
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	p {
		margin: 0;
	}
}
.bk-facts {
	margin: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	> div {
		display: grid;
		grid-template-columns: minmax(6rem, 9rem) minmax(0, 1fr);
		gap: var(--space-3);
	}
	dt {
		font-size: var(--font-sm);
		color: var(--theme-text-secondary);
	}
	dd {
		margin: 0;
		font-size: var(--font-sm);
		color: var(--theme-text-primary);
		overflow-wrap: anywhere;
	}
}
.bk-w-phone .bk-facts > div {
	grid-template-columns: minmax(0, 1fr);
	gap: 0;
}
.bk-detail-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	margin-top: var(--space-3);
}
.bk-more {
	align-self: center;
	margin-top: var(--space-3);
}
.bk-inline-error {
	color: var(--color-danger-fg);
	font-size: var(--font-sm);
}
</style>
