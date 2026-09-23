<!-- src/apps/download-station/DownloadStationApp.vue -->
<!-- Download Station: multi-connection download manager + lite browser +
     built-in ad blocker, backed by nivaroos-download-sidecar. Same shell as
     VM Manager (left nav, content pane); every secondary surface - Add
     Download, download properties, folder picking, confirmations - opens as
     its own movable desktop window instead of a blocking overlay. -->
<template>
	<div class="ds-app" ref="root" :class="{ 'nav-collapsed': navCollapsed }">
		<aside class="ds-nav">
			<button v-for="s in sections" :key="s.id" class="nav-item hover-effect _is-radius"
				:class="{ active: activeSection === s.id }" :title="navCollapsed ? $t(s.label) : ''" @click="activeSection = s.id">
				<b-icon :icon="s.icon" pack="mdi" size="is-20"></b-icon>
				<span>{{ $t(s.label) }}</span>
				<span v-if="s.id === 'downloads' && activeCount" class="nav-badge">{{ activeCount }}</span>
			</button>
			<div class="nav-spacer"></div>
			<div v-if="!navCollapsed && totalSpeed" class="nav-speed" :title="$t('Total download speed')">
				<b-icon icon="speedometer" custom-size="mdi-16px"></b-icon>
				<span>{{ formatSpeed(totalSpeed) }}</span>
			</div>
		</aside>

		<div class="ds-content">
			<div v-if="offline" class="ds-offline">
				<b-icon icon="lan-disconnect" custom-size="mdi-48px"></b-icon>
				<p class="ds-offline-title">{{ $t('Download Station service is not running') }}</p>
				<p class="ds-offline-hint">{{ $t('Start it with') }} <code>sudo systemctl start nivaroos-download-sidecar</code></p>
				<button class="ds-primary-btn" @click="refresh">
					<b-icon icon="refresh" custom-size="mdi-18px"></b-icon><span>{{ $t('Retry') }}</span>
				</button>
			</div>
			<template v-else>
				<ds-download-list v-show="activeSection === 'downloads'" :downloads="downloads" :loading="loading"
					@refresh="refresh" @open-browser="openBrowserAt"></ds-download-list>
				<!-- Kept mounted (v-show, not v-if) so open tabs and their pages
				     survive switching to another section and back. -->
				<ds-browser v-if="browserStarted" v-show="activeSection === 'browser'" ref="browser"
					:visible="activeSection === 'browser'" :adblock-enabled="adblockEnabled"
					@toggle-adblock="toggleAdblock"></ds-browser>
				<ds-adblock-panel v-if="activeSection === 'adblock'" @changed="loadSettings"></ds-adblock-panel>
				<ds-settings-panel v-if="activeSection === 'settings'" @changed="loadSettings"></ds-settings-panel>
			</template>
		</div>
	</div>
</template>

<script>
import DsDownloadList from './DsDownloadList.vue'
import DsBrowser from './DsBrowser.vue'
import DsAdblockPanel from './DsAdblockPanel.vue'
import DsSettingsPanel from './DsSettingsPanel.vue'
import { downloadSidecar, formatSpeed } from '@/api/downloadSidecar'
import { activityService } from '@/service/activity'
import { escapeHtml } from '@/utils/escapeHtml'

// Same collapse threshold/behavior as VmManagerApp's nav.
const NAV_COLLAPSE_WIDTH = 760
const POLL_MS = 1000
const IDLE_POLL_MS = 4000

export default {
	name: 'download-station-app',
	components: { DsDownloadList, DsBrowser, DsAdblockPanel, DsSettingsPanel },
	props: {
		// Lets other code open the app straight into the browser at a URL.
		initialUrl: { type: String, default: '' },
		initialSection: { type: String, default: '' }
	},
	provide() {
		return { downloadStation: this }
	},
	data() {
		return {
			activeSection: this.initialSection || (this.initialUrl ? 'browser' : 'downloads'),
			sections: [
				{ id: 'downloads', label: 'Downloads', icon: 'tray-arrow-down' },
				{ id: 'browser', label: 'Browser', icon: 'web' },
				{ id: 'adblock', label: 'Ad Blocker', icon: 'shield-check-outline' },
				{ id: 'settings', label: 'Settings', icon: 'cog-outline' }
			],
			downloads: [],
			loading: true,
			offline: false,
			settings: null,
			navCollapsed: false,
			browserStarted: false,
			timer: null,
			lastEventSeq: null
		}
	},
	computed: {
		activeCount() {
			return this.downloads.filter(d => d.state === 'downloading' || d.state === 'queued').length
		},
		totalSpeed() {
			return this.downloads.reduce((n, d) => n + (d.state === 'downloading' ? d.speed || 0 : 0), 0)
		},
		adblockEnabled() {
			return !!(this.settings && this.settings.adblock_enabled)
		},
		isMinimized() {
			const win = this.$store.state.windows.find(w => w.id === 'download-station')
			return !!(win && win.minimized)
		}
	},
	watch: {
		activeSection: {
			immediate: true,
			handler(s) {
				if (s === 'browser') this.browserStarted = true
			}
		}
	},
	created() {
		this.refresh()
		this.loadSettings()
		this.schedulePoll()
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(entries => {
			this.navCollapsed = entries[0].contentRect.width < NAV_COLLAPSE_WIDTH
		})
		this.resizeObserver.observe(this.$refs.root)
		if (this.initialUrl) this.$nextTick(() => this.openBrowserAt(this.initialUrl))
	},
	beforeDestroy() {
		clearTimeout(this.timer)
		if (this.resizeObserver) this.resizeObserver.disconnect()
	},
	methods: {
		formatSpeed,
		schedulePoll() {
			clearTimeout(this.timer)
			// Fast while something is moving and the window is visible;
			// relaxed otherwise, but never stopped - completion notifications
			// still need to fire while minimized.
			const busy = this.activeCount > 0 && !this.isMinimized
			this.timer = setTimeout(async () => {
				await this.refresh()
				await this.pollEvents()
				this.schedulePoll()
			}, busy ? POLL_MS : IDLE_POLL_MS)
		},
		async refresh() {
			try {
				this.downloads = (await downloadSidecar.listDownloads()) || []
				this.offline = false
			} catch (e) {
				this.offline = true
			} finally {
				this.loading = false
			}
		},
		async loadSettings() {
			try {
				this.settings = await downloadSidecar.getSettings()
			} catch (e) {}
		},
		async pollEvents() {
			if (this.offline) return
			try {
				const res = await downloadSidecar.events(this.lastEventSeq || 0)
				// First poll only establishes "now" - don't replay history
				// from before this window opened.
				if (this.lastEventSeq !== null) {
					for (const ev of res.events || []) this.notify(ev)
				}
				this.lastEventSeq = res.last
			} catch (e) {}
		},
		notify(ev) {
			if (ev.kind === 'completed') {
				activityService.add({ title: this.$t('Download complete'), message: ev.filename, type: 'system', status: 'success' })
				this.$buefy.toast.open({ message: `${this.$t('Downloaded')} ${escapeHtml(ev.filename)}`, type: 'is-success', position: 'is-bottom-right' })
			} else if (ev.kind === 'failed') {
				activityService.add({ title: this.$t('Download failed'), message: `${ev.filename}: ${ev.message}`, type: 'system', status: 'error' })
			}
		},
		async toggleAdblock() {
			if (!this.settings) return
			this.settings = await downloadSidecar.updateSettings({ adblock_enabled: !this.settings.adblock_enabled })
		},
		openBrowserAt(url) {
			this.activeSection = 'browser'
			this.browserStarted = true
			this.$nextTick(() => this.$refs.browser && this.$refs.browser.openUrl(url, true))
		},

		// ---- windows shared by the child components ----
		openAddDownload(props = {}) {
			const id = 'ds-add-' + Date.now()
			this.$store.commit('OPEN_WINDOW', {
				id,
				title: this.$t('Add Download'),
				component: 'DsAddDownloadWindow',
				props: { ...props, winId: id },
				width: 560,
				height: 520
			})
		},
		openDetails(download) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'ds-detail-' + download.id,
				title: download.filename,
				component: 'DsDownloadDetailWindow',
				props: { downloadId: download.id },
				width: 580,
				height: 520
			})
		},
		openFolder(dir) {
			this.$store.commit('SET_CURRENT_PATH', dir)
			this.$store.commit('OPEN_WINDOW', { id: 'files', title: this.$t('Files'), component: 'FilesApp', width: 960, height: 620 })
		}
	}
}
</script>

<style lang="scss" scoped>
.ds-app {
	position: relative;
	display: flex;
	height: 100%;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
	font-family: $family-sans-serif;
}

.ds-nav {
	flex-shrink: 0;
	width: 13.5rem;
	padding: var(--space-5) var(--space-3);
	background: var(--theme-card-bg, #ffffff);
	border-right: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	user-select: none;
}

.nav-collapsed .ds-nav {
	width: 3.75rem;
	padding: var(--space-5) var(--space-2);

	.nav-item span {
		display: none;
	}

	.nav-item {
		justify-content: center;
		padding: var(--space-2);
		position: relative;
	}

	.nav-badge {
		display: block !important;
		position: absolute;
		top: 2px;
		right: 2px;
		min-width: 1rem;
		font-size: 0.6rem;
		padding: 0 0.2rem;
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	border: none;
	background: transparent;
	color: var(--theme-text-secondary, #475569);
	padding: var(--space-2) var(--space-3);
	font-size: var(--font-base);
	font-weight: 400;
	border-radius: var(--radius-control);
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: background 0.12s ease, color 0.12s ease;

	.icon {
		color: var(--theme-text-muted, #94a3b8);
		transition: color 0.12s ease;
		width: 20px;
		height: 20px;
		font-size: var(--font-xl);
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;

		i {
			font-size: var(--font-xl);
			line-height: 1;
		}
	}

	&:hover {
		background: var(--theme-bg-window, #f8fafc);
		color: var(--theme-text-primary, #1e293b);

		.icon {
			color: var(--theme-text-primary, #1e293b);
		}
	}

	&.active {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #1e293b);
		font-weight: 500;

		.icon {
			color: #2563eb;
		}
	}
}

.nav-badge {
	margin-left: auto;
	min-width: 1.25rem;
	padding: 0 0.35rem;
	border-radius: var(--radius-pill);
	background: #2563eb;
	color: #fff;
	font-size: var(--font-2xs);
	font-weight: 600;
	line-height: 1.25rem;
	text-align: center;
}

.nav-spacer {
	flex: 1 1 auto;
}

.nav-speed {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	font-size: var(--font-xs);
	font-variant-numeric: tabular-nums;
	color: var(--theme-text-secondary, #475569);
	border-radius: var(--radius-control);
	background: var(--theme-card-subtle, #f1f5f9);

	.icon {
		color: #2563eb;
	}
}

.ds-content {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	background: var(--theme-bg-window, #f8fafc);
	overflow: hidden;

	> * {
		flex: 1 1 auto;
		min-height: 0;
	}
}

.ds-offline {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-muted, #94a3b8);

	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	code {
		font-size: var(--font-xs);
	}
}

.ds-offline-title {
	font-size: var(--font-md);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
	margin: var(--space-2) 0 var(--space-1);
}

.ds-offline-hint {
	margin: 0 0 var(--space-4);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary, #64748b);
}
</style>
