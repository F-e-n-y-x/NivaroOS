<!-- src/apps/download-station/DsDownloadList.vue -->
<template>
	<div class="ds-list" tabindex="-1" @paste="onPaste" @dragover.prevent @drop.prevent="onDrop">
		<div class="ds-list-toolbar">
			<h2 class="ds-section-title">{{ $t('Downloads') }}</h2>
			<div class="toolbar-actions">
				<button class="ds-icon-btn" :title="$t('Resume all')" :aria-label="$t('Resume all')" :disabled="!hasResumable" @click="resumeAll">
					<b-icon icon="play" custom-size="mdi-18px"></b-icon>
				</button>
				<button class="ds-icon-btn" :title="$t('Pause all')" :aria-label="$t('Pause all')" :disabled="!hasActive" @click="pauseAll">
					<b-icon icon="pause" custom-size="mdi-18px"></b-icon>
				</button>
				<button class="ds-icon-btn" :title="$t('Clear completed from list')" :aria-label="$t('Clear completed from list')" :disabled="!hasCompleted" @click="clearCompleted">
					<b-icon icon="playlist-remove" custom-size="mdi-18px"></b-icon>
				</button>
				<button class="ds-primary-btn" @click="ds.openAddDownload()">
					<b-icon icon="plus" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Add URL') }}</span>
				</button>
			</div>
		</div>

		<div class="ds-filters">
			<div class="segmented-control">
				<button v-for="f in filters" :key="f.id" class="segmented-option" :class="{ active: filter === f.id }" :aria-pressed="filter === f.id ? 'true' : 'false'" @click="filter = f.id">
					{{ $t(f.label) }}<span v-if="counts[f.id]" class="seg-count">{{ counts[f.id] }}</span>
				</button>
			</div>
			<div class="ds-search">
				<b-icon icon="magnify" custom-size="mdi-16px"></b-icon>
				<input v-model="query" class="ds-input" :aria-label="$t('Search downloads')" :placeholder="$t('Search downloads')" />
			</div>
		</div>
		<div v-if="presentCategories.length > 1" class="ds-categories">
			<button class="cat-chip" :class="{ active: category === '' }" @click="category = ''">{{ $t('All types') }}</button>
			<button v-for="c in presentCategories" :key="c.id" class="cat-chip" :class="{ active: category === c.id }" :aria-pressed="category === c.id ? 'true' : 'false'" @click="category = category === c.id ? '' : c.id">
				<b-icon :icon="c.icon" custom-size="mdi-14px"></b-icon>{{ $t(c.label) }}
			</button>
		</div>

		<div class="ds-list-body scrollbars-light">
			<div v-if="loading" class="ds-empty">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
			</div>
			<div v-else-if="!downloads.length" class="ds-empty">
				<b-icon icon="tray-arrow-down" custom-size="mdi-48px"></b-icon>
				<p class="ds-empty-title">{{ $t('No downloads yet') }}</p>
				<p class="ds-empty-hint">{{ $t('Paste a link, drop one here, or find a file with the built-in browser.') }}</p>
				<div class="ds-empty-actions">
					<button class="ds-primary-btn" @click="ds.openAddDownload()">
						<b-icon icon="plus" custom-size="mdi-18px"></b-icon><span>{{ $t('Add URL') }}</span>
					</button>
					<button class="ds-secondary-btn" @click="$emit('open-browser', '')">
						<b-icon icon="web" custom-size="mdi-18px"></b-icon><span>{{ $t('Open Browser') }}</span>
					</button>
				</div>
			</div>
			<div v-else-if="!visible.length" class="ds-empty is-small">
				<p class="ds-empty-hint">{{ $t('Nothing matches this filter.') }}</p>
			</div>

			<div v-else class="ds-rows">
				<div v-for="d in visible" :key="d.id" class="ds-row" :class="'is-' + d.state" @dblclick="onRowDblClick(d)">
					<div class="row-icon-wrap" :class="'cat-' + categoryOf(d.filename)">
						<b-icon :icon="fileIcon(d.filename)" custom-size="mdi-22px"></b-icon>
					</div>
					<div class="row-main">
						<div class="row-head">
							<span class="row-name" :title="d.filename">{{ d.filename }}</span>
							<span class="ds-state-badge" :class="'is-' + d.state">{{ stateLabel(d) }}</span>
						</div>
						<div class="ds-progress" :class="progressClass(d)">
							<div class="ds-progress-fill" :style="{ width: percent(d) + '%' }"></div>
						</div>
						<div class="row-meta">
							<span v-if="d.state === 'failed'" class="ds-error-text one-line" :title="d.error">{{ d.error }}</span>
							<template v-else>
								<span>{{ sizeText(d) }}</span>
								<span v-if="d.state === 'downloading' && d.speed" class="meta-speed">{{ formatSpeed(d.speed) }}</span>
								<span v-if="d.state === 'downloading' && d.eta >= 0">{{ $t('{time} left', { time: formatEta(d.eta) }) }}</span>
								<span v-if="d.state === 'downloading'" class="meta-conns" :title="$t('Active connections')">
									<b-icon icon="lan-connect" custom-size="mdi-12px"></b-icon>{{ d.active_connections }}/{{ d.resumable ? d.connections : 1 }}
								</span>
								<span v-if="d.state === 'completed'" class="one-line meta-dir" :title="d.dir">{{ d.dir }}</span>
								<span v-else class="one-line meta-host" :title="d.url">{{ hostOf(d.url) }}</span>
							</template>
						</div>
					</div>
					<div class="row-actions">
						<button v-if="d.state === 'downloading' || d.state === 'queued'" class="ds-icon-btn" :title="$t('Pause')" :aria-label="$t('Pause {name}', { name: d.filename })" @click="act('pause', d)">
							<b-icon icon="pause" custom-size="mdi-18px"></b-icon>
						</button>
						<button v-else-if="d.state === 'paused' || d.state === 'failed'" class="ds-icon-btn" :title="d.state === 'failed' ? $t('Retry') : $t('Resume')"
							:aria-label="d.state === 'failed' ? $t('Retry {name}', { name: d.filename }) : $t('Resume {name}', { name: d.filename })" @click="act('resume', d)">
							<b-icon :icon="d.state === 'failed' ? 'restart' : 'play'" custom-size="mdi-18px"></b-icon>
						</button>
						<button v-else class="ds-icon-btn" :title="$t('Show in Files')" :aria-label="$t('Show {name} in Files', { name: d.filename })" @click="ds.openFolder(d.dir)">
							<b-icon icon="folder-open-outline" custom-size="mdi-18px"></b-icon>
						</button>
						<button class="ds-icon-btn" :title="$t('Properties')" :aria-label="$t('Properties of {name}', { name: d.filename })" @click="ds.openDetails(d)">
							<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
						</button>
						<button class="ds-icon-btn is-danger" :title="$t('Remove')" :aria-label="$t('Remove {name}', { name: d.filename })" @click="confirmRemove(d)">
							<b-icon icon="close" custom-size="mdi-18px"></b-icon>
						</button>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes, formatSpeed, formatEta, CATEGORIES, categoryOf, fileIcon } from '@/api/downloadSidecar'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { escapeHtml } from '@/utils/escapeHtml'

export default {
	name: 'ds-download-list',
	mixins: [confirmWindowMixin],
	inject: { ds: 'downloadStation' },
	props: {
		downloads: { type: Array, default: () => [] },
		loading: { type: Boolean, default: false }
	},
	data() {
		return {
			filter: 'all',
			category: '',
			query: '',
			filters: [
				{ id: 'all', label: 'All' },
				{ id: 'active', label: 'Active' },
				{ id: 'completed', label: 'Completed' },
				{ id: 'failed', label: 'Failed' }
			]
		}
	},
	computed: {
		counts() {
			const c = { all: this.downloads.length, active: 0, completed: 0, failed: 0 }
			for (const d of this.downloads) {
				if (d.state === 'completed') c.completed++
				else if (d.state === 'failed') c.failed++
				else c.active++
			}
			return c
		},
		presentCategories() {
			const ids = new Set(this.downloads.map(d => categoryOf(d.filename)))
			const cats = CATEGORIES.filter(c => ids.has(c.id))
			if (ids.has('other')) cats.push({ id: 'other', label: 'Other', icon: 'file-outline' })
			return cats
		},
		visible() {
			const q = this.query.trim().toLowerCase()
			return this.downloads.filter(d => {
				if (this.filter === 'active' && (d.state === 'completed' || d.state === 'failed')) return false
				if (this.filter === 'completed' && d.state !== 'completed') return false
				if (this.filter === 'failed' && d.state !== 'failed') return false
				if (this.category && categoryOf(d.filename) !== this.category) return false
				if (q && !d.filename.toLowerCase().includes(q) && !d.url.toLowerCase().includes(q)) return false
				return true
			})
		},
		hasActive() {
			return this.downloads.some(d => d.state === 'downloading' || d.state === 'queued')
		},
		hasResumable() {
			return this.downloads.some(d => d.state === 'paused' || d.state === 'failed')
		},
		hasCompleted() {
			return this.downloads.some(d => d.state === 'completed')
		}
	},
	methods: {
		formatSpeed,
		formatEta,
		categoryOf,
		fileIcon,
		percent(d) {
			if (d.state === 'completed') return 100
			if (!d.size || d.size <= 0) return 0
			return Math.min(100, Math.floor((d.downloaded / d.size) * 1000) / 10)
		},
		progressClass(d) {
			return {
				'is-paused': d.state === 'paused' || d.state === 'queued',
				'is-failed': d.state === 'failed',
				'is-completed': d.state === 'completed',
				'is-indeterminate': d.state === 'downloading' && (!d.size || d.size <= 0)
			}
		},
		stateLabel(d) {
			switch (d.state) {
				case 'downloading':
					return d.size > 0 ? `${this.percent(d).toFixed(1)}%` : this.$t('Downloading')
				case 'queued':
					return this.$t('Queued')
				case 'paused':
					return this.$t('Paused')
				case 'completed':
					return this.$t('Completed')
				case 'failed':
					return this.$t('Failed')
			}
			return d.state
		},
		sizeText(d) {
			if (d.state === 'completed') return formatBytes(d.size)
			if (d.size > 0) return `${formatBytes(d.downloaded)} / ${formatBytes(d.size)}`
			return d.downloaded ? formatBytes(d.downloaded) : this.$t('Size unknown')
		},
		hostOf(u) {
			try {
				return new URL(u).host
			} catch (e) {
				return u
			}
		},
		toastError(text) {
			this.$buefy.toast.open({ message: escapeHtml(text), type: 'is-danger' })
		},
		async act(kind, d) {
			try {
				await downloadSidecar[kind](d.id)
			} catch (e) {
				this.toastError(e.message)
			}
			this.$emit('refresh')
		},
		async pauseAll() {
			try {
				await downloadSidecar.pauseAll()
			} catch (e) {
				this.toastError(this.$t('Could not pause the downloads: {error}', { error: e.message }))
			}
			this.$emit('refresh')
		},
		async resumeAll() {
			try {
				await downloadSidecar.resumeAll()
			} catch (e) {
				this.toastError(this.$t('Could not resume the downloads: {error}', { error: e.message }))
			}
			this.$emit('refresh')
		},
		async clearCompleted() {
			try {
				await downloadSidecar.clearCompleted()
			} catch (e) {
				this.toastError(this.$t('Could not clear the completed downloads: {error}', { error: e.message }))
			}
			this.$emit('refresh')
		},
		onRowDblClick(d) {
			if (d.state === 'completed') this.ds.openFolder(d.dir)
			else this.ds.openDetails(d)
		},
		confirmRemove(d) {
			const done = d.state === 'completed'
			const name = `<b>${escapeHtml(d.filename)}</b>`
			this.confirmWindow({
				title: this.$t('Remove download'),
				// Whole sentences with the (escaped) name as a placeholder, so
				// translations can put it wherever their grammar needs.
				message: done
					? this.$t('Remove {name} from the list?', { name })
					: this.$t('Cancel and remove {name}? The partially downloaded data is deleted.', { name }),
				confirmText: this.$t('Remove'),
				type: 'is-danger',
				icon: 'close-circle',
				checkbox: done
					? {
							label: this.$t('Also delete the downloaded file'),
							checked: false,
							checkedHint: this.$t('The file is permanently deleted from disk.'),
							uncheckedHint: this.$t('The downloaded file is kept.'),
							checkedIsDanger: true
					  }
					: null,
				onConfirm: async checked => {
					try {
						await downloadSidecar.deleteDownload(d.id, done && checked === true)
					} catch (e) {
						this.toastError(this.$t('Could not remove {name}: {error}', { name: d.filename, error: e.message }))
					}
					this.$emit('refresh')
				}
			})
		},
		// IDM habit: Ctrl+V a link anywhere in the list to add it.
		onPaste(e) {
			if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) return
			const text = (e.clipboardData && e.clipboardData.getData('text')) || ''
			const urls = extractUrls(text)
			if (urls.length) this.ds.openAddDownload({ url: urls.join('\n') })
		},
		onDrop(e) {
			const dt = e.dataTransfer
			const text = (dt && (dt.getData('text/uri-list') || dt.getData('text/plain'))) || ''
			const urls = extractUrls(text)
			if (urls.length) this.ds.openAddDownload({ url: urls.join('\n') })
		}
	}
}

function extractUrls(text) {
	return (text.match(/https?:\/\/[^\s"'<>]+/gi) || []).slice(0, 200)
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-list {
	display: flex;
	flex-direction: column;
	height: 100%;
	padding: var(--space-6) var(--space-6) 0;
	outline: none;
}

.ds-list-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-3);
	margin-bottom: var(--space-4);
}

.toolbar-actions {
	display: flex;
	align-items: center;
	gap: var(--space-1);

	.ds-primary-btn {
		margin-left: var(--space-2);
	}
}

.ds-filters {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	flex-wrap: wrap;
	margin-bottom: var(--space-3);

	.segmented-control {
		margin-bottom: 0;
	}
}

.seg-count {
	margin-left: 0.35rem;
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	font-variant-numeric: tabular-nums;
}

.ds-search {
	position: relative;
	flex: 1 1 10rem;
	max-width: 18rem;
	margin-left: auto;

	.icon {
		position: absolute;
		left: 0.55rem;
		top: 50%;
		transform: translateY(-50%);
		color: var(--theme-text-muted, #94a3b8);
		pointer-events: none;
	}

	.ds-input {
		padding-left: 1.9rem;
	}
}

.ds-categories {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	margin-bottom: var(--space-3);
}

.cat-chip {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-secondary, #64748b);
	font-family: inherit;
	font-size: var(--font-xs);
	padding: 0.2rem 0.6rem;
	border-radius: var(--radius-pill);
	cursor: pointer;

	::v-deep .icon {
		width: 0.9rem;
		height: 0.9rem;
	}

	&:hover {
		color: var(--theme-text-primary, #1e293b);
	}
	&.active {
		border-color: var(--color-primary-fg, #1d4ed8);
		background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
		color: var(--color-primary-fg, #1d4ed8);
	}
}

.ds-list-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	margin: 0 calc(-1 * var(--space-6));
	padding: 0 var(--space-6) var(--space-6);
}

.ds-rows {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.ds-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-3) var(--space-3) var(--space-3);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	box-shadow: var(--shadow-sm);
	transition: box-shadow 0.18s ease, border-color 0.18s ease;

	&:hover {
		border-color: var(--color-border-strong, rgba(0, 0, 0, 0.14));
		box-shadow: var(--shadow-md, 0 4px 12px rgba(0, 0, 0, 0.06));
	}
}

.row-icon-wrap {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(100, 116, 139, 0.1);
	color: var(--theme-text-muted, #64748b);
}

.row-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}

.row-head {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-width: 0;
}

.row-name {
	flex: 1 1 auto;
	min-width: 0;
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.row-meta {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	min-width: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	font-variant-numeric: tabular-nums;

	> span {
		flex-shrink: 0;
	}
	.meta-host,
	.meta-dir,
	.ds-error-text {
		flex-shrink: 1;
		min-width: 0;
	}
	.meta-speed {
		color: var(--color-primary-fg, #1d4ed8);
		font-weight: 500;
	}
	.meta-conns {
		display: inline-flex;
		align-items: center;
		gap: 0.2rem;
	}
}

.row-actions {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-1);
}

.ds-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-muted, #94a3b8);

	&.is-small {
		padding: var(--space-6) var(--space-4);
	}

	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
		color: var(--theme-text-muted, #cbd5e1);
	}
}

.ds-empty-title {
	font-size: var(--font-md);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
	margin: var(--space-2) 0 var(--space-1);
}

.ds-empty-hint {
	margin: 0 0 var(--space-4);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary, #64748b);
}

.ds-empty-actions {
	display: flex;
	gap: var(--space-2);
}
</style>
