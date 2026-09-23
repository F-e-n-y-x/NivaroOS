<!-- src/apps/download-station/DsDownloadDetailWindow.vue -->
<!-- Per-download properties window: live IDM-style segment map (one block
     per connection's byte range), link refresh for expired URLs, and
     per-download connection count. -->
<template>
	<div class="ds-detail">
		<div v-if="!d" class="detail-status">
			<b-icon v-if="!gone" icon="loading" custom-class="mdi-spin" custom-size="mdi-32px"></b-icon>
			<p v-else>{{ $t('This download was removed.') }}</p>
		</div>
		<template v-else>
			<div class="detail-body scrollbars-light">
				<div class="detail-head">
					<div class="head-icon" :class="'cat-' + categoryOf(d.filename)">
						<b-icon :icon="fileIcon(d.filename)" custom-size="mdi-26px"></b-icon>
					</div>
					<div class="head-text">
						<div class="head-name" :title="d.filename">{{ d.filename }}</div>
						<div class="head-sub">
							<span class="ds-state-badge" :class="'is-' + d.state">{{ stateText }}</span>
							<span class="head-size">{{ sizeText }}</span>
							<span v-if="d.state === 'downloading' && d.speed" class="head-speed">{{ formatSpeed(d.speed) }}</span>
							<span v-if="d.state === 'downloading' && d.eta >= 0">{{ formatEta(d.eta) }} {{ $t('left') }}</span>
						</div>
					</div>
				</div>

				<div class="segment-map" :title="$t('Each block is one connection\'s share of the file')">
					<div v-for="(s, i) in segments" :key="i" class="seg" :style="s.style" :class="{ active: s.active }">
						<div class="seg-fill" :style="{ width: s.pct + '%' }"></div>
					</div>
				</div>
				<div class="segment-legend">
					<span>{{ percentText }}</span>
					<span v-if="d.state === 'downloading'">{{ d.active_connections }} {{ $t('active connections') }} &middot; {{ d.segments.length }} {{ $t('segments') }}</span>
					<span v-else>{{ d.segments.length }} {{ $t('segments') }}</span>
				</div>

				<p v-if="d.error" class="ds-error-text detail-error">{{ d.error }}</p>

				<div class="setting-card">
					<div class="setting-row">
						<b-icon class="row-icon" icon="link-variant" custom-size="mdi-20px"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Address') }}</div>
							<div class="setting-desc mono" :title="d.url">{{ d.url }}</div>
						</div>
						<div class="row-control">
							<button class="ds-icon-btn" :title="$t('Copy')" @click="copy(d.url)"><b-icon icon="content-copy" custom-size="mdi-16px"></b-icon></button>
						</div>
					</div>
					<div v-if="canRefresh" class="setting-row refresh-row">
						<b-icon class="row-icon" icon="link-variant-plus" custom-size="mdi-20px"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Refresh link') }}</div>
							<div class="setting-desc">{{ $t('Link expired? Paste a fresh one for the same file - downloaded data is kept.') }}</div>
							<div class="refresh-form">
								<input v-model="newUrl" class="ds-input" spellcheck="false" placeholder="https://..." />
								<button class="ds-primary-btn" :disabled="!/^https?:\/\//.test(newUrl)" @click="refreshLink">{{ $t('Update') }}</button>
							</div>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="folder-outline" custom-size="mdi-20px"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Saved to') }}</div>
							<div class="setting-desc mono" :title="d.path">{{ d.path }}</div>
						</div>
						<div class="row-control">
							<button class="ds-icon-btn" :title="$t('Show in Files')" @click="openFolder"><b-icon icon="folder-open-outline" custom-size="mdi-16px"></b-icon></button>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="lan-connect" custom-size="mdi-20px"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Connections') }}</div>
							<div class="setting-desc">{{ d.resumable ? $t('Used from the next start or resume.') : $t('This server doesn\'t support multiple connections.') }}</div>
						</div>
						<div class="row-control slider-control">
							<input v-model.number="connections" class="pretty-range" type="range" min="1" max="32" :disabled="!d.resumable || d.state === 'completed'"
								:style="{ '--pct': ((connections - 1) / 31) * 100 + '%' }" @change="saveConnections" />
							<span class="slider-value">{{ connections }}</span>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="information-outline" custom-size="mdi-20px"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Details') }}</div>
							<div class="setting-desc">
								{{ d.resumable ? $t('Resumable') : $t('Not resumable') }}
								<template v-if="d.content_type"> &middot; {{ d.content_type.split(';')[0] }}</template>
								<template v-if="d.has_cookies"> &middot; {{ $t('with browser cookies') }}</template>
								&middot; {{ $t('added') }} {{ formatDate(d.created_at) }}
								<template v-if="d.completed_at"> &middot; {{ $t('finished') }} {{ formatDate(d.completed_at) }}</template>
							</div>
						</div>
					</div>
				</div>
			</div>

			<div class="detail-foot">
				<button v-if="d.state === 'completed'" class="ds-secondary-btn is-danger-text" @click="confirmDeleteFile">
					<b-icon icon="delete-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Delete file') }}</span>
				</button>
				<button v-else class="ds-secondary-btn" @click="confirmRestart">
					<b-icon icon="restart" custom-size="mdi-18px"></b-icon><span>{{ $t('Restart') }}</span>
				</button>
				<div class="foot-right">
					<button v-if="d.state === 'downloading' || d.state === 'queued'" class="ds-secondary-btn" @click="act('pause')">
						<b-icon icon="pause" custom-size="mdi-18px"></b-icon><span>{{ $t('Pause') }}</span>
					</button>
					<button v-else-if="d.state === 'paused' || d.state === 'failed'" class="ds-primary-btn" @click="act('resume')">
						<b-icon icon="play" custom-size="mdi-18px"></b-icon><span>{{ $t('Resume') }}</span>
					</button>
					<button v-else class="ds-primary-btn" @click="openFolder">
						<b-icon icon="folder-open-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Show in Files') }}</span>
					</button>
				</div>
			</div>
		</template>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes, formatSpeed, formatEta, categoryOf, fileIcon } from '@/api/downloadSidecar'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { escapeHtml } from '@/utils/escapeHtml'

export default {
	name: 'DsDownloadDetailWindow',
	mixins: [confirmWindowMixin],
	props: {
		downloadId: { type: String, required: true }
	},
	data() {
		return { d: null, gone: false, newUrl: '', connections: 8, timer: null, connectionsTouched: false }
	},
	computed: {
		canRefresh() {
			return this.d && (this.d.state === 'paused' || this.d.state === 'failed')
		},
		stateText() {
			return { downloading: this.$t('Downloading'), queued: this.$t('Queued'), paused: this.$t('Paused'), completed: this.$t('Completed'), failed: this.$t('Failed') }[this.d.state]
		},
		sizeText() {
			const d = this.d
			if (d.state === 'completed') return formatBytes(d.size)
			return d.size > 0 ? `${formatBytes(d.downloaded)} / ${formatBytes(d.size)}` : formatBytes(d.downloaded)
		},
		percentText() {
			const d = this.d
			if (d.state === 'completed') return '100%'
			if (!(d.size > 0)) return this.$t('Size unknown')
			return ((d.downloaded / d.size) * 100).toFixed(1) + '%'
		},
		segments() {
			const d = this.d
			const total = d.size > 0 ? d.size : Math.max(1, d.downloaded)
			return (d.segments || []).map(s => {
				const len = s.end >= 0 ? s.end - s.start + 1 : Math.max(1, total - s.start)
				const pct = d.state === 'completed' ? 100 : Math.min(100, (s.done / len) * 100)
				return {
					pct,
					active: d.state === 'downloading' && pct < 100,
					style: { left: (s.start / total) * 100 + '%', width: Math.max(0.3, (len / total) * 100) + '%' }
				}
			})
		}
	},
	created() {
		this.load()
		this.timer = setInterval(this.load, 1000)
	},
	beforeDestroy() {
		clearInterval(this.timer)
	},
	methods: {
		formatSpeed,
		formatEta,
		categoryOf,
		fileIcon,
		formatDate(t) {
			try {
				return new Date(t).toLocaleString()
			} catch (e) {
				return t
			}
		},
		async load() {
			try {
				this.d = await downloadSidecar.getDownload(this.downloadId)
				if (!this.connectionsTouched) this.connections = this.d.connections
			} catch (e) {
				if (/not found|404/i.test(e.message)) {
					this.d = null
					this.gone = true
					clearInterval(this.timer)
				}
			}
		},
		async act(kind) {
			try {
				this.d = await downloadSidecar[kind](this.downloadId)
			} catch (e) {
				this.$buefy.toast.open({ message: escapeHtml(e.message), type: 'is-danger' })
			}
		},
		async saveConnections() {
			this.connectionsTouched = true
			try {
				this.d = await downloadSidecar.updateDownload(this.downloadId, { connections: this.connections })
			} catch (e) {}
			this.connectionsTouched = false
		},
		async refreshLink() {
			try {
				this.d = await downloadSidecar.updateDownload(this.downloadId, { url: this.newUrl.trim() })
				this.newUrl = ''
				await downloadSidecar.resume(this.downloadId)
				this.$buefy.toast.open({ message: this.$t('Link updated - resuming'), type: 'is-success' })
			} catch (e) {
				this.$buefy.toast.open({ message: escapeHtml(e.message), type: 'is-danger' })
			}
		},
		async copy(text) {
			try {
				await navigator.clipboard.writeText(text)
				this.$buefy.toast.open({ message: this.$t('Copied'), type: 'is-success' })
			} catch (e) {}
		},
		openFolder() {
			this.$store.commit('SET_CURRENT_PATH', this.d.dir)
			this.$store.commit('OPEN_WINDOW', { id: 'files', title: this.$t('Files'), component: 'FilesApp', width: 960, height: 620 })
		},
		confirmRestart() {
			this.confirmWindow({
				title: this.$t('Restart download'),
				message: `${this.$t('Throw away what has been downloaded of')} <b>${escapeHtml(this.d.filename)}</b> ${this.$t('and start again from the beginning?')}`,
				confirmText: this.$t('Restart'),
				type: 'is-warning',
				icon: 'restart',
				onConfirm: async () => {
					if (this.d.state === 'downloading' || this.d.state === 'queued') await downloadSidecar.pause(this.downloadId).catch(() => {})
					await new Promise(r => setTimeout(r, 400))
					await downloadSidecar.redownload(this.downloadId).catch(e => this.$buefy.toast.open({ message: escapeHtml(e.message), type: 'is-danger' }))
				}
			})
		},
		confirmDeleteFile() {
			this.confirmWindow({
				title: this.$t('Delete file'),
				message: `${this.$t('Permanently delete')} <b>${escapeHtml(this.d.path)}</b> ${this.$t('from disk?')}`,
				confirmText: this.$t('Delete'),
				type: 'is-danger',
				icon: 'delete-outline',
				onConfirm: async () => {
					await downloadSidecar.deleteDownload(this.downloadId, true).catch(() => {})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-detail {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
}

.detail-status {
	flex: 1 1 auto;
	display: flex;
	align-items: center;
	justify-content: center;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-sm);
}

.detail-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-5);
}

.detail-head {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	margin-bottom: var(--space-4);
}

.head-icon {
	flex-shrink: 0;
	width: 3rem;
	height: 3rem;
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(37, 99, 235, 0.1);
	color: #2563eb;
}

.head-text {
	min-width: 0;
	flex: 1 1 auto;
}

.head-name {
	font-size: var(--font-md);
	font-weight: 600;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.head-sub {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	margin-top: 0.2rem;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	font-variant-numeric: tabular-nums;
}

.head-speed {
	color: #2563eb;
	font-weight: 500;
}

.segment-map {
	position: relative;
	height: 1.1rem;
	border-radius: var(--radius-xs);
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
	overflow: hidden;
}

.seg {
	position: absolute;
	top: 0;
	bottom: 0;
	border-right: 1px solid var(--theme-bg-window, #f8fafc);

	.seg-fill {
		height: 100%;
		background: #3b82f6;
		transition: width 0.5s ease;
	}

	&.active .seg-fill {
		background: linear-gradient(90deg, #3b82f6, #60a5fa);
	}
}

.segment-legend {
	display: flex;
	justify-content: space-between;
	margin: var(--space-1) 0 var(--space-4);
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	font-variant-numeric: tabular-nums;
}

.detail-error {
	margin: 0 0 var(--space-3);
}

.mono {
	font-family: $family-monospace;
	word-break: break-all;
	display: -webkit-box;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
	overflow: hidden;
}

.setting-row .row-label {
	min-width: 0;
}

.refresh-row {
	align-items: flex-start;
}

.refresh-form {
	display: flex;
	gap: var(--space-2);
	margin-top: var(--space-2);

	.ds-primary-btn {
		height: 2.1rem;
	}
}

.slider-control .pretty-range {
	width: 8rem;
}

.detail-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));
	background: var(--theme-bg-window, #fff);
}

.foot-right {
	display: flex;
	gap: var(--space-2);
}

.is-danger-text {
	color: #dc2626;
}
</style>
