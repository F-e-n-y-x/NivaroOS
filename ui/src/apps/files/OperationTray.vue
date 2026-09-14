<!-- src/apps/files/OperationTray.vue -->
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
	<transition name="tray-pop">
		<div v-if="taskList.length" class="operation-tray" role="status" aria-live="polite" :aria-label="$t('File operation progress')">
			<div class="operation-tray-header">
				<b-icon icon="swap-horizontal" custom-size="mdi-18px" class="header-icon"></b-icon>
				<span class="header-title">{{ headerText }}</span>
				<button type="button" class="icon-btn close-icon" :aria-label="$t('Close')" @click="closeTray">
					<b-icon icon="close" custom-size="mdi-16px"></b-icon>
				</button>
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
	</transition>
</template>

<script>
import { baseName } from '@/utils/files/path'

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
				// No auto-remove here - a finished task stays listed with its
				// "Done" checkmark until the user closes the tray themselves.
			})
		},
	},
	methods: {
		baseName,
		closeTray() {
			this.tasks = {}
		},
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
	background: var(--theme-card-bg, #fff); border-radius: var(--radius-card); border: 1px solid var(--theme-card-border, rgb(228 233 237)); color: var(--theme-text-primary, #2c3e50);
	box-shadow: var(--shadow-lg);
	overflow: hidden;
}
.operation-tray-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	font-weight: 600;
	font-size: var(--font-sm);
	border-bottom: 1px solid var(--theme-card-border, rgb(228 233 237));
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.015));
}
.header-icon {
	color: var(--color-primary, #3273dc);
	flex-shrink: 0;
}
.header-title {
	flex: 1 1 auto;
}
// Reset for the icon-only close <button> below - a bare <button> carries the
// browser/OS's own default border and background, which must be reset
// explicitly or it shows through in both themes.
.icon-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	border: none;
	background: transparent;
	padding: 2px;
	border-radius: var(--radius-xs);
	cursor: pointer;
	line-height: 1;
	color: var(--color-text-muted, #64748b);

	&:hover {
		color: var(--theme-text-primary, rgba(0, 0, 0, 0.7));
	}
	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, rgba(37, 99, 235, 0.4));
		outline-offset: 1px;
	}
}
.operation-tray-list {
	overflow-y: auto;
	margin: 0;
	padding: var(--space-2);
	list-style: none;
}
.operation-tray-item {
	display: flex;
	align-items: flex-start;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-2);
	border-radius: var(--radius-control);
	font-size: var(--font-xs);

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.03));
	}
}
.item-icon {
	flex-shrink: 0;
	margin-top: var(--space-1);
	color: var(--color-text-muted, #64748b);

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
	gap: var(--space-2);
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
	color: var(--theme-text-secondary, rgba(0, 0, 0, 0.6));
}
.status-text.is-success {
	flex-shrink: 0;
	font-weight: 600;
	color: var(--color-success, #257942);
}
.progress-track {
	margin-top: var(--space-1);
	height: 4px;
	border-radius: var(--radius-pill);
	background: var(--theme-info-soft, rgba(50, 115, 220, 0.12));
	overflow: hidden;
}
.progress-fill {
	height: 100%;
	border-radius: var(--radius-pill);
	background: var(--color-primary, #3273dc);
	transition: width 0.15s ease;
}
.tray-pop-enter-active,
.tray-pop-leave-active {
	transition: opacity 0.18s ease, transform 0.18s ease;
}
.tray-pop-enter,
.tray-pop-leave-to {
	opacity: 0;
	transform: translateY(8px);
}
</style>
