<!-- src/components/desktop/FileOperationStatus.vue -->
<!--
	Windows-style copy/move progress - mounted once, globally (see
	WindowManager.vue), same reasoning as DragDropMenu.vue: a task's
	source and destination can be in entirely different windows/tabs,
	so this can't live inside any one Files window. Reuses the same
	nivaroos:file:operate socket event ContentView.vue listens to for its
	own real-time-refresh fix (see that file's `sockets` block) - this
	is the same backend data, just rendered as a progress list instead
	of a refresh trigger. Transfer speed isn't sent by the backend
	(only cumulative processed_size), so it's estimated client-side
	from the size delta between successive samples of the same task.
-->
<template>
	<div v-if="tasks.length" class="file-operation-status">
		<div v-for="task in tasks" :key="task.id" class="task-row" :class="{ finished: task.finished }">
			<b-icon :icon="task.type === 'move' ? 'file-move-outline' : 'content-copy'" custom-size="mdi-16px" class="task-icon"></b-icon>
			<div class="task-body">
				<div class="task-title one-line">
					{{ task.finished ? $t('Done') : task.type === 'move' ? $t('Moving to {name}', { name: destName(task) }) : $t('Copying to {name}', { name: destName(task) }) }}
				</div>
				<div class="task-progress-track">
					<div class="task-progress-fill" :style="{ width: percent(task) + '%' }"></div>
				</div>
				<div class="task-meta">
					<span>{{ percent(task) }}%</span>
					<span v-if="!task.finished && task.speedBps > 0">{{ renderSize(task.speedBps) }}/s</span>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { renderSize } from '@/mixins/file_utils'
import { baseName } from '@/utils/files/path'

// Below this interval between samples, a freshly-estimated speed is too
// noisy (a burst of tiny files can look like an instantaneous spike) -
// wait for a bit more elapsed time/progress before recomputing it.
const MIN_SAMPLE_INTERVAL_MS = 400
// Keep a finished task visible briefly so "Done" is actually seen,
// instead of the row just vanishing the instant it completes.
const FINISHED_LINGER_MS = 1500

export default {
	name: 'file-operation-status',
	data() {
		return { tasks: [] }
	},
	methods: {
		renderSize,
		destName(task) {
			return baseName(task.to) || task.to
		},
		percent(task) {
			if (!task.totalSize) return task.finished ? 100 : 0
			return Math.min(100, Math.round((task.processedSize / task.totalSize) * 100))
		},
	},
	sockets: {
		'nivaroos:file:operate'(res) {
			let fileOperate
			try {
				fileOperate = JSON.parse(res.Properties.file_operate)
			} catch (e) {
				return
			}
			const now = Date.now()
			;(fileOperate.data || []).forEach((task) => {
				const existing = this.tasks.find((t) => t.id === task.id)
				if (task.finished) {
					if (existing) {
						existing.finished = true
						existing.processedSize = existing.totalSize
						setTimeout(() => {
							this.tasks = this.tasks.filter((t) => t.id !== task.id)
						}, FINISHED_LINGER_MS)
					}
					return
				}
				if (existing) {
					const elapsed = now - existing.lastSampleTime
					const delta = task.processed_size - existing.lastSampleSize
					if (elapsed >= MIN_SAMPLE_INTERVAL_MS && delta >= 0) {
						existing.speedBps = delta / (elapsed / 1000)
						existing.lastSampleTime = now
						existing.lastSampleSize = task.processed_size
					}
					existing.processedSize = task.processed_size
					existing.totalSize = task.total_size
				} else {
					this.tasks.push({
						id: task.id,
						type: task.type,
						to: task.to,
						totalSize: task.total_size,
						processedSize: task.processed_size,
						finished: false,
						speedBps: 0,
						lastSampleTime: now,
						lastSampleSize: task.processed_size,
					})
				}
			})
		},
	},
}
</script>

<style lang="scss" scoped>
.file-operation-status {
	position: fixed;
	right: 1rem;
	bottom: 1rem;
	z-index: 1900;
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
	width: 18rem;
	max-width: calc(100vw - 2rem);
}
.task-row {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	background: rgba(30, 30, 30, 0.92);
	backdrop-filter: blur(10px);
	color: #fff;
	border-radius: 10px;
	padding: 0.6rem 0.75rem;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
	&.finished {
		opacity: 0.7;
	}
}
.task-icon {
	flex-shrink: 0;
}
.task-body {
	flex: 1 1 auto;
	min-width: 0;
}
.task-title {
	font-size: 0.8rem;
	margin-bottom: 0.3rem;
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
.task-progress-track {
	height: 4px;
	border-radius: 2px;
	background: rgba(255, 255, 255, 0.2);
	overflow: hidden;
}
.task-progress-fill {
	height: 100%;
	background: #3273dc;
	transition: width 0.2s ease;
}
.task-meta {
	display: flex;
	justify-content: space-between;
	font-size: 0.7rem;
	color: rgba(255, 255, 255, 0.65);
	margin-top: 0.25rem;
}
</style>
