<template>
	<div class="container-console-panel">
		<!-- Top Window Toolbar / Titlebar -->
		<div class="panel-header" @pointerdown="$emit('drag-start', $event)">
			<div class="header-left is-flex is-align-items-center">
				<b-icon icon="docker" pack="mdi" size="is-20" class="docker-icon mr-2"></b-icon>
				<span class="container-title one-line font-weight-bold" :title="displayTitle">{{ displayTitle }}</span>
				<span v-if="shortImage" class="image-tag ml-2" :title="containerImage">{{ shortImage }}</span>
				<span class="status-badge ml-2" :class="isContainerRunning ? 'is-running' : 'is-stopped'">
					<span class="status-dot"></span>
					{{ isContainerRunning ? $t('Running') : $t('Stopped') }}
				</span>
			</div>

			<!-- Spacer pushes switcher and controls to the right -->
			<div class="header-spacer"></div>

			<!-- Right side: Tab switchers, tools, and window controls -->
			<div class="header-right is-flex is-align-items-center">
				<div class="console-tabs mr-3">
					<button
						type="button"
						class="console-tab-btn"
						:class="{ active: activeTab === 'terminal' }"
						:aria-pressed="activeTab === 'terminal' ? 'true' : 'false'"
						@click="switchTab('terminal')"
					>
						<i class="mdi mdi-console mr-1"></i>
						{{ $t('Terminal') }}
					</button>
					<button
						type="button"
						class="console-tab-btn"
						:class="{ active: activeTab === 'logs' }"
						:aria-pressed="activeTab === 'logs' ? 'true' : 'false'"
						@click="switchTab('logs')"
					>
						<i class="mdi mdi-text-box-search-outline mr-1"></i>
						{{ $t('Logs') }}
					</button>
				</div>

				<button
					v-if="activeTab === 'logs'"
					type="button"
					class="header-tool-btn mr-3"
					:class="{ 'is-spinning': loadingLogs }"
					:title="$t('Refresh logs')"
					:aria-label="$t('Refresh logs')"
					@click="fetchLogs()"
				>
					<i class="mdi mdi-refresh" aria-hidden="true"></i>
				</button>

				<div class="window-controls">
					<button type="button" class="window-btn window-btn-minimize" :title="$t('Minimize')" :aria-label="$t('Minimize')" @click.stop="$emit('minimize')"></button>
					<button type="button" class="window-btn window-btn-close" :title="$t('Close')" :aria-label="$t('Close')" @click.stop="$emit('close')"></button>
				</div>
			</div>
		</div>

		<!-- Panel Content Body -->
		<div class="panel-body">
			<!-- Tab 1: Interactive Terminal -->
			<div v-show="activeTab === 'terminal'" class="tab-pane terminal-pane">
				<div v-if="!isContainerRunning" class="stopped-warning is-flex is-flex-direction-column is-align-items-center is-justify-content-center">
					<i class="mdi mdi-alert-circle-outline is-size-1 has-text-warning mb-3"></i>
					<p class="is-size-6 mb-3 text-muted">{{ $t('Container is stopped. Start it to open interactive terminal.') }}</p>
					<b-button rounded type="is-primary" size="is-small" :loading="starting" @click="startContainer">
						<i class="mdi mdi-play mr-1"></i>
						{{ $t('Start Container') }}
					</b-button>
				</div>
				<terminal-card
					v-else
					ref="terminalCard"
					:id="containerId"
					:init-ws-url="terminalWsUrl"
				></terminal-card>
			</div>

			<!-- Tab 2: Logs Viewer -->
			<div v-show="activeTab === 'logs'" class="tab-pane logs-pane">
				<!-- Logs Action Bar (Dark Frosted Glass Theme) -->
				<div class="logs-toolbar is-flex is-align-items-center is-justify-content-between">
					<div class="toolbar-left is-flex is-align-items-center">
						<!-- Search Filter -->
						<div class="search-box">
							<i class="mdi mdi-magnify search-icon"></i>
							<input
								v-model="logSearch"
								type="text"
								class="log-search-input"
								:placeholder="$t('Filter logs...')"
								:aria-label="$t('Filter logs...')"
							/>
							<button v-if="logSearch" type="button" class="clear-search-btn" :aria-label="$t('Clear filter')" @click="logSearch = ''">
								<i class="mdi mdi-close" aria-hidden="true"></i>
							</button>
						</div>

						<!-- Line Count Selector -->
						<div class="custom-select-wrap">
							<select v-model="lineCount" :aria-label="$t('Number of lines')" @change="fetchLogs()">
								<option :value="100">100 {{ $t('lines') }}</option>
								<option :value="500">500 {{ $t('lines') }}</option>
								<option :value="1000">1000 {{ $t('lines') }}</option>
								<option :value="2000">2000 {{ $t('lines') }}</option>
								<option :value="5000">5000 {{ $t('lines') }}</option>
							</select>
							<i class="mdi mdi-chevron-down select-chevron"></i>
						</div>

						<!-- Timestamps checkbox -->
						<label class="custom-check">
							<input type="checkbox" v-model="showTimestamps" @change="fetchLogs()" />
							<span>{{ $t('Timestamps') }}</span>
						</label>

						<!-- Auto-refresh checkbox -->
						<label class="custom-check">
							<input type="checkbox" v-model="autoRefresh" />
							<span>{{ $t('Live Auto-refresh') }}</span>
						</label>

						<!-- Auto-scroll checkbox -->
						<label class="custom-check">
							<input type="checkbox" v-model="autoScroll" />
							<span>{{ $t('Follow') }}</span>
						</label>
					</div>

					<div class="toolbar-right is-flex is-align-items-center">
						<span v-if="logSearch" class="match-pill mr-3">
							{{ filteredLines.length }} / {{ visibleLines.length }} {{ $t('matches') }}
							</span>
							<button type="button" class="toolbar-btn mr-2" :disabled="!visibleLines.length" :aria-label="$t('Copy')" @click="copyLogs">
								<i class="mdi mdi-content-copy mr-1" aria-hidden="true"></i>
								<span class="toolbar-btn-text">{{ $t('Copy') }}</span>
							</button>
							<button type="button" class="toolbar-btn mr-2" :disabled="!visibleLines.length" :aria-label="$t('Download')" @click="downloadLogs">
								<i class="mdi mdi-download mr-1" aria-hidden="true"></i>
								<span class="toolbar-btn-text">{{ $t('Download') }}</span>
							</button>
							<button v-if="!clearMarker" type="button" class="toolbar-btn" :disabled="!visibleLines.length" :aria-label="$t('Clear View')" @click="clearLogsView">
								<i class="mdi mdi-trash-can-outline mr-1" aria-hidden="true"></i>
								<span class="toolbar-btn-text">{{ $t('Clear View') }}</span>
							</button>
							<button v-else type="button" class="toolbar-btn" :aria-label="$t('Show earlier lines')" @click="restoreLogsView">
								<i class="mdi mdi-history mr-1" aria-hidden="true"></i>
								<span class="toolbar-btn-text">{{ $t('Show earlier lines') }}</span>
							</button>
					</div>
				</div>

				<!-- Log Lines Viewport -->
				<div v-if="logError" class="logs-error" role="alert">
					<i class="mdi mdi-alert-circle-outline mr-1" aria-hidden="true"></i>{{ logError }}
				</div>
				<div ref="logViewport" class="log-viewport scrollbars" tabindex="0" role="log" :aria-label="$t('Container logs')" @scroll="onLogScroll">
					<div v-if="loadingLogs && !logLines.length" class="loading-state is-flex is-align-items-center is-justify-content-center">
						<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin mr-2"></b-icon>
						<span>{{ $t('Loading logs...') }}</span>
					</div>
					<div v-else-if="!visibleLines.length" class="empty-logs is-flex is-align-items-center is-justify-content-center">
						<span class="text-muted is-size-7">{{ clearMarker ? $t('No new log lines since the view was cleared.') : $t('No log output recorded yet for this container.') }}</span>
					</div>
					<div v-else class="log-content">
						<!-- Keyed by a per-line sequence id, so a refresh that only adds
						     lines patches just the new rows instead of every row. -->
						<div
							v-for="line in filteredLines"
							:key="line.id"
							class="log-line"
							:class="{ 'highlight': logSearch }"
						>
							<span class="line-num">{{ line.id }}</span>
							<span class="line-text">{{ line.text }}</span>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import qs from 'qs'
import TerminalCard from '@/apps/terminal/TerminalCard.vue'

const LOG_POLL_MS = 3000
const STATUS_POLL_MS = 10000
// Within this many px of the bottom counts as "following" the log.
const FOLLOW_SLACK_PX = 48

export default {
	name: 'container-console-panel',
	components: {
		TerminalCard
	},
	props: {
		containerId: {
			type: String,
			required: true
		},
		containerName: {
			type: [String, Object],
			default: ''
		},
		containerImage: {
			type: String,
			default: ''
		},
		initialTab: {
			type: String,
			default: 'terminal'
		},
		status: {
			type: String,
			default: 'running'
		},
		// Stamped by OPEN_WINDOW when an already-open console is asked for
		// again (e.g. "Logs" clicked for a container whose Terminal is open).
		requestedAt: {
			type: Number,
			default: 0
		}
	},
	data() {
		return {
			activeTab: this.initialTab || 'terminal',
			// [{ id, text }] - id is a sequence number that stays with the
			// line across refreshes (see mergeLogs()).
			logLines: [],
			nextLineId: 1,
			clearMarker: null,
			logError: '',
			loadingLogs: false,
			logsInFlight: false,
			lineCount: 500,
			showTimestamps: true,
			autoRefresh: true,
			autoScroll: true,
			nearBottom: true,
			logSearch: '',
			logTimer: null,
			statusTimer: null,
			restarting: false,
			starting: false,
			currentStatus: this.status || 'running'
		}
	},
	computed: {
		displayTitle() {
			let val = this.containerName || this.containerId
			if (!val) return this.containerId || ''
			if (typeof val === 'object') {
				return val.custom || val.en_us || val.en_US || Object.values(val)[0] || this.containerId
			}
			if (typeof val === 'string' && val.trim().startsWith('{')) {
				try {
					const p = JSON.parse(val)
					if (typeof p === 'object' && p !== null) {
						return p.custom || p.en_us || p.en_US || Object.values(p)[0] || this.containerId
					}
				} catch (e) { /* not JSON */ }
			}
			return val
		},
		isContainerRunning() {
			return this.currentStatus === 'running' || this.currentStatus === 'healthy'
		},
		shortImage() {
			if (!this.containerImage) return ''
			const parts = this.containerImage.split('/')
			return parts[parts.length - 1]
		},
		// cols/rows are placeholders: TerminalCard replaces them with the
		// fitted size of the terminal when it connects.
		terminalWsUrl() {
			const query = {
				token: this.$store.state.access_token,
				cols: 120,
				rows: 32
			}
			return `${this.$wsProtocol}//${this.$baseURL}/v1/container/${this.containerId}/terminal?${qs.stringify(query)}`
		},
		// Lines after the "Clear view" marker.
		visibleLines() {
			if (!this.clearMarker) return this.logLines
			return this.logLines.filter(l => l.id > this.clearMarker)
		},
		filteredLines() {
			if (!this.logSearch) return this.visibleLines
			const q = this.logSearch.toLowerCase()
			return this.visibleLines.filter(line => line.text.toLowerCase().includes(q))
		}
	},
	mounted() {
		if (this.activeTab === 'logs') {
			this.fetchLogs()
		}
		this.startLogPolling()
		this.statusTimer = setInterval(this.refreshStatus, STATUS_POLL_MS)
		document.addEventListener('visibilitychange', this.onVisibility)
	},
	beforeDestroy() {
		this.stopLogPolling()
		clearInterval(this.statusTimer)
		document.removeEventListener('visibilitychange', this.onVisibility)
	},
	watch: {
		autoRefresh(val) {
			if (val) {
				this.startLogPolling()
			} else {
				this.stopLogPolling()
			}
		},
		// Props are not copied once: a re-open of the same console (same
		// window id) updates them, and the window follows.
		initialTab(tab) {
			if (tab) this.switchTab(tab)
		},
		requestedAt() {
			if (this.initialTab) this.switchTab(this.initialTab)
			this.refreshStatus()
		},
		status(val) {
			if (val) this.currentStatus = val
		},
		currentStatus(val, old) {
			// Came back up / went down: pick up the new log lines at once.
			if (val !== old && this.activeTab === 'logs') this.fetchLogs(true)
		}
	},
	methods: {
		// On screen = page visible and this window not minimised
		// (a minimised window is display:none -> no client rects).
		isOnScreen() {
			return !document.hidden && !!this.$el && this.$el.getClientRects().length > 0
		},
		onVisibility() {
			if (!this.isOnScreen()) return
			this.refreshStatus()
			if (this.activeTab === 'logs' && this.autoRefresh) this.fetchLogs(true)
		},
		switchTab(tab) {
			this.activeTab = tab
			if (tab === 'logs') {
				this.fetchLogs()
			} else if (tab === 'terminal') {
				this.$nextTick(() => {
					if (this.$refs.terminalCard && typeof this.$refs.terminalCard.active === 'function') {
						this.$refs.terminalCard.active(true)
					}
				})
			}
		},
		startLogPolling() {
			this.stopLogPolling()
			this.logTimer = setInterval(() => {
				if (this.activeTab === 'logs' && this.autoRefresh && this.isOnScreen()) {
					this.fetchLogs(true)
				}
			}, LOG_POLL_MS)
		},
		stopLogPolling() {
			if (this.logTimer) {
				clearInterval(this.logTimer)
				this.logTimer = null
			}
		},
		async refreshStatus() {
			if (!this.isOnScreen()) return
			try {
				const res = await this.$api.container.getAllContainersWithUpdates()
				const list = (res && res.data && res.data.data) || []
				const id = this.containerId
				const name = String(id).replace(/^\//, '')
				const c = list.find(x => x && (x.id === id || (x.id && (x.id.startsWith(id) || id.startsWith(x.id))) || x.name === name))
				if (c && c.state) this.currentStatus = c.state
			} catch (e) {
				// Keep the last known state; the next tick retries.
			}
		},
		async fetchLogs(silent = false) {
			if (this.logsInFlight) {
				// An explicit refresh (line count changed...) must not be lost
				// behind a background tick that is still running.
				if (!silent) this.pendingFullFetch = true
				return
			}
			this.logsInFlight = true
			if (!silent) this.loadingLogs = true
			try {
				const res = await this.$api.container.getRawLogs(
					this.containerId,
					this.lineCount,
					this.showTimestamps
				)
				let text = ''
				if (res && res.data) {
					if (typeof res.data === 'string') {
						text = res.data
					} else if (res.data.data !== undefined) {
						text = res.data.data || ''
					} else {
						text = JSON.stringify(res.data, null, 2)
					}
				}
				this.logError = ''
				this.mergeLogs(text, !silent)
			} catch (err) {
				const reason = (err && err.response && err.response.data && err.response.data.message) || (err && err.message) || this.$t('Unknown error')
				this.logError = this.$t('Could not load logs: {reason}', { reason })
			} finally {
				this.logsInFlight = false
				if (!silent) this.loadingLogs = false
				if (this.pendingFullFetch) {
					this.pendingFullFetch = false
					this.fetchLogs()
				}
			}
		},
		// The API only returns "the last N lines", so each refresh is lined
		// up with what is already shown: the longest suffix of the current
		// lines that is a prefix of the new ones is kept (same ids), only
		// the rest is appended. No new lines -> no reactive change at all.
		// replace=true (explicit refresh, line count/timestamps changed)
		// renders the response as-is.
		mergeLogs(text, replace = false) {
			const incoming = text.split('\n')
			if (incoming.length && incoming[incoming.length - 1] === '') incoming.pop()
			if (replace) this.clearMarker = null
			const old = replace ? [] : this.logLines
			let keepFrom = -1
			let overlap = 0
			if (old.length && incoming.length) {
				const last = old[old.length - 1].text
				// Try the most recent occurrence of the last shown line first.
				for (let j = incoming.length - 1; j >= 0 && keepFrom < 0; j--) {
					if (incoming[j] !== last) continue
					let ok = true
					for (let k = 1; k <= j && k < old.length && k <= 50; k++) {
						if (incoming[j - k] !== old[old.length - 1 - k].text) {
							ok = false
							break
						}
					}
					if (ok) {
						overlap = j + 1
						keepFrom = Math.max(0, old.length - overlap)
					}
				}
			}
			if (keepFrom >= 0) {
				const added = incoming.slice(overlap)
				const kept = old.slice(keepFrom)
				if (!added.length && keepFrom === 0) return
				const next = kept.concat(added.map(t => ({ id: this.nextLineId++, text: t })))
				this.logLines = next.length > this.lineCount ? next.slice(next.length - this.lineCount) : next
			} else {
				this.logLines = incoming.map(t => ({ id: this.nextLineId++, text: t }))
			}
			if (this.autoScroll && this.nearBottom) {
				this.$nextTick(this.scrollToBottom)
			}
		},
		onLogScroll() {
			const vp = this.$refs.logViewport
			if (!vp) return
			this.nearBottom = vp.scrollHeight - vp.scrollTop - vp.clientHeight <= FOLLOW_SLACK_PX
		},
		scrollToBottom() {
			const vp = this.$refs.logViewport
			if (vp) {
				vp.scrollTop = vp.scrollHeight
			}
		},
		// Hides what is on screen now without re-fetching: later refreshes
		// only show lines that arrive after this point.
		clearLogsView() {
			const last = this.logLines[this.logLines.length - 1]
			this.clearMarker = last ? last.id : this.nextLineId - 1
			this.nearBottom = true
		},
		restoreLogsView() {
			this.clearMarker = null
			this.$nextTick(this.scrollToBottom)
		},
		visibleText() {
			return this.visibleLines.map(l => l.text).join('\n')
		},
		copyLogs() {
			const text = this.visibleText()
			if (!text) return
			const done = () => this.$buefy.toast.open({
				message: this.$t('Logs copied to clipboard'),
				type: 'is-success',
				position: 'is-top',
				duration: 2000
			})
			const fail = () => this.$buefy.toast.open({
				message: this.$t('Failed to copy logs'),
				type: 'is-danger',
				position: 'is-top',
				duration: 2000
			})
			if (!navigator.clipboard || !navigator.clipboard.writeText) {
				fail()
				return
			}
			navigator.clipboard.writeText(text).then(done).catch(fail)
		},
		downloadLogs() {
			const text = this.visibleText()
			if (!text) return
			const blob = new Blob([text + '\n'], { type: 'text/plain;charset=utf-8' })
			const url = URL.createObjectURL(blob)
			const a = document.createElement('a')
			a.href = url
			a.download = `${this.displayTitle || this.containerId}-logs-${new Date().toISOString().slice(0, 10)}.log`
			document.body.appendChild(a)
			a.click()
			document.body.removeChild(a)
			URL.revokeObjectURL(url)
		},
		async restartContainer() {
			this.restarting = true
			try {
				await this.$api.container.updateState(this.containerId, 'restart')
				this.currentStatus = 'running'
				this.$buefy.toast.open({
					message: this.$t('Container restarted successfully'),
					type: 'is-success',
					position: 'is-top',
					duration: 2500
				})
				if (this.activeTab === 'logs') {
					setTimeout(() => this.fetchLogs(), 1500)
				}
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to restart container'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.restarting = false
			}
		},
		async startContainer() {
			this.starting = true
			try {
				await this.$api.container.updateState(this.containerId, 'start')
				this.currentStatus = 'running'
				this.$buefy.toast.open({
					message: this.$t('Container started'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to start container'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.starting = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.container-console-panel {
	container-type: inline-size;
	display: flex;
	flex-direction: column;
	width: 100%;
	height: 100%;
	background: #121214;
	color: #e4e4e7;
	overflow: hidden;
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
}

.panel-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	height: 44px;
	padding: 0 var(--space-4);
	background: #18181b;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	user-select: none;
	cursor: grab;

	&:active {
		cursor: grabbing;
	}
}

.header-left {
	flex: 0 1 auto;
	display: flex;
	align-items: center;
	min-width: 0;

	.docker-icon {
		color: #38bdf8 !important;
	}

	.container-title {
		font-size: var(--font-sm);
		color: #f4f4f5;
		max-width: 220px;
	}

	.image-tag {
		font-size: var(--font-2xs);
		padding: var(--space-1) var(--space-2);
		background: rgba(255, 255, 255, 0.08);
		border-radius: var(--radius-xs);
		color: #a1a1aa;
		font-family: monospace;
		max-width: 160px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
}

.status-badge {
	display: inline-flex;
	align-items: center;
	font-size: var(--font-2xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		margin-right: var(--space-1);
	}

	&.is-running {
		background: rgba(16, 185, 129, 0.15);
		color: #34d399;
		.status-dot {
			background: #10b981;
			box-shadow: 0 0 6px rgba(16, 185, 129, 0.6);
		}
	}

	&.is-stopped {
		background: rgba(239, 68, 68, 0.15);
		color: #f87171;
		.status-dot {
			background: #ef4444;
		}
	}
}

.header-spacer {
	flex: 1 1 auto;
	min-width: 1rem;
}

.header-right {
	flex-shrink: 0;
	display: flex;
	align-items: center;
}

.console-tabs {
	display: flex;
	background: rgba(255, 255, 255, 0.06);
	padding: var(--space-1);
	border-radius: var(--radius-control);

	.console-tab-btn {
		background: transparent;
		border: none;
		color: #a1a1aa;
		font-size: var(--font-xs);
		font-weight: 500;
		padding: var(--space-1) var(--space-4);
		border-radius: var(--radius-sm);
		cursor: pointer;
		display: flex;
		align-items: center;
		transition: all 0.15s ease;

		&:hover {
			color: #ffffff;
		}

		&.active {
			background: #27272a;
			color: #ffffff;
			box-shadow: 0 1px 3px rgba(0, 0, 0, 0.4);
		}
	}
}

.header-tool-btn {
	background: rgba(255, 255, 255, 0.08);
	border: 1px solid rgba(255, 255, 255, 0.1);
	color: #e4e4e7;
	width: 28px;
	height: 28px;
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	padding: 0;
	font-size: var(--font-base);
	transition: all 0.15s ease;

	&:hover {
		background: rgba(255, 255, 255, 0.16);
		color: #ffffff;
		border-color: rgba(255, 255, 255, 0.2);
	}

	&:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	&.is-spinning i {
		animation: spin 1s infinite linear;
	}
}

@keyframes spin {
	from { transform: rotate(0deg); }
	to { transform: rotate(360deg); }
}

.window-controls {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin-left: var(--space-3);
}

.window-btn {
	width: 12px;
	height: 12px;
	border-radius: 50%;
	border: none;
	padding: 0;
	cursor: pointer;
	opacity: 0.85;
	transition: opacity 0.15s ease;

	&:hover {
		opacity: 1;
	}

	&.window-btn-minimize {
		background: #ffbd2e;
	}

	&.window-btn-close {
		background: #ff5f56;
	}
}

.panel-body {
	flex: 1 1 auto;
	min-height: 0;
	position: relative;
	overflow: hidden;
	background: #18181b;
}

.tab-pane {
	position: absolute;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	display: flex;
	flex-direction: column;
	overflow: hidden;
	background: #18181b;
}

.terminal-pane {
	position: absolute;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	background: #18181b;
	padding: 0;
	margin: 0;
	overflow: hidden;

	::v-deep .terminal-instance {
		width: 100%;
		height: 100%;
		min-height: 0;
		background: #18181b;
		padding: var(--space-1) var(--space-2);
		box-sizing: border-box;
	}

	::v-deep .xterm {
		width: 100%;
		height: 100%;
	}
}

.stopped-warning {
	width: 100%;
	height: 100%;
	color: #a1a1aa;
}

.logs-pane {
	display: flex;
	flex-direction: column;
	width: 100%;
	height: 100%;
}

.logs-toolbar {
	flex-wrap: wrap;
	row-gap: var(--space-2);
	padding: var(--space-2) var(--space-4);
	background: #18181b;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	font-size: var(--font-xs);
}

.search-box {
	position: relative;
	display: flex;
	align-items: center;
	margin-right: var(--space-3);

	.search-icon {
		position: absolute;
		left: 8px;
		color: #71717a;
		font-size: var(--font-sm);
		pointer-events: none;
	}

	.log-search-input {
		padding: 0 24px 0 26px;
		background: rgba(255, 255, 255, 0.08);
		border: 1px solid rgba(255, 255, 255, 0.12);
		border-radius: var(--radius-sm);
		color: #f4f4f5;
		font-size: var(--font-2xs);
		height: 26px;
		width: 160px;
		outline: none;
		transition: all 0.15s ease;

		&::placeholder {
			color: #8a8a93;
		}

		&:focus {
			width: 200px;
			background: rgba(255, 255, 255, 0.12);
			border-color: #3b82f6;
		}
	}

	.clear-search-btn {
		position: absolute;
		right: 6px;
		background: transparent;
		border: none;
		color: #71717a;
		cursor: pointer;
		padding: 0;
		font-size: var(--font-sm);

		&:hover {
			color: #e4e4e7;
		}
	}
}

.custom-select-wrap {
	position: relative;
	display: inline-flex;
	align-items: center;
	margin-right: var(--space-3);

	select {
		appearance: none;
		-webkit-appearance: none;
		background: rgba(255, 255, 255, 0.08);
		border: 1px solid rgba(255, 255, 255, 0.12);
		color: #e4e4e7;
		font-size: var(--font-2xs);
		height: 26px;
		padding: 0 24px 0 var(--space-2);
		border-radius: var(--radius-sm);
		outline: none;
		cursor: pointer;
		transition: all 0.15s ease;

		&:hover {
			background: rgba(255, 255, 255, 0.12);
			border-color: rgba(255, 255, 255, 0.2);
		}

		&:focus {
			border-color: #3b82f6;
		}

		option {
			background: #18181b;
			color: #e4e4e7;
		}
	}

	.select-chevron {
		position: absolute;
		right: 6px;
		color: #a1a1aa;
		pointer-events: none;
		font-size: var(--font-sm);
	}
}

.custom-check {
	display: inline-flex;
	align-items: center;
	cursor: pointer;
	font-size: var(--font-2xs);
	color: #a1a1aa;
	user-select: none;
	margin-right: var(--space-3);
	transition: color 0.15s ease;

	&:hover {
		color: #f4f4f5;
	}

	input[type="checkbox"] {
		appearance: none;
		-webkit-appearance: none;
		width: 14px;
		height: 14px;
		background: rgba(255, 255, 255, 0.08);
		border: 1px solid rgba(255, 255, 255, 0.2);
		border-radius: var(--radius-xs);
		margin-right: var(--space-2);
		display: grid;
		place-content: center;
		cursor: pointer;
		transition: all 0.15s ease;

		&:checked {
			background: #2563eb;
			border-color: #2563eb;

			&::before {
				content: "";
				width: 7px;
				height: 4px;
				border-left: 2px solid #fff;
				border-bottom: 2px solid #fff;
				transform: rotate(-45deg) translate(1px, -1px);
			}
		}
	}
}

.match-pill {
	font-size: var(--font-2xs);
	color: #a1a1aa;
	background: rgba(255, 255, 255, 0.06);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-xs);
}

.toolbar-btn {
	background: rgba(255, 255, 255, 0.08);
	border: 1px solid rgba(255, 255, 255, 0.12);
	color: #e4e4e7;
	font-size: var(--font-2xs);
	height: 26px;
	padding: 0 var(--space-3);
	border-radius: var(--radius-sm);
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	transition: all 0.15s ease;

	&:hover {
		background: rgba(255, 255, 255, 0.16);
		border-color: rgba(255, 255, 255, 0.2);
		color: #ffffff;
	}

	&:disabled {
		opacity: 0.4;
		background: rgba(255, 255, 255, 0.04);
		border-color: rgba(255, 255, 255, 0.06);
		color: #71717a;
		cursor: not-allowed;
	}
}

.log-viewport {
	flex: 1;
	background: #0d0d10;
	padding: var(--space-3) var(--space-4);
	overflow-y: auto;
	font-family: "Monaco", "Consolas", "Courier New", monospace;
	font-size: var(--font-xs);
	line-height: 1.55;
	color: #e4e4e7;
}

.log-line {
	display: flex;
	white-space: pre-wrap;
	word-break: break-all;
	padding: 1px 0;

	&:hover {
		background: rgba(255, 255, 255, 0.05);
	}

	&.highlight {
		background: rgba(234, 179, 8, 0.25);
	}

	.line-num {
		user-select: none;
		color: #8a8a93;
		min-width: 48px;
		flex-shrink: 0;
		text-align: right;
		padding-right: var(--space-4);
		font-variant-numeric: tabular-nums;
	}

	.line-text {
		flex: 1;
		min-width: 0;
	}
}

.loading-state, .empty-logs {
	height: 100%;
	min-height: 200px;
}
.logs-error {
	flex-shrink: 0;
	padding: var(--space-2) var(--space-4);
	background: rgba(239, 68, 68, 0.14);
	border-bottom: 1px solid rgba(239, 68, 68, 0.3);
	color: #fca5a5;
	font-size: var(--font-xs);
}

.toolbar-left,
.toolbar-right {
	flex-wrap: wrap;
	row-gap: var(--space-2);
}

.console-tab-btn,
.header-tool-btn,
.window-btn,
.toolbar-btn,
.clear-search-btn,
.log-viewport {
	&:focus-visible {
		outline: 2px solid #93c5fd;
		outline-offset: 1px;
	}
}

.custom-check input[type="checkbox"]:focus-visible {
	outline: 2px solid #93c5fd;
	outline-offset: 1px;
}

// Narrow window (phone: always full-screen): header wraps into two rows,
// toolbar buttons collapse to icons (they keep their aria-label).
@container (max-width: 640px) {
	.panel-header {
		height: auto;
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
	}

	.header-spacer {
		display: none;
	}

	.header-left {
		flex: 1 1 100%;
		min-width: 0;

		.container-title {
			max-width: none;
			flex: 0 1 auto;
			min-width: 0;
		}

		.image-tag {
			display: none;
		}
	}

	.header-right {
		flex: 1 1 100%;
		justify-content: space-between;

		.console-tabs {
			margin-right: 0 !important;
		}
	}

	.logs-toolbar {
		padding: var(--space-2) var(--space-3);
	}

	.search-box {
		flex: 1 1 100%;
		margin-right: 0;

		.log-search-input,
		.log-search-input:focus {
			width: 100%;
		}
	}

	.toolbar-btn-text {
		display: none;
	}

	.toolbar-btn .mdi {
		margin-right: 0 !important;
	}

	.log-viewport {
		padding: var(--space-2) var(--space-3);
	}

	.log-line .line-num {
		display: none;
	}
}
</style>
