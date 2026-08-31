<template>
	<div class="about-panel">
		<!-- System Hardware Specifications -->
		<h3 class="setting-card-title">{{ $t('System Specifications') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Hostname') }}</div>
				<div class="row-control">
					<template v-if="!editingHostname">
						<span class="mr-2">{{ hardware.hostname || $t('Unknown') }}</span>
						<button class="icon-button" type="button" :title="$t('Edit')" @click="startEditHostname">
							<b-icon icon="edit-outline" pack="casa" size="is-16"></b-icon>
						</button>
					</template>
					<template v-else>
						<b-input v-model="hostnameInput" size="is-small" class="port-input" @keyup.enter.native="saveHostname"></b-input>
						<button v-if="hostnameInput !== hardware.hostname" class="icon-button is-confirm" type="button"
							:title="$t('Apply')" :disabled="savingHostname" @click="saveHostname">
							<b-icon icon="check-outline" pack="casa" size="is-16"></b-icon>
						</button>
						<button class="icon-button" type="button" :title="$t('Cancel')" @click="editingHostname = false">
							<b-icon icon="close-outline" pack="casa" size="is-16"></b-icon>
						</button>
					</template>
				</div>
			</div>
			<p v-if="hostnameError" class="error-note">{{ hostnameError }}</p>

			<div v-for="row in systemRows" :key="row.label" class="setting-row">
				<b-icon class="row-icon" :icon="row.icon" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t(row.label) }}</div>
				<div class="row-control">{{ row.value || $t('Unknown') }}</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="docker-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Docker') }}</div>
				<div class="row-control">
					<span v-if="dockerUpdateAvailable" class="update-dot" :title="$t('Update available')"></span>
					{{ hardware.docker_version || $t('Not installed') }}
				</div>
			</div>
		</div>

		<!-- Storage Usage -->
		<h3 class="setting-card-title">{{ $t('Storage Usage') }}</h3>
		<div class="setting-card">
			<div v-for="d in disksUsage" :key="d.mount_point" class="setting-row">
				<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ d.mount_point }}</div>
				<div class="row-control">{{ d.used }} / {{ d.total }} ({{ d.percent }}) &middot; {{ d.fstype }}</div>
			</div>
		</div>

		<!-- System Logs -->
		<h3 class="setting-card-title">{{ $t('System Logs') }}</h3>
		<div class="setting-card">
			<div class="setting-row log-header-row">
				<div class="row-label">{{ $t('Recent NivaroOS log lines') }}</div>
				<div class="row-control">
					<b-button rounded size="is-small" @click="loadLogs">{{ $t('Refresh') }}</b-button>
				</div>
			</div>
			<pre class="log-view">{{ logText }}</pre>
		</div>
	</div>
</template>

<script>
function formatBytes(bytes) {
	if (!bytes) return ''
	const units = ['B', 'KB', 'MB', 'GB', 'TB']
	let i = 0
	let size = bytes
	while (size >= 1024 && i < units.length - 1) {
		size /= 1024
		i++
	}
	return `${size.toFixed(1)} ${units[i]}`
}

export default {
	name: 'about-panel',
	data() {
		return {
			hardware: {},
			cpu: {},
			mem: {},
			disksUsage: [],
			editingHostname: false,
			hostnameInput: '',
			savingHostname: false,
			hostnameError: '',
			logText: ''
		}
	},
	computed: {
		systemRows() {
			return [
				{ label: 'Operating System', icon: 'system-outline', value: this.hardware.os_name },
				{ label: 'Kernel', icon: 'system-outline', value: this.hardware.kernel },
				{ label: 'Uptime', icon: 'restart-outline', value: this.hardware.uptime },
				{ label: 'CPU', icon: 'control-outline', value: this.cpuLabel },
				{ label: 'Memory', icon: 'control-outline', value: this.memLabel },
				{ label: 'Architecture', icon: 'control-outline', value: this.hardware.arch },
				{ label: 'Shell', icon: 'system-outline', value: this.hardware.shell },
				{ label: 'Locale', icon: 'language-outline', value: this.hardware.locale }
			]
		},
		cpuLabel() {
			if (!this.cpu.model_name) return ''
			const cores = this.cpu.num ? ` (${this.cpu.num} cores)` : ''
			const ghz = this.cpu.mhz ? ` @ ${(this.cpu.mhz / 1000).toFixed(2)} GHz` : ''
			return `${this.cpu.model_name}${cores}${ghz}`
		},
		memLabel() {
			if (!this.mem.total) return ''
			return `${formatBytes(this.mem.used)} / ${formatBytes(this.mem.total)}`
		},
		dockerUpdateAvailable() {
			return this.hardware.docker_update_available === 'true'
		}
	},
	created() {
		this.loadHardware()
		this.loadUtilization()
		this.loadDisksUsage()
		this.loadLogs()
	},
	methods: {
		loadHardware() {
			this.$api.sys.hardwareInfo().then(res => {
				if (res.data.success === 200) this.hardware = res.data.data
			})
		},
		loadUtilization() {
			this.$api.sys.getUtilization().then(res => {
				if (res.data.success === 200) {
					this.cpu = res.data.data.cpu || {}
					this.mem = res.data.data.mem || {}
				}
			})
		},
		loadDisksUsage() {
			this.$api.sys.getDisksUsage().then(res => {
				if (res.data.success === 200) this.disksUsage = res.data.data || []
			})
		},
		startEditHostname() {
			this.hostnameInput = this.hardware.hostname
			this.hostnameError = ''
			this.editingHostname = true
		},
		saveHostname() {
			if (this.hostnameInput === this.hardware.hostname) return
			this.hostnameError = ''
			this.savingHostname = true
			this.$api.sys.setHostname(this.hostnameInput).then(res => {
				if (res.data.success === 200) {
					this.hardware.hostname = res.data.data
					this.editingHostname = false
				} else {
					this.hostnameError = res.data.message
				}
			}).catch(e => {
				this.hostnameError = e.response && e.response.data ? e.response.data.data : this.$t('Failed to change hostname')
			}).finally(() => {
				this.savingHostname = false
			})
		},
		loadLogs() {
			this.$api.sys.getLogs().then(res => {
				if (res.data.success === 200) {
					const lines = res.data.data || []
					this.logText = Array.isArray(lines) ? lines.join('\n') : String(lines)
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.about-panel {
	display: flex;
	flex-direction: column;
}

.setting-card {
	margin-bottom: 1.25rem;
}

.icon-button {
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.6rem;
	height: 1.6rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: #64748b;
	margin-left: 0.35rem;

	&:hover {
		background: rgba(0, 0, 0, 0.09);
		color: #1e293b;
	}

	&.is-confirm {
		background: rgba(16, 185, 129, 0.15);
		color: #059669;

		&:hover {
			background: rgba(16, 185, 129, 0.25);
		}
	}
}

.update-dot {
	display: inline-block;
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 50%;
	background: #10b981;
	margin-right: 0.4rem;
}

.log-header-row {
	.row-label {
		font-weight: 500;
		color: #1e293b;
	}
}

.log-view {
	margin: 0 1.25rem 1.25rem;
	max-height: 14rem;
	overflow: auto;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	padding: 0.75rem;
	font-family: $family-monospace;
	font-size: 0.725rem;
	color: #334155;
	white-space: pre-wrap;
	word-break: break-word;
}
</style>
