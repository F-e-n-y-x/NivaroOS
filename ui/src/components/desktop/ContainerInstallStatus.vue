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
						<img :src="task.icon || defaultAppIcon" class="task-app-icon" @error="onIconError" />
						<div v-if="!task.finished && !task.error" class="icon-spinner-ring"></div>
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
								{{ task.finished ? $t('Installed') : task.error ? $t('Failed') : (task.progress + '%') }}
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
import defaultAppIcon from '@/assets/img/app/default.svg'
import { ice_i18n } from '@/mixins/base/common-i18n'
import events from '@/events/events'

const FINISHED_LINGER_MS = 2800

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
		getOrCreateTask(id, name, title, icon) {
			let task = this.tasks.find(t => t.id === id || t.name === name)
			if (!task) {
				task = {
					id: id || name,
					name: name,
					title: title || name,
					icon: icon || defaultAppIcon,
					progress: 0,
					statusText: this.$t('Starting installation...'),
					finished: false,
					error: false,
					errorMessage: ''
				}
				this.tasks.push(task)
			}
			if (title) task.title = title
			if (icon) task.icon = icon
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

			const task = this.getOrCreateTask(name, name, title, icon)
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

			const task = this.getOrCreateTask(name, name, title, icon)
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
			const task = this.getOrCreateTask(name, name, '', '')
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

			const task = this.getOrCreateTask(name, name, title, icon)
			task.progress = 20
			task.statusText = this.$t('Applying container configuration...')
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
				task.statusText = this.$t('Configuration updated successfully!')
				setTimeout(() => {
					this.dismissTask(task.id)
				}, FINISHED_LINGER_MS)
			}

			this.$EventBus.$emit(events.RELOAD_APP_LIST)
		},

		'app:apply-changes-error'(res) {
			const props = res.Properties || {}
			const name = props['app:name'] || props.name || 'app'
			const task = this.getOrCreateTask(name, name, '', '')
			task.error = true
			task.statusText = this.$t('Update failed')
			task.errorMessage = props.message || this.$t('Failed to apply configuration.')
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
}

.install-tasks-stack {
	display: flex;
	flex-direction: column;
	gap: 0.625rem;
}

.install-task-card {
	pointer-events: auto;
	background: rgba(15, 23, 42, 0.92);
	backdrop-filter: blur(16px);
	-webkit-backdrop-filter: blur(16px);
	border: 1px solid rgba(255, 255, 255, 0.12);
	border-radius: 0.75rem;
	padding: 0.875rem 1rem;
	box-shadow: 0 12px 30px -4px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(255, 255, 255, 0.06) inset;
	display: flex;
	flex-direction: column;
	gap: 0.6rem;
	transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);

	&.is-finished {
		border-color: rgba(34, 197, 94, 0.4);
		background: rgba(15, 23, 42, 0.96);
	}

	&.is-error {
		border-color: rgba(239, 68, 68, 0.4);
	}
}

.install-task-header {
	display: flex;
	align-items: center;
	gap: 0.75rem;
}

.task-icon-wrapper {
	position: relative;
	width: 36px;
	height: 36px;
	flex-shrink: 0;
}

.task-app-icon {
	width: 36px;
	height: 36px;
	border-radius: 0.5rem;
	object-fit: cover;
	background: #ffffff;
	padding: 2px;
	box-shadow: 0 2px 6px rgba(0, 0, 0, 0.25);
}

.icon-spinner-ring {
	position: absolute;
	inset: -3px;
	border: 2px solid transparent;
	border-top-color: #38bdf8;
	border-right-color: #38bdf8;
	border-radius: 0.65rem;
	animation: spinRing 1s linear infinite;
}

.icon-success-badge {
	position: absolute;
	bottom: -4px;
	right: -4px;
	width: 16px;
	height: 16px;
	border-radius: 50%;
	background: #22c55e;
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 11px;
	border: 1.5px solid #0f172a;
}

.icon-error-badge {
	position: absolute;
	bottom: -4px;
	right: -4px;
	width: 16px;
	height: 16px;
	border-radius: 50%;
	background: #ef4444;
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 11px;
	border: 1.5px solid #0f172a;
}

@keyframes spinRing {
	100% {
		transform: rotate(360deg);
	}
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
	gap: 0.5rem;
}

.task-title {
	font-size: 0.8125rem;
	font-weight: 700;
	color: #f8fafc;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	line-height: 1.2;
}

.task-pct-badge {
	font-size: 0.6875rem;
	font-weight: 700;
	color: #38bdf8;
	background: rgba(56, 189, 248, 0.15);
	padding: 0.1rem 0.4rem;
	border-radius: 9999px;
	white-space: nowrap;

	&.is-success {
		color: #4ade80;
		background: rgba(74, 222, 128, 0.15);
	}

	&.is-danger {
		color: #f87171;
		background: rgba(248, 113, 113, 0.15);
	}
}

.task-status-text {
	font-size: 0.71875rem;
	color: #94a3b8;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.task-dismiss-btn {
	background: transparent;
	border: none;
	color: #64748b;
	font-size: 16px;
	cursor: pointer;
	padding: 0.2rem;
	border-radius: 0.25rem;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.15s ease;

	&:hover {
		color: #f8fafc;
		background: rgba(255, 255, 255, 0.1);
	}
}

.install-progress-bar-track {
	width: 100%;
	height: 4px;
	background: rgba(255, 255, 255, 0.1);
	border-radius: 9999px;
	overflow: hidden;
}

.install-progress-bar-fill {
	height: 100%;
	background: linear-gradient(90deg, #2563eb, #38bdf8);
	border-radius: 9999px;
	transition: width 0.3s ease;
	box-shadow: 0 0 8px rgba(56, 189, 248, 0.5);

	&.is-finished {
		background: #22c55e;
		box-shadow: 0 0 8px rgba(34, 197, 94, 0.5);
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
	background: rgba(239, 68, 68, 0.15);
	border: 1px solid rgba(239, 68, 68, 0.3);
	border-radius: 0.375rem;
	padding: 0.35rem 0.5rem;
	font-size: 0.6875rem;
	color: #fca5a5;
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
