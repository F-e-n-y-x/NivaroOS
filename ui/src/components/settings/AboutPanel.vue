<template>
	<div class="about-panel">
		<!-- NivaroOS & System Updates Card -->
		<h3 class="setting-card-title is-flex is-align-items-center is-justify-content-between">
			<span class="is-flex is-align-items-center">
				<i class="mdi mdi-update mr-2 title-icon"></i>
				{{ $t('NivaroOS & System Updates') }}
			</span>
			<b-button rounded size="is-small" :loading="checkingGithub" :disabled="checkingGithub || updating" @click="checkForUpdates">
				<i class="mdi mdi-refresh mr-1"></i>
				{{ $t('Check for updates') }}
			</b-button>
		</h3>

		<div class="setting-card update-studio-card">
			<!-- Version Status Banner -->
			<div class="update-header-box is-flex is-align-items-center is-justify-content-between">
				<div class="is-flex is-align-items-center">
					<div class="status-icon-box" :class="{ 'is-update-available': hasUpdate, 'is-up-to-date': !hasUpdate }">
						<i :class="hasUpdate ? 'mdi mdi-cloud-download' : 'mdi mdi-check-circle'"></i>
					</div>
					<div class="ml-3">
						<div class="version-status-title">
							{{ hasUpdate ? $t('New update available') : $t('NivaroOS is up to date') }}
						</div>
						<div class="version-status-sub">
							<span>{{ $t('Installed') }}: <strong>{{ currentVersion || 'v0.4.5' }}</strong></span>
							<span class="mx-2">&middot;</span>
							<span>{{ $t('Latest') }}: <strong>{{ latestGithubVersion || latestVersion || 'v0.4.5' }}</strong></span>
						</div>
					</div>
				</div>

				<div>
					<b-button v-if="hasUpdate" rounded size="is-small" type="is-primary" :loading="updating" @click="applyUpdate">
						<i class="mdi mdi-download mr-1"></i>
						{{ $t('Update Now') }}
					</b-button>
					<span v-else class="tag is-success is-light is-rounded">
						<i class="mdi mdi-check mr-1"></i>
						{{ $t('Latest build') }}
					</span>
				</div>
			</div>

			<!-- Release Details / Commit Info -->
			<div v-if="latestCommit" class="github-commit-info mt-3">
				<div class="is-flex is-align-items-center is-justify-content-between mb-2">
					<span class="commit-label is-flex is-align-items-center">
						<i class="mdi mdi-github mr-1"></i>
						{{ $t('GitHub Master Branch') }} ({{ latestCommit.sha.substring(0, 7) }})
					</span>
					<span class="commit-time">{{ formatDate(latestCommit.commit.author.date) }}</span>
				</div>
				<div class="commit-msg">{{ latestCommit.commit.message }}</div>
			</div>

			<!-- Changelog Accordion if available -->
			<div v-if="githubReleaseNotes" class="release-notes-box mt-3">
				<div class="release-notes-head is-flex is-align-items-center is-justify-content-between" @click="showChangelog = !showChangelog">
					<span class="font-semibold is-size-7">{{ $t('Release Highlights & Changelog') }}</span>
					<i :class="showChangelog ? 'mdi mdi-chevron-up' : 'mdi mdi-chevron-down'"></i>
				</div>
				<div v-if="showChangelog" class="release-notes-body">
					<pre class="notes-pre">{{ githubReleaseNotes }}</pre>
				</div>
			</div>

			<div class="is-flex is-align-items-center is-justify-content-between pt-3 mt-2 border-t">
				<a href="https://github.com/F-e-n-y-x/NivaroOS/releases" target="_blank" rel="noopener noreferrer" class="github-link is-flex is-align-items-center">
					<i class="mdi mdi-open-in-new mr-1"></i>
					{{ $t('View releases on GitHub') }}
				</a>
				<span class="text-muted is-size-7">{{ $t('Last checked') }}: {{ lastCheckedTime || $t('Just now') }}</span>
			</div>
		</div>

		<!-- System Hardware Specifications -->
		<h3 class="setting-card-title mt-4">{{ $t('System Specifications') }}</h3>
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
		<h3 class="setting-card-title mt-4">{{ $t('Storage Usage') }}</h3>
		<div class="setting-card">
			<div v-for="d in disksUsage" :key="d.mount_point" class="setting-row">
				<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ d.mount_point }}</div>
				<div class="row-control">{{ d.used }} / {{ d.total }} ({{ d.percent }}) &middot; {{ d.fstype }}</div>
			</div>
		</div>

		<!-- Error Logs -->
		<h3 class="setting-card-title mt-4">{{ $t('System Logs') }}</h3>
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
import axios from 'axios'

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
			currentVersion: 'v0.4.5',
			needUpdate: false,
			latestVersion: '',
			latestGithubVersion: '',
			latestCommit: null,
			githubReleaseNotes: '',
			showChangelog: false,
			checkingGithub: false,
			lastCheckedTime: '',
			updating: false,
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
		hasUpdate() {
			if (this.needUpdate) return true
			if (this.latestGithubVersion && this.currentVersion && this.latestGithubVersion !== this.currentVersion) {
				return true
			}
			return false
		},
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
		this.loadVersion()
		this.loadHardware()
		this.loadUtilization()
		this.loadDisksUsage()
		this.loadLogs()
		this.checkGithubUpdates()
	},
	methods: {
		formatDate(d) {
			if (!d) return ''
			try {
				return new Date(d).toLocaleString()
			} catch (e) {
				return d
			}
		},
		loadVersion() {
			this.$api.sys.getVersion().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.currentVersion = data.current_version || 'v0.4.5'
					this.needUpdate = !!data.need_update
					this.latestVersion = data.version && data.version.version ? data.version.version : ''
				}
			}).catch(() => {})
		},
		async checkGithubUpdates() {
			this.checkingGithub = true
			try {
				const commitRes = await axios.get('https://api.github.com/repos/F-e-n-y-x/NivaroOS/commits/master')
				if (commitRes.data) {
					this.latestCommit = commitRes.data
				}
			} catch (e) {
				console.warn('GitHub commits lookup:', e.message)
			}

			try {
				const releaseRes = await axios.get('https://api.github.com/repos/F-e-n-y-x/NivaroOS/releases/latest')
				if (releaseRes.data) {
					this.latestGithubVersion = releaseRes.data.tag_name || releaseRes.data.name
					this.githubReleaseNotes = releaseRes.data.body || ''
				}
			} catch (e) {
				// No releases created yet on GitHub or rate-limited
			} finally {
				this.checkingGithub = false
				this.lastCheckedTime = new Date().toLocaleTimeString()
			}
		},
		checkForUpdates() {
			this.loadVersion()
			this.checkGithubUpdates()
			this.$buefy.toast.open({
				message: this.$t('Checking for latest NivaroOS updates...'),
				type: 'is-info',
				duration: 2500
			})
		},
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
		},
		applyUpdate() {
			this.updating = true
			this.$api.sys.updateRecasa().then(() => {
				this.$buefy.toast.open({
					message: this.$t('NivaroOS update initiated in background...'),
					type: 'is-success',
					duration: 4000
				})
				const timer = setInterval(() => {
					this.$api.sys.getVersion().then(res => {
						if (res.data.success === 200 && !res.data.data.need_update) {
							clearInterval(timer)
							this.updating = false
							this.loadVersion()
						}
					})
				}, 5000)
			}).catch(() => {
				this.updating = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.title-icon {
	font-size: 1.25rem;
	color: #3b82f6;
}

.update-studio-card {
	padding: 1.25rem;
}

.update-header-box {
	background: rgba(0, 0, 0, 0.02);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 12px;
	padding: 1rem;
}

.status-icon-box {
	width: 44px;
	height: 44px;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 1.5rem;

	&.is-up-to-date {
		background: rgba(16, 185, 129, 0.12);
		color: #10b981;
	}

	&.is-update-available {
		background: rgba(59, 130, 246, 0.12);
		color: #2563eb;
	}
}

.version-status-title {
	font-size: 1rem;
	font-weight: 700;
	color: #1f2937;
}

.version-status-sub {
	font-size: 0.8125rem;
	color: #6b7280;
	margin-top: 0.15rem;
}

.github-commit-info {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	padding: 0.75rem 1rem;

	.commit-label {
		font-size: 0.75rem;
		font-weight: 600;
		color: #475569;
	}

	.commit-time {
		font-size: 0.75rem;
		color: #94a3b8;
	}

	.commit-msg {
		font-size: 0.8125rem;
		color: #1e293b;
		font-family: monospace;
		white-space: pre-wrap;
		word-break: break-word;
	}
}

.release-notes-box {
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	overflow: hidden;

	.release-notes-head {
		background: #f1f5f9;
		padding: 0.5rem 0.85rem;
		cursor: pointer;
		user-select: none;
	}

	.release-notes-body {
		padding: 0.75rem;
		background: #ffffff;
		max-height: 10rem;
		overflow-y: auto;
	}

	.notes-pre {
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
		background: transparent;
		padding: 0;
	}
}

.border-t {
	border-top: 1px solid rgba(0, 0, 0, 0.06);
}

.github-link {
	font-size: 0.8125rem;
	font-weight: 600;
	color: #2563eb;

	&:hover {
		text-decoration: underline;
	}
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
	color: rgba(44, 62, 80, 0.6);
	margin-left: 0.35rem;

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}

	&.is-confirm {
		background: hsla(140, 60%, 45%, 0.15);
		color: hsla(140, 60%, 32%, 1);

		&:hover {
			background: hsla(140, 60%, 45%, 0.25);
		}
	}
}

.update-dot {
	display: inline-block;
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 50%;
	background: hsla(140, 60%, 45%, 1);
	margin-right: 0.4rem;
}

.log-header-row {
	.row-label {
		font-weight: 600;
	}
}

.log-view {
	margin: 0 1.25rem 1.25rem;
	max-height: 14rem;
	overflow: auto;
	background: rgba(0, 0, 0, 0.03);
	border-radius: 8px;
	padding: 0.75rem;
	font-size: 0.7rem;
	white-space: pre-wrap;
	word-break: break-word;
}
</style>
