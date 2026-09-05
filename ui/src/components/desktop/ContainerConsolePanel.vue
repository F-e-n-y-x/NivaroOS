<template>
	<div class="container-console-panel">
		<!-- Top Window Toolbar / Titlebar -->
		<div class="panel-header" @mousedown="$emit('drag-start', $event)">
			<div class="header-left is-flex is-align-items-center">
				<b-icon icon="docker" pack="mdi" size="is-20" class="docker-icon mr-2"></b-icon>
				<span class="container-title one-line font-weight-bold" :title="displayTitle">{{ displayTitle }}</span>
				<span v-if="shortImage" class="image-tag ml-2" :title="containerImage">{{ shortImage }}</span>
				<span class="status-badge ml-2" :class="isContainerRunning ? 'is-running' : 'is-stopped'">
					<span class="status-dot"></span>
					{{ isContainerRunning ? $t('Running') : $t('Stopped') }}
				</span>
			</div>

			<!-- Tab switchers in center -->
			<div class="header-center">
				<div class="console-tabs">
					<button
						type="button"
						class="console-tab-btn"
						:class="{ active: activeTab === 'terminal' }"
						@click="switchTab('terminal')"
					>
						<i class="mdi mdi-console mr-1"></i>
						{{ $t('Terminal') }}
					</button>
					<button
						type="button"
						class="console-tab-btn"
						:class="{ active: activeTab === 'logs' }"
						@click="switchTab('logs')"
					>
						<i class="mdi mdi-text-box-search-outline mr-1"></i>
						{{ $t('Logs') }}
					</button>
				</div>
			</div>

			<!-- Window controls / Quick actions on right -->
			<div class="header-right is-flex is-align-items-center">
				<button
					v-if="activeTab === 'logs'"
					class="header-tool-btn mr-3"
					:class="{ 'is-spinning': loadingLogs }"
					:title="$t('Refresh logs')"
					@click="fetchLogs"
				>
					<i class="mdi mdi-refresh"></i>
				</button>

				<div class="window-controls">
					<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="$emit('minimize')"></button>
					<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="$emit('close')"></button>
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
							/>
							<button v-if="logSearch" class="clear-search-btn" @click="logSearch = ''">
								<i class="mdi mdi-close"></i>
							</button>
						</div>

						<!-- Line Count Selector -->
						<div class="custom-select-wrap">
							<select v-model="lineCount" @change="fetchLogs">
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
							<input type="checkbox" v-model="showTimestamps" @change="fetchLogs" />
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
							<span>{{ $t('Auto-scroll') }}</span>
						</label>
					</div>

					<div class="toolbar-right is-flex is-align-items-center">
						<span v-if="logSearch" class="match-pill mr-3">
							{{ filteredLines.length }} / {{ rawLines.length }} {{ $t('matches') }}
						</span>
						<button class="toolbar-btn mr-2" @click="copyLogs" :disabled="!logContent">
							<i class="mdi mdi-content-copy mr-1"></i>
							{{ $t('Copy') }}
						</button>
						<button class="toolbar-btn mr-2" @click="downloadLogs" :disabled="!logContent">
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Download') }}
						</button>
						<button class="toolbar-btn" @click="clearLogsView" :disabled="!logContent">
							<i class="mdi mdi-trash-can-outline mr-1"></i>
							{{ $t('Clear View') }}
						</button>
					</div>
				</div>

				<!-- Log Lines Viewport -->
				<div ref="logViewport" class="log-viewport scrollbars">
					<div v-if="loadingLogs && !logContent" class="loading-state is-flex is-align-items-center is-justify-content-center">
						<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin mr-2"></b-icon>
						<span>{{ $t('Loading logs...') }}</span>
					</div>
					<div v-else-if="!logContent" class="empty-logs is-flex is-align-items-center is-justify-content-center">
						<span class="text-muted is-size-7">{{ $t('No log output recorded yet for this container.') }}</span>
					</div>
					<div v-else class="log-content">
						<div
							v-for="(line, idx) in filteredLines"
							:key="idx"
							class="log-line"
							:class="{ 'highlight': logSearch && line.toLowerCase().includes(logSearch.toLowerCase()) }"
						>
							<span class="line-num">{{ idx + 1 }}</span>
							<span class="line-text">{{ line }}</span>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import qs from 'qs'
import TerminalCard from '@/components/logsAndTerminal/TerminalCard.vue'

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
		}
	},
	data() {
		return {
			activeTab: this.initialTab || 'terminal',
			logContent: '',
			loadingLogs: false,
			lineCount: 500,
			showTimestamps: true,
			autoRefresh: true,
			autoScroll: true,
			logSearch: '',
			logTimer: null,
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
				} catch (e) {}
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
		terminalWsUrl() {
			const query = {
				token: this.$store.state.access_token,
				cols: 120,
				rows: 32
			}
			return `${this.$wsProtocol}//${this.$baseURL}/v1/container/${this.containerId}/terminal?${qs.stringify(query)}`
		},
		rawLines() {
			if (!this.logContent) return []
			const lines = this.logContent.split('\n')
			if (lines.length > 0 && lines[lines.length - 1] === '') {
				return lines.slice(0, lines.length - 1)
			}
			return lines
		},
		filteredLines() {
			if (!this.logSearch) return this.rawLines
			const q = this.logSearch.toLowerCase()
			return this.rawLines.filter(line => line.toLowerCase().includes(q))
		}
	},
	mounted() {
		if (this.activeTab === 'logs') {
			this.fetchLogs()
		}
		this.startLogPolling()
	},
	beforeDestroy() {
		this.stopLogPolling()
	},
	watch: {
		autoRefresh(val) {
			if (val) {
				this.startLogPolling()
			} else {
				this.stopLogPolling()
			}
		},
		activeTab(tab) {
			if (tab === 'logs' && !this.logContent) {
				this.fetchLogs()
			}
		}
	},
	methods: {
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
				if (this.activeTab === 'logs' && this.autoRefresh) {
					this.fetchLogs(true)
				}
			}, 3000)
		},
		stopLogPolling() {
			if (this.logTimer) {
				clearInterval(this.logTimer)
				this.logTimer = null
			}
		},
		async fetchLogs(silent = false) {
			if (!silent) this.loadingLogs = true
			try {
				const res = await this.$api.container.getRawLogs(
					this.containerId,
					this.lineCount,
					this.showTimestamps
				)
				if (res && res.data) {
					if (typeof res.data === 'string') {
						this.logContent = res.data
					} else if (res.data.data !== undefined) {
						this.logContent = res.data.data || ''
					} else {
						this.logContent = JSON.stringify(res.data, null, 2)
					}
					if (this.autoScroll) {
						this.$nextTick(() => {
							this.scrollToBottom()
						})
					}
				}
			} catch (err) {
				if (!silent) {
					console.error('Failed to fetch container logs:', err)
				}
			} finally {
				if (!silent) this.loadingLogs = false
			}
		},
		scrollToBottom() {
			const vp = this.$refs.logViewport
			if (vp) {
				vp.scrollTop = vp.scrollHeight
			}
		},
		clearLogsView() {
			this.logContent = ''
		},
		copyLogs() {
			if (!this.logContent) return
			navigator.clipboard.writeText(this.logContent).then(() => {
				this.$buefy.toast.open({
					message: this.$t('Logs copied to clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
			}).catch(() => {
				this.$buefy.toast.open({
					message: this.$t('Failed to copy logs'),
					type: 'is-danger',
					position: 'is-top',
					duration: 2000
				})
			})
		},
		downloadLogs() {
			if (!this.logContent) return
			const blob = new Blob([this.logContent], { type: 'text/plain;charset=utf-8' })
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
	padding: 0 16px;
	background: #18181b;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	user-select: none;
	cursor: grab;

	&:active {
		cursor: grabbing;
	}
}

.header-left {
	min-width: 0;
	flex: 1;

	.docker-icon {
		color: #38bdf8 !important;
	}

	.container-title {
		font-size: 13px;
		color: #f4f4f5;
		max-width: 200px;
	}

	.image-tag {
		font-size: 11px;
		padding: 2px 7px;
		background: rgba(255, 255, 255, 0.08);
		border-radius: 4px;
		color: #a1a1aa;
		font-family: monospace;
		max-width: 150px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
}

.status-badge {
	display: inline-flex;
	align-items: center;
	font-size: 11px;
	font-weight: 500;
	padding: 2px 8px;
	border-radius: 9999px;

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		margin-right: 5px;
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

.console-tabs {
	display: flex;
	background: rgba(255, 255, 255, 0.06);
	padding: 3px;
	border-radius: 8px;

	.console-tab-btn {
		background: transparent;
		border: none;
		color: #a1a1aa;
		font-size: 12px;
		font-weight: 500;
		padding: 4px 14px;
		border-radius: 6px;
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

.header-right {
	display: flex;
	align-items: center;
}

.header-tool-btn {
	background: rgba(255, 255, 255, 0.08);
	border: 1px solid rgba(255, 255, 255, 0.1);
	color: #e4e4e7;
	width: 28px;
	height: 28px;
	border-radius: 6px;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	padding: 0;
	font-size: 14px;
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
	gap: 7px;
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
		padding: 4px 6px;
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
	padding: 8px 14px;
	background: #18181b;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	font-size: 12px;
}

.search-box {
	position: relative;
	display: flex;
	align-items: center;
	margin-right: 12px;

	.search-icon {
		position: absolute;
		left: 8px;
		color: #71717a;
		font-size: 13px;
		pointer-events: none;
	}

	.log-search-input {
		padding: 0 24px 0 26px;
		background: rgba(255, 255, 255, 0.08);
		border: 1px solid rgba(255, 255, 255, 0.12);
		border-radius: 6px;
		color: #f4f4f5;
		font-size: 11px;
		height: 26px;
		width: 160px;
		outline: none;
		transition: all 0.15s ease;

		&::placeholder {
			color: #71717a;
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
		font-size: 13px;

		&:hover {
			color: #e4e4e7;
		}
	}
}

.custom-select-wrap {
	position: relative;
	display: inline-flex;
	align-items: center;
	margin-right: 12px;

	select {
		appearance: none;
		-webkit-appearance: none;
		background: rgba(255, 255, 255, 0.08);
		border: 1px solid rgba(255, 255, 255, 0.12);
		color: #e4e4e7;
		font-size: 11px;
		height: 26px;
		padding: 0 24px 0 8px;
		border-radius: 6px;
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
		font-size: 13px;
	}
}

.custom-check {
	display: inline-flex;
	align-items: center;
	cursor: pointer;
	font-size: 11px;
	color: #a1a1aa;
	user-select: none;
	margin-right: 12px;
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
		border-radius: 4px;
		margin-right: 6px;
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
	font-size: 11px;
	color: #a1a1aa;
	background: rgba(255, 255, 255, 0.06);
	padding: 2px 8px;
	border-radius: 4px;
}

.toolbar-btn {
	background: rgba(255, 255, 255, 0.08);
	border: 1px solid rgba(255, 255, 255, 0.12);
	color: #e4e4e7;
	font-size: 11px;
	height: 26px;
	padding: 0 10px;
	border-radius: 6px;
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
	padding: 10px 14px;
	overflow-y: auto;
	font-family: "Monaco", "Consolas", "Courier New", monospace;
	font-size: 12px;
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
		color: #52525b;
		width: 48px;
		flex-shrink: 0;
		text-align: right;
		padding-right: 14px;
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
</style>
