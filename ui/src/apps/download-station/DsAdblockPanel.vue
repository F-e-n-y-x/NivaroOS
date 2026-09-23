<!-- src/apps/download-station/DsAdblockPanel.vue -->
<!-- Ad blocker controls, laid out like uBlock Origin's dashboard: a master
     switch, the filter lists (uBO's own defaults), "My filters", and the
     per-site allowlist ("trusted sites"). -->
<template>
	<div class="ds-panel scrollbars-light">
		<div class="panel-head">
			<h2 class="ds-section-title">{{ $t('Ad Blocker') }}</h2>
			<button class="ds-secondary-btn" :disabled="stats && stats.updating" @click="updateLists">
				<b-icon icon="refresh" :custom-class="stats && stats.updating ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
				<span>{{ stats && stats.updating ? $t('Updating...') : $t('Update lists now') }}</span>
			</button>
		</div>

		<div class="shield-hero" :class="{ off: !enabled }">
			<div class="hero-icon">
				<b-icon :icon="enabled ? 'shield-check-outline' : 'shield-off-outline'" custom-size="mdi-32px"></b-icon>
			</div>
			<div class="hero-text">
				<div class="hero-title">{{ enabled ? $t('Blocking ads and trackers') : $t('Ad blocker is off') }}</div>
				<div class="hero-sub" v-if="stats">
					{{ stats.network_rules.toLocaleString() }} {{ $t('network filters') }} &middot; {{ stats.cosmetic_rules.toLocaleString() }} {{ $t('element-hiding filters') }}
					&middot; {{ stats.blocked_total.toLocaleString() }} {{ $t('blocked since start') }}
				</div>
			</div>
			<b-switch :value="enabled" @input="setEnabled"></b-switch>
		</div>

		<h3 class="setting-card-title">{{ $t('Filter lists') }}</h3>
		<div class="setting-card">
			<div v-for="l in lists" :key="l.id" class="setting-row">
				<div class="row-label">
					<div class="setting-title">{{ l.name }} <span class="list-group">{{ l.group }}</span></div>
					<div class="setting-desc">
						{{ l.description }}
						<template v-if="l.enabled">
							&middot;
							<span v-if="l.error" class="ds-error-text">{{ $t('update failed') }}: {{ l.error }}</span>
							<span v-else-if="l.updated_at">{{ formatBytes(l.size) }}, {{ $t('updated') }} {{ ago(l.updated_at) }}</span>
							<span v-else>{{ $t('downloading...') }}</span>
						</template>
					</div>
				</div>
				<div class="row-control">
					<b-switch size="is-small" :value="l.enabled" :disabled="!enabled" @input="v => toggleList(l.id, v)"></b-switch>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Trusted sites') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<div class="row-label">
					<div class="setting-desc">{{ $t('The ad blocker is switched off on these sites (and their subdomains).') }}</div>
					<div class="chips">
						<span v-for="s in allowedSites" :key="s" class="site-chip">
							{{ s }}
							<button :title="$t('Remove')" @click="removeSite(s)"><b-icon icon="close" custom-size="mdi-12px"></b-icon></button>
						</span>
						<span v-if="!allowedSites.length" class="ds-hint">{{ $t('None') }}</span>
					</div>
					<form class="inline-form" @submit.prevent="addSite">
						<input v-model="newSite" class="ds-input" placeholder="example.com" spellcheck="false" />
						<button class="ds-secondary-btn" type="submit" :disabled="!newSite.trim()">{{ $t('Add') }}</button>
					</form>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('My filters') }}</h3>
		<div class="setting-card">
			<div class="setting-row filters-row">
				<div class="row-label">
					<div class="setting-desc">{{ $t('Your own rules, in uBlock Origin syntax - e.g.') }} <code>||ads.example.com^</code> {{ $t('or') }} <code>example.com##.banner</code></div>
					<textarea v-model="customFilters" class="ds-input" rows="6" spellcheck="false"></textarea>
					<div class="filters-actions">
						<button class="ds-primary-btn" :disabled="customFilters === savedCustom" @click="saveCustom">{{ $t('Apply changes') }}</button>
					</div>
				</div>
			</div>
		</div>

		<p class="ds-hint footnote">
			{{ $t('Uses uBlock Origin\'s own filter lists and filter syntax, applied by the lite browser\'s proxy. Network blocking and element hiding are supported; scriptlet (+js) and procedural filters are skipped.') }}
		</p>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes } from '@/api/downloadSidecar'

export default {
	name: 'ds-adblock-panel',
	data() {
		return { stats: null, settings: null, newSite: '', customFilters: '', savedCustom: '', timer: null }
	},
	computed: {
		enabled() {
			return !!(this.settings && this.settings.adblock_enabled)
		},
		lists() {
			return (this.stats && this.stats.lists) || []
		},
		allowedSites() {
			return (this.settings && this.settings.allowed_sites) || []
		}
	},
	async created() {
		await this.load()
		this.customFilters = this.savedCustom = (this.settings && this.settings.custom_filters) || ''
		this.timer = setInterval(this.loadStats, 3000)
	},
	beforeDestroy() {
		clearInterval(this.timer)
	},
	methods: {
		formatBytes,
		ago(t) {
			const s = Math.round((Date.now() - new Date(t).getTime()) / 1000)
			if (s < 90) return this.$t('just now')
			if (s < 5400) return Math.round(s / 60) + ' ' + this.$t('min ago')
			if (s < 129600) return Math.round(s / 3600) + ' ' + this.$t('h ago')
			return Math.round(s / 86400) + ' ' + this.$t('days ago')
		},
		async load() {
			try {
				;[this.settings, this.stats] = await Promise.all([downloadSidecar.getSettings(), downloadSidecar.adblockStats()])
			} catch (e) {}
		},
		async loadStats() {
			try {
				this.stats = await downloadSidecar.adblockStats()
			} catch (e) {}
		},
		async patch(p) {
			try {
				this.settings = await downloadSidecar.updateSettings(p)
				this.$emit('changed')
				// The engine rebuilds in the background; refresh counts shortly.
				setTimeout(this.loadStats, 800)
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t('Could not save'), type: 'is-danger' })
			}
		},
		setEnabled(v) {
			this.patch({ adblock_enabled: v })
		},
		async toggleList(id, on) {
			const cur = new Set(this.settings.enabled_lists || [])
			on ? cur.add(id) : cur.delete(id)
			await this.patch({ enabled_lists: [...cur] })
			// A newly enabled list needs downloading.
			if (on) downloadSidecar.updateFilterLists().then(s => (this.stats = s)).catch(() => {})
		},
		async updateLists() {
			try {
				this.stats = await downloadSidecar.updateFilterLists()
			} catch (e) {}
		},
		addSite() {
			let s = this.newSite.trim().toLowerCase()
			try {
				if (/^https?:\/\//.test(s)) s = new URL(s).hostname
			} catch (e) {}
			s = s.replace(/^www\./, '').replace(/\/.*$/, '')
			if (!s) return
			const next = [...new Set([...this.allowedSites, s])]
			this.newSite = ''
			this.patch({ allowed_sites: next })
		},
		removeSite(s) {
			this.patch({ allowed_sites: this.allowedSites.filter(x => x !== s) })
		},
		async saveCustom() {
			await this.patch({ custom_filters: this.customFilters })
			this.savedCustom = this.customFilters
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-panel {
	height: 100%;
	overflow-y: auto;
	padding: var(--space-6);
}

.panel-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-3);
	margin-bottom: var(--space-4);
}

.shield-hero {
	display: flex;
	align-items: center;
	gap: var(--space-4);
	padding: var(--space-4) var(--space-5);
	margin-bottom: var(--space-2);
	border-radius: 14px;
	background: linear-gradient(135deg, rgba(37, 99, 235, 0.1), rgba(56, 189, 248, 0.08));
	border: 1px solid rgba(37, 99, 235, 0.18);

	&.off {
		background: var(--theme-card-bg, #fff);
		border-color: var(--theme-card-border, rgba(0, 0, 0, 0.08));

		.hero-icon {
			background: var(--theme-card-subtle, #f1f5f9);
			color: var(--theme-text-muted, #94a3b8);
		}
	}
}

.hero-icon {
	flex-shrink: 0;
	width: 3.25rem;
	height: 3.25rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: #2563eb;
	color: #fff;

	::v-deep .icon {
		width: 2rem;
		height: 2rem;
	}
}

.hero-text {
	flex: 1 1 auto;
	min-width: 0;
}

.hero-title {
	font-size: var(--font-md);
	font-weight: 600;
}

.hero-sub {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #64748b);
	margin-top: 0.2rem;
	font-variant-numeric: tabular-nums;
}

.list-group {
	margin-left: var(--space-1);
	font-size: var(--font-2xs);
	padding: 0 0.4rem;
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);
}

.chips {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	margin: var(--space-2) 0;
}

.site-chip {
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	padding: 0.15rem 0.3rem 0.15rem 0.6rem;
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	font-size: var(--font-xs);

	button {
		display: inline-flex;
		border: none;
		background: transparent;
		cursor: pointer;
		color: var(--theme-text-muted, #94a3b8);
		padding: 0;

		&:hover {
			color: #dc2626;
		}
	}
}

.inline-form {
	display: flex;
	gap: var(--space-2);
	max-width: 24rem;

	.ds-secondary-btn {
		height: 2.1rem;
	}
}

.filters-row .row-label {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);

	code {
		font-size: var(--font-2xs);
	}
}

.filters-actions {
	display: flex;
	justify-content: flex-end;
}

.footnote {
	margin-top: var(--space-4);
}
</style>
