<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Updates & Maintenance') }}</h2>

		<!-- ==================== 1. NIVAROOS SYSTEM UPDATE ==================== -->
		<h3 class="setting-card-title">{{ $t('NivaroOS System Update') }}</h3>
		<div class="setting-card">
			<!-- Main Status Row -->
			<div class="setting-row">
				<b-icon class="row-icon" :icon="hasNivaroUpdate ? 'cloud-download-outline' : 'check-circle-outline'" pack="mdi" size="is-20" :class="{ 'has-text-success': !hasNivaroUpdate, 'has-text-primary': hasNivaroUpdate }"></b-icon>
				<div class="row-label">
					<div class="setting-title">
						{{ hasNivaroUpdate ? $t('New NivaroOS update available') : $t('NivaroOS is up to date') }}
					</div>
					<div class="setting-desc">
						<span>{{ $t('Installed') }}: {{ currentVersion || 'v0.4.5' }}</span>
						<span class="mx-2">&middot;</span>
						<span>{{ $t('Latest') }}: {{ latestGithubVersion || latestVersion || currentVersion || 'v0.4.5' }}</span>
						<span class="mx-2">&middot;</span>
						<span>{{ $t('Checked') }}: {{ lastNivaroCheckTime || $t('Just now') }}</span>
					</div>
				</div>
				<div class="row-control">
					<div class="buttons are-small mb-0">
						<b-button rounded size="is-small" :loading="checkingNivaro" :disabled="checkingNivaro || nivaroUpdating" @click="checkNivaroUpdates">
							<i class="mdi mdi-refresh mr-1"></i>
							{{ $t('Check for updates') }}
						</b-button>
						<b-button v-if="hasNivaroUpdate" rounded size="is-small" type="is-primary" :loading="nivaroUpdating" @click="startNivaroUpdate">
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Update NivaroOS') }}
						</b-button>
						<span v-else class="tag is-success is-light is-rounded">
							<i class="mdi mdi-check mr-1"></i>
							{{ $t('Up to date') }}
						</span>
					</div>
				</div>
			</div>

			<!-- GitHub Master Commit Details (if available) -->
			<div v-if="latestCommit" class="update-inset-box">
				<div class="is-flex is-align-items-center is-justify-content-between mb-1">
					<span class="commit-label is-flex is-align-items-center">
						<i class="mdi mdi-github mr-1"></i>
						{{ $t('GitHub Master Branch') }} (<code>{{ latestCommit.sha.substring(0, 7) }}</code>)
					</span>
					<span class="commit-time">{{ formatDate(latestCommit.commit.author.date) }}</span>
				</div>
				<div class="commit-msg">{{ latestCommit.commit.message }}</div>
			</div>

			<!-- Release Notes Accordion -->
			<div v-if="githubReleaseNotes" class="update-inset-box mt-0">
				<div class="release-notes-head is-flex is-align-items-center is-justify-content-between" @click="showChangelog = !showChangelog">
					<span class="is-size-7 text-muted">{{ $t('Release Highlights & Changelog') }}</span>
					<i :class="showChangelog ? 'mdi mdi-chevron-up' : 'mdi mdi-chevron-down'"></i>
				</div>
				<div v-if="showChangelog" class="release-notes-body">
					<pre class="notes-pre">{{ githubReleaseNotes }}</pre>
				</div>
			</div>

			<!-- Footer Row: GitHub link -->
			<div class="setting-row sub-row">
				<b-icon class="row-icon" icon="github" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<a href="https://github.com/F-e-n-y-x/NivaroOS/releases" target="_blank" rel="noopener noreferrer" class="github-link is-flex is-align-items-center">
						{{ $t('View releases and changelog on GitHub') }}
						<b-icon icon="open-in-new" pack="mdi" size="is-14" class="ml-1"></b-icon>
					</a>
				</div>
			</div>
		</div>

		<!-- ==================== 2. LINUX SYSTEM PACKAGES (APT) ==================== -->
		<h3 class="setting-card-title">{{ $t('Linux System Packages (APT)') }}</h3>
		<div class="setting-card">
			<!-- Main Status Row -->
			<div class="setting-row">
				<b-icon class="row-icon" :icon="pkgCount > 0 ? 'package-down' : 'check-circle-outline'" pack="mdi" size="is-20" :class="{ 'has-text-success': pkgCount === 0, 'has-text-primary': pkgCount > 0 }"></b-icon>
				<div class="row-label">
					<div class="setting-title">
						{{ pkgCount > 0 ? `${pkgCount} ${$t('package updates available')}` : $t('All system packages are up to date') }}
					</div>
					<div class="setting-desc">
						<span v-if="securityCount > 0" class="has-text-danger">
							<i class="mdi mdi-shield-alert mr-1"></i>
							{{ securityCount }} {{ $t('security updates') }}
						</span>
						<span v-else>{{ $t('Debian / Linux base system packages') }}</span>
						<span class="mx-2">&middot;</span>
						<span>{{ $t('Checked') }}: {{ lastAptCheckTime || $t('Just now') }}</span>
					</div>
				</div>
				<div class="row-control">
					<div class="buttons are-small mb-0">
						<b-button rounded size="is-small" :loading="checkingApt" :disabled="checkingApt || aptUpgrading" @click="refreshAptPackages">
							<i class="mdi mdi-refresh mr-1"></i>
							{{ $t('Check for OS updates') }}
						</b-button>
						<b-button v-if="pkgCount > 0" rounded size="is-small" type="is-primary" :loading="aptUpgrading" @click="openSystemUpgradeWindow">
							<i class="mdi mdi-arrow-up-bold-circle mr-1"></i>
							{{ $t('Upgrade All Packages') }} ({{ pkgCount }})
						</b-button>
						<span v-else class="tag is-success is-light is-rounded">
							<i class="mdi mdi-check mr-1"></i>
							{{ $t('Up to date') }}
						</span>
					</div>
				</div>
			</div>

			<!-- Search & Filter bar (if packages exist) -->
			<div v-if="pkgCount > 0" class="setting-row filter-row">
				<div class="row-label">
					<b-input v-model="pkgSearch" size="is-small" icon="magnify" icon-pack="mdi" :placeholder="$t('Filter packages...')" class="pkg-search-input"></b-input>
				</div>
				<div class="row-control">
					<span class="text-muted is-size-7">{{ filteredPackages.length }} / {{ pkgCount }} {{ $t('packages') }}</span>
				</div>
			</div>

			<!-- Upgradable Package List Rows -->
			<div v-if="filteredPackages.length > 0" class="package-list-wrapper">
				<div v-for="pkg in filteredPackages" :key="pkg.name" class="setting-row">
					<b-icon class="row-icon" :icon="pkg.is_security ? 'shield-alert-outline' : 'package-up'" pack="mdi" size="is-20" :class="{ 'has-text-danger': pkg.is_security }"></b-icon>
					<div class="row-label">
						<div class="setting-title is-flex is-align-items-center">
							<span>{{ pkg.name }}</span>
							<span v-if="pkg.is_security" class="tag is-danger is-light is-rounded ml-2 is-size-7">
								<i class="mdi mdi-shield mr-1"></i>{{ $t('Security') }}
							</span>
							<span class="setting-chip ml-2">{{ pkg.arch }}</span>
						</div>
						<div class="setting-desc">{{ pkg.suite }}</div>
					</div>
					<div class="row-control">
						<div class="package-version-badge">
							<span class="ver-curr">{{ pkg.current_version }}</span>
							<i class="mdi mdi-arrow-right mx-1 ver-arrow"></i>
							<span class="ver-new">{{ pkg.new_version }}</span>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== 3. LIVE UPGRADE PROGRESS (IF RUNNING OR LOGS EXIST) ==================== -->
		<template v-if="aptUpgradeLogs.length > 0 || aptUpgrading">
			<h3 class="setting-card-title">{{ $t('Live Upgrade Progress') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<div class="row-label is-flex is-align-items-center">
						<span class="font-semibold">{{ $t('Terminal Console Output') }}</span>
						<span v-if="aptUpgrading" class="tag is-info is-light is-rounded ml-2">
							<i class="mdi mdi-loading mdi-spin mr-1"></i>{{ $t('Running...') }}
						</span>
						<span v-else-if="aptExitCode === 0" class="tag is-success is-light is-rounded ml-2">
							<i class="mdi mdi-check mr-1"></i>{{ $t('Finished') }}
						</span>
						<span v-else class="tag is-danger is-light is-rounded ml-2">
							<i class="mdi mdi-alert-circle mr-1"></i>{{ $t('Error') }}
						</span>
					</div>
					<div class="row-control">
						<div class="buttons are-small mb-0">
							<b-button rounded size="is-small" @click="openSystemUpgradeWindow">
								<i class="mdi mdi-open-in-new mr-1"></i>
								{{ $t('Open Window') }}
							</b-button>
							<b-button rounded size="is-small" @click="copyLogs">
								<i class="mdi mdi-content-copy mr-1"></i>
								{{ $t('Copy') }}
							</b-button>
						</div>
					</div>
				</div>
				<pre ref="logConsole" class="terminal-log-view">{{ aptUpgradeLogs.join('\n') }}</pre>
			</div>
		</template>
	</section>
</template>

<script>
import axios from 'axios'

export const ROWS = [
	{ label: 'NivaroOS System Update' },
	{ label: 'Linux System Packages (APT)' }
]

export default {
	name: 'updates-section',
	data() {
		return {
			currentVersion: 'v0.4.5',
			needUpdate: false,
			latestVersion: '',
			latestGithubVersion: '',
			latestCommit: null,
			githubReleaseNotes: '',
			showChangelog: false,
			checkingNivaro: false,
			lastNivaroCheckTime: '',
			nivaroUpdating: false,

			// APT state
			checkingApt: false,
			aptUpgrading: false,
			pkgCount: 0,
			securityCount: 0,
			packages: [],
			lastAptCheckTime: '',
			pkgSearch: '',
			aptUpgradeLogs: [],
			aptExitCode: 0,
			pollTimer: null
		}
	},
	computed: {
		hasNivaroUpdate() {
			if (this.needUpdate) return true
			if (this.latestGithubVersion && this.currentVersion && this.latestGithubVersion !== this.currentVersion) {
				return true
			}
			return false
		},
		filteredPackages() {
			if (!this.pkgSearch.trim()) return this.packages
			const q = this.pkgSearch.toLowerCase().trim()
			return this.packages.filter(p => p.name.toLowerCase().includes(q) || (p.suite && p.suite.toLowerCase().includes(q)))
		}
	},
	created() {
		this.loadNivaroVersion()
		this.checkGithubUpdates()
		this.loadAptPackages()
		this.checkUpgradeStatus()
	},
	beforeDestroy() {
		if (this.pollTimer) clearInterval(this.pollTimer)
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
		loadNivaroVersion() {
			this.$api.sys.getVersion().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.currentVersion = data.current_version || 'v1.0.0'
					this.needUpdate = !!data.need_update
					this.latestVersion = data.version && data.version.version ? data.version.version : ''
				}
			}).catch(() => {})
		},
		async checkGithubUpdates() {
			this.checkingNivaro = true
			try {
				const commitRes = await axios.get('https://api.github.com/repos/F-e-n-y-x/NivaroOS/commits/master')
				if (commitRes.data) {
					this.latestCommit = commitRes.data
				}
			} catch (e) {
				console.warn('GitHub commits check:', e.message)
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
				this.checkingNivaro = false
				this.lastNivaroCheckTime = new Date().toLocaleTimeString()
			}
		},
		checkNivaroUpdates() {
			this.loadNivaroVersion()
			this.checkGithubUpdates()
			this.$buefy.toast.open({
				message: this.$t('Checking for latest NivaroOS updates...'),
				type: 'is-info',
				duration: 2000
			})
		},
		startNivaroUpdate() {
			this.openSystemUpgradeWindow({ mode: 'nivaroos' })
		},
		loadAptPackages() {
			this.$api.sys.getPackageUpdates().then(res => {
				if (res.data.success === 200) {
					this.pkgCount = res.data.data.count || 0
					this.securityCount = res.data.data.security_count || 0
					this.packages = res.data.data.packages || []
					this.lastAptCheckTime = new Date().toLocaleTimeString()
				}
			}).catch(() => {})
		},
		refreshAptPackages() {
			this.checkingApt = true
			this.$api.sys.refreshPackageUpdates().then(res => {
				if (res.data.success === 200) {
					this.pkgCount = res.data.data.count || 0
					this.securityCount = res.data.data.security_count || 0
					this.packages = res.data.data.packages || []
					this.lastAptCheckTime = new Date().toLocaleTimeString()
					this.$buefy.toast.open({
						message: this.pkgCount > 0 ? `${this.pkgCount} ${this.$t('updates found')}` : this.$t('System packages are up to date'),
						type: this.pkgCount > 0 ? 'is-info' : 'is-success',
						duration: 2500
					})
				}
			}).catch(err => {
				this.$buefy.toast.open({
					message: this.$t('Failed to refresh package updates: ') + (err.message || ''),
					type: 'is-danger',
					duration: 3000
				})
			}).finally(() => {
				this.checkingApt = false
			})
		},
		openSystemUpgradeWindow(opts = {}) {
			const mode = opts.mode || 'apt'
			this.$store.commit('OPEN_WINDOW', {
				id: 'system-updater',
				title: mode === 'nivaroos' ? this.$t('NivaroOS Updater') : this.$t('Linux System Package Updater'),
				component: 'SystemUpdateWindow',
				props: {
					initialMode: mode,
					packages: this.packages,
					pkgCount: this.pkgCount
				},
				width: 780,
				height: 560
			})
		},
		checkUpgradeStatus() {
			this.$api.sys.getPackageUpgradeStatus().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.aptUpgrading = data.running
					this.aptExitCode = data.exit_code
					this.aptUpgradeLogs = data.logs || []
					if (data.running) {
						this.startStatusPolling()
					}
				}
			}).catch(() => {})
		},
		startStatusPolling() {
			if (this.pollTimer) clearInterval(this.pollTimer)
			this.pollTimer = setInterval(() => {
				this.$api.sys.getPackageUpgradeStatus().then(res => {
					if (res.data.success === 200) {
						const data = res.data.data
						this.aptUpgrading = data.running
						this.aptExitCode = data.exit_code
						this.aptUpgradeLogs = data.logs || []
						if (!data.running) {
							clearInterval(this.pollTimer)
							this.loadAptPackages()
						}
					}
				}).catch(() => {
					clearInterval(this.pollTimer)
				})
			}, 1500)
		},
		copyLogs() {
			navigator.clipboard.writeText(this.aptUpgradeLogs.join('\n')).then(() => {
				this.$buefy.toast.open({
					message: this.$t('Logs copied to clipboard'),
					type: 'is-success',
					duration: 2000
				})
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.update-hero-row {
	padding: var(--space-5) var(--space-5);
}

.update-hero-icon {
	width: 42px;
	height: 42px;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: var(--font-2xl);
	flex-shrink: 0;
	margin-right: var(--space-4);

	&.is-up-to-date {
		background: rgba(16, 185, 129, 0.12);
		color: #10b981;
	}

	&.is-update-available {
		background: rgba(59, 130, 246, 0.12);
		color: #2563eb;
	}
}

.update-hero-title {
	font-size: var(--font-md);
	font-weight: 500;
	color: var(--theme-text-primary, #1e293b);
	line-height: 1.25;
}

.update-hero-meta {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	margin-top: var(--space-1);
}

.update-inset-box {
	margin: 0 var(--space-5) var(--space-3);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-control);
	padding: var(--space-3) var(--space-4);

	.commit-label {
		font-size: var(--font-xs);
		font-weight: 500;
		color: var(--theme-text-secondary, #475569);
	}

	.commit-time {
		font-size: var(--font-xs);
		color: var(--theme-text-muted, #94a3b8);
	}

	.commit-msg {
		font-size: var(--font-sm);
		color: var(--theme-text-primary, #1e293b);
		font-family: monospace;
		white-space: pre-wrap;
		word-break: break-word;
	}
}

.release-notes-head {
	cursor: pointer;
	user-select: none;
}

.release-notes-body {
	padding-top: var(--space-2);
	max-height: 10rem;
	overflow-y: auto;
}

.notes-pre {
	font-size: var(--font-xs);
	white-space: pre-wrap;
	word-break: break-word;
	background: transparent;
	padding: 0;
}

.sub-row {
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.015));
	padding: var(--space-3) var(--space-5);
}

.github-link {
	font-size: var(--font-xs);
	font-weight: 500;
	color: var(--color-primary, #2563eb);

	&:hover {
		text-decoration: underline;
	}
}

.filter-row {
	background: var(--theme-card-subtle, #f8fafc);
	padding: var(--space-3) var(--space-5);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
}

.pkg-search-input {
	width: 14rem;
}

.package-list-wrapper {
	max-height: 18rem;
	overflow-y: auto;
}

.package-item-row {
	padding: var(--space-3) var(--space-5);

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.015));
	}
}

.package-name-line {
	font-size: var(--font-base);
	color: var(--theme-text-primary, #1e293b);
}

.suite-tag {
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-2xs);
}

.package-version-badge {
	font-size: var(--font-xs);
	font-family: 'Consolas', 'Monaco', monospace;

	.ver-curr {
		color: var(--theme-text-muted, #94a3b8);
	}

	.ver-arrow {
		font-size: var(--font-xs);
		color: var(--theme-text-muted, #cbd5e1);
	}

	.ver-new {
		color: #10b981;
		font-weight: 500;
	}
}

.terminal-log-view {
	margin: 0 var(--space-5) var(--space-5);
	max-height: 14rem;
	overflow: auto;
	background: #1e1e1e;
	color: #ffffff;
	border-radius: var(--radius-control);
	padding: var(--space-3) var(--space-4);
	font-family: 'Consolas', 'Monaco', monospace;
	font-size: 13px;
	line-height: 1.5em;
	white-space: pre-wrap;
	word-break: break-word;
	scrollbar-width: thin;
	scrollbar-color: rgba(255, 255, 255, 0.2) transparent;

}
</style>
