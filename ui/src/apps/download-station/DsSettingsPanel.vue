<!-- src/apps/download-station/DsSettingsPanel.vue -->
<template>
	<div class="ds-panel scrollbars-light">
		<div class="panel-head">
			<h2 class="ds-section-title">{{ $t('Settings') }}</h2>
		</div>
		<template v-if="s">
			<h3 class="setting-card-title">{{ $t('Downloads') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="folder-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Default save folder') }}</div>
						<div class="setting-desc mono">{{ s.default_dir }}<template v-if="diskFree"> &middot; {{ formatBytes(diskFree) }} {{ $t('free') }}</template></div>
					</div>
					<div class="row-control">
						<button class="ds-secondary-btn" @click="browse">{{ $t('Change') }}</button>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="lan-connect" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Connections per download') }}</div>
						<div class="setting-desc">{{ $t('Default for new downloads. Each connection fetches its own part of the file.') }}</div>
					</div>
					<div class="row-control slider-control">
						<input v-model.number="s.default_connections" class="pretty-range" type="range" min="1" max="32"
							:style="{ '--pct': ((s.default_connections - 1) / 31) * 100 + '%' }" @change="save({ default_connections: s.default_connections })" />
						<span class="slider-value">{{ s.default_connections }}</span>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="download-multiple" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Simultaneous downloads') }}</div>
						<div class="setting-desc">{{ $t('More than this wait in the queue.') }}</div>
					</div>
					<div class="row-control slider-control">
						<input v-model.number="s.max_concurrent" class="pretty-range" type="range" min="1" max="10"
							:style="{ '--pct': ((s.max_concurrent - 1) / 9) * 100 + '%' }" @change="save({ max_concurrent: s.max_concurrent })" />
						<span class="slider-value">{{ s.max_concurrent }}</span>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="speedometer" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Speed limit') }}</div>
						<div class="setting-desc">{{ $t('Shared by all downloads. Leave at 0 for unlimited.') }}</div>
					</div>
					<div class="row-control limit-control">
						<input v-model.number="limitMB" class="ds-input limit-input" type="number" min="0" step="0.5" @change="saveLimit" />
						<span class="limit-unit">MB/s</span>
					</div>
				</div>
			</div>

			<h3 class="setting-card-title">{{ $t('Browser') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="home-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Home page') }}</div>
						<input v-model="s.home_page" class="ds-input home-input" spellcheck="false" @change="save({ home_page: s.home_page })" />
					</div>
				</div>
			</div>
		</template>
	</div>
</template>

<script>
import { downloadSidecar, formatBytes } from '@/api/downloadSidecar'

export default {
	name: 'ds-settings-panel',
	data() {
		return { s: null, limitMB: 0, diskFree: 0 }
	},
	async created() {
		try {
			this.s = await downloadSidecar.getSettings()
			this.limitMB = Math.round((this.s.speed_limit / (1024 * 1024)) * 10) / 10
			const st = await downloadSidecar.status()
			this.diskFree = st.disk_free || 0
		} catch (e) {}
	},
	methods: {
		formatBytes,
		async save(patch) {
			try {
				this.s = await downloadSidecar.updateSettings(patch)
				this.$emit('changed')
			} catch (e) {
				this.$buefy.toast.open({ message: this.$t('Could not save'), type: 'is-danger' })
			}
		},
		saveLimit() {
			const v = Math.max(0, Number(this.limitMB) || 0)
			this.save({ speed_limit: Math.round(v * 1024 * 1024) })
		},
		browse() {
			const id = 'ds-folder-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Default save folder'),
				component: 'DsFolderPickerWindow',
				props: { winId: id, startPath: this.s.default_dir, onSelect: path => this.save({ default_dir: path }) },
				width: 460,
				height: 440
			})
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
	margin-bottom: var(--space-4);
}

.mono {
	font-family: $family-monospace;
	word-break: break-all;
}

.slider-control .pretty-range {
	width: 9rem;
}

.limit-control {
	gap: var(--space-2);
}

.limit-input {
	width: 5.5rem;
	text-align: right;
}

.limit-unit {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}

.home-input {
	margin-top: var(--space-2);
	max-width: 28rem;
}
</style>
