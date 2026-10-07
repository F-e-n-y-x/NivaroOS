<!-- src/apps/download-station/torrent/DsTorrentList.vue -->
<!-- The Torrents section: every torrent with live progress, speeds, peers,
     ratio and time left, whichever engine runs (the sidecar's torrent API
     is the same for all three). Paste a magnet or drop .torrent files
     anywhere in the list to add them. -->
<template>
	<div class="ds-list" tabindex="-1" @paste="onPaste" @dragover.prevent @drop.prevent="onDrop">
		<div class="ds-list-toolbar">
			<div class="title-wrap">
				<h2 class="ds-section-title">{{ $t('Torrents') }}</h2>
				<span v-if="info" class="engine-chip" :title="$t('Torrent engine - change it in Settings')">
					<span class="engine-dot" :class="{ on: info.running }"></span>{{ engineName(info.engine) }} · {{ info.running ? $t('running') : $t('idle') }}
				</span>
			</div>
			<div class="toolbar-actions">
				<span v-if="info && (info.dl_speed || info.up_speed)" class="totals">
					<b-icon icon="arrow-down" custom-size="mdi-14px"></b-icon>{{ formatSpeed(info.dl_speed) || '0 B/s' }}
					<b-icon icon="arrow-up" custom-size="mdi-14px"></b-icon>{{ formatSpeed(info.up_speed) || '0 B/s' }}
				</span>
				<button class="ds-icon-btn" :class="{ 'is-active': altOn }" :aria-pressed="altOn ? 'true' : 'false'"
					:title="altOn ? $t('Alternative speed limits are on') : $t('Use alternative speed limits')" :aria-label="$t('Alternative speed limits')" @click="toggleAlt">
					<b-icon icon="speedometer-slow" custom-size="mdi-18px"></b-icon>
				</button>
				<button class="ds-primary-btn" @click="openAdd()">
					<b-icon icon="magnet" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Add torrent') }}</span>
				</button>
			</div>
		</div>

		<div class="ds-filters">
			<div class="segmented-control">
				<button v-for="f in FILTERS" :key="f.id" class="segmented-option" :class="{ active: filter === f.id }" :aria-pressed="filter === f.id ? 'true' : 'false'" @click="filter = f.id">
					{{ $t(f.label) }}<span v-if="counts[f.id]" class="seg-count">{{ counts[f.id] }}</span>
				</button>
			</div>
			<div class="ds-search">
				<b-icon icon="magnify" custom-size="mdi-16px"></b-icon>
				<input v-model="query" class="ds-input" :aria-label="$t('Search torrents')" :placeholder="$t('Search torrents')" />
			</div>
		</div>

		<div class="ds-list-body scrollbars-light">
			<div v-if="!info && !error" class="ds-empty">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
			</div>
			<div v-else-if="error && !torrents.length" class="ds-empty" role="alert">
				<b-icon icon="alert-circle-outline" custom-size="mdi-48px"></b-icon>
				<p class="ds-empty-title">{{ $t('The torrent engine is not answering') }}</p>
				<p class="ds-empty-hint">{{ error }}</p>
			</div>
			<div v-else-if="!torrents.length" class="ds-empty">
				<b-icon icon="magnet" custom-size="mdi-48px"></b-icon>
				<p class="ds-empty-title">{{ $t('No torrents yet') }}</p>
				<p class="ds-empty-hint">{{ $t('Paste a magnet link, drop .torrent files here, or add a link to one.') }}</p>
				<div class="ds-empty-actions">
					<button class="ds-primary-btn" @click="openAdd()">
						<b-icon icon="magnet" custom-size="mdi-18px"></b-icon><span>{{ $t('Add torrent') }}</span>
					</button>
				</div>
			</div>
			<div v-else-if="!visible.length" class="ds-empty is-small">
				<p class="ds-empty-hint">{{ $t('Nothing matches this filter.') }}</p>
			</div>

			<div v-else class="ds-rows">
				<div v-for="t in visible" :key="t.hash" class="ds-row" @dblclick="openDetails(t)">
					<div class="row-icon-wrap" :class="t.private ? 'cat-programs' : 'cat-compressed'">
						<b-icon :icon="t.private ? 'lock-outline' : 'magnet'" custom-size="mdi-22px"></b-icon>
					</div>
					<div class="row-main">
						<div class="row-head">
							<span class="row-name" :title="t.name">{{ t.name }}</span>
							<span v-if="t.category" class="cat-pill">{{ t.category }}</span>
							<span class="ds-state-badge" :class="badgeClass(t.state)">{{ t.state === 'downloading' ? percent(t).toFixed(1) + '%' : $t(stateLabel(t.state)) }}</span>
						</div>
						<div class="ds-progress" :class="{ 'is-paused': isPaused(t) && t.progress < 1, 'is-completed': t.progress >= 1, 'is-failed': t.state === 'error', 'is-indeterminate': t.state === 'metadata' }">
							<div class="ds-progress-fill" :style="{ transform: `translateX(${percent(t) - 100}%)` }"></div>
						</div>
						<div class="row-meta">
							<span v-if="t.state === 'error'" class="ds-error-text one-line" :title="t.error">{{ t.error }}</span>
							<template v-else>
								<span>{{ t.progress >= 1 ? formatBytes(t.size) : `${formatBytes(t.done)} / ${formatBytes(t.size)}` }}</span>
								<span v-if="t.dl_speed" class="meta-speed"><b-icon icon="arrow-down" custom-size="mdi-12px"></b-icon>{{ formatSpeed(t.dl_speed) }}</span>
								<span v-if="t.up_speed" class="meta-up"><b-icon icon="arrow-up" custom-size="mdi-12px"></b-icon>{{ formatSpeed(t.up_speed) }}</span>
								<span v-if="t.eta >= 0 && t.progress < 1">{{ $t('{time} left', { time: formatEta(t.eta) }) }}</span>
								<span v-if="!isPaused(t)" :title="$t('Seeds (connected / in swarm) · peers')">
									<b-icon icon="account-multiple-outline" custom-size="mdi-12px"></b-icon>{{ t.seeds }}/{{ t.seeds_total }} · {{ t.peers }}
								</span>
								<span :title="$t('Share ratio')">{{ $t('Ratio {ratio}', { ratio: formatRatio(t.ratio) }) }}</span>
							</template>
						</div>
					</div>
					<div class="row-actions">
						<button v-if="isPaused(t)" class="ds-icon-btn" :title="$t('Resume')" :aria-label="$t('Resume {name}', { name: t.name })" @click="act(t, 'resume')">
							<b-icon icon="play" custom-size="mdi-18px"></b-icon>
						</button>
						<button v-else class="ds-icon-btn" :title="$t('Pause')" :aria-label="$t('Pause {name}', { name: t.name })" @click="act(t, 'pause')">
							<b-icon icon="pause" custom-size="mdi-18px"></b-icon>
						</button>
						<button v-if="t.progress >= 1 && t.dir" class="ds-icon-btn" :title="$t('Show in Files')" :aria-label="$t('Show {name} in Files', { name: t.name })" @click="ds.openFolder(t.dir)">
							<b-icon icon="folder-open-outline" custom-size="mdi-18px"></b-icon>
						</button>
						<button class="ds-icon-btn" :title="$t('Properties')" :aria-label="$t('Properties of {name}', { name: t.name })" @click="openDetails(t)">
							<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
						</button>
						<button class="ds-icon-btn is-danger" :title="$t('Remove')" :aria-label="$t('Remove {name}', { name: t.name })" @click="confirmRemove(t)">
							<b-icon icon="close" custom-size="mdi-18px"></b-icon>
						</button>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes, formatSpeed, formatEta } from '@/api/downloadSidecar'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { escapeHtml } from '@/utils/escapeHtml'
import { FILTERS, filterTorrents, countByFilter, isPaused, percent, formatRatio, stateLabel, badgeClass, parseSources, isTorrentFile, fileToBase64, ACTIVE_STATES } from './torrentUtil'

export const ENGINE_NAMES = { qbittorrent: 'qBittorrent', external: 'My qBittorrent', builtin: 'Built-in engine' }

export default {
	name: 'ds-torrent-list',
	mixins: [confirmWindowMixin],
	inject: { ds: 'downloadStation' },
	data() {
		return { info: null, error: '', filter: 'all', query: '', timer: null, destroyed: false, FILTERS }
	},
	computed: {
		torrents() {
			return (this.info && this.info.torrents) || []
		},
		visible() {
			return filterTorrents(this.torrents, this.filter, this.query)
		},
		counts() {
			return countByFilter(this.torrents)
		},
		altOn() {
			return !!(this.info && this.info.alt_speed_active)
		}
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
		engineName(e) {
			return this.$t(ENGINE_NAMES[e] || e)
		},
		async refresh() {
			try {
				this.info = await downloadSidecar.listTorrents()
				this.error = ''
			} catch (e) {
				this.error = e.message
			}
		},
		async poll() {
			await this.refresh()
			if (this.destroyed) return
			const busy = this.torrents.some(t => ACTIVE_STATES.includes(t.state))
			this.timer = setTimeout(this.poll, busy ? 1500 : 4000)
		},
		toastError(text) {
			this.$buefy.toast.open({ message: escapeHtml(text), type: 'is-danger' })
		},
		async act(t, action) {
			try {
				await downloadSidecar.torrentAction(t.hash, action)
			} catch (e) {
				this.toastError(e.message)
			}
			this.refresh()
		},
		async toggleAlt() {
			const cur = this.ds.settings && this.ds.settings.torrent
			try {
				// The manual switch; a running schedule keeps applying on its own.
				await downloadSidecar.updateSettings({ torrent: { alt_speed: !(cur && cur.alt_speed) } })
				await this.ds.loadSettings()
			} catch (e) {
				this.toastError(e.message)
			}
			this.refresh()
		},
		openAdd(props = {}) {
			const id = 'ds-add-torrent-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Add torrent'),
				component: 'DsAddTorrentWindow',
				props: { ...props, winId: id, onAdded: () => this.refresh() },
				width: 560,
				height: 520
			})
		},
		openDetails(t) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'ds-torrent-' + t.hash,
				title: t.name,
				component: 'DsTorrentDetailWindow',
				props: { hash: t.hash },
				width: 640,
				height: 560
			})
		},
		confirmRemove(t) {
			this.confirmWindow({
				title: this.$t('Remove torrent'),
				message: this.$t('Remove {name} from the list?', { name: `<b>${escapeHtml(t.name)}</b>` }),
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
				onConfirm: async checked => {
					try {
						await downloadSidecar.removeTorrent(t.hash, checked === true)
					} catch (e) {
						this.toastError(this.$t('Could not remove {name}: {error}', { name: t.name, error: e.message }))
					}
					this.refresh()
				}
			})
		},
		onPaste(e) {
			if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) return
			const sources = parseSources((e.clipboardData && e.clipboardData.getData('text')) || '')
			if (sources.length) this.openAdd({ sources: sources.join('\n') })
		},
		async onDrop(e) {
			const dt = e.dataTransfer
			const files = Array.from((dt && dt.files) || []).filter(isTorrentFile)
			if (files.length) {
				const torrents = await Promise.all(files.map(async f => ({ name: f.name, data: await fileToBase64(f) })))
				this.openAdd({ torrents })
				return
			}
			const sources = parseSources((dt && (dt.getData('text/uri-list') || dt.getData('text/plain'))) || '')
			if (sources.length) this.openAdd({ sources: sources.join('\n') })
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';
@import '../ds-list.scss';

.title-wrap {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	min-width: 0;
}

.engine-chip {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	font-size: var(--font-2xs);
	padding: 0.15rem 0.55rem;
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-secondary, #475569);
	white-space: nowrap;
}

.engine-dot {
	width: 0.45rem;
	height: 0.45rem;
	border-radius: 50%;
	background: var(--theme-text-muted, #94a3b8);

	&.on {
		background: var(--color-success-fg, #047857);
	}
}

.totals {
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	margin-right: var(--space-2);
	font-size: var(--font-xs);
	font-variant-numeric: tabular-nums;
	color: var(--theme-text-secondary, #475569);
}

.cat-pill {
	flex-shrink: 0;
	font-size: var(--font-2xs);
	padding: 0.05rem 0.45rem;
	border-radius: var(--radius-pill);
	background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
	color: var(--color-primary-fg, #1d4ed8);
}

.row-meta .meta-up {
	color: var(--color-success-fg, #047857);
}

.row-meta > span {
	display: inline-flex;
	align-items: center;
	gap: 0.15rem;
}
</style>
