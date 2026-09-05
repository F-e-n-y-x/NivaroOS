<template>
	<section class="settings-section">
		<div class="section-header is-flex is-align-items-center is-justify-content-between mb-4">
			<div>
				<h2 class="section-title mb-1">{{ $t('Scheduled Tasks & Automation') }}</h2>
				<p class="section-subtitle text-muted is-size-7">{{ $t('Automate cloud backups, directory sync, VM power cycles, container restarts, and maintenance schedules.') }}</p>
			</div>
			<div class="header-actions is-flex is-align-items-center">
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					@click="openCreateModal()"
				>
					<i class="mdi mdi-plus mr-1"></i>
					{{ $t('New Task') }}
				</b-button>
			</div>
		</div>

		<!-- ==================== STATS ROW ==================== -->
		<div class="columns is-multiline is-mobile mb-3">
			<div class="column is-4-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-purple">
						<i class="mdi mdi-clock-check-outline"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ activeTasksCount }}</div>
						<div class="stat-lbl">{{ $t('Active Schedules') }}</div>
					</div>
				</div>
			</div>
			<div class="column is-4-desktop is-6-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-blue">
						<i class="mdi mdi-format-list-bulleted"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val">{{ tasks.length }}</div>
						<div class="stat-lbl">{{ $t('Total Tasks') }}</div>
					</div>
				</div>
			</div>
			<div class="column is-4-desktop is-12-mobile">
				<div class="stat-card">
					<div class="stat-icon bg-green">
						<i class="mdi mdi-play-network-outline"></i>
					</div>
					<div class="stat-info">
						<div class="stat-val one-line" :title="nextUpcomingTaskTitle">{{ nextUpcomingTaskTitle }}</div>
						<div class="stat-lbl">{{ nextUpcomingTaskSubtitle }}</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== QUICK AUTOMATION TEMPLATES ==================== -->
		<h3 class="setting-card-title">{{ $t('Quick Automation Templates') }}</h3>
		<div class="columns is-multiline is-mobile mb-4">
			<div v-for="tpl in quickTemplates" :key="tpl.id" class="column is-3-desktop is-6-tablet is-12-mobile">
				<div class="template-card" @click="applyTemplate(tpl)">
					<div class="template-icon mb-2" :style="{ color: tpl.color, background: tpl.bg }">
						<i :class="['mdi', 'mdi-' + tpl.icon]"></i>
					</div>
					<div class="template-title">{{ tpl.name }}</div>
					<div class="template-desc is-flex is-align-items-center mt-1">
						<i class="mdi mdi-clock-outline mr-1"></i>
						<span>{{ tpl.scheduleText }}</span>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== SCHEDULED TASKS LIST ==================== -->
		<div class="is-flex is-align-items-center is-justify-content-between mb-3">
			<h3 class="setting-card-title mb-0">{{ $t('Configured Tasks') }}</h3>
			<div class="is-flex is-align-items-center">
				<b-button
					rounded
					size="is-small"
					:loading="loadingTasks"
					@click="fetchTasks"
					:title="$t('Refresh tasks')"
					class="mr-2"
				>
					<i class="mdi mdi-refresh"></i>
				</b-button>
			</div>
		</div>

		<div v-if="loadingTasks && !tasks.length" class="p-6 has-text-centered text-muted">
			<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
			<div class="mt-2 is-size-7">{{ $t('Loading scheduled tasks...') }}</div>
		</div>

		<div v-else-if="!tasks.length" class="empty-card has-text-centered p-6 mb-4">
			<i class="mdi mdi-calendar-clock is-size-1 text-muted mb-2"></i>
			<div class="is-size-6 font-medium text-muted">{{ $t('No scheduled tasks yet') }}</div>
			<div class="is-size-7 text-muted mt-1 mb-4">{{ $t('Create a custom cron schedule or pick one of the quick automation templates above.') }}</div>
			<b-button rounded type="is-primary" size="is-small" @click="openCreateModal()">
				<i class="mdi mdi-plus mr-1"></i>
				{{ $t('Create First Scheduled Task') }}
			</b-button>
		</div>

		<div v-else class="setting-card p-0 mb-4">
			<div
				v-for="t in tasks"
				:key="t.id"
				class="setting-row task-item-row is-align-items-center"
			>
				<!-- Icon -->
				<div class="task-avatar mr-3" :class="'type-' + t.type">
					<i :class="['mdi', 'mdi-' + getTypeIcon(t.type)]"></i>
				</div>

				<!-- Info -->
				<div class="row-label">
					<div class="setting-title is-flex is-align-items-center is-flex-wrap-wrap">
						<span class="task-name mr-2">{{ t.name }}</span>
						<span class="type-pill mr-2" :class="'type-' + t.type">
							{{ formatTypeLabel(t.type) }}
						</span>
						<span class="target-pill mr-2">
							<span class="target-name">{{ getTargetDisplay(t) }}</span>
							<span class="mx-1">&middot;</span>
							<span>{{ formatActionLabel(t.action || t.sync_mode) }}</span>
						</span>
					</div>
					<div class="setting-desc is-flex is-align-items-center is-flex-wrap-wrap mt-1">
						<span class="cron-pill mr-3">
							<i class="mdi mdi-clock-outline mr-1"></i>
							{{ humanizeCron(t.cron) }}
						</span>
						<span v-if="t.next_run" class="text-muted is-size-7 mr-3">
							{{ $t('Next') }}: {{ formatNextRun(t.next_run) }}
						</span>
						<span v-if="t.last_run" class="is-size-7 is-flex is-align-items-center mr-3">
							<span class="status-dot mr-1" :class="t.last_status === 'success' ? 'bg-success' : 'bg-danger'"></span>
							<span class="text-muted">{{ $t('Last run') }}: {{ formatTimeAgo(t.last_run) }} ({{ t.last_status || 'done' }})</span>
						</span>
					</div>
				</div>

				<!-- Controls -->
				<div class="row-control is-flex is-align-items-center">
					<!-- Enable toggle -->
					<div class="mr-3" :title="t.enabled ? $t('Task is enabled') : $t('Task is disabled')">
						<b-switch
							v-model="t.enabled"
							size="is-small"
							type="is-primary"
							@input="toggleTask(t)"
						></b-switch>
					</div>

					<!-- Actions -->
					<div class="buttons are-small mb-0">
						<!-- Run Now -->
						<b-button
							rounded
							size="is-small"
							:loading="runningId === t.id"
							:disabled="runningId === t.id"
							@click="runNow(t)"
							:title="$t('Run immediately now')"
						>
							<i class="mdi mdi-play mr-1"></i>
							{{ $t('Run Now') }}
						</b-button>

						<!-- View Output Log -->
						<b-button
							rounded
							size="is-small"
							@click="viewTaskLog(t)"
							:title="$t('View last execution output')"
							:disabled="!t.last_output && !t.last_run"
						>
							<i class="mdi mdi-text-box-outline"></i>
						</b-button>

						<!-- Edit in Window -->
						<b-button
							rounded
							size="is-small"
							@click="openEditModal(t)"
							:title="$t('Edit schedule')"
						>
							<i class="mdi mdi-pencil-outline"></i>
						</b-button>

						<!-- Delete -->
						<b-button
							rounded
							size="is-small"
							class="has-text-danger"
							@click="confirmDelete(t)"
							:title="$t('Delete task')"
						>
							<i class="mdi mdi-trash-can-outline"></i>
						</b-button>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== IN-WINDOW DELETE CONFIRMATION ==================== -->
		<div v-if="targetDeleteTask" class="window-overlay">
			<div class="window-overlay-backdrop" @click="targetDeleteTask = null"></div>
			<div class="window-overlay-card delete-card">
				<header class="window-overlay-head">
					<span class="window-overlay-title">
						<i class="mdi mdi-trash-can-outline mr-1 has-text-danger"></i>
						{{ $t('Delete Scheduled Task') }}
					</span>
					<button type="button" class="window-overlay-close" @click="targetDeleteTask = null">
						<b-icon icon="close" size="is-small"></b-icon>
					</button>
				</header>
				<div class="window-overlay-body">
					<p class="mb-1">
						{{ $t('Are you sure you want to delete scheduled task') }}
						<strong>"{{ targetDeleteTask.name }}"</strong>?
					</p>
					<p class="is-size-7 text-muted">{{ $t('This operation cannot be undone.') }}</p>
				</div>
				<footer class="window-overlay-foot">
					<b-button rounded size="is-small" @click="targetDeleteTask = null">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-danger" :loading="deletingTask" @click="performDeleteTask">
						<i class="mdi mdi-trash-can-outline mr-1"></i>
						{{ $t('Delete') }}
					</b-button>
				</footer>
			</div>
		</div>

		<!-- ==================== IN-WINDOW LOG VIEWER ==================== -->
		<div v-if="selectedLogTask" class="window-overlay">
			<div class="window-overlay-backdrop" @click="selectedLogTask = null"></div>
			<div class="window-overlay-card log-card">
				<header class="window-overlay-head">
					<span class="window-overlay-title">
						<i class="mdi mdi-text-box-outline mr-1 has-text-info"></i>
						{{ selectedLogTask.name }} &middot; {{ $t('Execution Output') }}
					</span>
					<button type="button" class="window-overlay-close" @click="selectedLogTask = null">
						<b-icon icon="close" size="is-small"></b-icon>
					</button>
				</header>
				<div class="window-overlay-body p-0">
					<div class="task-log-viewer scrollbars">
						<div class="log-meta p-3 is-flex is-align-items-center is-justify-content-between">
							<div>
								<span class="log-target-name mr-2">{{ selectedLogTask.target_name || selectedLogTask.name }}</span>
								<span class="text-muted is-size-7">Action: {{ selectedLogTask.action || selectedLogTask.type }}</span>
							</div>
							<div class="is-size-7 text-muted">
								{{ selectedLogTask.last_run }}
							</div>
						</div>
						<pre class="task-output-pre p-3">{{ selectedLogTask.last_output || $t('No output text recorded.') }}</pre>
					</div>
				</div>
				<footer class="window-overlay-foot">
					<b-button rounded size="is-small" @click="selectedLogTask = null">{{ $t('Close') }}</b-button>
				</footer>
			</div>
		</div>
	</section>
</template>

<script>
export const ROWS = [
	{ label: 'Scheduled Tasks' },
	{ label: 'Cron Jobs & Automation' },
	{ label: 'Cloud Storage Backup & Sync' },
	{ label: 'Automated VM Shutdown' },
	{ label: 'Container Auto-Restart' },
	{ label: 'System Maintenance Tasks' }
]

export default {
	name: 'scheduled-tasks-section',
	data() {
		return {
			tasks: [],
			loadingTasks: false,
			runningId: null,
			targetDeleteTask: null,
			deletingTask: false,
			selectedLogTask: null,
			targetVms: [],
			targetContainers: [],
			targetClouds: [],
			quickTemplates: [
				{
					id: 'cloud-backup-nightly',
					name: this.$t('Cloud Backup (Local → Cloud)'),
					type: 'backup',
					action: 'copy',
					direction: 'local_to_cloud',
					source_path: '/DATA/Documents',
					dest_path: '',
					cron: '0 2 * * *',
					scheduleText: this.$t('Every night at 2:00 AM'),
					icon: 'cloud-upload',
					color: '#2563eb',
					bg: 'rgba(37, 99, 235, 0.12)'
				},
				{
					id: 'cloud-sync-daily',
					name: this.$t('Cloud Sync (Cloud → Local)'),
					type: 'backup',
					action: 'sync',
					direction: 'cloud_to_local',
					source_path: '',
					dest_path: '/DATA/CloudSync',
					cron: '0 12 * * *',
					scheduleText: this.$t('Every day at 12:00 PM'),
					icon: 'cloud-sync',
					color: '#0ea5e9',
					bg: 'rgba(14, 165, 233, 0.12)'
				},
				{
					id: 'local-backup-weekly',
					name: this.$t('Weekly Local Snapshot / Archive'),
					type: 'backup',
					action: 'archive',
					direction: 'local_to_local',
					source_path: '/DATA',
					dest_path: '/DATA/Backup',
					cron: '0 3 * * 0',
					scheduleText: this.$t('Every Sunday at 3:00 AM'),
					icon: 'folder-zip-outline',
					color: '#10b981',
					bg: 'rgba(16, 185, 129, 0.12)'
				},
				{
					id: 'vm-shutdown',
					name: this.$t('Nightly VM Shutdown'),
					type: 'vm',
					action: 'stop',
					cron: '0 23 * * *',
					scheduleText: this.$t('Every night at 11:00 PM'),
					icon: 'power',
					color: '#f43f5e',
					bg: 'rgba(244, 63, 94, 0.12)'
				},
				{
					id: 'container-restart',
					name: this.$t('Weekly Container Restart'),
					type: 'container',
					action: 'restart',
					cron: '0 3 * * 0',
					scheduleText: this.$t('Every Sunday at 3:00 AM'),
					icon: 'docker',
					color: '#0284c7',
					bg: 'rgba(2, 132, 199, 0.12)'
				},
				{
					id: 'fstrim-maintenance',
					name: this.$t('SSD Trim & Cache Flush'),
					type: 'maintenance',
					action: 'fstrim',
					cron: '0 0 * * *',
					scheduleText: this.$t('Every midnight at 00:00'),
					icon: 'harddisk',
					color: '#f59e0b',
					bg: 'rgba(245, 158, 11, 0.12)'
				},
				{
					id: 'docker-prune',
					name: this.$t('Docker System Cleanup'),
					type: 'maintenance',
					action: 'docker_prune',
					cron: '0 4 * * 0',
					scheduleText: this.$t('Every Sunday at 4:00 AM'),
					icon: 'broom',
					color: '#8b5cf6',
					bg: 'rgba(139, 92, 246, 0.12)'
				}
			]
		}
	},
	computed: {
		activeTasksCount() {
			return this.tasks.filter(t => t.enabled).length
		},
		nextUpcomingTask() {
			const active = this.tasks.filter(t => t.enabled && t.next_run)
			if (!active.length) return null
			const sorted = [...active].sort((a, b) => new Date(a.next_run) - new Date(b.next_run))
			return sorted[0]
		},
		nextUpcomingTaskTitle() {
			return this.nextUpcomingTask ? this.nextUpcomingTask.name : this.$t('None Scheduled')
		},
		nextUpcomingTaskSubtitle() {
			if (!this.nextUpcomingTask) return this.$t('No active schedules')
			return this.formatNextRun(this.nextUpcomingTask.next_run)
		}
	},
	mounted() {
		this.fetchTasks()
		this.fetchTargets()
		this.$EventBus.$on('scheduled-tasks-changed', this.fetchTasks)
	},
	beforeDestroy() {
		this.$EventBus.$off('scheduled-tasks-changed', this.fetchTasks)
	},
	methods: {
		async fetchTasks() {
			this.loadingTasks = true
			try {
				const res = await this.$api.schedules.getSchedules()
				if (res && res.data && res.data.data) {
					this.tasks = res.data.data || []
				}
			} catch (err) {
				console.error('Failed to load scheduled tasks:', err)
			} finally {
				this.loadingTasks = false
			}
		},
		async fetchTargets() {
			try {
				const res = await this.$api.schedules.getTargets()
				if (res && res.data && res.data.data) {
					const data = res.data.data
					this.targetVms = data.vms || []
					this.targetContainers = data.containers || []
					this.targetClouds = data.clouds || []
				}
			} catch (err) {
				console.error('Failed to load targets:', err)
			}
		},
		openCreateModal(tpl = null) {
			// Opens a movable desktop window instead of blocking the entire screen
			this.$store.commit('OPEN_WINDOW', {
				id: 'scheduled-task-editor',
				title: tpl ? tpl.name : this.$t('New Automation Task'),
				component: 'ScheduledTaskWindow',
				props: { initialTemplate: tpl },
				width: 580,
				height: 640
			})
		},
		openEditModal(task) {
			// Opens a movable desktop window instead of blocking the entire screen
			this.$store.commit('OPEN_WINDOW', {
				id: 'scheduled-task-editor',
				title: this.$t('Edit Scheduled Task'),
				component: 'ScheduledTaskWindow',
				props: { task },
				width: 580,
				height: 640
			})
		},
		applyTemplate(tpl) {
			this.openCreateModal(tpl)
		},
		getTargetDisplay(t) {
			if (t.type === 'backup') {
				if (t.source_path && t.dest_path) {
					return `${t.source_path} → ${t.dest_path}`
				}
				if (t.target_name) return t.target_name
				return this.$t('Cloud & Storage Backup')
			}
			return t.target_name || t.target_id || t.type
		},
		async toggleTask(t) {
			try {
				await this.$api.schedules.toggleSchedule(t.id, t.enabled)
			} catch (err) {
				t.enabled = !t.enabled
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to update task status'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			}
		},
		async runNow(t) {
			this.runningId = t.id
			try {
				const res = await this.$api.schedules.runScheduleNow(t.id)
				this.$buefy.toast.open({
					message: res.data.message || `${t.name}: ${this.$t('Execution finished')}`,
					type: 'is-success',
					position: 'is-top',
					duration: 3000
				})
				await this.fetchTasks()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Execution failed'),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000
				})
			} finally {
				this.runningId = null
			}
		},
		viewTaskLog(t) {
			this.selectedLogTask = t
		},
		confirmDelete(t) {
			this.targetDeleteTask = t
		},
		async performDeleteTask() {
			if (!this.targetDeleteTask) return
			this.deletingTask = true
			try {
				await this.$api.schedules.deleteSchedule(this.targetDeleteTask.id)
				this.$buefy.toast.open({
					message: this.$t('Task deleted'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000
				})
				this.targetDeleteTask = null
				await this.fetchTasks()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to delete task'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.deletingTask = false
			}
		},
		getTypeIcon(type) {
			switch (type) {
				case 'backup': return 'cloud-sync'
				case 'vm': return 'monitor'
				case 'container': return 'docker'
				case 'maintenance': return 'wrench-outline'
				case 'command': return 'console'
				default: return 'calendar-clock'
			}
		},
		formatTypeLabel(type) {
			switch (type) {
				case 'backup': return this.$t('Backup & Sync')
				case 'vm': return 'VM'
				case 'container': return 'Container'
				case 'maintenance': return 'Maintenance'
				case 'command': return 'Script'
				default: return type
			}
		},
		formatActionLabel(action) {
			switch (action) {
				case 'copy': return this.$t('Incremental Backup')
				case 'sync': return this.$t('Exact Mirror')
				case 'archive': return this.$t('Compressed Archive')
				case 'move': return this.$t('Move & Archive')
				case 'rsync': return this.$t('Rsync Mirror')
				case 'stop': return this.$t('Shut Down')
				case 'start': return this.$t('Start')
				case 'restart': return this.$t('Restart')
				case 'reboot': return this.$t('Reboot')
				case 'update': return this.$t('Update & Recreate')
				case 'fstrim': return 'fstrim'
				case 'drop_caches': return 'Drop Caches'
				case 'docker_prune': return 'Docker Prune'
				case 'disk_standby_check': return 'Disk Standby Check'
				case 'run_command': return this.$t('Execute Command')
				default: return action
			}
		},
		humanizeCron(cron) {
			if (!cron) return ''
			if (cron === '0 2 * * *') return this.$t('Every day at 2:00 AM')
			if (cron === '0 3 * * *') return this.$t('Every day at 3:00 AM')
			if (cron === '0 23 * * *') return this.$t('Every day at 11:00 PM')
			if (cron === '0 0 * * *') return this.$t('Every day at Midnight (00:00)')
			if (cron === '0 12 * * *') return this.$t('Every day at Noon (12:00 PM)')
			if (cron === '0 3 * * 0') return this.$t('Every Sunday at 3:00 AM')
			if (cron === '0 4 * * 0') return this.$t('Every Sunday at 4:00 AM')
			if (cron === '0 * * * *') return this.$t('Every hour')
			if (cron === '*/30 * * * *') return this.$t('Every 30 minutes')
			if (cron === '0 0 1 * *') return this.$t('Monthly (1st at Midnight)')
			return `Cron: ${cron}`
		},
		formatNextRun(dateStr) {
			if (!dateStr) return ''
			const d = new Date(dateStr)
			if (isNaN(d.getTime())) return dateStr
			const now = new Date()
			const diffMin = Math.round((d - now) / (1000 * 60))
			const diffHours = Math.round((d - now) / (1000 * 3600))
			const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
			if (diffMin <= 0) return `${this.$t('Due now')} (${timeStr})`
			if (diffMin < 60) return `${this.$t('in')} ${diffMin} ${diffMin === 1 ? this.$t('min') : this.$t('mins')} (${timeStr})`
			if (diffHours < 24) {
				return `${this.$t('in')} ${diffHours} ${diffHours === 1 ? this.$t('hour') : this.$t('hours')} (${timeStr})`
			}
			return `${d.toLocaleString([], { month: 'short', day: 'numeric' })} at ${timeStr}`
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

		&.bg-purple {
			background: rgba(139, 92, 246, 0.12);
			color: #8b5cf6;
		}
		&.bg-blue {
			background: rgba(37, 99, 235, 0.12);
			color: #2563eb;
		}
		&.bg-green {
			background: rgba(16, 185, 129, 0.12);
			color: #10b981;
		}
	}

	.stat-val {
		font-size: 15px;
		font-weight: 500;
		line-height: 1.2;
		color: #18181b;
	}

	.stat-lbl {
		font-size: 11px;
		color: #71717a;
	}
}

.template-card {
	display: flex;
	flex-direction: column;
	padding: 12px 14px;
	background: rgba(255, 255, 255, 0.7);
	border: 1px solid rgba(0, 0, 0, 0.07);
	border-radius: 12px;
	cursor: pointer;
	height: 100%;
	transition: all 0.15s ease;

	&:hover {
		background: #ffffff;
		border-color: rgba(37, 99, 235, 0.3);
		box-shadow: 0 4px 14px rgba(0, 0, 0, 0.05);
		transform: translateY(-2px);
	}

	.template-icon {
		width: 34px;
		height: 34px;
		border-radius: 8px;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 18px;
		flex-shrink: 0;
	}

	.template-title {
		font-size: 13px;
		font-weight: 500;
		color: #18181b;
		line-height: 1.35;
	}

	.template-desc {
		font-size: 11px;
		color: #71717a;
		line-height: 1.3;
	}
}

.task-item-row {
	padding: 12px 16px;
	border-bottom: 1px solid rgba(0, 0, 0, 0.05);

	&:last-child {
		border-bottom: none;
	}
}

.task-name {
	font-size: 0.85rem;
	font-weight: 500;
	color: #18181b;
}

.target-name {
	font-weight: 500;
}

.log-target-name {
	font-weight: 500;
	color: #f4f4f5;
}

.font-medium {
	font-weight: 500;
}

.task-avatar {
	width: 40px;
	height: 40px;
	border-radius: 10px;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 20px;
	flex-shrink: 0;

	&.type-backup {
		background: rgba(37, 99, 235, 0.1);
		color: #2563eb;
	}
	&.type-vm {
		background: rgba(244, 63, 94, 0.1);
		color: #f43f5e;
	}
	&.type-container {
		background: rgba(14, 165, 233, 0.1);
		color: #0ea5e9;
	}
	&.type-maintenance {
		background: rgba(245, 158, 11, 0.1);
		color: #f59e0b;
	}
	&.type-command {
		background: rgba(139, 92, 246, 0.1);
		color: #8b5cf6;
	}
}

.type-pill {
	font-size: 10px;
	font-weight: 500;
	padding: 1px 6px;
	border-radius: 4px;
	text-transform: uppercase;
	letter-spacing: 0.5px;

	&.type-backup {
		background: rgba(37, 99, 235, 0.12);
		color: #2563eb;
	}
	&.type-vm {
		background: rgba(244, 63, 94, 0.12);
		color: #f43f5e;
	}
	&.type-container {
		background: rgba(14, 165, 233, 0.12);
		color: #0ea5e9;
	}
	&.type-maintenance {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
	}
	&.type-command {
		background: rgba(139, 92, 246, 0.12);
		color: #8b5cf6;
	}
}

.target-pill {
	font-size: 11px;
	padding: 1px 8px;
	background: rgba(0, 0, 0, 0.05);
	border-radius: 9999px;
	color: #3f3f46;
	max-width: 320px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.cron-pill {
	font-size: 11px;
	color: #2563eb;
	font-weight: 500;
}

.status-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	display: inline-block;

	&.bg-success {
		background: #10b981;
	}
	&.bg-danger {
		background: #ef4444;
	}
}

/* In-Window Overlay & Dialogs */
.window-overlay {
	position: absolute;
	inset: 0;
	z-index: 2000;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: 1rem;
}

.window-overlay-backdrop {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.25);
	backdrop-filter: blur(2px);
}

.window-overlay-card {
	position: relative;
	background: #ffffff;
	border-radius: 12px;
	box-shadow: 0 16px 40px rgba(0, 0, 0, 0.22);
	max-height: calc(100% - 2rem);
	max-width: calc(100% - 2rem);
	display: flex;
	flex-direction: column;
	overflow: hidden;
	border: 1px solid rgba(0, 0, 0, 0.08);

	&.delete-card {
		width: 24rem;
	}

	&.log-card {
		width: 40rem;
	}
}

.window-overlay-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
	background: #ffffff;
}

.window-overlay-title {
	font-size: 0.875rem;
	font-weight: 600;
	color: #0f172a;
	display: flex;
	align-items: center;
}

.window-overlay-close {
	border: none;
	background: transparent;
	color: #94a3b8;
	cursor: pointer;
	padding: 2px;
	border-radius: 4px;
	display: flex;
	align-items: center;

	&:hover {
		color: #0f172a;
		background: rgba(0, 0, 0, 0.05);
	}
}

.window-overlay-body {
	padding: 1rem;
	overflow-y: auto;
	font-size: 0.85rem;
	color: #334155;
	line-height: 1.45;
}

.window-overlay-foot {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.5rem;
	padding: 0.75rem 1rem;
	border-top: 1px solid rgba(0, 0, 0, 0.06);
	background: #ffffff;
}

.task-log-viewer {
	background: #121214;
	color: #e4e4e7;
	max-height: 380px;
	overflow-y: auto;

	.log-meta {
		border-bottom: 1px solid rgba(255, 255, 255, 0.08);
		background: #18181b;
	}

	.task-output-pre {
		background: transparent;
		color: #a1a1aa;
		font-family: monospace;
		font-size: 12px;
		white-space: pre-wrap;
		word-break: break-all;
		margin: 0;
	}
}

.empty-card {
	background: rgba(255, 255, 255, 0.5);
	border: 1px dashed rgba(0, 0, 0, 0.12);
	border-radius: 12px;
}
</style>
