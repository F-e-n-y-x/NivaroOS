<!-- src/components/desktop/DragDropMenu.vue -->
<!--
	The "Copy here" / "Move here" menu shown wherever a Files drag is
	dropped - onto another folder row, a different tab, a different
	window, a sidebar location, or the desktop. Mounted once, globally
	(see WindowManager.vue), independent of whichever window/tab the drag
	started or ended in, since either side of a drag can be closed out
	from under the other mid-operation.
-->
<template>
	<div v-if="menu.visible" ref="menu" class="drag-drop-menu" :style="{ top: y + 'px', left: x + 'px' }">
		<button class="menu-item" @click="act('copy')">{{ $t('Copy here') }}</button>
		<button class="menu-item" @click="act('move')">{{ $t('Move here') }}</button>
	</div>
</template>

<script>
import events from '@/events/events'

const MENU_WIDTH = 140
const MENU_HEIGHT = 80

export default {
	name: 'drag-drop-menu',
	computed: {
		menu() {
			return this.$store.state.dragDropMenu
		},
		// Clamped to the viewport - this menu is position:fixed (not
		// confined to any one window), so it just needs to stay on-screen.
		x() {
			return Math.max(0, Math.min(this.menu.x, window.innerWidth - MENU_WIDTH))
		},
		y() {
			return Math.max(0, Math.min(this.menu.y, window.innerHeight - MENU_HEIGHT))
		},
	},
	mounted() {
		document.addEventListener('mousedown', this.onOutsideClick)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
	},
	methods: {
		onOutsideClick(event) {
			if (this.menu.visible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.close()
			}
		},
		close() {
			this.$store.commit('HIDE_DRAG_DROP_MENU')
		},
		act(type) {
			const { payload, targetPath } = this.menu
			this.close()
			if (!payload || !payload.items || !payload.items.length || !targetPath) return
			this.$api.batch
				.task({
					type,
					item: payload.items.map((from) => ({ from })),
					to: targetPath,
					style: 'overwrite',
				})
				.then((res) => {
					if (res.data.success === 200) {
						this.$EventBus.$emit(events.RELOAD_FILE_LIST)
						setTimeout(() => this.$EventBus.$emit(events.RELOAD_FILE_LIST), 400)
						setTimeout(() => this.$EventBus.$emit(events.RELOAD_FILE_LIST), 1200)
					} else {
						this.$buefy.toast.open({ message: res.data.message, type: 'is-danger' })
					}
				})
		},
	},
}
</script>

<style lang="scss" scoped>
.drag-drop-menu {
	position: fixed;
	z-index: 2000;
	background: #fff;
	border-radius: 6px;
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
	padding: 0.25rem;
	min-width: 140px;
}
.menu-item {
	display: block;
	width: 100%;
	text-align: left;
	padding: 0.4rem 0.6rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 4px;
	font-size: 0.85rem;
	&:hover {
		background: rgba(0, 0, 0, 0.06);
	}
}
</style>
