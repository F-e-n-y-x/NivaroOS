<template>
	<section class="settings-section">
		<div class="is-flex is-align-items-center is-justify-content-between mb-4">
			<h2 class="section-title mb-0">{{ $t('Package Manager (APT)') }}</h2>
			<div class="buttons are-small mb-0">
				<b-button rounded size="is-small" :loading="updatingRepos" @click="updateRepositories">
					<i class="mdi mdi-refresh mr-1"></i>{{ $t('Update Repositories') }}
				</b-button>
			</div>
		</div>

		<!-- Navigation Tabs -->
		<div class="segmented-control">
			<button
				v-for="tab in tabs"
				:key="tab.id"
				type="button"
				class="segmented-option"
				:class="{ active: activeTab === tab.id }"
				@click="activeTab = tab.id"
			>
				<i :class="'mdi ' + tab.icon + ' mr-1'"></i>{{ $t(tab.label) }}
				<span v-if="tab.id === 'upgrades' && upgradable.length" class="tab-badge">{{ upgradable.length }}</span>
			</button>
		</div>

		<!-- ==================== TAB 1: SEARCH & INSTALL ==================== -->
		<div v-if="activeTab === 'search'">
			<!-- Quick Install Box -->
			<div class="setting-card mb-4">
				<div class="setting-row">
					<b-icon class="row-icon" icon="download-box-outline" pack="mdi" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ $t('Quick Install by Package Name') }}</div>
						<div class="setting-desc">{{ $t('Install any Linux APT package directly by name') }}</div>
					</div>
					<div class="row-control">
						<div class="is-flex is-align-items-center">
							<b-input
								v-model="quickInstallName"
								size="is-small"
								placeholder="e.g. htop, neofetch, git"
								class="quick-install-input mr-2"
								@keyup.enter.native="installPackage(quickInstallName)"
							></b-input>
							<b-button
								rounded
								size="is-small"
								type="is-primary"
								:disabled="!quickInstallName.trim()"
								:loading="processingPkg === quickInstallName.trim()"
								@click="installPackage(quickInstallName)"
							>
								<i class="mdi mdi-plus mr-1"></i>{{ $t('Install') }}
							</b-button>
						</div>
					</div>
				</div>
			</div>

			<!-- Search Bar -->
			<div class="search-wrap mb-3">
				<b-icon class="search-icon" icon="magnify" pack="mdi" size="is-20"></b-icon>
				<input
					v-model="searchQuery"
					type="text"
					class="search-input"
					:placeholder="$t('Search APT packages (e.g. nginx, python3, ffmpeg, curl)...')"
					@input="onSearchInput"
				/>
				<button v-if="searchQuery" class="search-clear" type="button" @click="searchQuery = ''; searchResults = []">
					<b-icon icon="close" pack="mdi" size="is-16"></b-icon>
				</button>
			</div>

			<div v-if="searching" class="p-5 has-text-centered text-muted">
				<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				<div class="mt-2 is-size-7">{{ $t('Searching package repositories...') }}</div>
			</div>

			<div v-else-if="searchQuery && !searchResults.length" class="empty-state">
				<i class="mdi mdi-package-variant is-size-3 text-muted"></i>
				<div class="mt-2 text-muted is-size-7">{{ $t('No packages found matching') }} "{{ searchQuery }}"</div>
			</div>

			<div v-else-if="searchResults.length" class="setting-card">
				<div class="pkg-list">
					<div v-for="pkg in searchResults" :key="pkg.name" class="pkg-row">
						<div class="pkg-main">
							<div class="pkg-name-row">
								<span class="pkg-name">{{ pkg.name }}</span>
								<span v-if="pkg.installed" class="badge-installed">
									<i class="mdi mdi-check-circle mr-1"></i>{{ $t('Installed') }}
									<span v-if="pkg.version">({{ pkg.version }})</span>
								</span>
							</div>
							<div class="pkg-desc">{{ pkg.description }}</div>
						</div>
						<div class="pkg-actions">
							<template v-if="pkg.installed">
								<b-button
									rounded
									size="is-small"
									type="is-danger"
									outlined
									:loading="processingPkg === pkg.name"
									@click="confirmUninstall(pkg.name)"
								>
									<i class="mdi mdi-trash-can-outline mr-1"></i>{{ $t('Uninstall') }}
								</b-button>
								<b-button
									rounded
									size="is-small"
									class="ml-2"
									:loading="processingPkg === pkg.name"
									@click="installPackage(pkg.name, true)"
								>
									<i class="mdi mdi-refresh mr-1"></i>{{ $t('Reinstall') }}
								</b-button>
							</template>
							<template v-else>
								<b-button
									rounded
									size="is-small"
									type="is-primary"
									:loading="processingPkg === pkg.name"
									@click="installPackage(pkg.name)"
								>
									<i class="mdi mdi-download mr-1"></i>{{ $t('Install') }}
								</b-button>
							</template>
						</div>
					</div>
				</div>
			</div>

			<div v-else class="empty-state">
				<i class="mdi mdi-package-variant-closed is-size-2 text-muted"></i>
				<div class="mt-2 text-muted is-size-7">{{ $t('Type a package name above to search or quick-install.') }}</div>
			</div>
		</div>

		<!-- ==================== TAB 2: INSTALLED PACKAGES ==================== -->
		<div v-else-if="activeTab === 'installed'">
			<div class="search-wrap mb-3">
				<b-icon class="search-icon" icon="magnify" pack="mdi" size="is-20"></b-icon>
				<input
					v-model="installedQuery"
					type="text"
					class="search-input"
					:placeholder="$t('Filter installed packages...')"
					@input="fetchInstalled"
				/>
				<button v-if="installedQuery" class="search-clear" type="button" @click="installedQuery = ''; fetchInstalled()">
					<b-icon icon="close" pack="mdi" size="is-16"></b-icon>
				</button>
			</div>

			<div class="setting-card">
				<div v-if="loadingInstalled" class="p-5 has-text-centered text-muted">
					<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				</div>
				<div v-else-if="!installedList.length" class="empty-state">
					<div class="text-muted is-size-7">{{ $t('No installed packages found.') }}</div>
				</div>
				<div v-else class="pkg-list">
					<div v-for="pkg in installedList" :key="pkg.name" class="pkg-row">
						<div class="pkg-main">
							<div class="pkg-name-row">
								<span class="pkg-name">{{ pkg.name }}</span>
								<span class="pkg-version">{{ pkg.version }}</span>
								<span v-if="pkg.size" class="pkg-size">{{ formatBytes(pkg.size) }}</span>
								<span v-if="pkg.section" class="pkg-section">{{ pkg.section }}</span>
							</div>
							<div class="pkg-desc">{{ pkg.description }}</div>
						</div>
						<div class="pkg-actions">
							<b-button
								rounded
								size="is-small"
								type="is-danger"
								outlined
								:loading="processingPkg === pkg.name"
								@click="confirmUninstall(pkg.name)"
							>
								<i class="mdi mdi-trash-can-outline mr-1"></i>{{ $t('Uninstall') }}
							</b-button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== TAB 3: UPGRADES ==================== -->
		<div v-else-if="activeTab === 'upgrades'">
			<div class="is-flex is-align-items-center is-justify-content-between mb-3">
				<span class="text-muted is-size-7">
					{{ upgradable.length }} {{ $t('packages ready for upgrade') }}
				</span>
				<b-button
					v-if="upgradable.length"
					rounded
					size="is-small"
					type="is-primary"
					:loading="upgradingAll"
					@click="upgradeAllPackages"
				>
					<i class="mdi mdi-arrow-up-bold-circle-outline mr-1"></i>{{ $t('Upgrade All') }}
				</b-button>
			</div>

			<div class="setting-card">
				<div v-if="loadingUpgrades" class="p-5 has-text-centered text-muted">
					<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				</div>
				<div v-else-if="!upgradable.length" class="empty-state">
					<i class="mdi mdi-check-circle is-size-2 text-success"></i>
					<div class="mt-2 text-muted is-size-7">{{ $t('All APT system packages are up to date.') }}</div>
				</div>
				<div v-else class="pkg-list">
					<div v-for="pkg in upgradable" :key="pkg.name" class="pkg-row">
						<div class="pkg-main">
							<div class="pkg-name-row">
								<span class="pkg-name">{{ pkg.name }}</span>
								<span class="pkg-arch">{{ pkg.arch }}</span>
							</div>
							<div class="upgrade-meta">
								<span class="old-ver">{{ pkg.current_version }}</span>
								<i class="mdi mdi-arrow-right mx-2 text-muted"></i>
								<span class="new-ver">{{ pkg.candidate_version }}</span>
							</div>
						</div>
						<div class="pkg-actions">
							<b-button
								rounded
								size="is-small"
								type="is-primary"
								outlined
								:loading="processingPkg === pkg.name"
								@click="upgradeSingle(pkg.name)"
							>
								{{ $t('Upgrade') }}
							</b-button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== TAB 4: REPOSITORY SOURCES ==================== -->
		<div v-else-if="activeTab === 'sources'">
			<div class="is-flex is-align-items-center is-justify-content-between mb-3">
				<span class="text-muted is-size-7">{{ $t('Configure /etc/apt/sources.list repositories') }}</span>
				<b-button rounded size="is-small" type="is-dark" @click="showAddSourceModal = true">
					<i class="mdi mdi-plus mr-1"></i>{{ $t('Add Source') }}
				</b-button>
			</div>

			<div class="setting-card">
				<div v-if="loadingSources" class="p-5 has-text-centered text-muted">
					<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				</div>
				<div v-else-if="!sourcesList.length" class="empty-state">
					<div class="text-muted is-size-7">{{ $t('No repository sources configured.') }}</div>
				</div>
				<div v-else class="sources-list">
					<div v-for="(s, idx) in sourcesList" :key="s.file + s.line + idx" class="source-row">
						<div class="source-main">
							<div class="source-header">
								<span class="source-type" :class="s.type">{{ s.type }}</span>
								<span class="source-uri">{{ s.uri }}</span>
								<span class="source-suite">{{ s.suite }}</span>
								<span class="source-file">{{ getFilename(s.file) }}:{{ s.line }}</span>
							</div>
							<div class="source-components">
								<span v-for="c in s.components" :key="c" class="comp-chip">{{ c }}</span>
							</div>
						</div>
						<div class="source-actions">
							<b-button
								rounded
								size="is-small"
								type="is-danger"
								outlined
								@click="deleteSource(s)"
							>
								<i class="mdi mdi-trash-can-outline"></i>
							</b-button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Add Source Modal -->
		<b-modal v-model="showAddSourceModal" :width="520" scroll="clip">
			<div class="modal-card">
				<header class="modal-card-head">
					<p class="modal-card-title">{{ $t('Add APT Repository Source') }}</p>
					<button type="button" class="delete" @click="showAddSourceModal = false"></button>
				</header>
				<section class="modal-card-body">
					<b-field :label="$t('Source Line (e.g. deb http://deb.debian.org/debian trixie main)')">
						<b-input
							v-model="newSourceLine"
							placeholder="deb https://... trixie main contrib non-free"
						></b-input>
					</b-field>
					<b-field :label="$t('List File Name')">
						<b-input
							v-model="newSourceFile"
							placeholder="custom.list"
						></b-input>
					</b-field>
				</section>
				<footer class="modal-card-foot is-justify-content-flex-end">
					<b-button rounded size="is-small" @click="showAddSourceModal = false">{{ $t('Cancel') }}</b-button>
					<b-button
						rounded
						size="is-small"
						type="is-primary"
						:disabled="!newSourceLine.trim()"
						:loading="addingSource"
						@click="submitAddSource"
					>
						{{ $t('Add Repository') }}
					</b-button>
				</footer>
			</div>
		</b-modal>

		<!-- Operation Output Log Modal -->
		<b-modal v-model="showLogModal" :width="640" scroll="clip">
			<div class="modal-card">
				<header class="modal-card-head">
					<p class="modal-card-title">{{ logTitle }}</p>
					<button type="button" class="delete" @click="showLogModal = false"></button>
				</header>
				<section class="modal-card-body p-0">
					<pre class="terminal-output">{{ logContent }}</pre>
				</section>
				<footer class="modal-card-foot is-justify-content-flex-end">
					<b-button rounded size="is-small" type="is-primary" @click="showLogModal = false">{{ $t('Done') }}</b-button>
				</footer>
			</div>
		</b-modal>
	</section>
</template>

<script>
import debounce from 'lodash/debounce'

export const ROWS = [
	{ label: 'Search & Install Packages' },
	{ label: 'Installed Packages' },
	{ label: 'Upgradable Packages' },
	{ label: 'Repository Sources' }
]

export default {
	name: 'packages-section',
	data() {
		return {
			activeTab: 'search',
			tabs: [
				{ id: 'search', label: 'Search & Install', icon: 'mdi-magnify' },
				{ id: 'installed', label: 'Installed', icon: 'mdi-package-variant-closed' },
				{ id: 'upgrades', label: 'Upgrades', icon: 'mdi-arrow-up-bold-circle-outline' },
				{ id: 'sources', label: 'Repositories', icon: 'mdi-server-network' }
			],
			searchQuery: '',
			searching: false,
			searchResults: [],
			quickInstallName: '',
			processingPkg: null,

			installedQuery: '',
			loadingInstalled: false,
			installedList: [],

			loadingUpgrades: false,
			upgradable: [],
			upgradingAll: false,

			loadingSources: false,
			sourcesList: [],
			showAddSourceModal: false,
			newSourceLine: '',
			newSourceFile: 'custom.list',
			addingSource: false,
			updatingRepos: false,

			showLogModal: false,
			logTitle: '',
			logContent: ''
		}
	},
	created() {
		this.fetchUpgrades()
		this.fetchSources()
		this.fetchInstalled()
	},
	methods: {
		formatBytes(bytes) {
			if (!bytes) return ''
			const units = ['B', 'KB', 'MB', 'GB', 'TB']
			let i = 0
			let size = bytes
			while (size >= 1024 && i < units.length - 1) {
				size /= 1024
				i++
			}
			return `${size.toFixed(1)} ${units[i]}`
		},
		getFilename(path) {
			if (!path) return ''
			const parts = path.split('/')
			return parts[parts.length - 1]
		},
		onSearchInput: debounce(function () {
			const q = this.searchQuery.trim()
			if (!q) {
				this.searchResults = []
				return
			}
			this.searching = true
			this.$api.sys.searchAptPackages(q)
				.then(res => {
					this.searching = false
					if (res.data.success === 200) {
						this.searchResults = res.data.data || []
					}
				})
				.catch(() => {
					this.searching = false
				})
		}, 300),

		fetchInstalled: debounce(function () {
			this.loadingInstalled = true
			this.$api.sys.getInstalledAptPackages(this.installedQuery)
				.then(res => {
					this.loadingInstalled = false
					if (res.data.success === 200) {
						this.installedList = res.data.data || []
					}
				})
				.catch(() => {
					this.loadingInstalled = false
				})
		}, 250),

		fetchUpgrades() {
			this.loadingUpgrades = true
			this.$api.sys.getUpgradableAptPackages()
				.then(res => {
					this.loadingUpgrades = false
					if (res.data.success === 200) {
						this.upgradable = res.data.data || []
					}
				})
				.catch(() => {
					this.loadingUpgrades = false
				})
		},

		fetchSources() {
			this.loadingSources = true
			this.$api.sys.getAptSources()
				.then(res => {
					this.loadingSources = false
					if (res.data.success === 200) {
						this.sourcesList = res.data.data || []
					}
				})
				.catch(() => {
					this.loadingSources = false
				})
		},

		updateRepositories() {
			this.updatingRepos = true
			this.$api.sys.updateAptRepositories()
				.then(res => {
					this.updatingRepos = false
					this.$buefy.toast.open({ message: this.$t('Repositories updated successfully'), type: 'is-success' })
					this.fetchUpgrades()
					if (res.data.data && res.data.data.output) {
						this.logTitle = this.$t('Repository Update Log')
						this.logContent = res.data.data.output
						this.showLogModal = true
					}
				})
				.catch(err => {
					this.updatingRepos = false
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Failed to update repositories')
					this.logTitle = this.$t('Update Error')
					this.logContent = msg
					this.showLogModal = true
				})
		},

		installPackage(name, reinstall = false) {
			const pkgName = (name || '').trim()
			if (!pkgName) return
			this.processingPkg = pkgName
			this.$api.sys.installAptPackages([pkgName], reinstall)
				.then(res => {
					this.processingPkg = null
					this.quickInstallName = ''
					this.$buefy.toast.open({ message: this.$t('Package installed successfully'), type: 'is-success' })
					this.onSearchInput()
					this.fetchInstalled()
					this.fetchUpgrades()
					if (res.data.data && res.data.data.output) {
						this.logTitle = `${this.$t('Install')}: ${pkgName}`
						this.logContent = res.data.data.output
						this.showLogModal = true
					}
				})
				.catch(err => {
					this.processingPkg = null
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Installation failed')
					this.logTitle = `${this.$t('Install Failed')}: ${pkgName}`
					this.logContent = msg
					this.showLogModal = true
				})
		},

		confirmUninstall(name) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Uninstall Package'),
				message: this.$t('Are you sure you want to uninstall <b>{name}</b>?', { name }),
				type: 'is-danger',
				confirmText: this.$t('Uninstall'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.processingPkg = name
					this.$api.sys.uninstallAptPackages([name])
						.then(res => {
							this.processingPkg = null
							this.$buefy.toast.open({ message: this.$t('Package uninstalled successfully'), type: 'is-success' })
							this.onSearchInput()
							this.fetchInstalled()
							if (res.data.data && res.data.data.output) {
								this.logTitle = `${this.$t('Uninstall')}: ${name}`
								this.logContent = res.data.data.output
								this.showLogModal = true
							}
						})
						.catch(err => {
							this.processingPkg = null
							const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Uninstall failed')
							this.logTitle = `${this.$t('Uninstall Failed')}: ${name}`
							this.logContent = msg
							this.showLogModal = true
						})
				}
			})
		},

		upgradeSingle(name) {
			this.processingPkg = name
			this.$api.sys.upgradeAptPackages([name])
				.then(res => {
					this.processingPkg = null
					this.$buefy.toast.open({ message: this.$t('Package upgraded successfully'), type: 'is-success' })
					this.fetchUpgrades()
					this.fetchInstalled()
					if (res.data.data && res.data.data.output) {
						this.logTitle = `${this.$t('Upgrade')}: ${name}`
						this.logContent = res.data.data.output
						this.showLogModal = true
					}
				})
				.catch(err => {
					this.processingPkg = null
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Upgrade failed')
					this.logTitle = `${this.$t('Upgrade Failed')}: ${name}`
					this.logContent = msg
					this.showLogModal = true
				})
		},

		upgradeAllPackages() {
			this.upgradingAll = true
			this.$api.sys.upgradeAptPackages([])
				.then(res => {
					this.upgradingAll = false
					this.$buefy.toast.open({ message: this.$t('All packages upgraded successfully'), type: 'is-success' })
					this.fetchUpgrades()
					this.fetchInstalled()
					if (res.data.data && res.data.data.output) {
						this.logTitle = this.$t('System Upgrade Log')
						this.logContent = res.data.data.output
						this.showLogModal = true
					}
				})
				.catch(err => {
					this.upgradingAll = false
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Upgrade failed')
					this.logTitle = this.$t('Upgrade Failed')
					this.logContent = msg
					this.showLogModal = true
				})
		},

		submitAddSource() {
			if (!this.newSourceLine.trim()) return
			this.addingSource = true
			this.$api.sys.addAptSource(this.newSourceLine, this.newSourceFile)
				.then(() => {
					this.addingSource = false
					this.showAddSourceModal = false
					this.newSourceLine = ''
					this.$buefy.toast.open({ message: this.$t('Repository added successfully'), type: 'is-success' })
					this.fetchSources()
				})
				.catch(err => {
					this.addingSource = false
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Failed to add repository')
					this.$buefy.toast.open({ message: msg, type: 'is-danger' })
				})
		},

		deleteSource(source) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove Repository Source'),
				message: this.$t('Remove source line from <b>{file}</b>?', { file: source.file }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.sys.deleteAptSource(source.file, source.line)
						.then(() => {
							this.$buefy.toast.open({ message: this.$t('Source removed'), type: 'is-success' })
							this.fetchSources()
						})
						.catch(() => {
							this.$buefy.toast.open({ message: this.$t('Failed to remove source'), type: 'is-danger' })
						})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.tab-badge {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	margin-left: 0.35rem;
	padding: 0.1rem 0.45rem;
	border-radius: 999px;
	background: #ef4444;
	color: #ffffff;
	font-size: 0.6875rem;
	font-weight: 700;
	line-height: 1;
}

.search-wrap {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	background: #f8fafc;
	border-radius: 12px;
	border: 1px solid #e2e8f0;
	padding: 0.65rem 1rem;
	transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;

	&:focus-within {
		border-color: #2563eb;
		background: #ffffff;
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
	}
}

.search-icon {
	color: #94a3b8;
	flex-shrink: 0;
}

.search-input {
	flex: 1;
	min-width: 0;
	border: none;
	outline: none;
	background: transparent;
	font-family: inherit;
	font-size: 0.875rem;
	font-weight: 400;
	color: #1e293b;

	&::placeholder {
		color: #94a3b8;
	}
}

.search-clear {
	border: none;
	background: rgba(0, 0, 0, 0.05);
	color: #64748b;
	border-radius: 50%;
	width: 1.35rem;
	height: 1.35rem;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}

.quick-install-input {
	width: 14rem;
}

.empty-state {
	padding: 3rem 1.5rem;
	text-align: center;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
}

.pkg-list {
	display: flex;
	flex-direction: column;
}

.pkg-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 1rem;
	padding: 0.95rem 1.25rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
	transition: background 0.12s ease;

	&:hover {
		background: rgba(0, 0, 0, 0.015);
	}

	&:last-child {
		border-bottom: none;
	}
}

.pkg-main {
	flex: 1;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
}

.pkg-name-row {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-wrap: wrap;
}

.pkg-name {
	font-size: 0.875rem;
	font-weight: 600;
	color: #1e293b;
	font-family: $family-monospace;
}

.pkg-version {
	font-size: 0.75rem;
	font-weight: 500;
	color: #64748b;
	font-family: $family-monospace;
	background: #f1f5f9;
	padding: 0.15rem 0.45rem;
	border-radius: 4px;
}

.pkg-size,
.pkg-section,
.pkg-arch {
	font-size: 0.725rem;
	color: #94a3b8;
	background: rgba(0, 0, 0, 0.03);
	padding: 0.1rem 0.4rem;
	border-radius: 4px;
}

.badge-installed {
	display: inline-flex;
	align-items: center;
	padding: 0.15rem 0.5rem;
	border-radius: 999px;
	background: rgba(16, 185, 129, 0.12);
	color: #059669;
	font-size: 0.725rem;
	font-weight: 600;
}

.pkg-desc {
	font-size: 0.775rem;
	font-weight: 400;
	color: #64748b;
	line-height: 1.35;
}

.pkg-actions {
	display: flex;
	align-items: center;
	flex-shrink: 0;
}

.upgrade-meta {
	display: flex;
	align-items: center;
	font-size: 0.775rem;
	font-family: $family-monospace;

	.old-ver {
		color: #94a3b8;
		text-decoration: line-through;
	}

	.new-ver {
		color: #10b981;
		font-weight: 600;
	}
}

.sources-list {
	display: flex;
	flex-direction: column;
}

.source-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 1rem;
	padding: 0.95rem 1.25rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);

	&:last-child {
		border-bottom: none;
	}
}

.source-main {
	flex: 1;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}

.source-header {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-wrap: wrap;
	font-size: 0.8125rem;
	font-family: $family-monospace;
}

.source-type {
	font-weight: 700;
	padding: 0.1rem 0.4rem;
	border-radius: 4px;
	background: #f1f5f9;
	color: #1e293b;
	font-size: 0.725rem;

	&.deb-src {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
	}
}

.source-uri {
	font-weight: 600;
	color: #2563eb;
}

.source-suite {
	font-weight: 600;
	color: #1e293b;
}

.source-file {
	color: #94a3b8;
	font-size: 0.725rem;
	margin-left: auto;
}

.source-components {
	display: flex;
	gap: 0.3rem;
	flex-wrap: wrap;
}

.comp-chip {
	padding: 0.1rem 0.45rem;
	border-radius: 4px;
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	color: #64748b;
	font-size: 0.7rem;
	font-family: $family-monospace;
}

.terminal-output {
	background: #0f172a;
	color: #f8fafc;
	padding: 1rem;
	font-family: $family-monospace;
	font-size: 0.75rem;
	line-height: 1.45;
	max-height: 22rem;
	overflow-y: auto;
	white-space: pre-wrap;
	word-break: break-word;
	border-radius: 0;
}
</style>
