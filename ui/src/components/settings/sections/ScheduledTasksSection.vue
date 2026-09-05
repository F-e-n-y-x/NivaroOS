<template>
	<section class="settings-section">
		<div class="section-header is-flex is-align-items-center is-justify-content-between mb-4">
			<div>
				<h2 class="section-title mb-1">{{ $t('Scheduled Tasks & Automation') }}</h2>
				<p class="section-subtitle text-muted is-size-7">{{ $t('Automate periodic operations like VM power states, container updates, backups, system maintenance, and custom cron commands.') }}</p>
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
							<span class="target-name">{{ t.target_name || t.target_id || t.type }}</span>
							<span class="mx-1">&middot;</span>
							<span>{{ formatActionLabel(t.action) }}</span>
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

						<!-- Edit -->
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

		<!-- ==================== CREATE / EDIT MODAL ==================== -->
		<b-modal
			v-model="showEditModal"
			has-modal-card
			trap-focus
			:can-cancel="['escape', 'x']"
			aria-modal
		>
			<div class="modal-card schedule-modal-card">
				<header class="modal-card-head">
					<p class="modal-card-title is-size-6 font-medium">
						<i class="mdi mdi-calendar-clock mr-2 has-text-primary"></i>
						{{ editingTask.id ? $t('Edit Scheduled Task') : $t('Create Scheduled Task') }}
					</p>
					<button type="button" class="delete" @click="showEditModal = false" />
				</header>

				<section class="modal-card-body">
					<!-- 1. Task Name & Type -->
					<b-field :label="$t('Task Name')">
						<b-input
							v-model="editingTask.name"
							:placeholder="$t('e.g. Shut down gaming VM nightly')"
							required
						></b-input>
					</b-field>

					<div class="columns mb-0">
						<div class="column is-6">
							<b-field :label="$t('Task Type')">
								<b-select v-model="editingTask.type" expanded @input="onTypeChange">
									<option value="vm">{{ $t('Virtual Machine (VM)') }}</option>
									<option value="container">{{ $t('Docker Container') }}</option>
									<option value="maintenance">{{ $t('System Maintenance') }}</option>
									<option value="command">{{ $t('Custom Bash Command') }}</option>
								</b-select>
							</b-field>
						</div>

						<div class="column is-6">
							<b-field :label="$t('Action')">
								<!-- VM Actions -->
								<b-select v-if="editingTask.type === 'vm'" v-model="editingTask.action" expanded>
									<option value="stop">{{ $t('Shut Down (ACPI Poweroff)') }}</option>
									<option value="start">{{ $t('Start VM') }}</option>
									<option value="reboot">{{ $t('Reboot VM') }}</option>
									<option value="force_off">{{ $t('Force Power Off') }}</option>
								</b-select>

								<!-- Container Actions -->
								<b-select v-else-if="editingTask.type === 'container'" v-model="editingTask.action" expanded>
									<option value="restart">{{ $t('Restart Container') }}</option>
									<option value="stop">{{ $t('Stop Container') }}</option>
									<option value="start">{{ $t('Start Container') }}</option>
									<option value="update">{{ $t('Check Update & Recreate') }}</option>
								</b-select>

								<!-- Maintenance Actions -->
								<b-select v-else-if="editingTask.type === 'maintenance'" v-model="editingTask.action" expanded>
									<option value="fstrim">{{ $t('SSD / Disk fstrim (TRIM)') }}</option>
									<option value="drop_caches">{{ $t('Drop Memory Page Caches (sync & drop)') }}</option>
									<option value="docker_prune">{{ $t('Docker Prune Unused Objects') }}</option>
									<option value="disk_standby_check">{{ $t('Verify Disk Standby State') }}</option>
								</b-select>

								<!-- Command Action -->
								<b-input v-else v-model="editingTask.action" disabled placeholder="run_command"></b-input>
							</b-field>
						</div>
					</div>

					<!-- 2. Target Selector -->
					<!-- For VM: select target VM -->
					<b-field v-if="editingTask.type === 'vm'" :label="$t('Target Virtual Machine')">
						<b-select v-model="editingTask.target_id" expanded @input="onVmTargetSelected">
							<option v-for="vm in targetVms" :key="vm.id" :value="vm.id">
								{{ vm.name }} ({{ vm.id }}) - {{ vm.state }}
							</option>
						</b-select>
					</b-field>

					<!-- For Container: select target Container -->
					<b-field v-else-if="editingTask.type === 'container'" :label="$t('Target Docker Container')">
						<b-select v-model="editingTask.target_id" expanded @input="onContainerTargetSelected">
							<option v-for="c in targetContainers" :key="c.id" :value="c.name || c.id">
								{{ c.name }} ({{ c.image }})
							</option>
						</b-select>
					</b-field>

					<!-- For Custom Command: bash script input -->
					<b-field v-else-if="editingTask.type === 'command'" :label="$t('Bash Command / Shell Script')">
						<b-input
							v-model="editingTask.command"
							type="textarea"
							placeholder="echo 'Running nightly backup...' && rsync -av /DATA /BACKUP"
							rows="3"
						></b-input>
					</b-field>

					<!-- 3. Schedule Builder -->
					<b-field :label="$t('Cron Schedule')">
						<div class="schedule-builder-box">
							<div class="select is-small mb-2 is-fullwidth">
								<select v-model="modalPresetSchedule" @change="onModalPresetChange">
									<option value="0 23 * * *">{{ $t('Every night at 11:00 PM (23:00)') }}</option>
									<option value="0 0 * * *">{{ $t('Every night at Midnight (00:00)') }}</option>
									<option value="0 3 * * *">{{ $t('Every night at 3:00 AM') }}</option>
									<option value="0 4 * * *">{{ $t('Every night at 4:00 AM') }}</option>
									<option value="0 3 * * 0">{{ $t('Every Sunday at 3:00 AM (Weekly)') }}</option>
									<option value="0 * * * *">{{ $t('Every hour') }}</option>
									<option value="*/30 * * * *">{{ $t('Every 30 minutes') }}</option>
									<option value="0 0 1 * *">{{ $t('Monthly (1st day of month at midnight)') }}</option>
									<option value="custom">{{ $t('Custom Cron Expression') }}</option>
								</select>
							</div>

							<div v-if="modalPresetSchedule === 'custom'" class="cron-input-row is-flex is-align-items-center mt-2">
								<b-input
									v-model="editingTask.cron"
									placeholder="0 23 * * *"
									size="is-small"
									class="mr-2"
								></b-input>
								<span class="is-size-7 text-muted font-mono">(min hour dom month dow)</span>
							</div>

							<div class="cron-preview mt-2 is-size-7 text-muted is-flex is-align-items-center">
								<i class="mdi mdi-information-outline mr-1"></i>
								<span>{{ humanizeCron(editingTask.cron) }}</span>
							</div>
						</div>
					</b-field>

					<div class="is-flex is-align-items-center mt-3">
						<b-switch v-model="editingTask.enabled" type="is-primary" size="is-small">
							{{ $t('Enable schedule immediately') }}
						</b-switch>
					</div>
				</section>

				<footer class="modal-card-foot is-justify-content-flex-end">
					<b-button rounded size="is-small" @click="showEditModal = false">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-primary" :loading="savingTask" @click="saveTask">
						{{ editingTask.id ? $t('Save Changes') : $t('Create Task') }}
					</b-button>
				</footer>
			</div>
		</b-modal>

		<!-- ==================== LOG VIEWER MODAL ==================== -->
		<b-modal
			v-model="showLogModal"
			has-modal-card
			trap-focus
			aria-modal
		>
			<div class="modal-card log-modal-card">
				<header class="modal-card-head">
					<p class="modal-card-title is-size-6 font-medium">
						<i class="mdi mdi-text-box-outline mr-2"></i>
						{{ selectedLogTask ? selectedLogTask.name : $t('Execution Output') }}
					</p>
					<button type="button" class="delete" @click="showLogModal = false" />
				</header>
				<section class="modal-card-body p-0">
					<div class="task-log-viewer scrollbars">
						<div class="log-meta p-3 is-flex is-align-items-center is-justify-content-between">
							<div>
								<span class="log-target-name mr-2">{{ selectedLogTask ? selectedLogTask.target_name : '' }}</span>
								<span class="text-muted is-size-7">Action: {{ selectedLogTask ? selectedLogTask.action : '' }}</span>
							</div>
							<div class="is-size-7 text-muted">
								{{ selectedLogTask ? selectedLogTask.last_run : '' }}
							</div>
						</div>
						<pre class="task-output-pre p-3">{{ selectedLogTask ? (selectedLogTask.last_output || $t('No output text recorded.')) : '' }}</pre>
					</div>
				</section>
				<footer class="modal-card-foot is-justify-content-flex-end">
					<b-button rounded size="is-small" @click="showLogModal = false">{{ $t('Close') }}</b-button>
				</footer>
			</div>
		</b-modal>
	</section>
</template>

<script>
export const ROWS = [
	{ label: 'Scheduled Tasks' },
	{ label: 'Cron Jobs & Automation' },
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
			savingTask: false,
			showEditModal: false,
			showLogModal: false,
			selectedLogTask: null,
			editingTask: {
				id: '',
				name: '',
				type: 'vm',
				action: 'stop',
				target_id: '',
				target_name: '',
				command: '',
				cron: '0 23 * * *',
				enabled: true
			},
			modalPresetSchedule: '0 23 * * *',
			targetVms: [],
			targetContainers: [],
			targetMaintenance: [],
			quickTemplates: [
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
					color: '#0ea5e9',
					bg: 'rgba(14, 165, 233, 0.12)'
				},
				{
					id: 'fstrim-maintenance',
					name: this.$t('Nightly SSD Trim & Cache Flush'),
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
					this.targetMaintenance = data.maintenance || []
				}
			} catch (err) {
				console.error('Failed to load targets:', err)
			}
		},
		openCreateModal() {
			this.editingTask = {
				id: '',
				name: '',
				type: 'vm',
				action: 'stop',
				target_id: this.targetVms.length ? this.targetVms[0].id : '',
				target_name: this.targetVms.length ? this.targetVms[0].name : '',
				command: '',
				cron: '0 23 * * *',
				enabled: true
			}
			this.modalPresetSchedule = '0 23 * * *'
			this.showEditModal = true
		},
		openEditModal(task) {
			this.editingTask = JSON.parse(JSON.stringify(task))
			if (['0 23 * * *', '0 0 * * *', '0 3 * * *', '0 4 * * *', '0 3 * * 0', '0 * * * *', '*/30 * * * *', '0 0 1 * *'].includes(this.editingTask.cron)) {
				this.modalPresetSchedule = this.editingTask.cron
			} else {
				this.modalPresetSchedule = 'custom'
			}
			this.showEditModal = true
		},
		applyTemplate(tpl) {
			this.editingTask = {
				id: '',
				name: tpl.name,
				type: tpl.type,
				action: tpl.action,
				target_id: '',
				target_name: '',
				command: '',
				cron: tpl.cron,
				enabled: true
			}
			if (tpl.type === 'vm' && this.targetVms.length) {
				this.editingTask.target_id = this.targetVms[0].id
				this.editingTask.target_name = this.targetVms[0].name
			} else if (tpl.type === 'container' && this.targetContainers.length) {
				this.editingTask.target_id = this.targetContainers[0].name || this.targetContainers[0].id
				this.editingTask.target_name = this.targetContainers[0].name
			} else if (tpl.type === 'maintenance') {
				this.editingTask.target_id = tpl.action
				this.editingTask.target_name = tpl.name
			}
			this.modalPresetSchedule = tpl.cron
			this.showEditModal = true
		},
		onTypeChange(type) {
			if (type === 'vm') {
				this.editingTask.action = 'stop'
				if (this.targetVms.length) {
					this.editingTask.target_id = this.targetVms[0].id
					this.editingTask.target_name = this.targetVms[0].name
				}
			} else if (type === 'container') {
				this.editingTask.action = 'restart'
				if (this.targetContainers.length) {
					this.editingTask.target_id = this.targetContainers[0].name || this.targetContainers[0].id
					this.editingTask.target_name = this.targetContainers[0].name
				}
			} else if (type === 'maintenance') {
				this.editingTask.action = 'fstrim'
				this.editingTask.target_id = 'fstrim'
				this.editingTask.target_name = 'SSD / Disk TRIM'
			} else if (type === 'command') {
				this.editingTask.action = 'run_command'
				this.editingTask.target_id = 'bash'
				this.editingTask.target_name = 'Custom Command'
			}
		},
		onVmTargetSelected(id) {
			const vm = this.targetVms.find(v => v.id === id)
			if (vm) this.editingTask.target_name = vm.name
		},
		onContainerTargetSelected(id) {
			const c = this.targetContainers.find(item => item.name === id || item.id === id)
			if (c) this.editingTask.target_name = c.name
		},
		onModalPresetChange() {
			if (this.modalPresetSchedule !== 'custom') {
				this.editingTask.cron = this.modalPresetSchedule
			}
		},
		async saveTask() {
			if (!this.editingTask.name) {
				this.$buefy.toast.open({
					message: this.$t('Please enter a task name'),
					type: 'is-warning',
					position: 'is-top',
					duration: 2000
				})
				return
			}
			this.savingTask = true
			try {
				if (this.editingTask.id) {
					await this.$api.schedules.updateSchedule(this.editingTask.id, this.editingTask)
					this.$buefy.toast.open({
						message: this.$t('Task updated successfully'),
						type: 'is-success',
						position: 'is-top',
						duration: 2000
					})
				} else {
					await this.$api.schedules.createSchedule(this.editingTask)
					this.$buefy.toast.open({
						message: this.$t('Task created successfully'),
						type: 'is-success',
						position: 'is-top',
						duration: 2000
					})
				}
				this.showEditModal = false
				await this.fetchTasks()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to save task'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.savingTask = false
			}
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
			this.showLogModal = true
		},
		confirmDelete(t) {
			this.$buefy.dialog.confirm({
				title: this.$t('Delete Scheduled Task'),
				message: this.$t('Are you sure you want to delete this scheduled task: "{name}"?', { name: t.name }),
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				hasIcon: true,
				icon: 'trash-can-outline',
				iconPack: 'mdi',
				onConfirm: async () => {
					try {
						await this.$api.schedules.deleteSchedule(t.id)
						this.$buefy.toast.open({
							message: this.$t('Task deleted'),
							type: 'is-success',
							position: 'is-top',
							duration: 2000
						})
						await this.fetchTasks()
					} catch (err) {
						this.$buefy.toast.open({
							message: err.message || this.$t('Failed to delete task'),
							type: 'is-danger',
							position: 'is-top',
							duration: 3000
						})
					}
				}
			})
		},
		getTypeIcon(type) {
			switch (type) {
				case 'vm': return 'monitor'
				case 'container': return 'docker'
				case 'maintenance': return 'wrench-outline'
				case 'command': return 'console'
				default: return 'calendar-clock'
			}
		},
		formatTypeLabel(type) {
			switch (type) {
				case 'vm': return 'VM'
				case 'container': return 'Container'
				case 'maintenance': return 'System Maintenance'
				case 'command': return 'Script'
				default: return type
			}
		},
		formatActionLabel(action) {
			switch (action) {
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
			if (cron === '0 23 * * *') return this.$t('Every day at 11:00 PM')
			if (cron === '0 0 * * *') return this.$t('Every day at Midnight (00:00)')
			if (cron === '0 3 * * *') return this.$t('Every day at 3:00 AM')
			if (cron === '0 4 * * *') return this.$t('Every day at 4:00 AM')
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

.schedule-modal-card {
	width: 540px;
	max-width: 90vw;
}

.schedule-builder-box {
	background: rgba(0, 0, 0, 0.03);
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 8px;
	padding: 10px;
}

.log-modal-card {
	width: 680px;
	max-width: 95vw;
}

.task-log-viewer {
	background: #121214;
	color: #e4e4e7;
	max-height: 400px;
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
