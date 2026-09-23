<!-- src/apps/files/TrashView.vue -->
<!--
	The Trash section (sidebar → Trash): everything deleted from any drive
	that's currently connected, newest first. Select to Restore (back to
	where it was) or Delete forever; Empty Trash clears it all. Items are
	removed automatically after the retention period.
-->
<template>
	<div class="trash-view">
		<header class="trash-header">
			<div>
				<h3 class="title is-6 mb-0">{{ $t('Trash') }}</h3>
				<p v-if="items.length" class="trash-sub">
					{{ items.length === 1 ? $t('1 item') : $t('{n} items', { n: items.length }) }} · {{ renderSize(totalBytes) }} ·
					{{ $t('deleted forever after {n} days', { n: retentionDays }) }}
				</p>
			</div>
			<div class="trash-actions">
				<template v-if="selected.length">
					<b-button rounded size="is-small" type="is-primary" icon-left="restore" :loading="busy === 'restore'" @click="restore(selected)">
						{{ $t('Restore') }} ({{ selected.length }})
					</b-button>
					<b-button rounded size="is-small" outlined type="is-danger" :loading="busy === 'delete'" @click="confirmDeleteForever(selected)">
						{{ $t('Delete forever') }}
					</b-button>
				</template>
				<b-button v-else-if="items.length" rounded size="is-small" outlined type="is-danger" :loading="busy === 'empty'" @click="confirmEmpty">
					{{ $t('Empty Trash') }}
				</b-button>
			</div>
		</header>

		<div v-if="!loading && !items.length" class="trash-empty">
			<b-icon icon="delete-empty-outline" custom-size="mdi-48px"></b-icon>
			<p class="trash-empty-title">{{ $t('Trash is empty') }}</p>
			<p>{{ $t('Deleted files and folders stay here for {n} days, so you can restore them.', { n: retentionDays }) }}</p>
		</div>

		<div v-else class="trash-list scrollbars-light">
			<label class="trash-row trash-row-head">
				<b-checkbox :value="allSelected" :indeterminate="selected.length > 0 && !allSelected" size="is-small" @input="toggleAll"></b-checkbox>
				<span class="col-name">{{ $t('Name') }}</span>
				<span class="col-from">{{ $t('Original location') }}</span>
				<span class="col-when">{{ $t('Deleted') }}</span>
				<span class="col-size">{{ $t('Size') }}</span>
			</label>
			<label v-for="it in items" :key="it.id" class="trash-row" :class="{ selected: isSelected(it.id) }">
				<b-checkbox :value="isSelected(it.id)" size="is-small" @input="toggle(it.id)"></b-checkbox>
				<span class="col-name one-line" :title="it.name">
					<b-icon :icon="it.is_dir ? 'folder' : 'file-outline'" custom-size="mdi-18px" :class="it.is_dir ? 'folder-glyph' : 'file-glyph'"></b-icon>
					{{ it.name }}
				</span>
				<span class="col-from one-line" :title="it.original_path">{{ parentOf(it.original_path) }}</span>
				<span class="col-when" :title="new Date(it.deleted_at).toLocaleString()">{{ ago(it.deleted_at) }}</span>
				<span class="col-size">{{ it.is_dir ? $t('{n} items', { n: it.items || 0 }) + ' · ' : '' }}{{ renderSize(it.size) }}</span>
			</label>
		</div>

		<b-loading v-model="loading" :is-full-page="false"></b-loading>
		<confirm-dialog
			v-if="pendingConfirm"
			:title="pendingConfirm.title"
			:message="pendingConfirm.message"
			:confirm-text="pendingConfirm.confirmText"
			@confirm="runConfirm"
			@cancel="pendingConfirm = null"
		></confirm-dialog>
	</div>
</template>

<script>
import { renderSize } from '@/mixins/file_utils'
import { escapeHtml } from '@/utils/escapeHtml'
import events from '@/events/events'
import ConfirmDialog from './dialogs/ConfirmDialog.vue'

export default {
	name: 'files-trash-view',
	components: { ConfirmDialog },
	props: {
		active: { type: Boolean, default: false },
	},
	data() {
		return { items: [], selected: [], loading: false, busy: '', retentionDays: 30, totalBytes: 0, pendingConfirm: null }
	},
	computed: {
		allSelected() {
			return this.items.length > 0 && this.selected.length === this.items.length
		},
	},
	watch: {
		active: {
			immediate: true,
			handler(v) {
				if (v) this.load()
			},
		},
	},
	methods: {
		renderSize,
		parentOf(p) {
			return p.split('/').slice(0, -1).join('/') || '/'
		},
		ago(t) {
			const s = (Date.now() - new Date(t).getTime()) / 1000
			if (s < 60) return this.$t('just now')
			if (s < 3600) return this.$t('{n} min ago', { n: Math.round(s / 60) })
			if (s < 86400) return this.$t('{n} h ago', { n: Math.round(s / 3600) })
			return this.$t('{n} days ago', { n: Math.round(s / 86400) })
		},
		async load() {
			this.loading = true
			try {
				const res = await this.$api.trash.list()
				const d = (res.data && res.data.data) || {}
				this.items = d.items || []
				this.totalBytes = d.bytes || 0
				this.retentionDays = d.retention_days || 30
				this.selected = this.selected.filter((id) => this.items.some((i) => i.id === id))
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t("Couldn't load the Trash"), type: 'is-danger' })
			} finally {
				this.loading = false
			}
		},
		isSelected(id) {
			return this.selected.includes(id)
		},
		toggle(id) {
			this.selected = this.isSelected(id) ? this.selected.filter((x) => x !== id) : [...this.selected, id]
		},
		toggleAll() {
			this.selected = this.allSelected ? [] : this.items.map((i) => i.id)
		},
		async restore(ids) {
			this.busy = 'restore'
			try {
				const res = await this.$api.trash.restore(ids)
				const r = (res.data && res.data.data) || {}
				const restored = r.restored || []
				const failed = r.failed || []
				if (restored.length) {
					const renamed = restored.filter((x) => / \(restored( \d+)?\)/.test(x.path))
					this.$buefy.toast.open({
						message: escapeHtml(
							restored.length === 1 ? this.$t('Restored to {path}', { path: restored[0].path }) : this.$t('{n} items restored', { n: restored.length }) + (renamed.length ? ' · ' + this.$t('{n} renamed because the name was taken', { n: renamed.length }) : '')
						),
						type: 'is-dark',
						position: 'is-bottom',
						duration: 4000,
					})
				}
				if (failed.length) {
					this.$buefy.toast.open({ message: escapeHtml(failed.map((f) => `${f.name || f.id}: ${f.error}`).join('; ')), type: 'is-danger', duration: 6000 })
				}
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t('Restore failed'), type: 'is-danger' })
			} finally {
				this.busy = ''
				this.selected = []
				this.load()
			}
		},
		confirmDeleteForever(ids) {
			this.pendingConfirm = {
				title: this.$t('Delete forever?'),
				message: ids.length === 1 ? this.$t("This item will be <b>deleted permanently</b>. This can't be undone.") : this.$t("{n} items will be <b>deleted permanently</b>. This can't be undone.", { n: ids.length }),
				confirmText: this.$t('Delete forever'),
				run: () => this.deleteForever(ids),
			}
		},
		confirmEmpty() {
			this.pendingConfirm = {
				title: this.$t('Empty Trash?'),
				message: this.$t("All {n} items will be <b>deleted permanently</b>. This can't be undone.", { n: this.items.length }),
				confirmText: this.$t('Empty Trash'),
				run: () => this.empty(),
			}
		},
		runConfirm() {
			const run = this.pendingConfirm && this.pendingConfirm.run
			this.pendingConfirm = null
			if (run) run()
		},
		async deleteForever(ids) {
			this.busy = 'delete'
			try {
				const res = await this.$api.trash.deleteForever(ids)
				const failed = (res.data && res.data.data && res.data.data.failed) || []
				if (failed.length) this.$buefy.toast.open({ message: escapeHtml(failed.map((f) => `${f.name}: ${f.error}`).join('; ')), type: 'is-danger' })
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t('Delete failed'), type: 'is-danger' })
			} finally {
				this.busy = ''
				this.selected = []
				this.load()
			}
		},
		async empty() {
			this.busy = 'empty'
			try {
				await this.$api.trash.empty()
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t("Couldn't empty the Trash"), type: 'is-danger' })
			} finally {
				this.busy = ''
				this.load()
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.trash-view {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	min-width: 0;
	min-height: 0;
	padding: var(--space-4) var(--space-6);
	position: relative;
}
.trash-header {
	flex-shrink: 0;
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: var(--space-3);
	margin-bottom: var(--space-3);
}
.trash-sub {
	margin-top: 0.2rem;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.55));
}
.trash-actions {
	display: flex;
	gap: var(--space-2);
	flex-wrap: wrap;
	justify-content: flex-end;
}
.trash-empty {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	text-align: center;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.6));

	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}
}
.trash-empty-title {
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
}
.trash-list {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
}
.trash-row {
	display: grid;
	grid-template-columns: 1.6rem minmax(8rem, 2fr) minmax(6rem, 2fr) 6rem 7rem;
	align-items: center;
	gap: var(--space-2);
	padding: 0.45rem var(--space-2);
	border-radius: var(--radius-sm);
	font-size: var(--font-sm);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}
	&.selected {
		background: var(--theme-card-selected, rgba(37, 99, 235, 0.1));
	}
	::v-deep .checkbox {
		margin: 0;
	}
}
.trash-row-head {
	position: sticky;
	top: 0;
	z-index: 1;
	background: var(--theme-bg-window-opaque, #fff);
	font-size: var(--font-xs);
	font-weight: 600;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.55));
	cursor: default;

	&:hover {
		background: var(--theme-bg-window-opaque, #fff);
	}
}
.col-name {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	font-weight: 500;
}
.col-from,
.col-when,
.col-size {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.6));
}
.col-size {
	text-align: right;
	font-variant-numeric: tabular-nums;
}
.folder-glyph {
	color: #3b82f6;
}
.file-glyph {
	color: var(--theme-text-muted, #94a3b8);
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
@media (max-width: 640px) {
	.trash-row {
		grid-template-columns: 1.6rem 1fr 5rem;
	}
	.col-from,
	.col-size {
		display: none;
	}
}
</style>
