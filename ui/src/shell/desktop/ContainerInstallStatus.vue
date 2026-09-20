<template>
	<div v-if="tasks.length" class="container-install-status">
		<transition-group name="install-card-anim" tag="div" class="install-tasks-stack">
			<div
				v-for="task in tasks"
				:key="task.id"
				class="install-task-card"
				:class="{ 'is-finished': task.finished, 'is-error': task.error }"
			>
				<div class="install-task-header">
					<div class="task-icon-wrapper">
						<img :src="task.icon || defaultAppIcon" class="task-app-icon" :alt="task.title || task.name || ''" @error="onIconError" />
						<div v-if="!task.finished && !task.error" class="icon-loading-badge">
							<i class="mdi mdi-loading mdi-spin"></i>
						</div>
						<div v-else-if="task.finished" class="icon-success-badge">
							<i class="mdi mdi-check"></i>
						</div>
						<div v-else-if="task.error" class="icon-error-badge">
							<i class="mdi mdi-alert-circle"></i>
						</div>
					</div>

					<div class="task-info">
						<div class="task-title-row">
							<span class="task-title">{{ task.title || task.name || $t('Container Application') }}</span>
							<span class="task-pct-badge" :class="{ 'is-success': task.finished, 'is-danger': task.error }">
								{{ task.finished ? (task.isUpdate ? $t('Updated') : $t('Installed')) : task.error ? $t('Failed') : (task.progress + '%') }}
							</span>
						</div>
						<span class="task-status-text">{{ task.statusText }}</span>
					</div>

					<button class="task-dismiss-btn" :title="$t('Dismiss')" @click="dismissTask(task.id)">
						<i class="mdi mdi-close"></i>
					</button>
				</div>

				<div v-if="!task.error" class="install-progress-bar-track">
					<div
						class="install-progress-bar-fill"
						:class="{ 'is-finished': task.finished, 'is-indeterminate': !task.progress && !task.finished }"
						:style="{ width: (task.finished ? 100 : task.progress) + '%' }"
					></div>
				</div>
				<div v-else class="install-error-box">
					<span>{{ task.errorMessage || $t('An unexpected error occurred during deployment.') }}</span>
				</div>
			</div>
		</transition-group>
	</div>
</template>

<script>
import defaultAppIcon from '@/assets/img/app-icons/default.svg'
import { ice_i18n } from '@/mixins/base/common-i18n'
import events from '@/events/events'

const FINISHED_LINGER_MS = 3200

export default {
	name: 'ContainerInstallStatus',
	data() {
		return {
			defaultAppIcon,
			tasks: []
		}
	},
	methods: {
		onIconError(e) {
			e.target.src = defaultAppIcon
		},
		dismissTask(id) {
			this.tasks = this.tasks.filter(t => t.id !== id)
		},
		getOrCreateTask(id, name, title, icon, isUpdate = false) {
			let task = this.tasks.find(t => t.id === id || t.name === name)
			if (!task) {
				task = {
					id: id || name,
					name: name,
					title: title || name,
					icon: icon || defaultAppIcon,
					progress: 0,
					statusText: isUpdate ? this.$t('Starting container update...') : this.$t('Starting installation...'),
					finished: false,
					error: false,
					errorMessage: '',
					isUpdate: isUpdate
				}
				this.tasks.push(task)
			}
			if (title) task.title = title
			if (icon) task.icon = icon
			if (isUpdate) task.isUpdate = true
			return task
		},
		parseTitle(raw) {
			if (!raw) return ''
			try {
				if (typeof raw === 'string' && (raw.startsWith('{') || raw.startsWith('['))) {
					return ice_i18n(JSON.parse(raw))
				}
				return ice_i18n(raw)
			} catch (e) {
				return String(raw)
			}
		},
		formatProgressMessage(val) {
			const p = Number(val)
			if (isNaN(p) || p <= 0) return this.$t('Downloading container layers...')
			if (p >= 100) return this.$t('Finalizing container setup...')
			if (p < 30) return this.$t('Pulling image layers ({pct}%)', { pct: p })
			if (p < 70) return this.$t('Extracting filesystem layers ({pct}%)', { pct: p })
			return this.$t('Configuring networking & storage ({pct}%)', { pct: p })
		}
	},
	sockets: {
		'app:install-begin'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const title = this.parseTitle(props['app:title']) || name
			const icon = props['app:icon'] || ''

			const task = this.getOrCreateTask(name, name, title, icon, false)
			task.progress = 5
			task.statusText = this.$t('Starting download & container build...')
			task.finished = false
			task.error = false
		},

		'app:install-progress'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const title = this.parseTitle(props['app:title']) || name
			const icon = props['app:icon'] || ''
			const rawProgress = props['app:progress'] || props.progress || '0'

			const task = this.getOrCreateTask(name, name, title, icon, false)
			const num = parseInt(rawProgress, 10)
			if (!isNaN(num)) {
				task.progress = Math.min(99, Math.max(task.progress, num))
			}
			task.statusText = this.formatProgressMessage(task.progress)
		},

		'app:install-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.tasks.find(t => t.id === name || t.name === name)

			if (task) {
				task.progress = 100
				task.finished = true
				task.statusText = this.$t('Installation complete! Container is ready.')
				setTimeout(() => {
					this.dismissTask(task.id)
				}, FINISHED_LINGER_MS)
			}

			this.$EventBus.$emit(events.RELOAD_APP_LIST)
			this.$EventBus.$emit(events.UPDATE_SYNC_STATUS)
		},

		'app:install-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.getOrCreateTask(name, name, '', '', false)
			task.error = true
			task.finished = false
			task.statusText = this.$t('Installation failed')
			task.errorMessage = props.message || this.$t('Deployment encountered an error.')
		},

		'app:apply-changes-begin'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const title = this.parseTitle(props['app:title']) || name
			const icon = props['app:icon'] || ''

			const task = this.getOrCreateTask(name, name, title, icon, true)
			task.progress = 25
			task.statusText = this.$t('Applying container update...')
			task.finished = false
			task.error = false
		},

		'app:apply-changes-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.tasks.find(t => t.id === name || t.name === name)

			if (task) {
				task.progress = 100
				task.finished = true
				task.statusText = this.$t('Container updated successfully!')
				setTimeout(() => {
					this.dismissTask(task.id)
				}, FINISHED_LINGER_MS)
			}

			this.$EventBus.$emit(events.RELOAD_APP_LIST)
		},

		'app:apply-changes-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.getOrCreateTask(name, name, '', '', true)
			task.error = true
			task.statusText = this.$t('Update failed')
			task.errorMessage = props.message || this.$t('Failed to apply container update.')
		},

		'app:update-begin'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const title = this.parseTitle(props['app:title']) || name
			const icon = props['app:icon'] || ''

			const task = this.getOrCreateTask(name, name, title, icon, true)
			task.progress = 25
			task.statusText = this.$t('Downloading updated image layers...')
			task.finished = false
			task.error = false
		},

		'app:update-end'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.tasks.find(t => t.id === name || t.name === name)

			if (task) {
				task.progress = 100
				task.finished = true
				task.statusText = this.$t('Container updated successfully!')
				setTimeout(() => {
					this.dismissTask(task.id)
				}, FINISHED_LINGER_MS)
			}

			this.$EventBus.$emit(events.RELOAD_APP_LIST)
		},

		'app:update-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.getOrCreateTask(name, name, '', '', true)
			task.error = true
			task.statusText = this.$t('Update failed')
			task.errorMessage = props.message || this.$t('Container update encountered an error.')
		}
	}
}
</script>

<style lang="scss" scoped>
.container-install-status {
	position: fixed;
	bottom: 84px;
	right: 20px;
	z-index: 9999;
	pointer-events: none;
	display: flex;
	flex-direction: column;
	max-width: 380px;
	width: calc(100vw - 40px);
	background: transparent !important;
	border: none !important;
	box-shadow: none !important;
}

.install-tasks-stack {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.install-task-card {
	pointer-events: auto;
	background: var(--theme-card-bg, #ffffff);
	backdrop-filter: blur(20px);
	-webkit-backdrop-filter: blur(20px);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	border-radius: var(--radius-modal, 16px);
	padding: var(--space-3) var(--space-4);
	box-shadow: var(--shadow-xl, 0 12px 30px -4px rgba(0, 0, 0, 0.25));
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);

	&.is-finished {
		border-color: rgba(16, 185, 129, 0.5);
	}

	&.is-error {
		border-color: rgba(239, 68, 68, 0.5);
	}
}

.install-task-header {
	display: flex;
	align-items: center;
	gap: var(--space-3);
}

.task-icon-wrapper {
	position: relative;
	width: 38px;
	height: 38px;
	flex-shrink: 0;
}

.task-app-icon {
	width: 38px;
	height: 38px;
	border-radius: var(--radius-control, 8px);
	object-fit: cover;
	background: var(--theme-card-subtle, #f8fafc);
	padding: var(--space-1);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	box-shadow: var(--shadow-sm);
}

.icon-loading-badge {
	position: absolute;
	bottom: -4px;
	right: -4px;
	width: 18px;
	height: 18px;
	border-radius: 50%;
	background: var(--color-primary, #2563eb);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 11px;
	border: 2px solid var(--theme-card-bg, #ffffff);
	box-shadow: 0 2px 5px rgba(0, 0, 0, 0.18);
}

.icon-success-badge {
	position: absolute;
	bottom: -4px;
	right: -4px;
	width: 18px;
	height: 18px;
	border-radius: 50%;
	background: #10b981;
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 11px;
	border: 2px solid var(--theme-card-bg, #ffffff);
	box-shadow: 0 2px 5px rgba(0, 0, 0, 0.18);
}

.icon-error-badge {
	position: absolute;
	bottom: -4px;
	right: -4px;
	width: 18px;
	height: 18px;
	border-radius: 50%;
	background: #ef4444;
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 11px;
	border: 2px solid var(--theme-card-bg, #ffffff);
	box-shadow: 0 2px 5px rgba(0, 0, 0, 0.18);
}

.task-info {
	flex: 1;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.task-title-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
}

.task-title {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	line-height: 1.2;
}

.task-pct-badge {
	font-size: var(--font-2xs);
	font-weight: 600;
	color: var(--color-primary, #2563eb);
	background: rgba(37, 99, 235, 0.1);
	padding: 0.12rem var(--space-2);
	border-radius: var(--radius-pill);
	white-space: nowrap;

	&.is-success {
		color: #10b981;
		background: rgba(16, 185, 129, 0.12);
	}

	&.is-danger {
		color: #ef4444;
		background: rgba(239, 68, 68, 0.12);
	}
}

.task-status-text {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #64748b);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.task-dismiss-btn {
	background: transparent;
	border: none;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-md);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-xs);
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.15s ease;

	&:hover {
		color: var(--theme-text-primary, #0f172a);
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	}
}

.install-progress-bar-track {
	width: 100%;
	height: 4px;
	background: var(--theme-control-border, rgba(0, 0, 0, 0.08));
	border-radius: var(--radius-pill);
	overflow: hidden;
}

.install-progress-bar-fill {
	height: 100%;
	background: linear-gradient(90deg, #2563eb, #38bdf8);
	border-radius: var(--radius-pill);
	transition: width 0.3s ease;
	box-shadow: 0 0 8px rgba(56, 189, 248, 0.4);

	&.is-finished {
		background: #10b981;
		box-shadow: 0 0 8px rgba(16, 185, 129, 0.4);
	}

	&.is-indeterminate {
		width: 40% !important;
		animation: indeterminate 1.5s infinite ease-in-out;
	}
}

@keyframes indeterminate {
	0% { transform: translateX(-100%); }
	100% { transform: translateX(300%); }
}

.install-error-box {
	background: rgba(239, 68, 68, 0.1);
	border: 1px solid rgba(239, 68, 68, 0.25);
	border-radius: var(--radius-sm);
	padding: var(--space-1) var(--space-2);
	font-size: var(--font-2xs);
	color: #ef4444;
	line-height: 1.3;
}

/* Animations */
.install-card-anim-enter-active, .install-card-anim-leave-active {
	transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.install-card-anim-enter {
	opacity: 0;
	transform: translateY(16px) scale(0.95);
}

.install-card-anim-leave-to {
	opacity: 0;
	transform: translateX(30px) scale(0.95);
}
</style>

