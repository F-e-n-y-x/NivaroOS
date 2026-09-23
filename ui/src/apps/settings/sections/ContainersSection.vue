<template>
	<section class="settings-section">
		<div class="section-header is-flex is-align-items-center is-justify-content-between mb-4">
			<div>
				<h2 class="section-title mb-1">{{ $t('Container') }}</h2>
				<p class="section-subtitle text-muted is-size-7">{{ $t('Manage Docker containers, remote registry update tracking, and automated container update schedules without needing manual imports.') }}</p>
			</div>
			<div class="header-actions is-flex is-align-items-center">
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					:loading="checkingAll"
					:disabled="checkingAll || updatingAny"
					@click="checkAllUpdates"
				>
					<i class="mdi mdi-cloud-search-outline mr-1"></i>
					{{ $t('Check All Updates') }}
				</b-button>
			</div>
		</div>

		<!-- ==================== STATS SUMMARY ROW ==================== -->
		<div class="columns is-multiline is-mobile mb-2">
			<div class="column is-3-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-blue">
						<i class="mdi mdi-docker"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ containers.length }}</div>
						<div class="stat-lbl">{{ $t('Total Containers') }}</div>
					</div>
				</div>
			</div>
			<div class="column is-3-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-green">
						<i class="mdi mdi-play-circle-outline"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ runningCount }}</div>
						<div class="stat-lbl">{{ $t('Running') }}</div>
					</div>
				</div>
			</div>
			<div class="column is-3-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-amber">
						<i class="mdi mdi-update"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ updatesAvailableCount }}</div>
						<div class="stat-lbl">{{ $t('Updates Available') }}</div>
					</div>
				</div>
			</div>
			<div class="column is-3-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-purple">
						<i class="mdi mdi-calendar-sync-outline"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ autoUpdateCount }}</div>
						<div class="stat-lbl">{{ $t('Auto-Update Enabled') }}</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== 1. GLOBAL AUTO-UPDATE SCHEDULE ==================== -->
		<h3 class="setting-card-title">{{ $t('Automated Container Updates') }}</h3>
		<div class="setting-card mb-4">
			<div class="setting-row">
				<b-icon class="row-icon has-text-info" icon="calendar-clock-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Global Auto-Update Worker') }}</div>
					<div class="setting-desc">
						{{ $t('Periodically check and update containers with new remote registry image digests, recreating containers with intact volumes.') }}
					</div>
				</div>
				<div class="row-control">
					<b-switch v-model="globalAutoUpdate.enabled" type="is-primary" @input="saveGlobalConfig"></b-switch>
				</div>
			</div>

			<div v-if="globalAutoUpdate.enabled" class="setting-row sub-row">
				<b-icon class="row-icon" icon="clock-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Update Schedule') }}</div>
					<div class="setting-desc">
						{{ humanCron(globalAutoUpdate.schedule) }}
					</div>
				</div>
				<div class="row-control is-flex is-align-items-center">
					<div class="select is-small mr-2">
						<select v-model="presetSchedule" @change="onPresetScheduleChange">
							<option value="0 3 * * *">{{ $t('Every night at 3:00 AM') }}</option>
							<option value="0 4 * * *">{{ $t('Every night at 4:00 AM') }}</option>
							<option value="0 3 * * 0">{{ $t('Weekly (Every Sunday at 3:00 AM)') }}</option>
							<option value="0 */12 * * *">{{ $t('Every 12 hours') }}</option>
							<option value="custom">{{ $t('Custom Cron Expression') }}</option>
						</select>
					</div>
					<input
						v-if="presetSchedule === 'custom'"
						v-model="globalAutoUpdate.schedule"
						type="text"
						class="input is-small custom-cron-input mr-2"
						placeholder="0 3 * * *"
						@blur="saveGlobalConfig"
					/>
					<b-button rounded size="is-small" :loading="savingGlobal" @click="saveGlobalConfig">
						{{ $t('Save') }}
					</b-button>
				</div>
			</div>
		</div>

		<!-- ==================== 1.1 UPDATES AVAILABLE BANNER ==================== -->
		<div v-if="updatesAvailableCount > 0" class="update-alert-banner mb-4">
			<div class="is-flex is-align-items-center update-alert-left">
				<div class="update-alert-icon mr-3">
					<i class="mdi mdi-arrow-up-bold-circle-outline"></i>
				</div>
				<div class="update-alert-content">
					<div class="update-alert-title">
						{{ $t('Container Updates Available') }}
						<span class="tag is-info is-rounded is-small ml-2">{{ updatesAvailableCount }}</span>
					</div>
					<div class="update-alert-desc">
						{{ updateAlertMessage }}
					</div>
				</div>
			</div>
			<div class="update-alert-actions buttons are-small mb-0">
				<b-button
					rounded
					type="is-primary"
					:loading="updatingAny"
					:disabled="updatingAny"
					@click="updateAllAvailable"
				>
					<i class="mdi mdi-download mr-1"></i>
					{{ $t('Update All') }} ({{ updatesAvailableCount }})
				</b-button>
				<b-button
					rounded
					@click="activeFilter = activeFilter === 'updates' ? 'all' : 'updates'"
				>
					{{ activeFilter === 'updates' ? $t('Show All') : $t('Filter Updates') }}
				</b-button>
			</div>
		</div>

		<!-- ==================== 1.2 LIVE NOTIFICATION & PROGRESS CARD ==================== -->
		<transition name="fade">
			<div v-if="activeNotification" class="update-notification-card mb-4" :class="'is-' + activeNotification.type">
				<div class="is-flex is-align-items-center is-justify-content-between notification-content-row">
					<div class="is-flex is-align-items-center notif-left">
						<div class="notification-status-icon mr-3">
							<b-icon
								v-if="activeNotification.type === 'progress'"
								icon="loading"
								pack="mdi"
								size="is-medium"
								custom-class="mdi-spin"
							></b-icon>
							<i v-else-if="activeNotification.type === 'success'" class="mdi mdi-check-circle-outline has-text-success"></i>
							<i v-else-if="activeNotification.type === 'danger'" class="mdi mdi-alert-circle-outline has-text-danger"></i>
							<i v-else class="mdi mdi-information-outline has-text-info"></i>
						</div>
						<div>
							<div class="notification-title font-medium">
								{{ activeNotification.title }}
								<span v-if="activeNotification.container" class="tag is-dark is-rounded is-small ml-2">
									{{ activeNotification.container }}
								</span>
							</div>
							<div class="notification-subtitle text-muted is-size-7 mt-1">
								{{ activeNotification.message }}
							</div>
						</div>
					</div>
					<div class="is-flex is-align-items-center notif-actions">
						<b-button
							v-if="activeNotification.type === 'danger' && activeNotification.retryContainer"
							rounded
							size="is-small"
							type="is-danger"
							class="mr-2"
							@click="updateContainer(activeNotification.retryContainer)"
						>
							<i class="mdi mdi-refresh mr-1"></i>
							{{ $t('Retry') }}
						</b-button>
						<button class="dismiss-notif-btn" @click="activeNotification = null" :title="$t('Dismiss')">
							<i class="mdi mdi-close"></i>
						</button>
					</div>
				</div>
				<!-- Animated progress bar for in-progress operations -->
				<div v-if="activeNotification.type === 'progress'" class="update-progress-bar-wrap mt-3">
					<div class="update-progress-bar-inner"></div>
				</div>
			</div>
		</transition>

		<!-- ==================== 2. CONTAINERS LIST ==================== -->
		<div class="is-flex is-align-items-center is-justify-content-between mb-3 containers-list-header">
			<h3 class="setting-card-title mb-0">{{ $t('All Host Containers') }}</h3>
			<div class="is-flex is-align-items-center containers-list-header-controls">
				<!-- Search box -->
				<div class="search-box mr-2">
					<i class="mdi mdi-magnify search-icon"></i>
					<input
						v-model="searchQuery"
						type="text"
						class="container-search-input"
						:placeholder="$t('Filter by name or image...')"
					/>
					<button v-if="searchQuery" class="clear-btn" @click="searchQuery = ''">
						<i class="mdi mdi-close"></i>
					</button>
				</div>

				<!-- Filter buttons -->
				<div class="filter-pills">
					<button
						v-for="filter in filters"
						:key="filter.id"
						class="filter-pill"
						:class="{ active: activeFilter === filter.id }"
						@click="activeFilter = filter.id"
					>
						{{ filter.label }}
						<span v-if="filter.count !== undefined" class="pill-count">{{ filter.count }}</span>
					</button>
				</div>
			</div>
		</div>

		<!-- Container Cards List -->
		<div v-if="loadingContainers" class="p-6 has-text-centered text-muted">
			<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
			<div class="mt-2 is-size-7">{{ $t('Discovering containers and checking registry tags...') }}</div>
		</div>

		<div v-else-if="!filteredContainers.length" class="empty-card has-text-centered p-6">
			<i class="mdi mdi-docker is-size-1 text-muted mb-2"></i>
			<div class="is-size-6 font-medium text-muted">{{ $t('No containers found') }}</div>
			<div class="is-size-7 text-muted mt-1">{{ searchQuery ? $t('No containers matching search filter.') : $t('No Docker containers running on host.') }}</div>
		</div>

		<div v-else class="setting-card p-0">
			<div
				v-for="c in filteredContainers"
				:key="c.id"
				class="setting-row container-item-row is-align-items-center"
			>
				<!-- Container Icon -->
				<div class="container-avatar mr-3">
					<b-icon icon="docker" pack="mdi" size="is-20" class="has-text-info"></b-icon>
				</div>

				<!-- Info -->
				<div class="row-label">
					<div class="setting-title is-flex is-align-items-center">
						<span class="container-name mr-2">{{ c.name }}</span>
						<span class="status-pill mr-2" :class="c.state === 'running' ? 'is-running' : 'is-stopped'">
							<span class="dot"></span>
							{{ c.state }}
						</span>
						<span v-if="c.has_update" class="tag is-info is-rounded is-small is-light pulse-update">
							<i class="mdi mdi-arrow-up-bold-circle-outline mr-1"></i>
							{{ $t('Update Available') }}
						</span>
						<span v-else-if="c.last_checked_at" class="tag is-success is-rounded is-small is-light">
							<i class="mdi mdi-check mr-1"></i>
							{{ $t('Up to date') }}
						</span>
					</div>
					<div class="setting-desc is-flex is-align-items-center is-flex-wrap-wrap mt-1">
						<span class="image-text mr-3"><code>{{ c.image }}</code></span>
						<span v-if="c.has_update && c.latest_digest" class="digest-text text-info mr-3">
							<i class="mdi mdi-tag-outline mr-1"></i>{{ $t('New') }}: <code>{{ formatDigest(c.latest_digest) }}</code>
						</span>
						<span v-if="c.last_checked_at" class="text-muted is-size-7 mr-3">
							{{ $t('Checked') }}: {{ formatTimeAgo(c.last_checked_at) }}
						</span>
						<span v-if="c.last_updated_at" class="text-muted is-size-7">
							{{ $t('Updated') }}: {{ formatTimeAgo(c.last_updated_at) }}
						</span>
					</div>
				</div>

				<!-- Controls -->
				<div class="row-control is-flex is-align-items-center">
					<!-- Auto-update switch per container -->
					<div class="auto-update-toggle is-flex is-align-items-center mr-4" :title="$t('Auto-update this container')">
						<span class="is-size-7 text-muted mr-2">{{ $t('Auto') }}</span>
						<b-switch
							v-model="c.auto_update_enabled"
							size="is-small"
							type="is-primary"
							@input="toggleContainerAutoUpdate(c)"
						></b-switch>
					</div>

					<!-- Action Buttons -->
					<div class="buttons are-small mb-0">
						<!-- Update Now Button (highlighted if update available) -->
						<b-button
							v-if="c.has_update"
							rounded
							size="is-small"
							type="is-primary"
							class="update-now-btn pulse-glow"
							:loading="updatingId === c.id"
							:disabled="updatingId === c.id || updatingAny"
							@click="updateContainer(c)"
						>
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Update Now') }}
						</b-button>

						<!-- Check single update -->
						<b-button
							rounded
							size="is-small"
							:loading="checkingId === c.id"
							:disabled="checkingId === c.id || updatingId === c.id"
							@click="checkSingleUpdate(c)"
							:title="$t('Check remote registry for update')"
						>
							<i class="mdi mdi-refresh"></i>
						</b-button>

						<!-- Logs Button -->
						<b-button
							rounded
							size="is-small"
							@click="openConsole(c, 'logs')"
							:title="$t('View container logs')"
						>
							<i class="mdi mdi-text-box-search-outline"></i>
						</b-button>

						<!-- Terminal Button -->
						<b-button
							rounded
							size="is-small"
							@click="openConsole(c, 'terminal')"
							:title="$t('Open container terminal')"
						>
							<i class="mdi mdi-console"></i>
						</b-button>

						<!-- Restart Button -->
						<b-button
							rounded
							size="is-small"
							:loading="restartingId === c.id"
							@click="restartContainer(c)"
							:title="$t('Restart container')"
						>
							<i class="mdi mdi-restart"></i>
						</b-button>
					</div>
				</div>
			</div>
		</div>
	</section>
</template>

<script>
import { escapeHtml } from '@/utils/escapeHtml'
import activityService from '@/service/activity'

export const ROWS = [
	{ label: 'Container Updates' },
	{ label: 'Docker Container Auto-Update' },
	{ label: 'Container Registry Check' }
]

export default {
	name: 'containers-section',
	data() {
		return {
			containers: [],
			loadingContainers: false,
			checkingAll: false,
			updatingAny: false,
			checkingId: null,
			updatingId: null,
			restartingId: null,
			searchQuery: '',
			activeFilter: 'all',
			globalAutoUpdate: {
				enabled: true,
				schedule: '0 3 * * *'
			},
			presetSchedule: '0 3 * * *',
			savingGlobal: false,
			activeNotification: null,
			updateProgressInterval: null
		}
	},
	computed: {
		runningCount() {
			return this.containers.filter(c => c.state === 'running').length
		},
		updatesAvailableCount() {
			return this.containers.filter(c => c.has_update).length
		},
		updateAlertMessage() {
			const names = this.containers.filter(c => c.has_update).map(c => c.name)
			if (!names.length) return ''
			if (names.length <= 3) {
				return `${names.join(', ')} ${this.$t('can be upgraded to newer images without losing data.')}`
			}
			return `${names.slice(0, 2).join(', ')} ${this.$t('and')} ${names.length - 2} ${this.$t('other container(s) have new image versions ready to apply.')}`
		},
		autoUpdateCount() {
			return this.containers.filter(c => c.auto_update_enabled).length
		},
		filters() {
			return [
				{ id: 'all', label: this.$t('All'), count: this.containers.length },
				{ id: 'updates', label: this.$t('Updates Available'), count: this.updatesAvailableCount },
				{ id: 'running', label: this.$t('Running'), count: this.runningCount },
				{ id: 'auto', label: this.$t('Auto-Update'), count: this.autoUpdateCount }
			]
		},
		filteredContainers() {
			let list = this.containers
			if (this.activeFilter === 'updates') {
				list = list.filter(c => c.has_update)
			} else if (this.activeFilter === 'running') {
				list = list.filter(c => c.state === 'running')
			} else if (this.activeFilter === 'auto') {
				list = list.filter(c => c.auto_update_enabled)
			}

			if (this.searchQuery) {
				const q = this.searchQuery.toLowerCase()
				list = list.filter(c =>
					(c.name && c.name.toLowerCase().includes(q)) ||
					(c.image && c.image.toLowerCase().includes(q))
				)
			}
			return list
		}
	},
	mounted() {
		this.fetchContainers()
		this.fetchGlobalConfig()
	},
	beforeDestroy() {
		if (this.updateProgressInterval) {
			clearInterval(this.updateProgressInterval)
			this.updateProgressInterval = null
		}
	},
	methods: {
		async fetchContainers() {
			this.loadingContainers = true
			try {
				const res = await this.$api.container.getAllContainersWithUpdates()
				if (res && res.data && res.data.data) {
					this.containers = res.data.data || []
				}
			} catch (err) {
				console.error('Failed to load containers:', err)
			} finally {
				this.loadingContainers = false
			}
		},
		async fetchGlobalConfig() {
			try {
				const res = await this.$api.container.getAutoUpdateConfig()
				if (res && res.data && res.data.data) {
					this.globalAutoUpdate = res.data.data
					if (['0 3 * * *', '0 4 * * *', '0 3 * * 0', '0 */12 * * *'].includes(this.globalAutoUpdate.schedule)) {
						this.presetSchedule = this.globalAutoUpdate.schedule
					} else {
						this.presetSchedule = 'custom'
					}
				}
			} catch (err) {
				console.error('Failed to load auto-update config:', err)
			}
		},
		onPresetScheduleChange() {
			if (this.presetSchedule !== 'custom') {
				this.globalAutoUpdate.schedule = this.presetSchedule
				this.saveGlobalConfig()
			}
		},
		async saveGlobalConfig() {
			this.savingGlobal = true
			try {
				await this.$api.container.setAutoUpdateConfig(this.globalAutoUpdate)
				this.$buefy.toast.open({
					message: this.$t('Auto-update settings saved'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to save settings'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.savingGlobal = false
			}
		},
		async checkAllUpdates() {
			this.checkingAll = true
			this.activeNotification = {
				type: 'progress',
				title: this.$t('Checking Remote Registries'),
				message: this.$t('Querying Docker Hub and image registries for newer manifest digests across all containers...')
			}
			try {
				const res = await this.$api.container.checkAllContainersUpdate()
				if (res && res.data && res.data.data) {
					this.containers = res.data.data || []
				}
				const updates = this.containers.filter(c => c.has_update)
				if (updates.length > 0) {
					const names = updates.map(u => u.name).join(', ')
					this.activeNotification = {
						type: 'info',
						title: this.$t('Updates Discovered'),
						message: `${updates.length} ${this.$t('container(s) have newer image versions available')}: ${names}`
					}
					activityService.add({
						title: this.$t('Container Updates Available'),
						message: `${updates.length} container(s) have updates: ${names}`,
						type: 'app',
						status: 'info'
					})
					this.$buefy.toast.open({
						message: this.$t('Found {count} container update(s)!', { count: updates.length }),
						type: 'is-info',
						position: 'is-top',
						duration: 3500
					})
				} else {
					this.activeNotification = {
						type: 'success',
						title: this.$t('All Containers Up To Date'),
						message: this.$t('Every running container matches the latest manifest digest from remote registries.')
					}
					this.$buefy.toast.open({
						message: this.$t('All containers are up to date!'),
						type: 'is-success',
						position: 'is-top',
						duration: 2500
					})
					setTimeout(() => {
						if (this.activeNotification && this.activeNotification.type === 'success') {
							this.activeNotification = null
						}
					}, 5000)
				}
			} catch (err) {
				const errMsg = err.response?.data?.message || err.message || this.$t('Failed to check updates')
				this.activeNotification = {
					type: 'danger',
					title: this.$t('Registry Check Failed'),
					message: errMsg
				}
				this.$buefy.toast.open({
					message: errMsg,
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.checkingAll = false
			}
		},
		async checkSingleUpdate(c) {
			this.checkingId = c.id
			try {
				const res = await this.$api.container.checkContainerUpdate(c.id)
				if (res && res.data && res.data.data) {
					const updated = res.data.data
					const idx = this.containers.findIndex(item => item.id === c.id)
					if (idx !== -1) {
						this.$set(this.containers, idx, updated)
					}
					if (updated.has_update) {
						this.activeNotification = {
							type: 'info',
							title: this.$t('Update Available'),
							container: c.name,
							message: `${escapeHtml(c.name)} (${c.image}) ${this.$t('has a newer image version available in the registry.')}`
						}
						activityService.add({
							title: this.$t('Container Update Found'),
							message: `${c.name} (${c.image}) has an update available in the registry`,
							type: 'app',
							status: 'info'
						})
						this.$buefy.toast.open({
							message: `${escapeHtml(c.name)}: ${this.$t('New image update found!')}`,
							type: 'is-info',
							position: 'is-top',
							duration: 3500
						})
					} else {
						this.$buefy.toast.open({
							message: `${escapeHtml(c.name)}: ${this.$t('Container is already up to date')}`,
							type: 'is-success',
							position: 'is-top',
							duration: 2500
						})
					}
				}
			} catch (err) {
				const errMsg = err.response?.data?.message || err.message || this.$t('Check failed')
				this.$buefy.toast.open({
					message: errMsg,
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.checkingId = null
			}
		},
		async updateContainer(c) {
			this.updatingId = c.id
			this.updatingAny = true
			this.activeNotification = {
				type: 'progress',
				title: this.$t('Updating Container'),
				container: c.name,
				message: this.$t('Pulling latest image layers and preparing container update...')
			}

			// Cycle through helpful phase descriptions during download/recreate
			let stepIdx = 0
			const steps = [
				this.$t('Downloading new image layers from registry...'),
				this.$t('Stopping existing container and preserving volumes...'),
				this.$t('Cloning configuration, port bindings, and networking...'),
				this.$t('Starting updated container and validating health status...')
			]
			if (this.updateProgressInterval) {
				clearInterval(this.updateProgressInterval)
			}
			this.updateProgressInterval = setInterval(() => {
				stepIdx = (stepIdx + 1) % steps.length
				if (this.activeNotification && this.activeNotification.type === 'progress') {
					this.activeNotification.message = steps[stepIdx]
				}
			}, 3500)

			try {
				const res = await this.$api.container.updateContainer(c.id)
				clearInterval(this.updateProgressInterval)
				this.updateProgressInterval = null

				// The server only recreates when the pull brought a newer image.
				if (!(res.data && res.data.data && res.data.data.updated)) {
					this.activeNotification = {
						type: 'success',
						title: this.$t('Already up to date'),
						container: c.name,
						message: `${escapeHtml(c.name)} ${this.$t('already runs the latest image - nothing was changed.')}`
					}
					await this.fetchContainers()
					return
				}

				this.activeNotification = {
					type: 'success',
					title: this.$t('Update Completed'),
					container: c.name,
					message: `${escapeHtml(c.name)} ${this.$t('has been updated to the latest image! All volumes, ports, and settings preserved.')}`
				}

				activityService.add({
					title: this.$t('Container Updated'),
					message: `${c.name} (${c.image}) ${this.$t('successfully updated to the latest image')}`,
					type: 'app',
					status: 'success'
				})

				this.$buefy.toast.open({
					message: `${escapeHtml(c.name)} ${this.$t('updated successfully!')}`,
					type: 'is-success',
					position: 'is-top',
					duration: 3500
				})

				await this.fetchContainers()

				setTimeout(() => {
					if (this.activeNotification && this.activeNotification.type === 'success' && this.activeNotification.container === c.name) {
						this.activeNotification = null
					}
				}, 7000)
			} catch (err) {
				clearInterval(this.updateProgressInterval)
				this.updateProgressInterval = null

				const errMsg = err.response?.data?.message || err.message || this.$t('Update failed')
				this.activeNotification = {
					type: 'danger',
					title: this.$t('Update Failed'),
					container: c.name,
					message: errMsg,
					retryContainer: c
				}

				activityService.add({
					title: this.$t('Container Update Failed'),
					message: `${c.name}: ${errMsg}`,
					type: 'app',
					status: 'error'
				})

				this.$buefy.toast.open({
					message: `${escapeHtml(c.name)}: ${escapeHtml(errMsg)}`,
					type: 'is-danger',
					position: 'is-top',
					duration: 4500
				})
			} finally {
				this.updatingId = null
				this.updatingAny = false
			}
		},
		async updateAllAvailable() {
			const targets = this.containers.filter(c => c.has_update)
			if (!targets.length) return

			this.updatingAny = true
			let successCount = 0
			let failCount = 0
			let upToDate = 0

			for (let i = 0; i < targets.length; i++) {
				const c = targets[i]
				this.updatingId = c.id
				this.activeNotification = {
					type: 'progress',
					title: `${this.$t('Updating Containers')} (${i + 1}/${targets.length})`,
					container: c.name,
					message: this.$t('Pulling latest image and updating container...')
				}

				try {
					const res = await this.$api.container.updateContainer(c.id)
					if (!(res.data && res.data.data && res.data.data.updated)) {
						upToDate++
						continue
					}
					successCount++
					activityService.add({
						title: this.$t('Container Updated'),
						message: `${c.name} (${c.image}) ${this.$t('updated to the latest image')}`,
						type: 'app',
						status: 'success'
					})
				} catch (err) {
					failCount++
					activityService.add({
						title: this.$t('Container Update Failed'),
						message: `${c.name}: ${(err.response && err.response.data && err.response.data.message) || err.message}`,
						type: 'app',
						status: 'error'
					})
				}
			}

			this.updatingId = null
			this.updatingAny = false
			await this.fetchContainers()

			if (failCount === 0) {
				this.activeNotification = {
					type: 'success',
					title: this.$t('All Updates Complete'),
					message: `${this.$t('Successfully updated')} ${successCount} ${this.$t('container(s) to their latest images!')}` + (upToDate ? ` ${upToDate} ${this.$t('already up to date.')}` : '')
				}
				setTimeout(() => {
					if (this.activeNotification && this.activeNotification.type === 'success') {
						this.activeNotification = null
					}
				}, 7000)
			} else {
				this.activeNotification = {
					type: 'danger',
					title: this.$t('Batch Update Finished with Issues'),
					message: `${this.$t('Updated')} ${successCount} ${this.$t('container(s)')}, ${failCount} ${this.$t('failed.')}`
				}
			}
		},
		async toggleContainerAutoUpdate(c) {
			try {
				await this.$api.container.setContainerAutoUpdate(c.id, {
					enabled: c.auto_update_enabled,
					schedule: c.auto_update_schedule || '0 3 * * *'
				})
			} catch (err) {
				c.auto_update_enabled = !c.auto_update_enabled
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to toggle auto-update'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			}
		},
		openConsole(c, initialTab = 'terminal') {
			this.$store.commit('OPEN_WINDOW', {
				id: `container-console-${c.id}`,
				title: `${c.name} - ${initialTab === 'logs' ? this.$t('Logs') : this.$t('Terminal')}`,
				component: 'ContainerConsolePanel',
				width: 820,
				height: 520,
				props: {
					containerId: c.id,
					containerName: c.name,
					containerImage: c.image,
					initialTab: initialTab,
					status: c.state
				}
			})
		},
		async restartContainer(c) {
			this.restartingId = c.id
			try {
				await this.$api.container.updateState(c.id, 'restart')
				this.$buefy.toast.open({
					message: `${escapeHtml(c.name)} ${this.$t('restarted')}`,
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
				c.state = 'running'
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Restart failed'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.restartingId = null
			}
		},
		humanCron(cron) {
			if (!cron) return ''
			if (cron === '0 3 * * *') return this.$t('Every night at 3:00 AM')
			if (cron === '0 4 * * *') return this.$t('Every night at 4:00 AM')
			if (cron === '0 3 * * 0') return this.$t('Weekly (Every Sunday at 3:00 AM)')
			if (cron === '0 */12 * * *') return this.$t('Every 12 hours')
			return `Cron: ${cron}`
		},
		formatTimeAgo(dateStr) {
			if (!dateStr) return ''
			const d = new Date(dateStr)
			if (isNaN(d.getTime())) return dateStr
			const now = new Date()
			const diffSec = Math.floor((now - d) / 1000)
			if (diffSec < 60) return this.$t('Just now')
			if (diffSec < 3600) return `${Math.floor(diffSec / 60)} ${this.$t('min ago')}`
			if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} ${this.$t('hours ago')}`
			return d.toLocaleDateString()
		},
		formatDigest(digest) {
			if (!digest) return ''
			const parts = digest.split(':')
			const hash = parts.length > 1 ? parts[1] : parts[0]
			return hash.substring(0, 12)
		}
	}
}
</script>

<style lang="scss" scoped>
.section-subtitle {
	line-height: 1.4;
}

.stat-card {
	display: flex;
	align-items: center;
	padding: var(--space-3) var(--space-4);
	background: rgba(255, 255, 255, 0.6);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: var(--radius-card);

	.stat-icon {
		width: 36px;
		height: 36px;
		border-radius: var(--radius-control);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: var(--font-lg);
		margin-right: var(--space-3);

		&.bg-blue {
			background: rgba(37, 99, 235, 0.12);
			color: var(--color-primary);
		}
		&.bg-green {
			background: rgba(16, 185, 129, 0.12);
			color: #10b981;
		}
		&.bg-amber {
			background: rgba(245, 158, 11, 0.12);
			color: #f59e0b;
		}
		&.bg-purple {
			background: rgba(139, 92, 246, 0.12);
			color: #8b5cf6;
		}
	}

	.stat-val {
		font-size: var(--font-lg);
		font-weight: 500;
		line-height: 1.2;
		color: var(--theme-text-primary, #1e293b);
	}

	.stat-lbl {
		font-size: var(--font-2xs);
		color: var(--color-text-muted);
	}
}

.container-name {
	font-size: var(--font-base);
	font-weight: 500;
	color: var(--theme-text-primary, #1e293b);
}

.font-medium {
	font-weight: 500;
}

// The "All Host Containers" header row (title + search + filter pills) had
// no wrap at all - title, a 190px search box, and 4 filter pills easily
// exceed a phone-width window with nowhere for the overflow to go.
.containers-list-header {
	flex-wrap: wrap;
	row-gap: var(--space-2);
}

.containers-list-header-controls {
	flex-wrap: wrap;
	row-gap: var(--space-2);
}

.search-box {
	position: relative;
	display: flex;
	align-items: center;

	.search-icon {
		position: absolute;
		left: 8px;
		color: var(--color-text-muted-light);
		font-size: var(--font-base);
	}

	.container-search-input {
		padding: var(--space-1) var(--space-6) var(--space-1) var(--space-6);
		background: var(--theme-input-bg, rgba(0, 0, 0, 0.04));
		border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
		border-radius: var(--radius-sm);
		font-size: var(--font-xs);
		width: 100%;
		max-width: 190px;
		outline: none;
		color: var(--theme-text-primary, #1e293b);

		&:focus {
			border-color: var(--color-primary);
			background: var(--theme-card-bg, #fff);
		}
	}

	.clear-btn {
		position: absolute;
		right: 6px;
		background: transparent;
		border: none;
		color: var(--color-text-muted-light);
		cursor: pointer;
		padding: 0;
	}
}

.filter-pills {
	display: flex;
	gap: var(--space-1);

	.filter-pill {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
		border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
		color: var(--color-text-muted);
		font-size: var(--font-2xs);
		font-weight: 500;
		padding: var(--space-1) var(--space-3);
		border-radius: var(--radius-pill);
		cursor: pointer;
		display: flex;
		align-items: center;
		transition: all 0.15s ease;

		&:hover {
			background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
			color: var(--theme-text-primary, #1e293b);
		}

		&.active {
			background: var(--color-primary);
			border-color: var(--color-primary);
			color: #ffffff;

			.pill-count {
				background: rgba(255, 255, 255, 0.25);
				color: #ffffff;
			}
		}

		.pill-count {
			margin-left: var(--space-1);
			background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
			font-size: var(--font-2xs);
			padding: var(--space-1) var(--space-1);
			border-radius: var(--radius-pill);
		}
	}
}


.container-item-row {
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid rgba(0, 0, 0, 0.05);

	&:last-child {
		border-bottom: none;
	}

	// This row's .row-control (an Auto toggle + up to 4 action buttons)
	// had no wrap - the setting-card ancestor clips overflow (see
	// _settings.scss), so at phone width the later buttons (Restart,
	// Terminal) could end up entirely unreachable instead of just cramped.
	.row-control {
		flex-wrap: wrap;
		row-gap: var(--space-2);
		justify-content: flex-end;
	}
}

// Same clipped-and-unreachable risk for the "Update Schedule" row's Save
// button once the schedule select + custom cron input also need room.
.setting-row.sub-row .row-control {
	flex-wrap: wrap;
	row-gap: var(--space-2);
}

.container-avatar {
	width: 38px;
	height: 38px;
	border-radius: var(--radius-control);
	background: rgba(14, 165, 233, 0.1);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}

.status-pill {
	display: inline-flex;
	align-items: center;
	font-size: var(--font-2xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);

	.dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		margin-right: var(--space-1);
	}

	&.is-running {
		background: rgba(16, 185, 129, 0.12);
		color: #059669;
		.dot {
			background: #10b981;
		}
	}

	&.is-stopped {
		background: rgba(239, 68, 68, 0.12);
		color: #dc2626;
		.dot {
			background: #ef4444;
		}
	}
}

.image-text code {
	font-size: var(--font-2xs);
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	color: var(--color-text-muted);
	padding: var(--space-1) var(--space-1);
	border-radius: var(--radius-xs);
}

.pulse-update {
	animation: pulseUpdate 2s infinite ease-in-out;
}

@keyframes pulseUpdate {
	0%, 100% {
		box-shadow: 0 0 0 0 rgba(37, 99, 235, 0.4);
	}
	50% {
		box-shadow: 0 0 0 4px rgba(37, 99, 235, 0);
	}
}

.custom-cron-input {
	width: 120px;
}

.empty-card {
	background: var(--theme-card-subtle, rgba(255, 255, 255, 0.5));
	border: 1px dashed var(--theme-card-border, rgba(0, 0, 0, 0.12));
	border-radius: var(--radius-card);
}

// ==================== UPDATE ALERT BANNER ====================
.update-alert-banner {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	background: linear-gradient(135deg, rgba(37, 99, 235, 0.08) 0%, rgba(59, 130, 246, 0.14) 100%);
	border: 1px solid rgba(37, 99, 235, 0.28);
	border-radius: var(--radius-card);
	flex-wrap: wrap;
	gap: var(--space-3);

	.update-alert-left {
		flex: 1;
		min-width: 260px;
	}

	.update-alert-icon {
		width: 42px;
		height: 42px;
		border-radius: var(--radius-control);
		background: var(--color-primary);
		color: #ffffff;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 22px;
		flex-shrink: 0;
		box-shadow: 0 4px 14px rgba(37, 99, 235, 0.35);
	}

	.update-alert-title {
		font-size: var(--font-base);
		font-weight: 600;
		color: var(--theme-text-primary, #1e293b);
		display: flex;
		align-items: center;
	}

	.update-alert-desc {
		font-size: var(--font-xs);
		color: var(--color-text-muted);
		margin-top: 2px;
		line-height: 1.4;
	}

	.update-alert-actions {
		display: flex;
		align-items: center;
		flex-shrink: 0;
	}
}

// ==================== LIVE UPDATE NOTIFICATION CARD ====================
.update-notification-card {
	position: relative;
	padding: var(--space-3) var(--space-4);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	box-shadow: 0 4px 20px rgba(0, 0, 0, 0.07);

	&.is-progress {
		border-left: 4px solid var(--color-primary);
		background: rgba(37, 99, 235, 0.04);
		.notification-status-icon {
			color: var(--color-primary);
		}
	}

	&.is-success {
		border-left: 4px solid #10b981;
		background: rgba(16, 185, 129, 0.05);
	}

	&.is-danger {
		border-left: 4px solid #ef4444;
		background: rgba(239, 68, 68, 0.05);
	}

	&.is-info {
		border-left: 4px solid #0ea5e9;
		background: rgba(14, 165, 233, 0.05);
	}

	.notification-content-row {
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.notif-left {
		flex: 1;
		min-width: 240px;
	}

	.notification-status-icon {
		font-size: 24px;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}

	.notification-title {
		font-size: var(--font-base);
		color: var(--theme-text-primary, #1e293b);
		display: flex;
		align-items: center;
	}

	.notification-subtitle {
		line-height: 1.4;
	}

	.dismiss-notif-btn {
		background: transparent;
		border: none;
		color: var(--color-text-muted-light);
		cursor: pointer;
		font-size: var(--font-base);
		padding: 4px;
		display: flex;
		align-items: center;
		border-radius: 50%;
		transition: color 0.15s, background 0.15s;

		&:hover {
			color: var(--theme-text-primary, #1e293b);
			background: rgba(0, 0, 0, 0.05);
		}
	}
}

.update-progress-bar-wrap {
	width: 100%;
	height: 4px;
	background: rgba(0, 0, 0, 0.08);
	border-radius: 2px;
	overflow: hidden;
	position: relative;

	.update-progress-bar-inner {
		position: absolute;
		top: 0;
		left: 0;
		height: 100%;
		width: 40%;
		background: linear-gradient(90deg, var(--color-primary), #60a5fa);
		border-radius: 2px;
		animation: indeterminateProgress 1.6s infinite ease-in-out;
	}
}

@keyframes indeterminateProgress {
	0% {
		left: -40%;
		width: 40%;
	}
	50% {
		left: 30%;
		width: 50%;
	}
	100% {
		left: 100%;
		width: 40%;
	}
}

.digest-text {
	font-size: var(--font-2xs);
	font-weight: 500;
	color: #0284c7;
	display: inline-flex;
	align-items: center;

	code {
		font-size: var(--font-2xs);
		background: rgba(2, 132, 199, 0.08);
		color: #0284c7;
		padding: 1px 4px;
		border-radius: var(--radius-xs);
		margin-left: 2px;
	}
}

.pulse-glow {
	box-shadow: 0 0 10px rgba(37, 99, 235, 0.45);
	transition: box-shadow 0.2s ease;

	&:hover {
		box-shadow: 0 0 14px rgba(37, 99, 235, 0.65);
	}
}

.fade-enter-active,
.fade-leave-active {
	transition: opacity 0.25s ease, transform 0.25s ease;
}

.fade-enter,
.fade-leave-to {
	opacity: 0;
	transform: translateY(-6px);
}
</style>
