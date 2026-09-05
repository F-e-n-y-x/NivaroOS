<!-- src/components/desktop/ScheduledTaskWindow.vue -->
<template>
	<div class="task-window">
		<div class="task-window-body scrollbars-light">
			<!-- Header Banner -->
			<div class="task-header-card mb-4">
				<div class="task-header-icon" :class="'type-' + form.type">
					<i :class="['mdi', 'mdi-' + getTypeIcon(form.type)]"></i>
				</div>
				<div class="task-header-info">
					<div class="task-header-title">{{ form.id ? $t('Edit Scheduled Task') : $t('New Automation Task') }}</div>
					<div class="task-header-desc">{{ getCategorySubtitle(form.type) }}</div>
				</div>
			</div>

			<!-- 1. General Task Settings -->
			<div class="setting-card mb-3">
				<div class="setting-row">
					<b-icon class="row-icon" icon="tag-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Task Name') }}</div>
					<div class="row-control">
						<b-input
							v-model="form.name"
							:placeholder="getDefaultNamePlaceholder()"
							size="is-small"
							required
						></b-input>
					</div>
				</div>

				<div class="setting-row">
					<b-icon class="row-icon" icon="layers-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Task Type') }}</div>
					<div class="row-control">
						<b-select v-model="form.type" size="is-small" expanded @input="onTypeChange">
							<option value="backup">{{ $t('Cloud Sync & Backup') }}</option>
							<option value="vm">{{ $t('Virtual Machine (VM)') }}</option>
							<option value="container">{{ $t('Docker Container') }}</option>
							<option value="maintenance">{{ $t('System Maintenance') }}</option>
							<option value="command">{{ $t('Custom Bash Script') }}</option>
						</b-select>
					</div>
				</div>
			</div>

			<!-- 2. Dynamic Type Config: BACKUP & CLOUD SYNC -->
			<div v-if="form.type === 'backup'" class="setting-card mb-3">
				<!-- Direction -->
				<div class="setting-row">
					<b-icon class="row-icon" icon="swap-horizontal-bold" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Sync Direction') }}</div>
					<div class="row-control">
						<div class="segmented-control">
							<button
								type="button"
								class="segmented-option"
								:class="{ active: form.direction === 'local_to_cloud' }"
								@click="setDirection('local_to_cloud')"
							>
								<i class="mdi mdi-cloud-upload-outline mr-1"></i>
								{{ $t('Local → Cloud') }}
							</button>
							<button
								type="button"
								class="segmented-option"
								:class="{ active: form.direction === 'cloud_to_local' }"
								@click="setDirection('cloud_to_local')"
							>
								<i class="mdi mdi-cloud-download-outline mr-1"></i>
								{{ $t('Cloud → Local') }}
							</button>
							<button
								type="button"
								class="segmented-option"
								:class="{ active: form.direction === 'local_to_local' }"
								@click="setDirection('local_to_local')"
							>
								<i class="mdi mdi-folder-sync-outline mr-1"></i>
								{{ $t('Local → Local') }}
							</button>
						</div>
					</div>
				</div>

				<!-- Sync Mode -->
				<div class="setting-row">
					<b-icon class="row-icon" icon="sync" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div>{{ $t('Operation Mode') }}</div>
						<div class="is-size-7 text-muted">{{ getSyncModeDesc(form.action) }}</div>
					</div>
					<div class="row-control">
						<b-select v-model="form.action" size="is-small" expanded @input="onSyncModeChange">
							<option value="copy">{{ $t('Incremental Backup (Copy)') }}</option>
							<option value="sync">{{ $t('Exact Mirror / Sync') }}</option>
							<option value="archive">{{ $t('Timestamped Archive (.tar.gz)') }}</option>
							<option value="move">{{ $t('Move / Archive (Delete source)') }}</option>
						</b-select>
					</div>
				</div>

				<!-- Source Path -->
				<div class="setting-row align-start">
					<b-icon class="row-icon" icon="folder-upload-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div>{{ $t('Source Path / Remote') }}</div>
						<div class="is-size-7 text-muted">{{ $t('Where data is read from') }}</div>
					</div>
					<div class="row-control path-control-col">
						<b-input
							v-model="form.source_path"
							:placeholder="form.direction === 'cloud_to_local' ? 'google_drive_drive_...:Documents' : '/DATA/Documents'"
							size="is-small"
						></b-input>

						<!-- Suggestion chips -->
						<div class="chips-row mt-1">
							<span class="chips-label">{{ $t('Presets') }}:</span>
							<template v-if="form.direction === 'cloud_to_local'">
								<button
									v-for="c in targetClouds"
									:key="c.remote"
									type="button"
									class="chip-btn"
									@click="form.source_path = c.remote + 'Documents'"
								>
									{{ c.name || c.remote }}
								</button>
							</template>
							<template v-else>
								<button
									v-for="lp in commonLocalPaths"
									:key="lp"
									type="button"
									class="chip-btn"
									@click="form.source_path = lp"
								>
									{{ lp }}
								</button>
							</template>
						</div>
					</div>
				</div>

				<!-- Destination Path -->
				<div class="setting-row align-start">
					<b-icon class="row-icon" icon="folder-download-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div>{{ $t('Destination Path / Remote') }}</div>
						<div class="is-size-7 text-muted">{{ $t('Where data is copied/backed up to') }}</div>
					</div>
					<div class="row-control path-control-col">
						<b-input
							v-model="form.dest_path"
							:placeholder="form.direction === 'local_to_cloud' ? 'google_drive_drive_...:NivaroOS-Backup' : '/DATA/Backup'"
							size="is-small"
						></b-input>

						<!-- Suggestion chips -->
						<div class="chips-row mt-1">
							<span class="chips-label">{{ $t('Presets') }}:</span>
							<template v-if="form.direction === 'local_to_cloud'">
								<button
									v-for="c in targetClouds"
									:key="c.remote"
									type="button"
									class="chip-btn"
									@click="form.dest_path = c.remote + 'NivaroOS-Backup'"
								>
									{{ c.name || c.remote }} (Backup)
								</button>
							</template>
							<template v-else>
								<button
									v-for="lp in ['/DATA/Backup', '/DATA/CloudSync', '/DATA/Documents']"
									:key="lp"
									type="button"
									class="chip-btn"
									@click="form.dest_path = lp"
								>
									{{ lp }}
								</button>
							</template>
						</div>
					</div>
				</div>

				<!-- Advanced Options (Collapsible) -->
				<div class="advanced-section mt-2 pt-2">
					<div
						class="is-flex is-align-items-center is-justify-content-between advanced-toggle"
						@click="showAdvanced = !showAdvanced"
					>
						<span class="is-size-7 text-muted font-medium">
							<i class="mdi mdi-tune mr-1"></i>
							{{ $t('Advanced Transfer Options (Bandwidth & Filters)') }}
						</span>
						<i :class="['mdi', showAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down', 'text-muted']"></i>
					</div>

					<div v-show="showAdvanced" class="mt-2">
						<b-field :label="$t('Extra Flags / Bandwidth Limit')">
							<b-input
								v-model="form.extra_args"
								placeholder="--bwlimit 10M --transfers 4 --fast-list"
								size="is-small"
							></b-input>
						</b-field>
					</div>
				</div>
			</div>

			<!-- 2. Dynamic Type Config: VIRTUAL MACHINE -->
			<div v-else-if="form.type === 'vm'" class="setting-card mb-3">
				<div class="setting-row">
					<b-icon class="row-icon" icon="monitor" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Target Virtual Machine') }}</div>
					<div class="row-control">
						<b-select v-model="form.target_id" size="is-small" expanded @input="onVmTargetSelected">
							<option v-for="vm in targetVms" :key="vm.name" :value="vm.name">
								{{ vm.name }} ({{ vm.state }})
							</option>
						</b-select>
					</div>
				</div>

				<div class="setting-row">
					<b-icon class="row-icon" icon="power" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Power Action') }}</div>
					<div class="row-control">
						<b-select v-model="form.action" size="is-small" expanded>
							<option value="stop">{{ $t('Shut Down (ACPI Poweroff)') }}</option>
							<option value="start">{{ $t('Start VM') }}</option>
							<option value="reboot">{{ $t('Reboot VM') }}</option>
							<option value="force_off">{{ $t('Force Power Off (destroy)') }}</option>
						</b-select>
					</div>
				</div>
			</div>

			<!-- 2. Dynamic Type Config: DOCKER CONTAINER -->
			<div v-else-if="form.type === 'container'" class="setting-card mb-3">
				<div class="setting-row">
					<b-icon class="row-icon" icon="docker" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Target Container') }}</div>
					<div class="row-control">
						<b-select v-model="form.target_id" size="is-small" expanded @input="onContainerTargetSelected">
							<option v-for="c in targetContainers" :key="c.id" :value="c.name || c.id">
								{{ c.name }} ({{ c.image }})
							</option>
						</b-select>
					</div>
				</div>

				<div class="setting-row">
					<b-icon class="row-icon" icon="cog-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Container Action') }}</div>
					<div class="row-control">
						<b-select v-model="form.action" size="is-small" expanded>
							<option value="restart">{{ $t('Restart Container') }}</option>
							<option value="stop">{{ $t('Stop Container') }}</option>
							<option value="start">{{ $t('Start Container') }}</option>
							<option value="update">{{ $t('Check Update & Recreate') }}</option>
						</b-select>
					</div>
				</div>
			</div>

			<!-- 2. Dynamic Type Config: MAINTENANCE -->
			<div v-else-if="form.type === 'maintenance'" class="setting-card mb-3">
				<div class="setting-row">
					<b-icon class="row-icon" icon="wrench-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Maintenance Operation') }}</div>
					<div class="row-control">
						<b-select v-model="form.action" size="is-small" expanded @input="onMaintenanceActionChange">
							<option value="fstrim">{{ $t('SSD / Disk fstrim (TRIM)') }}</option>
							<option value="drop_caches">{{ $t('Drop Memory Page Caches (sync & drop)') }}</option>
							<option value="docker_prune">{{ $t('Docker System Prune Unused Objects') }}</option>
							<option value="disk_standby_check">{{ $t('Verify Disk Standby State') }}</option>
						</b-select>
					</div>
				</div>
			</div>

			<!-- 2. Dynamic Type Config: COMMAND -->
			<div v-else-if="form.type === 'command'" class="setting-card mb-3">
				<div class="setting-row align-start">
					<b-icon class="row-icon" icon="console" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div>{{ $t('Bash Command / Shell Script') }}</div>
						<div class="is-size-7 text-muted">{{ $t('Executes in non-interactive root shell') }}</div>
					</div>
					<div class="row-control path-control-col">
						<b-input
							v-model="form.command"
							type="textarea"
							placeholder="echo 'Backing up configs...' && rsync -av /DATA/AppData /DATA/Backup/AppData"
							rows="3"
							size="is-small"
						></b-input>
					</div>
				</div>
			</div>

			<!-- 3. Schedule Builder -->
			<div class="setting-card mb-3">
				<div class="setting-row align-start">
					<b-icon class="row-icon" icon="clock-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">
						<div>{{ $t('Execution Schedule') }}</div>
						<div class="is-size-7 text-primary font-medium mt-1">
							<i class="mdi mdi-calendar-clock mr-1"></i>
							{{ humanizeCron(form.cron) }}
						</div>
					</div>
					<div class="row-control path-control-col">
						<b-select v-model="presetSchedule" size="is-small" expanded @input="onPresetScheduleChange">
							<option value="0 2 * * *">{{ $t('Nightly at 2:00 AM (02:00)') }}</option>
							<option value="0 3 * * *">{{ $t('Nightly at 3:00 AM (03:00)') }}</option>
							<option value="0 23 * * *">{{ $t('Nightly at 11:00 PM (23:00)') }}</option>
							<option value="0 0 * * *">{{ $t('Nightly at Midnight (00:00)') }}</option>
							<option value="0 12 * * *">{{ $t('Daily at Noon (12:00 PM)') }}</option>
							<option value="0 3 * * 0">{{ $t('Weekly (Every Sunday at 3:00 AM)') }}</option>
							<option value="0 4 * * 0">{{ $t('Weekly (Every Sunday at 4:00 AM)') }}</option>
							<option value="0 * * * *">{{ $t('Every hour') }}</option>
							<option value="*/30 * * * *">{{ $t('Every 30 minutes') }}</option>
							<option value="0 0 1 * *">{{ $t('Monthly (1st day of month)') }}</option>
							<option value="custom">{{ $t('Custom Cron Expression') }}</option>
						</b-select>

						<div v-if="presetSchedule === 'custom'" class="cron-custom-row is-flex is-align-items-center mt-2">
							<b-input
								v-model="form.cron"
								placeholder="0 2 * * *"
								size="is-small"
								class="mr-2"
							></b-input>
							<span class="is-size-7 text-muted font-mono">(min hour dom month dow)</span>
						</div>
					</div>
				</div>

				<div class="setting-row">
					<b-icon class="row-icon" icon="toggle-switch-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Enable Schedule') }}</div>
					<div class="row-control">
						<b-switch v-model="form.enabled" size="is-small" type="is-primary"></b-switch>
					</div>
				</div>
			</div>
		</div>

		<!-- Footer -->
		<footer class="task-window-foot">
			<b-button rounded size="is-small" @click="close">
				{{ $t('Cancel') }}
			</b-button>
			<b-button
				rounded
				size="is-small"
				type="is-primary"
				:loading="saving"
				@click="saveTask"
			>
				<i class="mdi mdi-check mr-1"></i>
				{{ form.id ? $t('Save Changes') : $t('Create Task') }}
			</b-button>
		</footer>
	</div>
</template>

<script>
export default {
	name: 'ScheduledTaskWindow',
	props: {
		task: {
			type: Object,
			default: null
		},
		initialTemplate: {
			type: Object,
			default: null
		}
	},
	data() {
		return {
			saving: false,
			showAdvanced: false,
			presetSchedule: '0 2 * * *',
			targetVms: [],
			targetContainers: [],
			targetClouds: [],
			commonLocalPaths: ['/DATA', '/DATA/Documents', '/DATA/Media', '/DATA/AppData', '/DATA/Gallery', '/DATA/Backup'],
			form: {
				id: '',
				name: '',
				type: 'backup',
				action: 'copy',
				direction: 'local_to_cloud',
				sync_mode: 'copy',
				source_path: '/DATA/Documents',
				dest_path: '',
				extra_args: '',
				target_id: '',
				target_name: '',
				command: '',
				cron: '0 2 * * *',
				enabled: true
			}
		}
	},
	async created() {
		await this.fetchTargets()
		this.initFormData()
	},
	methods: {
		async fetchTargets() {
			try {
				const res = await this.$api.schedules.getTargets()
				if (res && res.data && res.data.data) {
					const data = res.data.data
					this.targetVms = data.vms || []
					this.targetContainers = data.containers || []
					this.targetClouds = data.clouds || []
					if (data.local_paths && data.local_paths.length) {
						this.commonLocalPaths = data.local_paths
					}
				}
			} catch (err) {
				console.error('Failed to load targets for task window:', err)
			}
		},
		initFormData() {
			if (this.task) {
				this.form = JSON.parse(JSON.stringify(this.task))
				if (!this.form.direction) {
					if (this.form.source_path && this.form.source_path.includes(':')) {
						this.form.direction = 'cloud_to_local'
					} else if (this.form.dest_path && this.form.dest_path.includes(':')) {
						this.form.direction = 'local_to_cloud'
					} else {
						this.form.direction = 'local_to_local'
					}
				}
				if (['0 2 * * *', '0 3 * * *', '0 23 * * *', '0 0 * * *', '0 12 * * *', '0 3 * * 0', '0 4 * * 0', '0 * * * *', '*/30 * * * *', '0 0 1 * *'].includes(this.form.cron)) {
					this.presetSchedule = this.form.cron
				} else {
					this.presetSchedule = 'custom'
				}
				return
			}

			if (this.initialTemplate) {
				const tpl = this.initialTemplate
				this.form = {
					id: '',
					name: tpl.name || '',
					type: tpl.type || 'backup',
					action: tpl.action || 'copy',
					direction: tpl.direction || (tpl.type === 'backup' ? 'local_to_cloud' : 'local_to_local'),
					sync_mode: tpl.action || 'copy',
					source_path: tpl.source_path || (tpl.type === 'backup' ? '/DATA/Documents' : ''),
					dest_path: tpl.dest_path || '',
					extra_args: tpl.extra_args || '',
					target_id: tpl.target_id || '',
					target_name: tpl.target_name || '',
					command: tpl.command || '',
					cron: tpl.cron || '0 2 * * *',
					enabled: true
				}

				// If destination empty and local_to_cloud, pick first cloud remote
				if (this.form.type === 'backup' && !this.form.dest_path && this.targetClouds.length) {
					this.form.dest_path = this.targetClouds[0].remote + 'NivaroOS-Backup'
				}

				if (['0 2 * * *', '0 3 * * *', '0 23 * * *', '0 0 * * *', '0 12 * * *', '0 3 * * 0', '0 4 * * 0', '0 * * * *', '*/30 * * * *', '0 0 1 * *'].includes(this.form.cron)) {
					this.presetSchedule = this.form.cron
				} else {
					this.presetSchedule = 'custom'
				}
				return
			}

			// Default fresh task: Cloud backup
			if (this.targetClouds.length) {
				this.form.dest_path = this.targetClouds[0].remote + 'NivaroOS-Backup'
			} else {
				this.form.dest_path = '/DATA/Backup'
			}
		},
		onTypeChange(type) {
			if (type === 'backup') {
				this.form.action = 'copy'
				this.form.direction = 'local_to_cloud'
				this.form.source_path = '/DATA/Documents'
				if (this.targetClouds.length) {
					this.form.dest_path = this.targetClouds[0].remote + 'NivaroOS-Backup'
				} else {
					this.form.dest_path = '/DATA/Backup'
				}
				this.presetSchedule = '0 2 * * *'
				this.form.cron = '0 2 * * *'
			} else if (type === 'vm') {
				this.form.action = 'stop'
				if (this.targetVms.length) {
					this.form.target_id = this.targetVms[0].name
					this.form.target_name = this.targetVms[0].name
				}
				this.presetSchedule = '0 23 * * *'
				this.form.cron = '0 23 * * *'
			} else if (type === 'container') {
				this.form.action = 'restart'
				if (this.targetContainers.length) {
					this.form.target_id = this.targetContainers[0].name || this.targetContainers[0].id
					this.form.target_name = this.targetContainers[0].name
				}
				this.presetSchedule = '0 3 * * 0'
				this.form.cron = '0 3 * * 0'
			} else if (type === 'maintenance') {
				this.form.action = 'fstrim'
				this.form.target_id = 'fstrim'
				this.form.target_name = 'SSD / Disk TRIM'
				this.presetSchedule = '0 0 * * *'
				this.form.cron = '0 0 * * *'
			} else if (type === 'command') {
				this.form.action = 'run_command'
				this.form.target_id = 'bash'
				this.form.target_name = 'Custom Command'
			}
		},
		setDirection(dir) {
			this.form.direction = dir
			if (dir === 'local_to_cloud') {
				this.form.source_path = '/DATA/Documents'
				if (this.targetClouds.length) {
					this.form.dest_path = this.targetClouds[0].remote + 'NivaroOS-Backup'
				}
			} else if (dir === 'cloud_to_local') {
				if (this.targetClouds.length) {
					this.form.source_path = this.targetClouds[0].remote + 'Documents'
				}
				this.form.dest_path = '/DATA/CloudSync'
			} else if (dir === 'local_to_local') {
				this.form.source_path = '/DATA'
				this.form.dest_path = '/DATA/Backup'
			}
		},
		onSyncModeChange(mode) {
			this.form.sync_mode = mode
		},
		onVmTargetSelected(name) {
			this.form.target_name = name
		},
		onContainerTargetSelected(id) {
			const c = this.targetContainers.find(item => item.name === id || item.id === id)
			if (c) this.form.target_name = c.name
		},
		onMaintenanceActionChange(action) {
			if (action === 'fstrim') this.form.target_name = 'SSD / Disk TRIM'
			else if (action === 'drop_caches') this.form.target_name = 'Drop Memory Page Caches'
			else if (action === 'docker_prune') this.form.target_name = 'Docker System Prune'
			else if (action === 'disk_standby_check') this.form.target_name = 'Disk Standby Check'
		},
		onPresetScheduleChange() {
			if (this.presetSchedule !== 'custom') {
				this.form.cron = this.presetSchedule
			}
		},
		getDefaultNamePlaceholder() {
			switch (this.form.type) {
				case 'backup': return 'e.g. Nightly Documents Backup to Cloud'
				case 'vm': return 'e.g. Shut down gaming VM nightly'
				case 'container': return 'e.g. Weekly container restart'
				case 'maintenance': return 'e.g. Nightly SSD trim & cache drop'
				case 'command': return 'e.g. Custom log rotation script'
				default: return 'e.g. Automated scheduled task'
			}
		},
		getSyncModeDesc(action) {
			switch (action) {
				case 'copy': return this.$t('Copies changed files. Safe: does not delete destination files.')
				case 'sync': return this.$t('Exact replica: deletes destination files if removed from source.')
				case 'archive': return this.$t('Creates timestamped compressed .tar.gz bundle.')
				case 'move': return this.$t('Transfers files and removes them from source path.')
				default: return ''
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
		getCategorySubtitle(type) {
			switch (type) {
				case 'backup': return this.$t('Automate incremental backups and two-way sync between local storage & cloud accounts.')
				case 'vm': return this.$t('Automate ACPI shutdown, startup, and reboot cycles for virtual machines.')
				case 'container': return this.$t('Automate container restarts, health checks, and image updates.')
				case 'maintenance': return this.$t('Automate SSD TRIM, filesystem cache flush, and docker pruning.')
				case 'command': return this.$t('Run custom shell scripts and cron automation.')
				default: return ''
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
		close() {
			this.$emit('close')
		},
		async saveTask() {
			if (!this.form.name) {
				this.$buefy.toast.open({
					message: this.$t('Please enter a task name'),
					type: 'is-warning',
					position: 'is-top',
					duration: 2000
				})
				return
			}
			if (this.form.type === 'backup') {
				if (!this.form.source_path || !this.form.dest_path) {
					this.$buefy.toast.open({
						message: this.$t('Please specify both source and destination paths'),
						type: 'is-warning',
						position: 'is-top',
						duration: 2500
					})
					return
				}
				this.form.target_name = `${this.form.source_path} → ${this.form.dest_path}`
			}

			this.saving = true
			try {
				if (this.form.id) {
					await this.$api.schedules.updateSchedule(this.form.id, this.form)
					this.$buefy.toast.open({
						message: this.$t('Task updated successfully'),
						type: 'is-success',
						position: 'is-top',
						duration: 2000
					})
				} else {
					await this.$api.schedules.createSchedule(this.form)
					this.$buefy.toast.open({
						message: this.$t('Task created successfully'),
						type: 'is-success',
						position: 'is-top',
						duration: 2000
					})
				}
				this.$EventBus.$emit('scheduled-tasks-changed')
				this.close()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to save task'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			} finally {
				this.saving = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.task-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	padding: 1.25rem 1.5rem;
	background: #f8fafc;
	color: #1e293b;
}

.task-window-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
	padding-right: 2px;
}

.task-header-card {
	display: flex;
	align-items: center;
	padding: 12px 14px;
	background: #ffffff;
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 12px;
	box-shadow: 0 2px 8px rgba(0, 0, 0, 0.03);

	.task-header-icon {
		width: 42px;
		height: 42px;
		border-radius: 10px;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 22px;
		margin-right: 12px;
		flex-shrink: 0;

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
			color: #f59e0b;
		}
		&.type-command {
			background: rgba(139, 92, 246, 0.12);
			color: #8b5cf6;
		}
	}

	.task-header-title {
		font-size: 14px;
		font-weight: 600;
		color: #0f172a;
	}

	.task-header-desc {
		font-size: 11px;
		color: #64748b;
		line-height: 1.3;
	}
}

.setting-card {
	background: #ffffff;
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 12px;
	padding: 8px 14px;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.02);
}

.setting-row {
	display: flex;
	align-items: center;
	padding: 10px 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.04);

	&:last-child {
		border-bottom: none;
	}

	&.align-start {
		align-items: flex-start;

		.row-icon {
			margin-top: 4px;
		}
	}

	.row-icon {
		margin-right: 12px;
		color: #64748b;
		flex-shrink: 0;
	}

	.row-label {
		flex: 1 1 auto;
		min-width: 0;
		font-size: 13px;
		font-weight: 500;
		color: #334155;
	}

	.row-control {
		flex-shrink: 0;
		min-width: 240px;
		max-width: 320px;
	}

	.path-control-col {
		flex: 1 1 auto;
		min-width: 0;
		max-width: 340px;
	}
}

.segmented-control {
	display: inline-flex;
	background: rgba(0, 0, 0, 0.04);
	padding: 2px;
	border-radius: 8px;
	width: 100%;

	.segmented-option {
		flex: 1 1 0;
		border: none;
		background: transparent;
		padding: 4px 6px;
		font-size: 11px;
		font-weight: 500;
		color: #64748b;
		border-radius: 6px;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		white-space: nowrap;
		transition: all 0.15s ease;

		&.active {
			background: #ffffff;
			color: #2563eb;
			box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
			font-weight: 600;
		}
	}
}

.chips-row {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: 4px;

	.chips-label {
		font-size: 10px;
		color: #94a3b8;
		margin-right: 2px;
	}

	.chip-btn {
		border: 1px solid rgba(0, 0, 0, 0.08);
		background: rgba(0, 0, 0, 0.02);
		border-radius: 4px;
		padding: 1px 6px;
		font-size: 10px;
		color: #475569;
		cursor: pointer;
		transition: all 0.12s ease;

		&:hover {
			background: rgba(37, 99, 235, 0.08);
			border-color: rgba(37, 99, 235, 0.3);
			color: #2563eb;
		}
	}
}

.advanced-toggle {
	cursor: pointer;
	padding: 4px 0;
	user-select: none;
}

.cron-custom-row {
	width: 100%;
}

.font-mono {
	font-family: monospace;
}

.font-medium {
	font-weight: 500;
}

.task-window-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 8px;
	padding-top: 12px;
	margin-top: 10px;
	border-top: 1px solid rgba(0, 0, 0, 0.06);
}
</style>
