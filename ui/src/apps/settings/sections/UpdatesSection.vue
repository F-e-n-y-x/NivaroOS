<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Updates & Maintenance') }}</h2>

		<!-- ==================== 1. NIVAROOS SYSTEM UPDATE ==================== -->
		<!-- Built from GitHub master: up to date when master has nothing this build lacks (core asks GitHub). -->
		<h3 class="setting-card-title">{{ $t('NivaroOS System Update') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" :icon="statusIcon" pack="mdi" size="is-20" :class="{ 'has-text-success': updateState === 'up_to_date', 'has-text-primary': hasNivaroUpdate }"></b-icon>
				<div class="row-label">
					<div class="setting-title" data-test="nivaro-status">{{ statusTitle }}</div>
					<div class="setting-desc">
						<span data-test="nivaro-build">{{ buildLabel }}</span>
						<template v-if="updateState === 'unknown' && update.reason">
							<span class="mx-2">&middot;</span>
							<span>{{ update.reason }}</span>
						</template>
						<template v-if="lastNivaroCheckTime">
							<span class="mx-2">&middot;</span>
							<span>{{ $t('Checked') }}: {{ lastNivaroCheckTime }}</span>
						</template>
					</div>
				</div>
				<div class="row-control">
					<div class="buttons are-small mb-0">
						<b-button rounded size="is-small" :loading="checkingNivaro" :disabled="checkingNivaro || nivaroUpdating" @click="checkNivaroUpdates">
							<i class="mdi mdi-refresh mr-1"></i>
							{{ $t('Check for updates') }}
						</b-button>
						<b-button v-if="hasNivaroUpdate" data-test="nivaro-update" rounded size="is-small" type="is-primary" :loading="nivaroUpdating" @click="startNivaroUpdate">
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Update NivaroOS') }}
						</b-button>
						<span v-else-if="updateState === 'up_to_date'" class="tag is-success is-light is-rounded">
							<i class="mdi mdi-check mr-1"></i>
							{{ $t('Up to date') }}
						</span>
					</div>
				</div>
			</div>

			<!-- The newest change on master this build doesn't have -->
			<div v-if="hasNivaroUpdate && update.latest_message" class="update-inset-box">
				<div class="is-flex is-align-items-center is-justify-content-between mb-1">
					<a class="commit-label is-flex is-align-items-center" :href="compareUrl" target="_blank" rel="noopener noreferrer">
						<i class="mdi mdi-github mr-1"></i>
						{{ $t('Latest on GitHub') }} (<code>{{ (update.latest_commit || '').substring(0, 7) }}</code>)
					</a>
					<span class="commit-time">{{ formatDate(update.latest_date) }}</span>
				</div>
				<div class="commit-msg" data-test="nivaro-latest">{{ update.latest_message }}</div>
			</div>
		</div>

		<!-- ==================== ANDROID APP (GitHub releases) ==================== -->
		<!-- GitHub releases are the Android app's, never the server's version. -->
		<template v-if="androidRelease">
			<h3 class="setting-card-title">{{ $t('Android app') }}</h3>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="android" pack="mdi" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title" data-test="android-release">{{ $t('Android app') }} {{ androidRelease.version }}</div>
						<div class="setting-desc">{{ $t('Latest release of the NivaroOS phone app') }}</div>
					</div>
					<div class="row-control">
						<b-button rounded size="is-small" tag="a" :href="androidRelease.url" target="_blank" rel="noopener noreferrer">
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Download APK') }}
						</b-button>
					</div>
				</div>
				<div v-if="androidRelease.notes" class="update-inset-box mt-0">
					<div class="release-notes-head is-flex is-align-items-center is-justify-content-between" @click="showChangelog = !showChangelog">
						<span class="is-size-7 text-muted">{{ $t('Release Highlights & Changelog') }}</span>
						<i :class="showChangelog ? 'mdi mdi-chevron-up' : 'mdi mdi-chevron-down'"></i>
					</div>
					<div v-if="showChangelog" class="release-notes-body">
						<pre class="notes-pre">{{ androidRelease.notes }}</pre>
					</div>
				</div>
			</div>
		</template>

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
			<div v-if="filteredPackages.length > 0" class="package-list-wrapper" tabindex="0" role="region" :aria-label="$t('Linux System Packages (APT)')">
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
import { apiError } from '@/utils/apiError'
import axios from 'axios'

// Search index: labels are the titles this section renders (the search
// jumps to them); keywords are other words people type for them.
export const ROWS = [
	{ label: 'NivaroOS System Update', keywords: 'nivaroos version update build commit' },
	{ label: 'Android app', keywords: 'apk phone mobile release download' },
	{ label: 'Linux System Packages (APT)', keywords: 'apt upgrade packages security' }
]

export default {
	name: 'updates-section',
	data() {
		return {
			build: {},
			update: {},
			androidRelease: null,
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
		// Only core's own answer: master is ahead of this build.
		updateState() {
			return this.update.state || ''
		},
		hasNivaroUpdate() {
			return this.updateState === 'behind'
		},
		statusIcon() {
			return { behind: 'cloud-download-outline', up_to_date: 'check-circle-outline', unknown: 'help-circle-outline' }[this.updateState] || 'help-circle-outline'
		},
		statusTitle() {
			if (this.hasNivaroUpdate && this.update.latest_version) return `NivaroOS ${this.update.latest_version} ${this.$t('is available')}`
			if (this.hasNivaroUpdate) {
				const n = this.update.ahead_by
				return `${n} ${n === 1 ? this.$t('new change on GitHub since your build') : this.$t('new changes on GitHub since your build')}`
			}
			if (this.updateState === 'up_to_date') return this.$t('NivaroOS is up to date')
			if (this.updateState === 'unknown') return this.$t("Couldn't check for NivaroOS updates")
			return this.$t('Checking for NivaroOS updates...')
		},
		buildLabel() {
			const b = this.build
			if (!b.short) return this.$t('Build') + ': ' + (b.version || '—')
			let d = ''
			try {
				if (b.date) d = ', ' + new Date(b.date).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
			} catch { /* bad date: no date */ }
			const label = `${this.$t('Build')} ${b.short}${d}`
			return b.release ? `NivaroOS ${b.version} (${label})` : label
		},
		compareUrl() {
			return `https://github.com/F-e-n-y-x/NivaroOS/compare/${this.build.commit}...master`
		},
		filteredPackages() {
			if (!this.pkgSearch.trim()) return this.packages
			const q = this.pkgSearch.toLowerCase().trim()
			return this.packages.filter(p => p.name.toLowerCase().includes(q) || (p.suite && p.suite.toLowerCase().includes(q)))
		}
	},
	created() {
		this.loadNivaroVersion()
		this.loadAndroidRelease()
		this.loadAptPackages()
		this.checkUpgradeStatus()
	},
	beforeUnmount() {
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
		loadNivaroVersion(refresh = false) {
			this.checkingNivaro = true
			return this.$api.sys.getVersion(refresh).then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.build = data.build || {}
					this.update = data.update || { state: 'unknown' }
				}
			}).catch(() => {
				this.update = { state: 'unknown', reason: this.$t('the server did not answer') }
			}).finally(() => {
				this.checkingNivaro = false
				this.lastNivaroCheckTime = new Date().toLocaleTimeString()
			})
		},
		// The Android app's latest GitHub release: only shown, never compared
		// with the server.
		async loadAndroidRelease() {
			try {
				// The newest release with an APK; server-v tags are the server's.
				const { data: list } = await axios.get('https://api.github.com/repos/F-e-n-y-x/NivaroOS/releases', { params: { per_page: 30 } })
				const apkOf = r => (r.assets || []).find(a => /\.apk$/i.test(a.name || ''))
				const data = (list || []).find(r => !r.draft && !r.prerelease && !(r.tag_name || '').startsWith('server-') && apkOf(r))
				if (!data) return
				const apk = apkOf(data)
				this.androidRelease = {
					version: String(data.tag_name || data.name || '').replace(/^v/i, ''),
					url: apk.browser_download_url,
					notes: data.body || ''
				}
			} catch {
				// No release yet, offline or rate limited: no card.
			}
		},
		checkNivaroUpdates() {
			this.loadNivaroVersion(true)
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
					message: this.$t('Failed to refresh package updates: ') + apiError(err),
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
		color: var(--color-success-fg);
	}

	&.is-update-available {
		background: rgba(59, 130, 246, 0.12);
		color: var(--color-primary-fg);
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
	color: var(--color-primary-fg);

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
		color: var(--color-success-fg);
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
