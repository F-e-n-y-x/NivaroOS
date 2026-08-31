<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Updates & Maintenance') }}</h2>

		<!-- ==================== NIVAROOS UPDATES ==================== -->
		<h3 class="setting-card-title is-flex is-align-items-center is-justify-content-between">
			<span class="is-flex is-align-items-center">
				<i class="mdi mdi-update mr-2 section-card-icon"></i>
				{{ $t('NivaroOS System Update') }}
			</span>
			<b-button rounded size="is-small" :loading="checkingNivaro" :disabled="checkingNivaro || nivaroUpdating" @click="checkNivaroUpdates">
				<i class="mdi mdi-refresh mr-1"></i>
				{{ $t('Check for updates') }}
			</b-button>
		</h3>

		<div class="setting-card update-studio-card">
			<!-- Version Status Banner -->
			<div class="update-header-box is-flex is-align-items-center is-justify-content-between">
				<div class="is-flex is-align-items-center">
					<div class="status-icon-box" :class="{ 'is-update-available': hasNivaroUpdate, 'is-up-to-date': !hasNivaroUpdate }">
						<i :class="hasNivaroUpdate ? 'mdi mdi-cloud-download' : 'mdi mdi-check-circle'"></i>
					</div>
					<div class="ml-3">
						<div class="version-status-title">
							{{ hasNivaroUpdate ? $t('New NivaroOS update available') : $t('NivaroOS is up to date') }}
						</div>
						<div class="version-status-sub">
							<span>{{ $t('Installed') }}: <strong>{{ currentVersion || 'v0.4.5' }}</strong></span>
							<span class="mx-2">&middot;</span>
							<span>{{ $t('Latest') }}: <strong>{{ latestGithubVersion || latestVersion || currentVersion || 'v0.4.5' }}</strong></span>
						</div>
					</div>
				</div>

				<div>
					<b-button v-if="hasNivaroUpdate" rounded size="is-small" type="is-primary" :loading="nivaroUpdating" @click="startNivaroUpdate">
						<i class="mdi mdi-download mr-1"></i>
						{{ $t('Update NivaroOS') }}
					</b-button>
					<span v-else class="tag is-success is-light is-rounded">
						<i class="mdi mdi-check mr-1"></i>
						{{ $t('Latest build') }}
					</span>
				</div>
			</div>

			<!-- GitHub Master Commit Details -->
			<div v-if="latestCommit" class="github-commit-info mt-3">
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
			<div v-if="githubReleaseNotes" class="release-notes-box mt-3">
				<div class="release-notes-head is-flex is-align-items-center is-justify-content-between" @click="showChangelog = !showChangelog">
					<span class="font-semibold is-size-7">{{ $t('Release Highlights & Changelog') }}</span>
					<i :class="showChangelog ? 'mdi mdi-chevron-up' : 'mdi mdi-chevron-down'"></i>
				</div>
				<div v-if="showChangelog" class="release-notes-body">
					<pre class="notes-pre">{{ githubReleaseNotes }}</pre>
				</div>
			</div>

			<div class="is-flex is-align-items-center is-justify-content-between pt-3 mt-3 border-t">
				<a href="https://github.com/F-e-n-y-x/NivaroOS/releases" target="_blank" rel="noopener noreferrer" class="github-link is-flex is-align-items-center">
					<i class="mdi mdi-open-in-new mr-1"></i>
					{{ $t('View releases on GitHub') }}
				</a>
				<span class="text-muted is-size-7">{{ $t('Last checked') }}: {{ lastNivaroCheckTime || $t('Just now') }}</span>
			</div>
		</div>

		<!-- ==================== LINUX SYSTEM PACKAGES (APT) ==================== -->
		<h3 class="setting-card-title is-flex is-align-items-center is-justify-content-between mt-4">
			<span class="is-flex is-align-items-center">
				<i class="mdi mdi-package-variant mr-2 section-card-icon"></i>
				{{ $t('Linux System Packages (APT)') }}
			</span>
			<div class="buttons are-small mb-0">
				<b-button rounded size="is-small" :loading="checkingApt" :disabled="checkingApt || aptUpgrading" @click="refreshAptPackages">
					<i class="mdi mdi-refresh mr-1"></i>
					{{ $t('Check for OS updates') }}
				</b-button>
				<b-button v-if="pkgCount > 0" rounded size="is-small" type="is-primary" :loading="aptUpgrading" @click="openSystemUpgradeWindow">
					<i class="mdi mdi-arrow-up-bold-circle mr-1"></i>
					{{ $t('Upgrade All Packages') }} ({{ pkgCount }})
				</b-button>
			</div>
		</h3>

		<div class="setting-card update-studio-card">
			<!-- Package Status Header -->
			<div class="update-header-box is-flex is-align-items-center is-justify-content-between">
				<div class="is-flex is-align-items-center">
					<div class="status-icon-box" :class="{ 'is-update-available': pkgCount > 0, 'is-up-to-date': pkgCount === 0 }">
						<i :class="pkgCount > 0 ? 'mdi mdi-package-down' : 'mdi mdi-check-circle'"></i>
					</div>
					<div class="ml-3">
						<div class="version-status-title">
							{{ pkgCount > 0 ? `${pkgCount} ${$t('package updates available')}` : $t('All system packages are up to date') }}
						</div>
						<div class="version-status-sub">
							<span v-if="securityCount > 0" class="has-text-danger font-semibold">
								<i class="mdi mdi-shield-alert mr-1"></i>
								{{ securityCount }} {{ $t('security updates') }}
							</span>
							<span v-else>{{ $t('Debian / Linux base system packages') }}</span>
							<span class="mx-2">&middot;</span>
							<span>{{ $t('Last checked') }}: {{ lastAptCheckTime || $t('Just now') }}</span>
						</div>
					</div>
				</div>

				<div class="is-flex is-align-items-center">
					<b-button v-if="pkgCount > 0" rounded size="is-small" type="is-dark" @click="openSystemUpgradeWindow">
						<i class="mdi mdi-console mr-1"></i>
						{{ $t('Open Updater Window') }}
					</b-button>
					<span v-else class="tag is-success is-light is-rounded">
						<i class="mdi mdi-check mr-1"></i>
						{{ $t('Up to date') }}
					</span>
				</div>
			</div>

			<!-- Search / Filter bar if packages exist -->
			<div v-if="pkgCount > 0" class="is-flex is-align-items-center is-justify-content-between mt-3 mb-2">
				<b-input v-model="pkgSearch" size="is-small" icon="magnify" icon-pack="mdi" :placeholder="$t('Filter packages...')" class="pkg-search-input"></b-input>
				<span class="text-muted is-size-7">{{ filteredPackages.length }} / {{ pkgCount }} {{ $t('packages') }}</span>
			</div>

			<!-- Upgradable Packages List -->
			<div v-if="filteredPackages.length > 0" class="package-list-box mt-2">
				<div v-for="pkg in filteredPackages" :key="pkg.name" class="package-item is-flex is-align-items-center is-justify-content-between">
					<div class="package-main">
						<div class="package-name is-flex is-align-items-center">
							<span class="font-semibold">{{ pkg.name }}</span>
							<span v-if="pkg.is_security" class="tag is-danger is-light is-rounded ml-2 is-size-7">
								<i class="mdi mdi-shield mr-1"></i>{{ $t('Security') }}
							</span>
							<span class="tag is-light is-rounded ml-2 is-size-7">{{ pkg.arch }}</span>
						</div>
						<div class="package-version-diff">
							<span class="ver-curr">{{ pkg.current_version }}</span>
							<i class="mdi mdi-arrow-right mx-1 ver-arrow"></i>
							<span class="ver-new">{{ pkg.new_version }}</span>
							<span v-if="pkg.suite" class="suite-tag ml-2">{{ pkg.suite }}</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Empty State when 0 packages -->
			<div v-else-if="pkgCount === 0" class="has-text-centered py-4">
				<i class="mdi mdi-check-all has-text-success is-size-3"></i>
				<p class="text-muted is-size-7 mt-1">{{ $t('Your Linux OS packages are completely up to date.') }}</p>
			</div>
		</div>

		<!-- ==================== UPGRADE TERMINAL / STATUS (IF RUNNING) ==================== -->
		<div v-if="aptUpgradeLogs.length > 0 || aptUpgrading" class="mt-4">
			<h3 class="setting-card-title is-flex is-align-items-center is-justify-content-between">
				<span class="is-flex is-align-items-center">
					<i class="mdi mdi-console-line mr-2 section-card-icon"></i>
					{{ $t('Live Upgrade Progress') }}
				</span>
				<span v-if="aptUpgrading" class="tag is-info is-light is-rounded">
					<i class="mdi mdi-loading mdi-spin mr-1"></i>{{ $t('Upgrading...') }}
				</span>
				<span v-else-if="aptExitCode === 0" class="tag is-success is-light is-rounded">
					<i class="mdi mdi-check mr-1"></i>{{ $t('Finished') }}
				</span>
			</h3>
			<div class="setting-card">
				<pre ref="logConsole" class="terminal-log-view">{{ aptUpgradeLogs.join('\n') }}</pre>
			</div>
		</div>
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
					this.currentVersion = data.current_version || 'v0.4.5'
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
		}
	}
}
</script>

<style lang="scss" scoped>
.section-card-icon {
	font-size: 1.25rem;
	color: #3b82f6;
}

.update-studio-card {
	padding: 1.25rem;
	margin-bottom: 1.25rem;
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
	flex-shrink: 0;

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
	font-size: 0.95rem;
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

.pkg-search-input {
	width: 14rem;
}

.package-list-box {
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 8px;
	max-height: 16rem;
	overflow-y: auto;
	background: #fff;
}

.package-item {
	padding: 0.65rem 0.9rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.05);

	&:last-child {
		border-bottom: none;
	}

	&:hover {
		background: rgba(0, 0, 0, 0.015);
	}
}

.package-name {
	font-size: 0.85rem;
	color: #1e293b;
}

.package-version-diff {
	font-size: 0.75rem;
	color: #64748b;
	margin-top: 0.15rem;

	.ver-curr {
		color: #94a3b8;
	}

	.ver-arrow {
		font-size: 0.75rem;
		color: #cbd5e1;
	}

	.ver-new {
		color: #10b981;
		font-weight: 600;
	}

	.suite-tag {
		color: #94a3b8;
		font-size: 0.7rem;
	}
}

.terminal-log-view {
	margin: 0;
	max-height: 16rem;
	overflow: auto;
	background: #18181b;
	color: #22c55e;
	border-radius: 12px;
	padding: 0.85rem 1rem;
	font-family: monospace;
	font-size: 0.75rem;
	line-height: 1.4;
	white-space: pre-wrap;
	word-break: break-word;
}
</style>
