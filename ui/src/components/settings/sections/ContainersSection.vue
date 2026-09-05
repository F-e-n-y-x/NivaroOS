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

		<!-- ==================== 2. CONTAINERS LIST ==================== -->
		<div class="is-flex is-align-items-center is-justify-content-between mb-3">
			<h3 class="setting-card-title mb-0">{{ $t('All Host Containers') }}</h3>
			<div class="is-flex is-align-items-center">
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
							:loading="updatingId === c.id"
							:disabled="updatingId === c.id"
							@click="updateContainer(c)"
						>
							<i class="mdi mdi-download mr-1"></i>
							{{ $t('Update Now') }}
						</b-button>

						<!-- Check single update -->
						<b-button
							v-else
							rounded
							size="is-small"
							:loading="checkingId === c.id"
							:disabled="checkingId === c.id"
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
			savingGlobal: false
		}
	},
	computed: {
		runningCount() {
			return this.containers.filter(c => c.state === 'running').length
		},
		updatesAvailableCount() {
			return this.containers.filter(c => c.has_update).length
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
			try {
				const res = await this.$api.container.checkAllContainersUpdate()
				if (res && res.data && res.data.data) {
					this.containers = res.data.data || []
				}
				this.$buefy.toast.open({
					message: this.$t('Registry updates check completed'),
					type: 'is-success',
					position: 'is-top',
					duration: 2500
				})
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to check updates'),
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
						this.$buefy.toast.open({
							message: `${c.name}: ${this.$t('New image update found!')}`,
							type: 'is-info',
							position: 'is-top',
							duration: 3000
						})
					} else {
						this.$buefy.toast.open({
							message: `${c.name}: ${this.$t('Container is already up to date')}`,
							type: 'is-success',
							position: 'is-top',
							duration: 2000
						})
					}
				}
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Check failed'),
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
			try {
				await this.$api.container.updateContainer(c.id)
				this.$buefy.toast.open({
					message: `${c.name} ${this.$t('updated successfully!')}`,
					type: 'is-success',
					position: 'is-top',
					duration: 3000
				})
				await this.fetchContainers()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Update failed'),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000
				})
			} finally {
				this.updatingId = null
				this.updatingAny = false
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
					message: `${c.name} ${this.$t('restarted')}`,
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
	padding: 12px 14px;
	background: rgba(255, 255, 255, 0.6);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 12px;

	.stat-icon {
		width: 36px;
		height: 36px;
		border-radius: 9px;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 18px;
		margin-right: 12px;

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
		font-size: 17px;
		font-weight: 500;
		line-height: 1.2;
		color: #1e293b;
	}

	.stat-lbl {
		font-size: 11px;
		color: var(--color-text-muted);
	}
}

.container-name {
	font-size: 0.85rem;
	font-weight: 500;
	color: #1e293b;
}

.font-medium {
	font-weight: 500;
}

.search-box {
	position: relative;
	display: flex;
	align-items: center;

	.search-icon {
		position: absolute;
		left: 8px;
		color: var(--color-text-muted-light);
		font-size: 14px;
	}

	.container-search-input {
		padding: 4px 24px 4px 26px;
		background: rgba(0, 0, 0, 0.04);
		border: 1px solid rgba(0, 0, 0, 0.08);
		border-radius: 6px;
		font-size: 12px;
		width: 190px;
		outline: none;

		&:focus {
			border-color: var(--color-primary);
			background: #fff;
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
	gap: 4px;

	.filter-pill {
		background: rgba(0, 0, 0, 0.04);
		border: 1px solid rgba(0, 0, 0, 0.06);
		color: var(--color-text-muted);
		font-size: 11px;
		font-weight: 500;
		padding: 4px 10px;
		border-radius: 9999px;
		cursor: pointer;
		display: flex;
		align-items: center;
		transition: all 0.15s ease;

		&:hover {
			background: rgba(0, 0, 0, 0.08);
			color: #1e293b;
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
			margin-left: 5px;
			background: rgba(0, 0, 0, 0.08);
			font-size: 10px;
			padding: 1px 5px;
			border-radius: 9999px;
		}
	}
}

.container-item-row {
	padding: 12px 16px;
	border-bottom: 1px solid rgba(0, 0, 0, 0.05);

	&:last-child {
		border-bottom: none;
	}
}

.container-avatar {
	width: 38px;
	height: 38px;
	border-radius: 10px;
	background: rgba(14, 165, 233, 0.1);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}

.status-pill {
	display: inline-flex;
	align-items: center;
	font-size: 11px;
	font-weight: 500;
	padding: 1px 7px;
	border-radius: 9999px;

	.dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		margin-right: 4px;
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
	font-size: 11px;
	background: rgba(0, 0, 0, 0.04);
	color: var(--color-text-muted);
	padding: 2px 5px;
	border-radius: 4px;
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
	background: rgba(255, 255, 255, 0.5);
	border: 1px dashed rgba(0, 0, 0, 0.12);
	border-radius: 12px;
}
</style>
