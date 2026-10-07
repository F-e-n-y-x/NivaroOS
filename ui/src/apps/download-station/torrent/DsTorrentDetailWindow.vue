<!-- src/apps/download-station/torrent/DsTorrentDetailWindow.vue -->
<!-- One torrent: live stats, its files (pick which to download and their
     priority), trackers, sequential / first-last piece, and pause /
     resume / recheck / remove. -->
<template>
	<div class="ds-tdetail">
		<div v-if="!t" class="detail-status">
			<b-icon v-if="!gone" icon="loading" custom-class="mdi-spin" custom-size="mdi-32px"></b-icon>
			<p v-else>{{ $t('This torrent was removed.') }}</p>
		</div>
		<template v-else>
			<div class="detail-body scrollbars-light">
				<div class="detail-head">
					<div class="head-name" :title="t.name">{{ t.name }}</div>
					<div class="head-sub">
						<span class="ds-state-badge" :class="badgeClass(t.state)">{{ $t(stateLabel(t.state)) }}</span>
						<span v-if="t.private" class="priv-pill"><b-icon icon="lock-outline" custom-size="mdi-12px"></b-icon>{{ $t('Private') }}</span>
						<span>{{ percent(t).toFixed(1) }}%</span>
					</div>
					<div class="ds-progress" :class="{ 'is-completed': t.progress >= 1, 'is-paused': isPaused(t) && t.progress < 1, 'is-failed': t.state === 'error' }">
						<div class="ds-progress-fill" :style="{ transform: `translateX(${percent(t) - 100}%)` }"></div>
					</div>
					<p v-if="t.error" class="ds-error-text">{{ t.error }}</p>
				</div>

				<div class="actions">
					<button v-if="isPaused(t)" class="ds-primary-btn" @click="act('resume')"><b-icon icon="play" custom-size="mdi-18px"></b-icon><span>{{ $t('Resume') }}</span></button>
					<button v-else class="ds-secondary-btn" @click="act('pause')"><b-icon icon="pause" custom-size="mdi-18px"></b-icon><span>{{ $t('Pause') }}</span></button>
					<button class="ds-secondary-btn" :disabled="!t.has_metadata" @click="act('recheck')"><b-icon icon="check-all" custom-size="mdi-18px"></b-icon><span>{{ $t('Recheck') }}</span></button>
					<button v-if="t.dir" class="ds-secondary-btn" @click="openFolder(t.dir)"><b-icon icon="folder-open-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Show in Files') }}</span></button>
					<button class="ds-secondary-btn is-danger-text" @click="confirmRemove"><b-icon icon="delete-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Remove') }}</span></button>
				</div>

				<dl class="stats">
					<div><dt>{{ $t('Size') }}</dt><dd>{{ formatBytes(t.size) }}</dd></div>
					<div><dt>{{ $t('Done') }}</dt><dd>{{ formatBytes(t.done) }}</dd></div>
					<div><dt>{{ $t('Download speed') }}</dt><dd>{{ formatSpeed(t.dl_speed) || '—' }}</dd></div>
					<div><dt>{{ $t('Upload speed') }}</dt><dd>{{ formatSpeed(t.up_speed) || '—' }}</dd></div>
					<div><dt>{{ $t('Downloaded') }}</dt><dd>{{ formatBytes(t.downloaded) }}</dd></div>
					<div><dt>{{ $t('Uploaded') }}</dt><dd>{{ formatBytes(t.uploaded) }}</dd></div>
					<div><dt>{{ $t('Seeds') }}</dt><dd>{{ t.seeds }} ({{ t.seeds_total }})</dd></div>
					<div><dt>{{ $t('Peers') }}</dt><dd>{{ t.peers }} ({{ t.peers_total }})</dd></div>
					<div><dt>{{ $t('Share ratio') }}</dt><dd>{{ formatRatio(t.ratio) }}</dd></div>
					<div><dt>{{ $t('Time left') }}</dt><dd>{{ t.eta >= 0 && t.progress < 1 ? formatEta(t.eta) : '—' }}</dd></div>
					<div><dt>{{ $t('Seeding time') }}</dt><dd>{{ t.seeding_time ? formatEta(t.seeding_time) : '—' }}</dd></div>
					<div><dt>{{ $t('Category') }}</dt><dd>{{ t.category || '—' }}</dd></div>
					<div class="wide"><dt>{{ $t('Save folder') }}</dt><dd class="mono">{{ t.dir || '—' }}</dd></div>
					<div class="wide"><dt>{{ $t('Info hash') }}</dt><dd class="mono">{{ t.hash }}</dd></div>
				</dl>

				<div class="opts">
					<b-switch :value="t.sequential" @input="v => setOptions(v, t.first_last)">{{ $t('Download in sequential order') }}</b-switch>
					<b-switch :value="t.first_last" @input="v => setOptions(t.sequential, v)">{{ $t('Download first and last pieces first') }}</b-switch>
				</div>

				<div class="segmented-control tabs-ctl">
					<button class="segmented-option" :class="{ active: tab === 'files' }" @click="tab = 'files'">{{ $t('Files') }} <span class="seg-count">{{ t.files.length }}</span></button>
					<button class="segmented-option" :class="{ active: tab === 'trackers' }" @click="tab = 'trackers'">{{ $t('Trackers') }} <span class="seg-count">{{ t.trackers.length }}</span></button>
				</div>

				<div v-if="tab === 'files'" class="files">
					<p v-if="!t.files.length" class="ds-hint">{{ $t('The file list appears once the metadata has arrived.') }}</p>
					<template v-else>
						<div class="files-bar">
							<button class="ds-secondary-btn" @click="setPrio(t.files.map(f => f.index), 1)">{{ $t('Select all') }}</button>
							<button class="ds-secondary-btn" @click="setPrio(t.files.map(f => f.index), 0)">{{ $t('Select none') }}</button>
						</div>
						<div v-for="f in t.files" :key="f.index" class="file-row">
							<b-checkbox :value="f.priority > 0" :aria-label="$t('Download {name}', { name: f.name })" @input="v => setPrio([f.index], v ? 1 : 0)"></b-checkbox>
							<div class="file-main">
								<div class="one-line file-name" :title="f.name">{{ f.name }}</div>
								<div class="file-meta">{{ formatBytes(f.size) }} · {{ (f.progress * 100).toFixed(1) }}%</div>
							</div>
							<div class="select is-small">
								<select :value="f.priority || 1" :disabled="!f.priority" :aria-label="$t('Priority of {name}', { name: f.name })" @change="e => setPrio([f.index], Number(e.target.value))">
									<option v-for="p in PRIORITIES" :key="p.value" :value="p.value">{{ $t(p.label) }}</option>
								</select>
							</div>
						</div>
					</template>
				</div>
				<div v-else class="trackers">
					<p v-if="!t.trackers.length" class="ds-hint">{{ $t('No trackers - peers come from DHT and PeX.') }}</p>
					<div v-for="(tr, i) in t.trackers" :key="i" class="tracker-row">
						<span class="tr-dot" :class="'is-' + tr.status" :title="$t(trackerStatus(tr.status))"></span>
						<div class="file-main">
							<div class="one-line mono" :title="tr.url">{{ tr.url }}</div>
							<div class="file-meta">{{ $t(trackerStatus(tr.status)) }}<template v-if="tr.peers > 0"> · {{ $t('{n} peers', { n: tr.peers }) }}</template><template v-if="tr.message"> · {{ tr.message }}</template></div>
						</div>
					</div>
				</div>
			</div>
		</template>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes, formatSpeed, formatEta } from '@/api/downloadSidecar'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { escapeHtml } from '@/utils/escapeHtml'
import { openFolderWindow } from '@/utils/files/openFolder'
import { isPaused, percent, formatRatio, stateLabel, badgeClass, PRIORITIES } from './torrentUtil'

const TRACKER_STATUS = { working: 'Working', updating: 'Updating', not_working: 'Not working', not_contacted: 'Not contacted yet', disabled: 'Disabled' }

export default {
	name: 'DsTorrentDetailWindow',
	mixins: [confirmWindowMixin],
	props: {
		hash: { type: String, required: true }
	},
	data() {
		return { t: null, gone: false, tab: 'files', timer: null, destroyed: false, PRIORITIES }
	},
	created() {
		this.poll()
	},
	beforeDestroy() {
		this.destroyed = true
		clearTimeout(this.timer)
	},
	methods: {
		formatBytes,
		formatSpeed,
		formatEta,
		isPaused,
		percent,
		formatRatio,
		stateLabel,
		badgeClass,
		trackerStatus: s => TRACKER_STATUS[s] || s,
		openFolder(dir) {
			openFolderWindow(this.$store, dir, this.$t('Files'))
		},
		async load() {
			try {
				this.t = await downloadSidecar.getTorrent(this.hash)
			} catch (e) {
				if (e.status === 404) {
					this.t = null
					this.gone = true
				}
			}
		},
		async poll() {
			await this.load()
			if (this.destroyed || this.gone) return
			this.timer = setTimeout(this.poll, 2000)
		},
		toastError(text) {
			this.$buefy.toast.open({ message: escapeHtml(text), type: 'is-danger' })
		},
		async run(fn) {
			try {
				await fn()
			} catch (e) {
				this.toastError(e.message)
			}
			this.load()
		},
		act(action) {
			return this.run(() => downloadSidecar.torrentAction(this.hash, action))
		},
		setOptions(sequential, firstLast) {
			return this.run(() => downloadSidecar.setTorrentOptions(this.hash, sequential, firstLast))
		},
		setPrio(ids, priority) {
			return this.run(() => downloadSidecar.setTorrentFiles(this.hash, ids, priority))
		},
		confirmRemove() {
			const name = this.t.name
			this.confirmWindow({
				title: this.$t('Remove torrent'),
				message: this.$t('Remove {name} from the list?', { name: `<b>${escapeHtml(name)}</b>` }),
				confirmText: this.$t('Remove'),
				type: 'is-danger',
				icon: 'close-circle',
				checkbox: {
					label: this.$t('Also delete the downloaded files'),
					checked: false,
					checkedHint: this.$t('The files are permanently deleted from disk.'),
					uncheckedHint: this.$t('The downloaded files are kept.'),
					checkedIsDanger: true
				},
				onConfirm: checked => this.run(() => downloadSidecar.removeTorrent(this.hash, checked === true))
			})
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';

.ds-tdetail {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
}

.detail-status {
	flex: 1;
	display: flex;
	align-items: center;
	justify-content: center;
	color: var(--theme-text-muted, #64748b);
}

.detail-body {
	flex: 1 1 auto;
	overflow-y: auto;
	padding: var(--space-5);
	display: flex;
	flex-direction: column;
	gap: var(--space-4);
}

.detail-head {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.head-name {
	font-size: var(--font-md);
	font-weight: 600;
	word-break: break-word;
}

.head-sub {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	font-variant-numeric: tabular-nums;
}

.priv-pill {
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	padding: 0.05rem 0.45rem;
	border-radius: var(--radius-pill);
	background: var(--color-warning-soft, rgba(245, 158, 11, 0.12));
	color: var(--color-warning-fg, #b45309);
}

.actions,
.files-bar {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.is-danger-text {
	color: var(--color-danger-fg, #b91c1c);
}

.stats {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
	gap: var(--space-2) var(--space-4);
	margin: 0;
	padding: var(--space-3) var(--space-4);
	border-radius: var(--radius-card);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);

	.wide {
		grid-column: 1 / -1;
	}

	dt {
		font-size: var(--font-2xs);
		color: var(--theme-text-muted, #64748b);
	}

	dd {
		margin: 0;
		font-size: var(--font-sm);
		font-variant-numeric: tabular-nums;
		word-break: break-all;
	}
}

.mono {
	font-family: $family-monospace;
	font-size: var(--font-xs);
}

.opts {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.tabs-ctl {
	align-self: flex-start;
	margin-bottom: 0;
}

.seg-count {
	margin-left: 0.25rem;
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
}

.files,
.trackers {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.file-row,
.tracker-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	background: var(--theme-card-bg, #fff);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
}

.file-main {
	flex: 1 1 auto;
	min-width: 0;
}

.file-name {
	font-size: var(--font-sm);
}

.file-meta {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	font-variant-numeric: tabular-nums;
}

.tr-dot {
	flex-shrink: 0;
	width: 0.6rem;
	height: 0.6rem;
	border-radius: 50%;
	background: var(--theme-text-muted, #94a3b8);

	&.is-working {
		background: var(--color-success-fg, #047857);
	}
	&.is-not_working {
		background: var(--color-danger-fg, #b91c1c);
	}
	&.is-updating {
		background: var(--color-warning-fg, #b45309);
	}
}
</style>
