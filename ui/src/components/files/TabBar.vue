<!-- src/components/files/TabBar.vue -->
<template>
	<div class="tab-bar" @mousedown="$emit('drag-start', $event)">
		<button
			v-for="tab in tabs"
			:key="tab.id"
			class="tab"
			:class="{ active: tab.id === activeTabId, 'drop-target': dragHoverTabId === tab.id }"
			:title="tab.path"
			@click="$emit('switch', tab.id)"
			@dragover="onDragOver(tab, $event)"
			@dragleave="onDragLeave(tab)"
			@drop="onDrop(tab, $event)"
		>
			<span class="tab-label one-line">{{ tabLabel(tab) }}</span>
			<b-icon
				icon="close"
				custom-size="mdi-14px"
				class="tab-close"
				@click.native.stop="$emit('close', tab.id)"
			></b-icon>
		</button>
		<button class="tab-action" :title="$t('New Tab')" @click="$emit('new-tab')">
			<b-icon icon="plus" custom-size="mdi-16px"></b-icon>
		</button>
		<button class="tab-action" :title="$t('New Window')" @click="$emit('new-window')">
			<b-icon icon="open-in-new" custom-size="mdi-16px"></b-icon>
		</button>
		<div class="tab-bar-spacer"></div>
		<!-- Files' own window controls, standing in for the shared
		     window-titlebar this bar replaces - no maximize button, by
		     design (see DesktopWindow.vue). -->
		<div class="window-controls">
			<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="$emit('minimize-window')"></button>
			<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="$emit('close-window')"></button>
		</div>
	</div>
</template>

<script>
import { baseName } from '@/utils/files/path'
import { isFilesDragEvent, getFilesDragData } from '@/utils/files/dragDrop'

const HOVER_OPEN_DELAY = 700

export default {
	name: 'files-tab-bar',
	props: {
		tabs: { type: Array, required: true },
		activeTabId: { type: [String, Number], required: true },
	},
	data() {
		return { dragHoverTabId: null, hoverTimer: null }
	},
	beforeDestroy() {
		clearTimeout(this.hoverTimer)
	},
	methods: {
		tabLabel(tab) {
			return baseName(tab.path) || tab.path
		},
		// Hovering a dragged file/folder over a different tab switches to it
		// (same spring-loaded behavior as the sidebar), so the user can keep
		// dragging further into whatever that tab is showing; dropping
		// directly on a tab (below) pastes into its folder immediately.
		onDragOver(tab, event) {
			if (!isFilesDragEvent(event) || tab.id === this.activeTabId) return
			event.preventDefault()
			if (this.dragHoverTabId === tab.id) return
			this.dragHoverTabId = tab.id
			clearTimeout(this.hoverTimer)
			this.hoverTimer = setTimeout(() => {
				this.$emit('switch', tab.id)
			}, HOVER_OPEN_DELAY)
		},
		onDragLeave(tab) {
			if (this.dragHoverTabId !== tab.id) return
			this.dragHoverTabId = null
			clearTimeout(this.hoverTimer)
		},
		onDrop(tab, event) {
			this.dragHoverTabId = null
			clearTimeout(this.hoverTimer)
			if (!isFilesDragEvent(event)) return
			event.preventDefault()
			event.stopPropagation()
			const payload = getFilesDragData(event)
			if (!payload) return
			if (payload.from === tab.path || payload.items.includes(tab.path)) return
			this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: tab.path })
		},
	},
}
</script>

<style lang="scss" scoped>
.tab-bar {
	flex-shrink: 0;
	display: flex;
	align-items: flex-end;
	gap: 2px;
	padding: 0.35rem 0.5rem 0;
	background: rgba(0, 0, 0, 0.03);
	overflow-x: auto;
	&::-webkit-scrollbar {
		display: none;
	}
}
.tab {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	padding: 0.35rem 0.5rem;
	border: none;
	background: transparent;
	border-radius: 6px 6px 0 0;
	cursor: pointer;
	max-width: 10rem;
	min-width: 4rem;
	color: rgba(0, 0, 0, 0.55);
	font-size: 0.8rem;
	&:hover {
		background: rgba(0, 0, 0, 0.04);
	}
	&.active {
		background: #fff;
		color: #2c3e50;
		font-weight: 600;
	}
	&.drop-target {
		background: rgba(50, 115, 220, 0.2);
		outline: 2px solid #3273dc;
		outline-offset: -2px;
	}
}
.tab-label {
	flex: 1 1 auto;
	min-width: 0;
	text-align: left;
}
.tab-close {
	flex-shrink: 0;
	opacity: 0.45;
	&:hover {
		opacity: 1;
	}
}
.tab-action {
	flex-shrink: 0;
	border: none;
	background: transparent;
	cursor: pointer;
	padding: 0.35rem;
	margin-bottom: 0.1rem;
	border-radius: 4px;
	opacity: 0.55;
	color: rgba(0, 0, 0, 0.6);
	&:hover {
		opacity: 1;
		background: rgba(0, 0, 0, 0.05);
	}
}
.tab-bar-spacer {
	flex: 1 1 auto;
	min-width: 0.5rem;
}
.window-controls {
	flex-shrink: 0;
	align-self: center;
	display: flex;
	align-items: center;
	gap: 0.5rem;
}
.window-btn {
	width: 0.85rem;
	height: 0.85rem;
	border-radius: 50%;
	border: none;
	cursor: pointer;
	padding: 0;
}
.window-btn-minimize {
	background: #f6bd3b;
}
.window-btn-close {
	background: #f2534a;
}
</style>
