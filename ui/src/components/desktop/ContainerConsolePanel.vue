<template>
	<div class="container-console-panel">
		<!-- Top Window Toolbar / Tabs -->
		<div class="panel-header" @mousedown="$emit('drag-start', $event)">
			<div class="header-left is-flex is-align-items-center">
				<b-icon icon="docker" pack="mdi" size="is-20" class="mr-2 has-text-info"></b-icon>
				<span class="container-title one-line font-weight-bold">{{ containerName || containerId }}</span>
				<span v-if="containerImage" class="image-tag ml-2">{{ shortImage }}</span>
				<span class="status-badge ml-2" :class="isContainerRunning ? 'is-running' : 'is-stopped'">
					<span class="status-dot"></span>
					{{ isContainerRunning ? $t('Running') : $t('Stopped') }}
				</span>
			</div>

			<!-- Tab switchers -->
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

			<!-- Window controls / Quick actions -->
			<div class="header-right is-flex is-align-items-center">
				<b-button
					v-if="activeTab === 'logs'"
					rounded
					size="is-small"
					class="panel-action-btn mr-2"
					:loading="loadingLogs"
					@click="fetchLogs"
					:title="$t('Refresh logs')"
				>
					<i class="mdi mdi-refresh"></i>
				</b-button>

				<b-button
					rounded
					size="is-small"
					class="panel-action-btn mr-3"
					:loading="restarting"
					@click="restartContainer"
					:title="$t('Restart container')"
				>
					<i class="mdi mdi-restart"></i>
				</b-button>

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
					<p class="is-size-6 mb-3">{{ $t('Container is stopped. Start it to open terminal.') }}</p>
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
				<!-- Logs Action Bar -->
				<div class="logs-toolbar is-flex is-align-items-center is-justify-content-between">
					<div class="toolbar-left is-flex is-align-items-center">
						<!-- Search Filter -->
						<div class="search-box mr-3">
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
						<div class="select is-small mr-3">
							<select v-model="lineCount" @change="fetchLogs">
								<option :value="100">100 {{ $t('lines') }}</option>
								<option :value="500">500 {{ $t('lines') }}</option>
								<option :value="1000">1000 {{ $t('lines') }}</option>
								<option :value="2000">2000 {{ $t('lines') }}</option>
								<option :value="5000">5000 {{ $t('lines') }}</option>
							</select>
						</div>

						<!-- Timestamps switch -->
						<label class="checkbox is-size-7 mr-3 is-flex is-align-items-center text-muted">
							<input type="checkbox" v-model="showTimestamps" @change="fetchLogs" class="mr-1" />
							{{ $t('Timestamps') }}
						</label>

						<!-- Auto-refresh switch -->
						<label class="checkbox is-size-7 mr-3 is-flex is-align-items-center text-muted">
							<input type="checkbox" v-model="autoRefresh" class="mr-1" />
							{{ $t('Live Auto-refresh') }}
						</label>

						<!-- Auto-scroll switch -->
						<label class="checkbox is-size-7 is-flex is-align-items-center text-muted">
							<input type="checkbox" v-model="autoScroll" class="mr-1" />
							{{ $t('Auto-scroll') }}
						</label>
					</div>

					<div class="toolbar-right is-flex is-align-items-center">
						<span v-if="logSearch" class="is-size-7 text-muted mr-3">
							{{ filteredLines.length }} / {{ rawLines.length }} {{ $t('matches') }}
						</span>
						<b-button rounded size="is-small" class="mr-2" @click="copyLogs" :disabled="!logContent">
							<i class="mdi mdi-content-copy mr-1"></i>
							{{ $t('Copy') }}
						</b-button>
						<b-button rounded size="is-small" class="mr-2" @click="downloadLogs" :disabled="!logContent">
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Download') }}
						</b-button>
						<b-button rounded size="is-small" @click="clearLogsView" :disabled="!logContent">
							<i class="mdi mdi-trash-can-outline mr-1"></i>
							{{ $t('Clear View') }}
						</b-button>
					</div>
				</div>

				<!-- Log Lines Viewport -->
				<div ref="logViewport" class="log-viewport scrollbars">
					<div v-if="loadingLogs && !logContent" class="loading-state is-flex is-align-items-center is-justify-content-center">
						<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin mr-2"></b-icon>
						<span>{{ $t('Loading logs...') }}</span>
					</div>
					<div v-else-if="!logContent" class="empty-logs is-flex is-align-items-center is-justify-content-center">
						<span class="text-muted">{{ $t('No log output recorded yet for this container.') }}</span>
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
			type: String,
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
			return this.logContent.split('\n')
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
					// could be string or json
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
			a.download = `${this.containerName || this.containerId}-logs-${new Date().toISOString().slice(0, 10)}.log`
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
	padding: 0 14px;
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

	.container-title {
		font-size: 13px;
		color: #f4f4f5;
		max-width: 180px;
	}

	.image-tag {
		font-size: 11px;
		padding: 2px 6px;
		background: rgba(255, 255, 255, 0.08);
		border-radius: 4px;
		color: #a1a1aa;
		font-family: monospace;
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
	border-radius: 7px;

	.console-tab-btn {
		background: transparent;
		border: none;
		color: #a1a1aa;
		font-size: 12px;
		font-weight: 500;
		padding: 4px 14px;
		border-radius: 5px;
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
			box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
		}
	}
}

.header-right {
	display: flex;
	align-items: center;
}

.panel-action-btn {
	background: rgba(255, 255, 255, 0.08) !important;
	border: none !important;
	color: #e4e4e7 !important;
	width: 28px;
	height: 28px;
	padding: 0 !important;

	&:hover {
		background: rgba(255, 255, 255, 0.15) !important;
		color: #fff !important;
	}
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
		background: #fbbf24;
	}

	&.window-btn-close {
		background: #f87171;
	}
}

.panel-body {
	flex: 1;
	display: flex;
	position: relative;
	overflow: hidden;
}

.tab-pane {
	width: 100%;
	height: 100%;
	display: flex;
	flex-direction: column;
}

.terminal-pane {
	position: relative;
}

.stopped-warning {
	width: 100%;
	height: 100%;
	color: #a1a1aa;
}

.logs-pane {
	display: flex;
	flex-direction: column;
}

.logs-toolbar {
	padding: 8px 12px;
	background: #18181b;
	border-bottom: 1px solid rgba(255, 255, 255, 0.06);
	font-size: 12px;

	.search-box {
		position: relative;
		display: flex;
		align-items: center;

		.search-icon {
			position: absolute;
			left: 8px;
			color: #71717a;
			font-size: 14px;
		}

		.log-search-input {
			padding: 4px 26px 4px 26px;
			background: #27272a;
			border: 1px solid rgba(255, 255, 255, 0.1);
			border-radius: 6px;
			color: #f4f4f5;
			font-size: 12px;
			width: 180px;
			outline: none;

			&:focus {
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
		}
	}

	.select select {
		background: #27272a;
		border-color: rgba(255, 255, 255, 0.1);
		color: #e4e4e7;
		font-size: 12px;
	}
}

.log-viewport {
	flex: 1;
	background: #09090b;
	padding: 10px 14px;
	overflow-y: auto;
	font-family: "Monaco", "Consolas", "Courier New", monospace;
	font-size: 12px;
	line-height: 1.6;
	color: #d4d4d8;
}

.log-line {
	display: flex;
	white-space: pre-wrap;
	word-break: break-all;

	&:hover {
		background: rgba(255, 255, 255, 0.04);
	}

	&.highlight {
		background: rgba(234, 179, 8, 0.2);
	}

	.line-num {
		user-select: none;
		color: #52525b;
		width: 44px;
		flex-shrink: 0;
		text-align: right;
		padding-right: 12px;
	}

	.line-text {
		flex: 1;
	}
}

.loading-state, .empty-logs {
	height: 100%;
	min-height: 200px;
}
</style>
