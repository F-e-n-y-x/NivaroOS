<!-- src/apps/files/GridItem.vue -->
<template>
	<div
		class="grid-item"
		:class="{ selected, 'drop-target': isDropTarget, large, 'full-width': singleColumn }"
		draggable="true"
		@mousedown.stop
		@click="$emit('select', $event)"
		@dblclick="$emit('open', item)"
		@contextmenu.prevent="$emit('contextmenu', $event)"
		@dragstart="$emit('dragstart', item, $event)"
		@dragover="onDragOver"
		@dragleave="isDropTarget = false"
		@drop="onDrop"
	>
		<div :class="item | coverType" class="cover">
			<img
				v-if="showThumb"
				:src="getThumbUrl(item)"
				:class="item | iconType"
				alt=""
				class="thumb"
				@error="thumbFailed = true"
			/>
			<img v-else :src="getIconFile(item)" :class="item | iconType" alt="" class="icon" />
		</div>
		<p class="name one-line" :title="item.name">{{ item.name }}</p>
		<p class="date one-line">{{ item.date | dateFmt }}</p>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import { isFilesDragEvent } from '@/utils/files/dragDrop'

export default {
	name: 'files-grid-item',
	mixins: [mixin],
	props: {
		item: {
			type: Object,
			required: true,
		},
		selected: {
			type: Boolean,
			default: false,
		},
		large: {
			type: Boolean,
			default: false,
		},
		singleColumn: {
			type: Boolean,
			default: false,
		},
	},
	data() {
		return { thumbFailed: false, isDropTarget: false }
	},
	computed: {
		showThumb() {
			return !this.thumbFailed && this.hasThumb(this.item)
		},
	},
	methods: {
		// Only folders are valid drop targets - dropping a file onto another
		// file doesn't mean anything, so that case is deliberately left
		// un-prevented/un-stopped, letting it bubble to ContentView's own
		// background drop handler instead (browsers require dragover's
		// default prevented for a `drop` to fire at all, and stopping
		// propagation only when we actually intend to handle it here).
		onDragOver(event) {
			if (!this.item.is_dir || !isFilesDragEvent(event)) return
			event.preventDefault()
			event.stopPropagation()
			this.isDropTarget = true
		},
		onDrop(event) {
			this.isDropTarget = false
			if (!this.item.is_dir || !isFilesDragEvent(event)) return
			event.preventDefault()
			event.stopPropagation()
			this.$emit('drop-item', this.item, event)
		},
	},
}
</script>

<style lang="scss" scoped>
.grid-item {
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: var(--space-2);
	border-radius: var(--radius-control);
	cursor: default;
	user-select: none;
	// justify-items: start (ContentView.vue) makes each item hug its own
	// content instead of stretching to fill its grid cell - needed so
	// leftover 1fr column space stays real empty ground for drag-select.
	// A fixed width (matching the grid's minmax minimum) keeps every item
	// a predictable, bounded box regardless of how much extra space 1fr
	// hands the column, which lets .name's ellipsis truncate a long
	// filename instead of the item growing to fit it (grid items default
	// to a min-width of their content's full nowrap text width).
	min-width: 0;
	width: 4.5rem;
	&.full-width { width: 100%; }
	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}
	&.selected {
		background: rgba(50, 115, 220, 0.15);
		&:hover {
			background: rgba(50, 115, 220, 0.2);
		}
	}
	&.drop-target {
		background: rgba(50, 115, 220, 0.25);
		outline: 2px solid #3273dc;
		outline-offset: -2px;
	}
}
.cover {
	// Was 2.5rem - read as too big for the default thumbnail/grid view.
	width: 1.9rem;
	height: 1.9rem;
	display: flex;
	align-items: center;
	justify-content: center;
	position: relative;
	.icon {
		width: 100%;
		height: 100%;
		object-fit: contain;
	}
	.thumb {
		max-width: 100%;
		max-height: 100%;
		object-fit: cover;
		border-radius: var(--radius-xs);
	}
}
.name {
	margin-top: var(--space-1);
	font-size: var(--font-xs);
	text-align: center;
	max-width: 100%;
}
.date {
	font-size: var(--font-2xs);
	text-align: center;
	max-width: 100%;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.6));
}
// Large thumbnail/grid view - a separate view mode (not a zoom level),
// so this scales the cover/text up together rather than just the icon.
.grid-item.large {
	padding: var(--space-3);
	width: 7.5rem;
	.cover {
		width: 4rem;
		height: 4rem;
	}
	.name {
		margin-top: var(--space-2);
		font-size: var(--font-sm);
	}
	.date {
		font-size: var(--font-xs);
	}
}
// Written after .large so it wins on equal specificity when both apply.
.grid-item.full-width {
	width: 100%;
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	width: 100%;
}
</style>
