<!-- src/components/files/ListRow.vue -->
<template>
	<div
		class="list-row"
		:class="{ selected, 'drop-target': isDropTarget }"
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
		<div class="name one-line" :title="item.name">{{ item.name }}</div>
		<div class="size">{{ item.is_dir ? '' : renderSize(item.size) }}</div>
		<div class="date">{{ item.date | dateFmt }}</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import { isFilesDragEvent } from '@/utils/files/dragDrop'

export default {
	name: 'files-list-row',
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
.list-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	padding: 0.35rem 0.5rem;
	border-radius: 0.35rem;
	cursor: default;
	user-select: none;
	&:hover {
		background: rgba(0, 0, 0, 0.04);
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
	flex: 0 0 auto;
	width: 1.5rem;
	height: 1.5rem;
	display: flex;
	align-items: center;
	justify-content: center;
	.icon {
		width: 100%;
		height: 100%;
		object-fit: contain;
	}
	.thumb {
		max-width: 100%;
		max-height: 100%;
		object-fit: cover;
		border-radius: 0.2rem;
	}
}
.name {
	flex: 1 1 auto;
	min-width: 0;
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
.size {
	flex: 0 0 6rem;
	text-align: right;
	font-size: 0.8rem;
	color: rgba(0, 0, 0, 0.6);
}
.date {
	flex: 0 0 9rem;
	text-align: right;
	font-size: 0.8rem;
	color: rgba(0, 0, 0, 0.6);
}
</style>
