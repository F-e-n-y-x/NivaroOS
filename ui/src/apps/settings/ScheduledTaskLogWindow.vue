<template>
	<div class="scheduled-task-log-window">
		<div class="task-log-meta">
			<div class="meta-details">
				<span class="task-target-name">{{ task.target_name || task.name }}</span>
				<span class="meta-tag">{{ task.action || task.type || 'Custom Command' }}</span>
			</div>
			<div class="meta-actions">
				<span v-if="task.last_run" class="task-timestamp">
					<i class="mdi mdi-clock-outline mr-1"></i>{{ task.last_run }}
				</span>
				<b-button rounded size="is-small" type="is-primary" icon-left="content-copy" @click="copyOutput">
					{{ $t('Copy Output') }}
				</b-button>
			</div>
		</div>

		<div class="task-log-body">
			<pre class="task-output-pre">{{ task.last_output || $t('No output text recorded.') }}</pre>
		</div>

		<div class="task-log-foot">
			<b-button rounded size="is-small" @click="close">
				{{ $t('Close') }}
			</b-button>
		</div>
	</div>
</template>

<script>
export default {
	name: 'ScheduledTaskLogWindow',
	props: {
		task: {
			type: Object,
			default: () => ({})
		}
	},
	methods: {
		close() {
			const id = `task-log-${(this.task && this.task.id) || ''}`
			if (this.$store) {
				this.$store.commit('CLOSE_WINDOW', id)
			}
			this.$emit('close')
		},
		copyOutput() {
			const text = this.task.last_output || ''
			if (!text) return
			if (navigator.clipboard && navigator.clipboard.writeText) {
				navigator.clipboard.writeText(text).then(() => {
					this.$buefy.toast.open({
						message: this.$t('Output copied to clipboard'),
						type: 'is-success',
						position: 'is-top',
						duration: 2000
					})
				})
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.scheduled-task-log-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: #18181b;
	color: #e4e4e7;
	user-select: text;
}

.task-log-meta {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	background: #202024;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	gap: var(--space-2);
	flex-wrap: wrap;
}

.meta-details {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-width: 0;
}

.task-target-name {
	font-weight: 600;
	font-size: var(--font-sm);
	color: #f4f4f5;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.meta-tag {
	font-size: var(--font-2xs);
	padding: 0.15rem 0.5rem;
	border-radius: var(--radius-pill);
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.75);
}

.meta-actions {
	display: flex;
	align-items: center;
	gap: var(--space-3);
}

.task-timestamp {
	font-size: var(--font-xs);
	color: rgba(255, 255, 255, 0.5);
	display: flex;
	align-items: center;
}

.task-log-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	background: #121214;
	padding: var(--space-4);
}

.task-output-pre {
	margin: 0;
	padding: 0;
	background: transparent;
	color: #d4d4d8;
	font-family: 'JetBrains Mono', 'Fira Code', Menlo, Monaco, Consolas, monospace;
	font-size: 0.8rem;
	line-height: 1.5;
	white-space: pre-wrap;
	word-break: break-all;
}

.task-log-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: flex-end;
	padding: var(--space-2) var(--space-4);
	background: #202024;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}
</style>
