<!-- Settings > Online storage > Cache: one cache setting set for every
     online drive, what each account keeps here, and uploads that can't
     finish. Logic lives in cloudCache.js (tested); the service validates
     and applies (local-storage /v1/cloud/cache). -->
<template>
	<div class="cloud-cache-panel">
		<div v-if="loading && !status" class="setting-row"><div class="row-label setting-desc">{{ $t('Loading...') }}</div></div>
		<div v-else-if="loadError" class="setting-row"><div class="row-label error-note" role="alert">{{ loadError }}</div></div>
		<template v-else-if="status">
			<p class="cache-intro">
				{{ $t('Online drives keep recently used parts of files on this server, so videos seek instantly and uploads finish in the background. Files you save to an online drive always wait here until they have uploaded.') }}
			</p>
			<p v-for="(w, i) in status.warnings" :key="'sw' + i" class="warn-note" role="status">
				<i class="mdi mdi-alert-outline"></i> {{ w }}
			</p>

			<!-- Mode -->
			<div class="setting-row">
				<i class="row-icon mdi mdi-cached"></i>
				<div class="row-label">
					<div class="setting-title">{{ $t('Cache') }}</div>
					<div class="setting-desc">{{ $t(modeDesc) }}</div>
				</div>
				<div class="row-control">
					<div class="mode-group" role="radiogroup" :aria-label="$t('Cache')">
						<button v-for="m in modes" :key="m.value" type="button" class="mode-btn" role="radio"
							:class="{ active: form.mode === m.value }" :aria-checked="form.mode === m.value ? 'true' : 'false'" @click="form.mode = m.value">
							{{ $t(m.label) }}
						</button>
					</div>
				</div>
			</div>

			<!-- Size -->
			<div class="setting-row" :class="{ muted: form.mode === 'off' }">
				<i class="row-icon mdi mdi-database-outline"></i>
				<div class="row-label">
					<div class="setting-title">{{ $t('Maximum size') }}</div>
					<div class="setting-desc">{{ $t('Older cached data is removed to stay under this. Files waiting to upload are never removed. {free} free there now.', { free: fmt(status.free) }) }}</div>
				</div>
				<div class="row-control">
					<input v-model.number="form.sizeGB" class="num-input" type="number" min="1" step="1" :aria-label="$t('Maximum size in GB')" />
					<span class="unit">GB</span>
				</div>
			</div>

			<!-- Age -->
			<div class="setting-row" :class="{ muted: form.mode === 'off' }">
				<i class="row-icon mdi mdi-timer-sand"></i>
				<div class="row-label">
					<div class="setting-title">{{ $t('Keep unused data for') }}</div>
					<div class="setting-desc">{{ $t('Cached data nobody has opened for this long is removed.') }}</div>
				</div>
				<div class="row-control">
					<input v-model.number="form.ageN" class="num-input" type="number" min="1" step="1" :aria-label="$t('How long')" />
					<b-select v-model.number="form.ageUnit" size="is-small" :aria-label="$t('Unit')">
						<option v-for="u in ageUnits" :key="u.value" :value="u.value">{{ $t(u.label) }}</option>
					</b-select>
				</div>
			</div>

			<!-- Location -->
			<div class="setting-row">
				<i class="row-icon mdi mdi-folder-outline"></i>
				<div class="row-label">
					<div class="setting-title">{{ $t('Location') }}</div>
					<div class="setting-desc one-line" :title="form.dir">{{ form.dir }}</div>
					<div v-if="status.system_disk && form.dir === status.settings.dir" class="setting-desc warn-text">
						{{ $t('On the system disk. For a big cache, choose a folder on a data drive.') }}
					</div>
				</div>
				<div class="row-control">
					<b-button v-if="form.dir !== status.defaults.dir" rounded size="is-small" @click="form.dir = status.defaults.dir">{{ $t('Default') }}</b-button>
					<b-button rounded size="is-small" @click="browse('dir')">{{ $t('Change') }}</b-button>
				</div>
			</div>

			<div class="save-row">
				<p v-if="formError" class="error-note" role="alert">{{ $t(formError) }}</p>
				<p v-else-if="saveError" class="error-note" role="alert">{{ saveError }}</p>
				<p v-for="(w, i) in saveWarnings" :key="'w' + i" class="warn-note" role="status"><i class="mdi mdi-information-outline"></i> {{ w }}</p>
				<div class="form-actions">
					<span v-if="dirty" class="setting-desc">{{ $t('Saving reconnects your online drives for a moment.') }}</span>
					<b-button rounded size="is-small" :disabled="!dirty || saving" @click="reset">{{ $t('Undo') }}</b-button>
					<b-button rounded size="is-small" type="is-primary" :disabled="!dirty || !!formError" :loading="saving" @click="save">{{ $t('Save') }}</b-button>
				</div>
			</div>

			<!-- Per account -->
			<h4 class="sub-title">{{ $t('Used by each account') }}</h4>
			<div v-if="!status.accounts.length" class="setting-row"><div class="row-label setting-desc">{{ $t('No online storage accounts yet.') }}</div></div>
			<div v-for="a in status.accounts" :key="a.name" class="setting-row">
				<i class="row-icon mdi mdi-cloud-outline"></i>
				<div class="row-label">
					<div class="setting-title">{{ a.label || a.name }}</div>
					<div class="setting-desc">{{ line(a) }}</div>
				</div>
				<div class="row-control">
					<b-button rounded size="is-small" :disabled="!canClear(a)" :loading="clearing === a.name" @click="clear(a)">{{ $t('Clear cache') }}</b-button>
				</div>
			</div>
			<div v-if="status.accounts.length > 1" class="form-actions all-row">
				<span class="setting-desc">{{ $t('{size} cached in all', { size: fmt(sum.used) }) }}</span>
				<b-button rounded size="is-small" :loading="clearing === '*'" @click="clear(null)">{{ $t('Clear all') }}</b-button>
			</div>
			<p class="setting-desc clear-help">{{ $t('Clearing removes only data that is already in the cloud. Files waiting to upload are kept.') }}</p>

			<!-- Stuck uploads -->
			<template v-if="stuck.length">
				<h4 class="sub-title danger">{{ $t('Upload can\'t finish') }}</h4>
				<p class="setting-desc clear-help">{{ $t('These files were saved to an online drive but the cloud keeps refusing them, so NivaroOS stopped retrying. They are safe on this server until you choose what to do.') }}</p>
				<div v-for="s in stuck" :key="s.key" class="setting-row stuck-row">
					<i class="row-icon mdi mdi-cloud-alert-outline"></i>
					<div class="row-label">
						<div class="setting-title one-line" :title="s.name">{{ s.fileName }} <span class="setting-desc">&middot; {{ fmt(s.size) }} &middot; {{ s.label }}</span></div>
						<div class="setting-desc reason">{{ s.reason }}</div>
					</div>
					<div class="row-control">
						<b-button rounded size="is-small" type="is-primary" :loading="acting === s.key + ':save'" :disabled="!!acting" @click="saveStuck(s)">{{ $t('Save to folder') }}</b-button>
						<b-button rounded size="is-small" :loading="acting === s.key + ':retry'" :disabled="!!acting" @click="stuckAction(s, 'retry')">{{ $t('Retry') }}</b-button>
						<b-button rounded size="is-small" type="is-danger" outlined :loading="acting === s.key + ':discard'" :disabled="!!acting" @click="confirmDiscard(s)">{{ $t('Discard') }}</b-button>
					</div>
				</div>
			</template>
		</template>
		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import { apiError } from '@/utils/apiError'
import { escapeHtml } from '@/utils/escapeHtml'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { MODES, AGE_UNITS, formatBytes, settingsToForm, formToSettings, sameSettings, validateForm, accountLine, clearable, totals, stuckList, clearResultText } from './cloudCache'

export default {
	name: 'cloud-cache-panel',
	mixins: [confirmWindowMixin],
	data() {
		return {
			status: null,
			form: { mode: 'full', sizeGB: 20, ageN: 1, ageUnit: 3600, dir: '' },
			loading: false,
			loadError: '',
			saving: false,
			saveError: '',
			saveWarnings: [],
			clearing: null,
			acting: null,
			timer: null,
			modes: MODES,
			ageUnits: AGE_UNITS
		}
	},
	computed: {
		modeDesc() {
			const m = MODES.find(x => x.value === this.form.mode)
			return m ? m.desc : ''
		},
		dirty() {
			return !!this.status && !sameSettings(formToSettings(this.form), this.status.settings)
		},
		formError() {
			if (!this.status) return ''
			const cur = this.status.accounts.reduce((n, a) => n + Math.max(0, (a.used_bytes || 0) - (a.pending_bytes || 0)), 0)
			return validateForm(this.form, { free: this.status.free, currentDir: this.status.settings.dir, used: cur })
		},
		sum() {
			return totals(this.status ? this.status.accounts : [])
		},
		stuck() {
			return stuckList(this.status ? this.status.accounts : [])
		}
	},
	mounted() {
		this.refresh(true)
		// usage and uploads change while the page is open
		this.timer = setInterval(() => this.refresh(false), 15000)
	},
	beforeUnmount() {
		clearInterval(this.timer)
	},
	methods: {
		fmt: formatBytes,
		line: accountLine,
		canClear(a) {
			return clearable(a) > 0
		},
		refresh(resetForm) {
			this.loading = true
			return this.$api.cloud.cacheGet().then(res => {
				const wasDirty = this.dirty
				this.status = res.data.data
				this.loadError = ''
				if (resetForm || !wasDirty) this.form = settingsToForm(this.status.settings)
			}).catch(e => {
				if (!this.status) this.loadError = apiError(e, this.$t('Could not load the cache settings'))
			}).finally(() => { this.loading = false })
		},
		reset() {
			this.form = settingsToForm(this.status.settings)
			this.saveError = ''
		},
		save() {
			this.saving = true
			this.saveError = ''
			this.saveWarnings = []
			this.$api.cloud.cacheSave(formToSettings(this.form)).then(res => {
				const d = res.data.data || {}
				this.saveWarnings = d.warnings || []
				if (d.status) {
					this.status = d.status
					this.form = settingsToForm(d.status.settings)
				}
				this.$buefy.toast.open({ message: this.$t('Cache settings saved'), type: 'is-success' })
			}).catch(e => {
				this.saveError = apiError(e, this.$t('Could not save the cache settings'))
			}).finally(() => { this.saving = false })
		},
		browse(target, stuckItem) {
			const id = 'cloud-cache-folder-' + Date.now()
			const isDir = target === 'dir'
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: isDir ? this.$t('Cache location') : this.$t('Save {name} to', { name: stuckItem.fileName }),
				component: 'DsFolderPickerWindow',
				props: {
					winId: id,
					startPath: isDir ? this.form.dir : '/DATA',
					onSelect: path => {
						if (isDir) this.form.dir = path
						else this.stuckAction(stuckItem, 'save', path)
					}
				},
				width: 460,
				height: 440
			})
		},
		clear(a) {
			const name = a ? a.name : ''
			this.clearing = a ? a.name : '*'
			this.$api.cloud.cacheClear(name).then(res => {
				this.$buefy.toast.open({ message: escapeHtml(clearResultText(res.data.data || {})), type: 'is-success' })
			}).catch(e => {
				this.$buefy.toast.open({ message: escapeHtml(apiError(e, this.$t('Could not clear the cache'))), type: 'is-danger', duration: 6000 })
			}).finally(() => {
				this.clearing = null
				this.refresh(false)
			})
		},
		saveStuck(s) {
			this.browse('save', s)
		},
		confirmDiscard(s) {
			this.confirmWindow({
				title: this.$t('Discard upload'),
				message: escapeHtml(this.$t('Delete {name} ({size}) from this server? It never reached {account}, so this is the only copy - use Save to folder first if you want to keep it.', { name: s.fileName, size: formatBytes(s.size), account: s.label })),
				type: 'is-danger',
				confirmText: this.$t('Discard'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.stuckAction(s, 'discard')
			})
		},
		stuckAction(s, action, folder) {
			this.acting = s.key + ':' + action
			this.$api.cloud.cacheStuck(action, { account: s.remote, file: s.name, folder: folder || '' }).then(res => {
				const msg = {
					save: this.$t('Saved to {path}', { path: (res.data.data || {}).path || folder }),
					retry: this.$t('Uploading {name} again', { name: s.fileName }),
					discard: this.$t('Discarded {name}', { name: s.fileName })
				}[action]
				this.$buefy.toast.open({ message: escapeHtml(msg), type: 'is-success', duration: 5000 })
			}).catch(e => {
				this.$buefy.toast.open({ message: escapeHtml(apiError(e, this.$t('That didn\'t work'))), type: 'is-danger', duration: 8000 })
			}).finally(() => {
				this.acting = null
				this.refresh(false)
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.cloud-cache-panel {
	position: relative;
}

.cache-intro,
.clear-help {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	padding: var(--space-3) var(--space-5) 0;
	margin: 0;
}

.clear-help {
	padding-bottom: var(--space-3);
}

.sub-title {
	font-size: var(--font-sm);
	font-weight: 600;
	padding: var(--space-4) var(--space-5) var(--space-1);
	margin: 0;

	&.danger {
		color: var(--color-danger-fg);
	}
}

.setting-row.muted {
	opacity: 0.55;
}

.mode-group {
	display: inline-flex;
	border: 1px solid var(--theme-border, rgba(0, 0, 0, 0.12));
	border-radius: 999px;
	overflow: hidden;
}

.mode-btn {
	border: none;
	background: transparent;
	padding: var(--space-1) var(--space-3);
	font-size: var(--font-xs);
	cursor: pointer;
	color: var(--theme-text-secondary, #475569);

	& + & {
		border-left: 1px solid var(--theme-border, rgba(0, 0, 0, 0.12));
	}

	&.active {
		background: var(--color-primary, #3b82f6);
		color: #fff;
	}

	&:focus-visible {
		outline: 2px solid var(--color-primary, #3b82f6);
		outline-offset: -2px;
	}
}

.num-input {
	width: 5rem;
	padding: var(--space-1) var(--space-2);
	border: 1px solid var(--theme-border, rgba(0, 0, 0, 0.15));
	border-radius: var(--radius-sm);
	background: var(--theme-input-bg, transparent);
	color: inherit;
	font-size: var(--font-sm);
	margin-right: var(--space-2);
}

.unit {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
}

.row-control {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	flex-wrap: wrap;
	justify-content: flex-end;
}

.one-line {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.save-row {
	padding: var(--space-2) var(--space-5) var(--space-3);
}

.form-actions {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	justify-content: flex-end;
	flex-wrap: wrap;
}

.all-row {
	padding: var(--space-2) var(--space-5) 0;
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
	margin-bottom: var(--space-2);
}

.warn-note,
.warn-text {
	color: var(--color-warning-fg, #b45309);
	font-size: var(--font-xs);
}

.warn-note {
	padding: var(--space-1) var(--space-5) 0;
	margin: 0;
}

.stuck-row .reason {
	white-space: normal;
	word-break: break-word;
}

@media (max-width: 600px) {
	.setting-row {
		flex-wrap: wrap;
	}

	.row-control {
		width: 100%;
		justify-content: flex-start;
		padding-left: var(--space-8);
	}
}
</style>
