<!-- src/apps/download-station/DsBrowserHistory.vue -->
<!-- The lite browser's History page (its own tab, like chrome://history):
     search, grouped by day, open / open in new tab, remove, clear all. -->
<template>
	<div class="ds-history scrollbars-light">
		<div class="history-inner">
			<div class="history-head">
				<h2 class="ds-section-title">{{ $t('History') }}</h2>
				<div class="history-tools">
					<div class="ds-search">
						<b-icon icon="magnify" custom-size="mdi-16px"></b-icon>
						<input v-model="query" class="ds-input" :placeholder="$t('Search history')" @input="onSearch" />
					</div>
					<button class="ds-secondary-btn" :disabled="!entries.length" @click="confirmClear">
						<b-icon icon="delete-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('Clear history') }}</span>
					</button>
				</div>
			</div>

			<div v-if="loading" class="history-empty">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-32px"></b-icon>
			</div>
			<div v-else-if="!entries.length" class="history-empty">
				<b-icon icon="history" custom-size="mdi-48px"></b-icon>
				<p>{{ query ? $t('No pages match your search.') : $t('Pages you visit in the browser show up here.') }}</p>
			</div>

			<template v-else>
				<div v-for="g in groups" :key="g.label" class="history-group">
					<h3 class="setting-card-title">{{ g.label }}</h3>
					<div class="setting-card">
						<div v-for="e in g.items" :key="e.id" class="history-row" :title="e.url"
							@click="open(e, $event)" @mousedown.middle.prevent="$emit('open', e.url, true)">
							<span class="h-time">{{ timeOf(e.time) }}</span>
							<b-icon icon="web" custom-size="mdi-16px" class="h-icon"></b-icon>
							<span class="h-title one-line">{{ e.title || e.url }}</span>
							<span class="h-host one-line">{{ hostOf(e.url) }}</span>
							<button class="ds-icon-btn is-flat h-del" :title="$t('Remove from history')" @click.stop="remove(e)">
								<b-icon icon="close" custom-size="mdi-16px"></b-icon>
							</button>
						</div>
					</div>
				</div>
			</template>
		</div>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { confirmWindowMixin } from '@/mixins/confirmWindow'

export default {
	name: 'ds-browser-history',
	mixins: [confirmWindowMixin],
	data() {
		return { entries: [], query: '', loading: true, searchTimer: null }
	},
	computed: {
		groups() {
			const out = []
			const today = new Date()
			today.setHours(0, 0, 0, 0)
			const yesterday = new Date(today.getTime() - 86400000)
			for (const e of this.entries) {
				const d = new Date(e.time)
				const day = new Date(d)
				day.setHours(0, 0, 0, 0)
				let label
				if (day.getTime() === today.getTime()) label = this.$t('Today')
				else if (day.getTime() === yesterday.getTime()) label = this.$t('Yesterday')
				else label = d.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })
				let g = out[out.length - 1]
				if (!g || g.label !== label) {
					g = { label, items: [] }
					out.push(g)
				}
				g.items.push(e)
			}
			return out
		}
	},
	created() {
		this.load()
	},
	beforeDestroy() {
		clearTimeout(this.searchTimer)
	},
	methods: {
		async load() {
			try {
				this.entries = (await downloadSidecar.listHistory(this.query, 1000)) || []
			} catch (e) {
				this.entries = []
			} finally {
				this.loading = false
			}
		},
		onSearch() {
			clearTimeout(this.searchTimer)
			this.searchTimer = setTimeout(this.load, 250)
		},
		hostOf(u) {
			try {
				return new URL(u).hostname
			} catch (e) {
				return ''
			}
		},
		timeOf(t) {
			return new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
		},
		open(e, ev) {
			this.$emit('open', e.url, !!(ev && (ev.ctrlKey || ev.metaKey)))
		},
		async remove(e) {
			this.entries = this.entries.filter(x => x.id !== e.id)
			await downloadSidecar.deleteHistory(e.id).catch(() => {})
		},
		confirmClear() {
			this.confirmWindow({
				title: this.$t('Clear history'),
				message: this.$t('Delete the whole browsing history? Cookies and downloads are not affected.'),
				confirmText: this.$t('Clear'),
				type: 'is-danger',
				icon: 'delete-outline',
				onConfirm: async () => {
					await downloadSidecar.clearHistory().catch(() => {})
					this.entries = []
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-history {
	position: absolute;
	inset: 0;
	overflow-y: auto;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
}

.history-inner {
	max-width: 52rem;
	margin: 0 auto;
	padding: var(--space-6);
}

.history-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	flex-wrap: wrap;
	gap: var(--space-3);
	margin-bottom: var(--space-3);
}

.history-tools {
	display: flex;
	align-items: center;
	gap: var(--space-2);

	.ds-secondary-btn {
		height: 2.1rem;
	}
}

.ds-search {
	position: relative;
	width: 16rem;

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

.history-group .setting-card {
	margin-bottom: var(--space-2);
}

.history-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: 0.5rem var(--space-3) 0.5rem var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	font-size: var(--font-sm);
	cursor: pointer;

	&:last-child {
		border-bottom: none;
	}

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.03));

		.h-del {
			opacity: 1;
		}
	}
}

.h-time {
	flex-shrink: 0;
	width: 3.5rem;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	font-variant-numeric: tabular-nums;
}

.h-icon {
	flex-shrink: 0;
	color: var(--theme-text-muted, #94a3b8);
}

.h-title {
	flex: 1 1 auto;
	min-width: 0;
	color: var(--theme-text-primary, #1e293b);
}

.h-host {
	flex: 0 1 12rem;
	min-width: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	text-align: right;
}

.h-del {
	opacity: 0;
	transition: opacity 0.12s ease;
}

.history-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-muted, #94a3b8);

	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	p {
		margin-top: var(--space-2);
		font-size: var(--font-sm);
		color: var(--theme-text-secondary, #64748b);
	}
}
</style>
