<template>
	<section class="settings-section">
		<div class="section-header">
			<h2 class="section-title">{{ $t('Package Manager (APT)') }}</h2>
			<div class="header-actions">
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
				<i class="mdi mdi-cube-outline is-size-3 text-muted"></i>
				<div class="mt-2 text-muted is-size-7">{{ $t('No packages found matching') }} "{{ searchQuery }}"</div>
			</div>

			<div v-else-if="searchResults.length" class="setting-card">
				<div v-for="pkg in searchResults" :key="pkg.name" class="setting-row">
					<b-icon class="row-icon" icon="cube-outline" pack="mdi" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title is-flex is-align-items-center">
							<span>{{ pkg.name }}</span>
							<span v-if="pkg.installed" class="tag is-success is-light is-rounded is-size-7 ml-2">
								<i class="mdi mdi-check mr-1"></i>{{ $t('Installed') }}
							</span>
						</div>
						<div class="setting-desc">{{ pkg.description || $t('No description available') }}</div>
					</div>
					<div class="row-control">
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

			<div v-else class="empty-state">
				<i class="mdi mdi-cube-outline is-size-2 text-muted"></i>
				<div class="mt-2 text-muted is-size-7">{{ $t('Type a package name above to search.') }}</div>
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
				<div v-else-if="!installedList.length" class="account-empty">
					{{ $t('No installed packages found.') }}
				</div>
				<div v-else>
					<div v-for="pkg in installedList" :key="pkg.name" class="setting-row">
						<b-icon class="row-icon" icon="cube-outline" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ pkg.name }}</div>
							<div class="setting-desc">{{ pkg.version }} &middot; {{ formatBytes(pkg.size) }} &middot; {{ pkg.description }}</div>
						</div>
						<div class="row-control">
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
				<div v-else-if="!upgradable.length" class="account-empty">
					{{ $t('All APT system packages are up to date.') }}
				</div>
				<div v-else>
					<div v-for="pkg in upgradable" :key="pkg.name" class="setting-row">
						<b-icon class="row-icon" icon="package-up" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ pkg.name }}</div>
							<div class="setting-desc">{{ pkg.current_version }} &rarr; <span class="has-text-success">{{ pkg.candidate_version }}</span> ({{ pkg.arch }})</div>
						</div>
						<div class="row-control">
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
				<span class="text-muted is-size-7">{{ $t('Manage where this server downloads software updates from') }}</span>
				<b-button rounded size="is-small" type="is-dark" @click="showAddSourceModal = true">
					<i class="mdi mdi-plus mr-1"></i>{{ $t('Add Source') }}
				</b-button>
			</div>

			<div class="setting-card">
				<div v-if="loadingSources" class="p-5 has-text-centered text-muted">
					<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				</div>
				<div v-else-if="!sourcesList.length" class="account-empty">
					{{ $t('No repository sources configured.') }}
				</div>
				<div v-else>
					<div v-for="(s, idx) in sourcesList" :key="s.file + s.line + idx" class="setting-row">
						<b-icon class="row-icon" icon="layers-triple-outline" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">
								<span class="setting-chip mr-2" :class="s.type">{{ s.type }}</span>
								{{ s.uri }}
							</div>
							<div class="setting-desc">{{ s.suite }} &middot; {{ (s.components || []).join(', ') }} &middot; {{ getFilename(s.file) }}:{{ s.line }}</div>
						</div>
						<div class="row-control">
							<button class="icon-button" type="button" :title="$t('Delete source')" @click="deleteSource(s)">
								<b-icon icon="trash-can-outline" pack="mdi" size="is-16"></b-icon>
							</button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Add Source Overlay -->
		<settings-overlay
			:active="showAddSourceModal"
			:title="$t('Add APT Repository Source')"
			width="34rem"
			@close="showAddSourceModal = false"
		>
			<div class="add-source-body">
				<!-- Input Mode Toggle -->
				<div class="segmented-control mb-3">
					<button
						type="button"
						class="segmented-option"
						:class="{ active: sourceInputMode === 'structured' }"
						@click="sourceInputMode = 'structured'"
					>
						<i class="mdi mdi-form-select mr-1"></i>{{ $t('Structured Builder') }}
					</button>
					<button
						type="button"
						class="segmented-option"
						:class="{ active: sourceInputMode === 'raw' }"
						@click="sourceInputMode = 'raw'"
					>
						<i class="mdi mdi-code-tags mr-1"></i>{{ $t('Raw APT Line') }}
					</button>
				</div>

				<div v-if="sourceInputMode === 'structured'" class="source-builder-grid">
					<!-- Type -->
					<div class="form-field-group">
						<label class="form-label">{{ $t('Package Type') }}</label>
						<div class="segmented-control is-inline">
							<button
								type="button"
								class="segmented-option"
								:class="{ active: sourceType === 'deb' }"
								@click="sourceType = 'deb'"
							>
								{{ $t('Binary (deb)') }}
							</button>
							<button
								type="button"
								class="segmented-option"
								:class="{ active: sourceType === 'deb-src' }"
								@click="sourceType = 'deb-src'"
							>
								{{ $t('Source (deb-src)') }}
							</button>
						</div>
					</div>

					<!-- URL -->
					<div class="form-field-group">
						<label class="form-label">{{ $t('Repository URL / Mirror') }}</label>
						<b-input
							v-model="sourceUri"
							placeholder="https://deb.debian.org/debian"
							icon="server-network"
							pack="mdi"
							size="is-small"
						></b-input>
					</div>

					<!-- Suite / Distro -->
					<div class="form-field-group">
						<label class="form-label">{{ $t('Distribution / Suite') }}</label>
						<b-input
							v-model="sourceSuite"
							placeholder="e.g. trixie, bookworm, stable"
							size="is-small"
						></b-input>
					</div>

					<!-- Components -->
					<div class="form-field-group">
						<label class="form-label">{{ $t('Repository Components') }}</label>
						<div class="component-chips-wrap mb-2">
							<button
								v-for="c in availableComponents"
								:key="c"
								type="button"
								class="comp-toggle-chip"
								:class="{ selected: sourceSelectedComponents.includes(c) }"
								@click="toggleComponent(c)"
							>
								<i v-if="sourceSelectedComponents.includes(c)" class="mdi mdi-check mr-1"></i>
								{{ c }}
							</button>
						</div>
						<b-input
							v-model="customComponentsInput"
							placeholder="Additional components (optional, space-separated)"
							size="is-small"
						></b-input>
					</div>
				</div>

				<div v-else class="form-field-group">
					<label class="form-label">{{ $t('Complete APT Source Line') }}</label>
					<b-input
						v-model="newSourceLine"
						type="textarea"
						rows="2"
						placeholder="deb [signed-by=/path/to/key.gpg] https://download.docker.com/linux/debian bookworm stable"
					></b-input>
				</div>

				<!-- Target List File -->
				<div class="form-field-group mt-3">
					<label class="form-label">{{ $t('Save to File (/etc/apt/sources.list.d/)') }}</label>
					<b-input
						v-model="newSourceFile"
						placeholder="custom.list"
						size="is-small"
					></b-input>
				</div>

				<!-- Live Preview -->
				<div v-if="finalSourceLine" class="live-preview-box mt-3">
					<div class="preview-label">
						<i class="mdi mdi-eye-outline mr-1"></i>{{ $t('Generated Line Preview') }}
					</div>
					<code class="preview-code">{{ finalSourceLine }}</code>
				</div>
			</div>

			<template #footer>
				<b-button rounded size="is-small" @click="showAddSourceModal = false">{{ $t('Cancel') }}</b-button>
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					:disabled="!finalSourceLine"
					:loading="addingSource"
					@click="submitAddSource"
				>
					{{ $t('Add Repository') }}
				</b-button>
			</template>
		</settings-overlay>

		<!-- Operation Output Log Overlay -->
		<settings-overlay
			:active="showLogModal"
			:title="logTitle"
			width="38rem"
			body-class="p-0"
			@close="showLogModal = false"
		>
			<pre class="terminal-output">{{ logContent }}</pre>

			<template #footer>
				<b-button rounded size="is-small" type="is-primary" @click="showLogModal = false">{{ $t('Done') }}</b-button>
			</template>
		</settings-overlay>

		<!-- Uninstall Confirm Overlay -->
		<settings-overlay
			:active="!!uninstallTarget"
			:title="$t('Uninstall Package')"
			width="24rem"
			@close="uninstallTarget = null"
		>
			<div>
				{{ $t('Are you sure you want to uninstall') }} <strong class="has-text-dark">{{ uninstallTarget }}</strong>?
			</div>
			<template #footer>
				<b-button rounded size="is-small" @click="uninstallTarget = null">{{ $t('Cancel') }}</b-button>
				<b-button rounded size="is-small" type="is-danger" :loading="processingPkg === uninstallTarget" @click="performUninstall">
					{{ $t('Uninstall') }}
				</b-button>
			</template>
		</settings-overlay>

		<!-- Delete Source Confirm Overlay -->
		<settings-overlay
			:active="!!deleteSourceTarget"
			:title="$t('Remove Repository Source')"
			width="26rem"
			@close="deleteSourceTarget = null"
		>
			<div v-if="deleteSourceTarget">
				{{ $t('Remove repository source from') }} <strong class="has-text-dark">{{ getFilename(deleteSourceTarget.file) }}:{{ deleteSourceTarget.line }}</strong>?
				<div class="is-size-7 text-muted mt-2">{{ deleteSourceTarget.raw }}</div>
			</div>
			<template #footer>
				<b-button rounded size="is-small" @click="deleteSourceTarget = null">{{ $t('Cancel') }}</b-button>
				<b-button rounded size="is-small" type="is-danger" @click="performDeleteSource">
					{{ $t('Remove') }}
				</b-button>
			</template>
		</settings-overlay>
	</section>
</template>

<script>
import debounce from 'lodash/debounce'
import SettingsOverlay from '@/components/settings/SettingsOverlay.vue'

export const ROWS = [
	{ label: 'Search & Install Packages' },
	{ label: 'Installed Packages' },
	{ label: 'Upgradable Packages' },
	{ label: 'Repository Sources' }
]

export default {
	name: 'packages-section',
	components: {
		SettingsOverlay
	},
	data() {
		return {
			activeTab: 'search',
			tabs: [
				{ id: 'search', label: 'Search & Install', icon: 'mdi-magnify' },
				{ id: 'installed', label: 'Installed', icon: 'mdi-cube-outline' },
				{ id: 'upgrades', label: 'Upgrades', icon: 'mdi-arrow-up-bold-circle-outline' },
				{ id: 'sources', label: 'Repositories', icon: 'mdi-server-network' }
			],
			searchQuery: '',
			searching: false,
			searchResults: [],
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
			sourceInputMode: 'structured',
			sourceType: 'deb',
			sourceUri: '',
			sourceSuite: '',
			sourceSelectedComponents: ['main'],
			availableComponents: ['main', 'contrib', 'non-free', 'non-free-firmware', 'universe', 'multiverse'],
			customComponentsInput: '',
			newSourceLine: '',
			newSourceFile: 'custom.list',
			addingSource: false,
			updatingRepos: false,

			showLogModal: false,
			logTitle: '',
			logContent: '',
			uninstallTarget: null,
			deleteSourceTarget: null
		}
	},
	computed: {
		finalSourceLine() {
			if (this.sourceInputMode === 'raw') {
				return this.newSourceLine.trim()
			}
			const uri = this.sourceUri.trim()
			const suite = this.sourceSuite.trim()
			if (!uri || !suite) return ''
			const custom = this.customComponentsInput.trim() ? this.customComponentsInput.trim().split(/\s+/) : []
			const allComps = [...new Set([...this.sourceSelectedComponents, ...custom])].filter(Boolean)
			return `${this.sourceType} ${uri} ${suite} ${allComps.join(' ')}`.trim()
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
			this.uninstallTarget = name
		},

		performUninstall() {
			const name = this.uninstallTarget
			if (!name) return
			this.processingPkg = name
			this.$api.sys.uninstallAptPackages([name])
				.then(res => {
					this.processingPkg = null
					this.uninstallTarget = null
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
					this.uninstallTarget = null
					const msg = err.response && err.response.data && err.response.data.data ? err.response.data.data : this.$t('Uninstall failed')
					this.logTitle = `${this.$t('Uninstall Failed')}: ${name}`
					this.logContent = msg
					this.showLogModal = true
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

		toggleComponent(comp) {
			const idx = this.sourceSelectedComponents.indexOf(comp)
			if (idx === -1) {
				this.sourceSelectedComponents.push(comp)
			} else {
				this.sourceSelectedComponents.splice(idx, 1)
			}
		},

		submitAddSource() {
			const line = this.finalSourceLine
			if (!line) return
			this.addingSource = true
			this.$api.sys.addAptSource(line, this.newSourceFile)
				.then(() => {
					this.addingSource = false
					this.showAddSourceModal = false
					this.newSourceLine = ''
					this.sourceUri = ''
					this.sourceSuite = ''
					this.sourceSelectedComponents = ['main']
					this.customComponentsInput = ''
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
			this.deleteSourceTarget = source
		},

		performDeleteSource() {
			const source = this.deleteSourceTarget
			if (!source) return
			this.$api.sys.deleteAptSource(source.file, source.line)
				.then(() => {
					this.deleteSourceTarget = null
					this.$buefy.toast.open({ message: this.$t('Source removed'), type: 'is-success' })
					this.fetchSources()
				})
				.catch(() => {
					this.deleteSourceTarget = null
					this.$buefy.toast.open({ message: this.$t('Failed to remove source'), type: 'is-danger' })
				})
		}
	}
}
</script>

<style lang="scss" scoped>
.add-source-body {
	display: flex;
	flex-direction: column;
}

.form-field-group {
	margin-bottom: 0.85rem;

	&:last-child {
		margin-bottom: 0;
	}
}

.form-label {
	display: block;
	font-size: 0.775rem;
	font-weight: 500;
	color: #334155;
	margin-bottom: 0.35rem;
}

.component-chips-wrap {
	display: flex;
	flex-wrap: wrap;
	gap: 0.4rem;
}

.comp-toggle-chip {
	border: 1px solid #e2e8f0;
	background: #f8fafc;
	color: #64748b;
	padding: 0.25rem 0.65rem;
	border-radius: 999px;
	font-size: 0.75rem;
	font-weight: 400;
	cursor: pointer;
	user-select: none;
	display: inline-flex;
	align-items: center;
	transition: all 0.12s ease;

	&:hover {
		background: #f1f5f9;
		color: #1e293b;
		border-color: #cbd5e1;
	}

	&.selected {
		background: rgba(37, 99, 235, 0.1);
		color: #2563eb;
		border-color: rgba(37, 99, 235, 0.35);
		font-weight: 500;
	}
}

.live-preview-box {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	padding: 0.65rem 0.85rem;
}

.preview-label {
	font-size: 0.7rem;
	font-weight: 500;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	color: #64748b;
	margin-bottom: 0.3rem;
	display: flex;
	align-items: center;
}

.preview-code {
	display: block;
	font-family: $family-monospace;
	font-size: 0.75rem;
	color: #1e293b;
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 6px;
	padding: 0.4rem 0.6rem;
	white-space: pre-wrap;
	word-break: break-all;
}

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
	font-weight: 500;
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
	font-weight: 500;
	color: #1e293b;
	font-family: $family-monospace;
}

.pkg-version {
	font-size: 0.75rem;
	font-weight: 400;
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
	font-weight: 500;
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
		font-weight: 500;
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
	font-weight: 400;
}

.source-type {
	font-weight: 500;
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
	font-weight: 500;
	color: #2563eb;
}

.source-suite {
	font-weight: 500;
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
