<template>
	<div class="system-update-window">
		<!-- Sub-header Status Bar (Dark theme matching Terminal chrome) -->
		<div class="updater-status-bar is-flex is-align-items-center is-justify-content-between">
			<div class="is-flex is-align-items-center">
				<div class="status-pulse-icon" :class="statusClass">
					<i :class="statusIconClass"></i>
				</div>
				<div class="ml-3">
					<h4 class="updater-title">{{ statusTitle }}</h4>
					<p class="updater-subtitle">{{ statusSubtitle }}</p>
				</div>
			</div>

			<div class="is-flex is-align-items-center gap-2">
				<b-button v-if="!isRunning && exitCode === 0 && hasRun" size="is-small" rounded type="is-success" @click="$emit('close')">
					<i class="mdi mdi-check mr-1"></i>
					{{ $t('Done') }}
				</b-button>
				<b-button v-if="!isRunning && exitCode === 0 && hasRun && isKernelUpdated" size="is-small" rounded type="is-warning" @click="rebootSystem">
					<i class="mdi mdi-restart mr-1"></i>
					{{ $t('Reboot System') }}
				</b-button>
				<b-button v-if="!isRunning && (!hasRun || exitCode !== 0)" size="is-small" rounded type="is-primary" :loading="isRunning" @click="startUpgrade">
					<i class="mdi mdi-play mr-1"></i>
					{{ hasRun ? $t('Retry Upgrade') : $t('Start Upgrade') }}
				</b-button>
			</div>
		</div>

		<!-- Progress Bar -->
		<div class="updater-progress-wrap">
			<div v-if="isRunning" class="progress-bar-animated"></div>
			<div v-else-if="exitCode === 0 && hasRun" class="progress-bar-done"></div>
			<div v-else-if="exitCode !== 0 && hasRun" class="progress-bar-failed"></div>
			<div v-else class="progress-bar-idle"></div>
		</div>

		<!-- Terminal / Console Window (Matching TerminalPanel / LogsCard) -->
		<div class="terminal-container">
			<div class="terminal-top-bar is-flex is-align-items-center is-justify-content-between">
				<div class="terminal-info is-flex is-align-items-center">
					<i class="mdi mdi-console mr-2 terminal-icon"></i>
					<span class="terminal-label">{{ mode === 'nivaroos' ? 'nivaroos-upgrade' : 'apt-get dist-upgrade' }}</span>
					<span v-if="isRunning" class="tag is-dark is-rounded ml-3 terminal-running-tag">
						<i class="mdi mdi-loading mdi-spin mr-1"></i>{{ $t('Running') }}
					</span>
				</div>
				<div class="terminal-actions is-flex is-align-items-center">
					<button class="terminal-action-btn" :title="$t('Toggle Auto-scroll')" :class="{ active: autoScroll }" @click="autoScroll = !autoScroll">
						<i class="mdi mdi-arrow-down-bold"></i>
					</button>
					<button class="terminal-action-btn" :title="$t('Copy Logs')" @click="copyLogs">
						<i class="mdi mdi-content-copy"></i>
					</button>
				</div>
			</div>

			<div ref="terminalBody" class="terminal-body" @scroll="onTerminalScroll">
				<div v-for="(line, idx) in logLines" :key="idx" class="terminal-line" :class="getLineClass(line)">
					{{ line }}
				</div>
				<div v-if="isRunning" class="terminal-cursor-line">
					<span class="terminal-prompt">$</span>
					<span class="terminal-cursor"></span>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'SystemUpdateWindow',
	props: {
		initialMode: {
			type: String,
			default: 'apt' // 'apt' or 'nivaroos'
		},
		packages: {
			type: Array,
			default: () => []
		},
		pkgCount: {
			type: Number,
			default: 0
		}
	},
	data() {
		return {
			mode: this.initialMode,
			isRunning: false,
			hasRun: false,
			exitCode: 0,
			logLines: [],
			autoScroll: true,
			pollTimer: null
		}
	},
	computed: {
		statusClass() {
			if (this.isRunning) return 'is-running'
			if (this.hasRun && this.exitCode === 0) return 'is-success'
			if (this.hasRun && this.exitCode !== 0) return 'is-failed'
			return 'is-idle'
		},
		statusIconClass() {
			if (this.isRunning) return 'mdi mdi-loading mdi-spin'
			if (this.hasRun && this.exitCode === 0) return 'mdi mdi-check-circle'
			if (this.hasRun && this.exitCode !== 0) return 'mdi mdi-alert-circle'
			return 'mdi mdi-package-up'
		},
		statusTitle() {
			if (this.isRunning) {
				return this.mode === 'nivaroos' ? this.$t('Upgrading NivaroOS Platform...') : this.$t('Upgrading Linux System Packages...')
			}
			if (this.hasRun && this.exitCode === 0) {
				return this.$t('System Upgrade Completed Successfully!')
			}
			if (this.hasRun && this.exitCode !== 0) {
				return this.$t('System Upgrade Failed')
			}
			return this.mode === 'nivaroos' ? this.$t('Ready to update NivaroOS') : `${this.pkgCount || this.packages.length} ${this.$t('packages ready for upgrade')}`
		},
		statusSubtitle() {
			if (this.isRunning) {
				return this.$t('Executing package upgrade in background. Do not turn off your device.')
			}
			if (this.hasRun && this.exitCode === 0) {
				return this.$t('All software packages have been safely upgraded to their latest versions.')
			}
			if (this.hasRun && this.exitCode !== 0) {
				return this.$t('An error occurred during package installation. Check log output below.')
			}
			return this.$t('Click Start Upgrade to begin installing latest packages.')
		},
		isKernelUpdated() {
			return this.logLines.some(l => l.toLowerCase().includes('linux-image') || l.toLowerCase().includes('kernel'))
		}
	},
	created() {
		this.checkExistingStatus()
	},
	mounted() {
		// Auto-start if opened with intent
		if (!this.isRunning && !this.hasRun) {
			this.startUpgrade()
		}
	},
	beforeDestroy() {
		if (this.pollTimer) clearInterval(this.pollTimer)
	},
	methods: {
		checkExistingStatus() {
			this.$api.sys.getPackageUpgradeStatus().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.isRunning = data.running
					this.exitCode = data.exit_code
					this.logLines = data.logs || []
					if (data.started_at) {
						this.hasRun = true
					}
					if (data.running) {
						this.startPolling()
					}
				}
			}).catch(() => {})
		},
		startUpgrade() {
			this.isRunning = true
			this.hasRun = true
			this.exitCode = 0
			this.logLines = [`[${new Date().toLocaleTimeString()}] Initializing package upgrade sequence...`]

			if (this.mode === 'nivaroos') {
				this.$api.sys.updateRecasa().then(() => {
					this.logLines.push(`[${new Date().toLocaleTimeString()}] NivaroOS self-updater script spawned.`)
				}).catch(err => {
					this.logLines.push(`[${new Date().toLocaleTimeString()}] Error triggering NivaroOS update: ${err.message}`)
				})
			} else {
				this.$api.sys.upgradePackages().then(res => {
					if (res.data.success === 200) {
						this.startPolling()
					} else {
						this.isRunning = false
						this.exitCode = 1
						this.logLines.push(`[Error] ${res.data.message}`)
					}
				}).catch(err => {
					this.isRunning = false
					this.exitCode = 1
					this.logLines.push(`[Network Error] ${err.message}`)
				})
			}
		},
		startPolling() {
			if (this.pollTimer) clearInterval(this.pollTimer)
			this.pollTimer = setInterval(() => {
				this.$api.sys.getPackageUpgradeStatus().then(res => {
					if (res.data.success === 200) {
						const data = res.data.data
						this.isRunning = data.running
						this.exitCode = data.exit_code
						this.logLines = data.logs || []
						if (this.autoScroll) {
							this.scrollToBottom()
						}
						if (!data.running) {
							clearInterval(this.pollTimer)
						}
					}
				}).catch(() => {
					clearInterval(this.pollTimer)
				})
			}, 1000)
		},
		scrollToBottom() {
			this.$nextTick(() => {
				const el = this.$refs.terminalBody
				if (el) el.scrollTop = el.scrollHeight
			})
		},
		onTerminalScroll(e) {
			const el = e.target
			const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30
			this.autoScroll = atBottom
		},
		getLineClass(line) {
			if (!line) return ''
			const l = line.toLowerCase()
			if (l.includes('error') || l.includes('fail') || l.includes('err:')) return 'is-error'
			if (l.includes('warn') || l.includes('warning')) return 'is-warning'
			if (l.includes('success') || l.includes('done') || l.includes('complete') || l.includes('setting up')) return 'is-success'
			if (l.includes('get:') || l.includes('hit:') || l.includes('unpacking')) return 'is-info'
			return ''
		},
		copyLogs() {
			navigator.clipboard.writeText(this.logLines.join('\n')).then(() => {
				this.$buefy.toast.open({
					message: this.$t('Logs copied to clipboard'),
					type: 'is-success',
					duration: 2000
				})
			})
		},
		rebootSystem() {
			this.$buefy.dialog.confirm({
				title: this.$t('Reboot System'),
				message: this.$t('A system reboot is recommended to apply kernel and base updates. Reboot now?'),
				type: 'is-warning',
				confirmText: this.$t('Reboot Now'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.sys.power('restart')
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.system-update-window {
	display: flex;
	flex-direction: column;
	width: 100%;
	height: 100%;
	background: #1e1e1e;
	box-sizing: border-box;
}

.updater-status-bar {
	padding: 0.75rem 1.25rem;
	background: #262626;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	flex-shrink: 0;
}

.status-pulse-icon {
	width: 38px;
	height: 38px;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 1.35rem;
	flex-shrink: 0;

	&.is-running {
		background: rgba(51, 136, 255, 0.15);
		color: #3388ff;
	}

	&.is-success {
		background: rgba(40, 204, 135, 0.15);
		color: #28cc87;
	}

	&.is-failed {
		background: rgba(242, 83, 74, 0.15);
		color: #f2534a;
	}

	&.is-idle {
		background: rgba(255, 255, 255, 0.08);
		color: rgba(255, 255, 255, 0.7);
	}
}

.updater-title {
	font-size: 0.9rem;
	font-weight: 600;
	color: #ffffff;
	margin-bottom: 0.1rem;
}

.updater-subtitle {
	font-size: 0.75rem;
	color: rgba(255, 255, 255, 0.6);
}

.updater-progress-wrap {
	height: 3px;
	width: 100%;
	background: rgba(255, 255, 255, 0.05);
	position: relative;
	overflow: hidden;
	flex-shrink: 0;
}

.progress-bar-animated {
	width: 40%;
	height: 100%;
	background: #3388ff;
	position: absolute;
	animation: progress-slide 1.5s infinite linear;
}

.progress-bar-done {
	width: 100%;
	height: 100%;
	background: #28cc87;
}

.progress-bar-failed {
	width: 100%;
	height: 100%;
	background: #f2534a;
}

.progress-bar-idle {
	width: 0%;
	height: 100%;
}

@keyframes progress-slide {
	0% { left: -40%; }
	100% { left: 100%; }
}

.terminal-container {
	flex: 1;
	display: flex;
	flex-direction: column;
	background: #1e1e1e;
	min-height: 0;
}

.terminal-top-bar {
	padding: 0.35rem 0.85rem;
	background: #222222;
	border-bottom: 1px solid rgba(255, 255, 255, 0.06);
	flex-shrink: 0;
}

.terminal-icon {
	color: rgba(255, 255, 255, 0.55);
	font-size: 0.9rem;
}

.terminal-label {
	font-size: 0.75rem;
	font-family: 'Consolas', 'Monaco', monospace;
	color: rgba(255, 255, 255, 0.75);
}

.terminal-running-tag {
	font-size: 0.65rem;
	height: 1.25rem;
	padding: 0 0.5rem;
	background: rgba(51, 136, 255, 0.2);
	color: #3388ff;
}

.terminal-action-btn {
	background: transparent;
	border: none;
	color: rgba(255, 255, 255, 0.5);
	font-size: 0.9rem;
	padding: 0.2rem 0.4rem;
	border-radius: 4px;
	cursor: pointer;
	margin-left: 0.3rem;
	transition: background 0.15s ease, color 0.15s ease;

	&:hover {
		background: rgba(255, 255, 255, 0.1);
		color: #ffffff;
	}

	&.active {
		color: #3388ff;
	}
}

.terminal-body {
	flex: 1;
	padding: 0.75rem 1rem;
	overflow-y: auto;
	font-family: 'Consolas', 'Monaco', monospace;
	font-size: 13px;
	line-height: 1.5em;
	color: #ffffff;
	white-space: pre-wrap;
	word-break: break-word;
	scrollbar-width: thin;
	scrollbar-color: rgba(255, 255, 255, 0.2) transparent;
}

.terminal-line {
	margin-bottom: 0.1rem;

	&.is-error {
		color: #f2534a;
	}
	&.is-warning {
		color: #f6bd3b;
	}
	&.is-success {
		color: #28cc87;
	}
	&.is-info {
		color: #3388ff;
	}
}

.terminal-cursor-line {
	display: flex;
	align-items: center;
	margin-top: 0.25rem;

	.terminal-prompt {
		color: #28cc87;
		font-weight: bold;
		margin-right: 0.4rem;
		font-family: 'Consolas', 'Monaco', monospace;
	}

	.terminal-cursor {
		width: 7px;
		height: 14px;
		background: #ffffff;
		animation: cursor-blink 1s infinite;
	}
}

@keyframes cursor-blink {
	0%, 50% { opacity: 1; }
	51%, 100% { opacity: 0; }
}
</style>
