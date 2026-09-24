<!-- Browse one version of a job's backup (spec §12.7), read-only and laid
     out like Files: a breadcrumb, the folder's entries with a checkbox
     each, and Download (a single-use link) and Restore… for the
     selection. Nothing selected restores the whole version. NO_SCROLL: the
     list scrolls inside, the header and actions stay put. -->
<template>
	<div class="bk-window bk-browse" @keydown="onWindowKeydown">
		<header class="bk-browse-head">
			<div class="bk-browse-title">
				<h2>{{ job ? job.name : jobName }}</h2>
				<span class="bk-pill is-info">
					<b-icon icon="lock-outline" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
					<span>{{ badge }}</span>
				</span>
			</div>
			<nav class="bk-crumbs" :aria-label="$t('backup.browse.path')">
				<ol>
					<li v-for="(c, i) in crumbs" :key="c.path">
						<button
							type="button"
							class="bk-crumb"
							:aria-current="i === crumbs.length - 1 ? 'location' : null"
							:disabled="i === crumbs.length - 1"
							@click="go(c.path)"
						>{{ c.name }}</button>
						<b-icon v-if="i < crumbs.length - 1" icon="chevron-right" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					</li>
				</ol>
			</nav>
		</header>

		<div class="bk-browse-bar">
			<button type="button" class="bk-btn is-small" :disabled="!path || loading" @click="go(parent)">
				<b-icon icon="arrow-up" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.browse.up') }}</span>
			</button>
			<label v-if="entries.length" class="bk-browse-all">
				<input type="checkbox" :checked="allChecked" :indeterminate.prop="someChecked && !allChecked" data-autofocus @change="toggleAll($event.target.checked)" />
				<span>{{ $t('backup.browse.select_all') }}</span>
			</label>
		</div>

		<div class="bk-browse-list" role="region" :aria-label="$t('backup.browse.contents')" :aria-busy="loading ? 'true' : null">
			<p v-if="error" class="bk-browse-msg bk-inline-error" role="alert">
				{{ errText(error) }}
				<button type="button" class="bk-btn is-small" @click="load">{{ $t('backup.retry') }}</button>
			</p>
			<p v-else-if="loading && !entries.length" class="bk-browse-msg bk-secondary" role="status">{{ $t('backup.loading') }}</p>
			<p v-else-if="!entries.length" class="bk-browse-msg bk-secondary">{{ $t('backup.browse.empty') }}</p>
			<table v-else class="bk-browse-table">
				<thead>
					<tr>
						<th scope="col" class="bk-col-check"><span class="bk-sr-only">{{ $t('backup.browse.selected') }}</span></th>
						<th scope="col">{{ $t('backup.browse.name') }}</th>
						<th scope="col" class="bk-col-size">{{ $t('backup.browse.size') }}</th>
						<th scope="col" class="bk-col-date">{{ $t('backup.browse.modified') }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="e in entries" :key="e.name" :class="{ 'is-checked': isChecked(e) }">
						<td class="bk-col-check">
							<input :id="rowId(e)" type="checkbox" :checked="isChecked(e)" :aria-label="$t('backup.browse.select_named', { name: e.name })" @change="toggle(e, $event.target.checked)" />
						</td>
						<td>
							<div class="bk-col-name">
								<b-icon :icon="e.dir ? 'folder' : 'file-outline'" pack="mdi" custom-size="mdi-18px" :class="{ 'bk-folder-icon': e.dir }" aria-hidden="true"></b-icon>
								<button v-if="e.dir" type="button" class="bk-entry-open" @click="go(joinPath(path, e.name))">{{ e.name }}</button>
								<label v-else :for="rowId(e)" class="bk-entry-name">{{ e.name }}</label>
							</div>
						</td>
						<td class="bk-col-size">{{ e.dir ? '' : fmt.bytes(e.size) }}</td>
						<td class="bk-col-date">{{ e.mtime ? fmt.dateTime(e.mtime) : '' }}</td>
					</tr>
				</tbody>
			</table>
			<p v-if="truncated" class="bk-browse-msg bk-muted">{{ $t('backup.browse.truncated') }}</p>
		</div>

		<footer class="bk-browse-foot">
			<span class="bk-browse-count" aria-live="polite">{{ selectionText }}</span>
			<button v-if="selected.length" type="button" class="bk-btn is-link is-small" @click="selected = []">{{ $t('backup.browse.clear') }}</button>
			<span class="bk-browse-spacer"></span>
			<button type="button" class="bk-btn" :disabled="!selected.length || downloading" @click="download">
				<b-icon icon="download" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.browse.download') }}</span>
			</button>
			<button type="button" class="bk-btn is-primary" :disabled="!job" @click="restore">
				<b-icon icon="backup-restore" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
				<span>{{ selected.length ? $t('backup.browse.restore_selected') : $t('backup.browse.restore_all') }}</span>
			</button>
		</footer>
	</div>
</template>

<script>
import { backupMixin, windowBehavior } from '../backupMixin'
import { joinPath, parentPath } from '../state'
import { downloadUrl } from '@/service/backup'

export default {
	name: 'BackupBrowseWindow',
	mixins: [backupMixin, windowBehavior],
	props: {
		winId: { type: String, default: '' },
		jobId: { type: String, required: true },
		versionId: { type: String, required: true },
		jobName: { type: String, default: '' }
	},
	data() {
		return { job: null, version: null, path: '', entries: [], truncated: false, loading: false, error: null, selected: [], downloading: false, seq: 0 }
	},
	computed: {
		parent() {
			return parentPath(this.path)
		},
		crumbs() {
			const out = [{ name: this.$t('backup.browse.root'), path: '' }]
			let acc = ''
			for (const part of this.path.split('/').filter(Boolean)) {
				acc = joinPath(acc, part)
				out.push({ name: part, path: acc })
			}
			return out
		},
		badge() {
			const v = this.version
			if (!v || v.kind === 'current') return this.$t('backup.browse.readonly_current')
			const label = this.$t(v.label_key || `backup.ver.${v.kind}`, { at: this.fmt.dateTime(v.time) })
			return this.$t('backup.browse.readonly', { version: label })
		},
		allChecked() {
			return this.entries.length > 0 && this.entries.every(e => this.isChecked(e))
		},
		someChecked() {
			return this.entries.some(e => this.isChecked(e))
		},
		selectionText() {
			if (!this.selected.length) return this.$t('backup.browse.none_selected')
			return this.$tc('backup.browse.n_selected', this.selected.length, { count: this.fmt.number(this.selected.length) })
		}
	},
	created() {
		this.load()
		this.loadMeta()
	},
	methods: {
		joinPath,
		rowId(e) {
			return `${this.winId || 'bk-browse'}-${encodeURIComponent(joinPath(this.path, e.name))}`
		},
		async loadMeta() {
			try {
				const [job, versions] = await Promise.all([this.bkApi.getJob(this.jobId), this.bkApi.listVersions(this.jobId)])
				this.job = job
				this.version = (versions || []).find(v => v.id === this.versionId) || null
			} catch (e) {
				this.error = this.error || e
			}
		},
		async load() {
			const seq = ++this.seq
			const path = this.path
			this.loading = true
			this.error = null
			try {
				const res = await this.bkApi.browseVersion(this.jobId, this.versionId, path)
				if (seq !== this.seq) return
				const entries = (res && res.entries) || []
				// Folders first, then by name - like Files.
				this.entries = entries.slice().sort((a, b) => (a.dir === b.dir ? String(a.name).localeCompare(String(b.name)) : a.dir ? -1 : 1))
				this.truncated = !!(res && res.truncated)
			} catch (e) {
				if (seq === this.seq) {
					this.entries = []
					this.error = e
				}
			} finally {
				if (seq === this.seq) this.loading = false
			}
		},
		go(path) {
			this.path = path || ''
			this.load().then(() => this.$nextTick(() => this.focusFirst()))
		},
		isChecked(e) {
			return this.selected.includes(joinPath(this.path, e.name))
		},
		toggle(e, on) {
			const p = joinPath(this.path, e.name)
			const has = this.selected.includes(p)
			if (on && !has) this.selected = this.selected.concat(p)
			else if (!on && has) this.selected = this.selected.filter(x => x !== p)
		},
		toggleAll(on) {
			for (const e of this.entries) this.toggle(e, on)
		},
		// A single-use, 60-second token; the browser downloads it itself.
		async download() {
			this.downloading = true
			try {
				const { token } = await this.bkApi.createDownload({ jobId: this.jobId, versionId: this.versionId, paths: this.selected })
				const a = document.createElement('a')
				a.href = downloadUrl(token)
				a.rel = 'noopener'
				a.setAttribute('download', '')
				document.body.appendChild(a)
				a.click()
				a.remove()
			} catch (e) {
				this.toastError(e)
			} finally {
				this.downloading = false
			}
		},
		restore() {
			this.openBackupWindow('restore', { jobId: this.jobId, versionId: this.versionId, paths: this.selected.slice(), jobName: this.job ? this.job.name : this.jobName })
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-browse {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 0;
	background: var(--theme-bg-window);
	color: var(--theme-text-primary);
	font-size: var(--font-base);
	p {
		margin: 0;
	}
}
.bk-browse-head {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-4) var(--space-4) var(--space-2);
}
.bk-browse-title {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2) var(--space-3);
	h2 {
		margin: 0;
		font-size: var(--font-lg);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
}
.bk-crumbs ol {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-1);
	margin: 0;
	padding: 0;
	list-style: none;
	li {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-width: 0;
	}
	.icon {
		color: var(--theme-text-muted);
	}
}
.bk-crumb {
	min-height: 2rem;
	padding: 0 var(--space-2);
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: var(--color-primary-fg);
	font-family: inherit;
	font-size: var(--font-sm);
	cursor: pointer;
	max-width: 14rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	&:hover:not(:disabled) {
		background: var(--theme-card-hover);
	}
	&:disabled {
		color: var(--theme-text-primary);
		font-weight: 600;
		cursor: default;
	}
}
.bk-browse-bar {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: 0 var(--space-4) var(--space-2);
}
.bk-browse-all {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-sm);
	cursor: pointer;
}
.bk-browse-list {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	margin: 0 var(--space-4);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
}
.bk-browse-msg {
	padding: var(--space-4);
	font-size: var(--font-sm);
}
.bk-inline-error {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	color: var(--color-danger-fg);
}
.bk-browse-table {
	width: 100%;
	border-collapse: collapse;
	font-size: var(--font-sm);
	th {
		position: sticky;
		top: 0;
		z-index: 1;
		padding: var(--space-2);
		background: var(--theme-table-head-bg);
		color: var(--theme-text-secondary);
		font-size: var(--font-xs);
		font-weight: 600;
		text-align: left;
		border-bottom: 1px solid var(--theme-card-border);
	}
	td {
		padding: var(--space-1) var(--space-2);
		border-bottom: 1px solid var(--theme-table-divider);
		vertical-align: middle;
	}
	tr.is-checked td {
		background: var(--theme-card-selected);
	}
	input[type='checkbox'] {
		width: 1.125rem;
		height: 1.125rem;
		cursor: pointer;
	}
}
.bk-col-check {
	width: 2.5rem;
	text-align: center !important;
}
.bk-col-name {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-width: 0;
	min-height: 2.5rem;
	overflow-wrap: anywhere;
}
.bk-col-size {
	width: 6rem;
	text-align: right !important;
	white-space: nowrap;
	font-variant-numeric: tabular-nums;
	color: var(--theme-text-secondary);
}
.bk-col-date {
	width: 11rem;
	white-space: nowrap;
	color: var(--theme-text-secondary);
}
.bk-folder-icon {
	color: var(--color-primary-fg);
}
.bk-entry-open {
	border: none;
	background: transparent;
	padding: var(--space-1) 0;
	color: var(--theme-text-primary);
	font-family: inherit;
	font-size: inherit;
	font-weight: 500;
	text-align: left;
	cursor: pointer;
	&:hover {
		color: var(--color-primary-fg);
		text-decoration: underline;
	}
}
.bk-entry-name {
	cursor: pointer;
}
.bk-browse-foot {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4) calc(var(--space-3) + env(safe-area-inset-bottom));
}
.bk-browse-count {
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
}
.bk-browse-spacer {
	flex: 1 1 0;
}
// Narrow window: size and date columns go (the name matters most).
@media (max-width: 599px) {
	.bk-col-date,
	.bk-col-size {
		display: none;
	}
}
@media (pointer: coarse) {
	.bk-crumb {
		min-height: 44px;
	}
	.bk-col-name {
		min-height: 44px;
	}
}
</style>
