<!-- src/components/files/OperationTray.vue -->
<!--
	Shows live progress for in-flight copy/move operations, backed by the
	same 'nivaroos:file:operate' message-bus broadcast ContentView already
	listens to (there, only to know when to reload() a folder's listing).
	The backend (service/notify.go's SendFileOperateNotify) polls its
	FileQueue every 3s while any task is running and broadcasts each
	task's real processed_size/total_size, so this is genuine progress,
	not a guess - paste() previously gave no visual feedback at all beyond
	a toast on failure.

	Rendered once in FilesApp.vue (not per-tab) since the operation itself
	is a global backend queue, not scoped to whichever folder happens to
	be open in a given tab.
-->
<template>
	<div v-if="taskList.length" class="operation-tray">
		<div class="operation-tray-header">
			<b-icon icon="swap-horizontal" custom-size="mdi-18px" class="header-icon"></b-icon>
			<span class="header-title">{{ headerText }}</span>
		</div>
		<ul class="operation-tray-list">
			<li v-for="task in taskList" :key="task.id" class="operation-tray-item" :class="{ 'is-finished': task.finished }">
				<b-icon class="item-icon" custom-size="mdi-18px" :icon="task.finished ? 'check-circle' : task.type === 'move' ? 'content-cut' : 'content-copy'"></b-icon>
				<div class="item-body">
					<div class="item-row">
						<span class="dest-name" :title="task.to">{{ baseName(task.to) || task.to }}</span>
						<span v-if="task.finished" class="status-text is-success">{{ $t('Done') }}</span>
						<span v-else class="percentage">{{ task.percent }}%</span>
					</div>
					<div v-if="!task.finished" class="progress-track">
						<div class="progress-fill" :style="{ width: task.percent + '%' }"></div>
					</div>
				</div>
			</li>
		</ul>
	</div>
</template>

<script>
import { baseName } from '@/utils/files/path'

// How long a finished task stays visible (with a "Done" checkmark) before
// being dropped from the tray - matches UploadTray's own completed-file
// fade delay.
const FINISHED_LINGER_MS = 2000

export default {
	name: 'operation-tray',
	data() {
		return { tasks: {} }
	},
	computed: {
		taskList() {
			return Object.values(this.tasks).sort((a, b) => a.startedAt - b.startedAt)
		},
		headerText() {
			const active = this.taskList.filter((t) => !t.finished)
			if (!active.length) return this.$t('Completed')
			return active.some((t) => t.type === 'move') && active.some((t) => t.type === 'copy')
				? this.$t('Processing files')
				: active[0].type === 'move'
				? this.$t('Moving')
				: this.$t('Copying')
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
			;(fileOperate.data || []).forEach((task) => {
				const existing = this.tasks[task.id]
				const percent = task.total_size > 0 ? Math.min(100, Math.floor((task.processed_size / task.total_size) * 100)) : task.finished ? 100 : 0
				this.$set(this.tasks, task.id, {
					id: task.id,
					to: task.to,
					type: task.type,
					finished: task.finished,
					percent,
					startedAt: existing ? existing.startedAt : Date.now(),
				})
				if (task.finished) {
					setTimeout(() => this.$delete(this.tasks, task.id), FINISHED_LINGER_MS)
				}
			})
		},
	},
	methods: {
		baseName,
	},
}
</script>

<style lang="scss" scoped>
.operation-tray {
	position: absolute;
	bottom: 0.75rem;
	right: 0.75rem;
	// A fixed-ish width (like a real notification panel) instead of
	// stretching edge-to-edge - full-width read as way too dominant in a
	// small/narrow window.
	width: min(22rem, calc(100% - 1.5rem));
	z-index: 20;
	max-height: 45%;
	display: flex;
	flex-direction: column;
	background: #fff;
	border-radius: 12px;
	border: 1px solid rgb(228 233 237);
	box-shadow: 0 10px 28px rgba(0, 0, 0, 0.14);
	overflow: hidden;
}
.operation-tray-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.6rem 0.85rem;
	font-weight: 600;
	font-size: 0.85rem;
	border-bottom: 1px solid rgb(228 233 237);
	background: rgba(0, 0, 0, 0.015);
}
.header-icon {
	color: #3273dc;
	flex-shrink: 0;
}
.operation-tray-list {
	overflow-y: auto;
	margin: 0;
	padding: 0.4rem;
	list-style: none;
}
.operation-tray-item {
	display: flex;
	align-items: flex-start;
	gap: 0.6rem;
	padding: 0.45rem 0.5rem;
	border-radius: 8px;
	font-size: 0.8rem;

	&:hover {
		background: rgba(0, 0, 0, 0.03);
	}
}
.item-icon {
	flex-shrink: 0;
	margin-top: 0.1rem;
	color: rgba(0, 0, 0, 0.35);

	.is-finished & {
		color: #48c774;
	}
}
.item-body {
	flex: 1 1 auto;
	min-width: 0;
}
.item-row {
	display: flex;
	align-items: baseline;
	gap: 0.5rem;
}
.dest-name {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.percentage {
	flex-shrink: 0;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.6);
}
.status-text.is-success {
	flex-shrink: 0;
	font-weight: 600;
	color: #257942;
}
.progress-track {
	margin-top: 0.35rem;
	height: 4px;
	border-radius: 999px;
	background: rgba(50, 115, 220, 0.12);
	overflow: hidden;
}
.progress-fill {
	height: 100%;
	border-radius: 999px;
	background: #3273dc;
	transition: width 0.15s ease;
}
</style>
