<template>
	<div class="folder-window" ref="root">
		<div v-if="!localApps.length" class="has-text-centered folder-empty-hint py-4">
			{{ $t('No apps in this folder yet.') }}
		</div>
		<draggable v-model="localApps" class="folder-window-grid" tag="div"
			:group="{ name: 'apps', pull: true, put: false }"
			@end="handleDragEnd" @start="handleDragStart" @mousedown.native.self="startMarquee">
			<div v-for="app in localApps" :key="app.name" :data-app-name="app.name"
				class="folder-item" :class="{ selected: selectedNames.includes(app.name) }">
				<app-card :folder-id="folderId" :item="app"
					@configApp="openConfig" @importApp="openImport"
					@removeFromFolder="doRemoveFromFolder" @updateState="doUpdateState"
					@editLegacyApp="openLegacyEdit">
				</app-card>
			</div>
		</draggable>
		<div v-if="marquee" class="marquee-box" :style="marqueeStyle"></div>
	</div>
</template>

<script>
import AppCard from '@/apps/app-store/AppCard.vue'
import draggable from 'vuedraggable'
import events from '@/events/events'

export default {
	name: 'FolderWindow',
	components: { AppCard, draggable },

	props: {
		folder: {
			type: Object,
			required: true
		}
	},

	data() {
		return {
			localApps: this.folder.apps ? [...this.folder.apps] : [],
			draggedApp: null,
			draggedGroup: null,
			lastDragPointer: null,
			selectedNames: [],
			marquee: null
		}
	},

	computed: {
		folderId() {
			return this.folder.id
		},
		marqueeStyle() {
			if (!this.marquee) return null
			return {
				left: this.marquee.x + 'px',
				top: this.marquee.y + 'px',
				width: this.marquee.width + 'px',
				height: this.marquee.height + 'px'
			}
		}
	},

	watch: {
		'folder.apps': {
			deep: true,
			handler(apps) {
				this.localApps = apps ? [...apps] : []
				this.selectedNames = this.selectedNames.filter(n => this.localApps.some(a => a.name === n))
			}
		}
	},

	methods: {
		openConfig(item) {
			this.$EventBus.$emit(events.SHOW_CONFIG_PANEL, item)
		},

		openImport(item) {
			this.$EventBus.$emit(events.SHOW_CONTAINER_PANEL, item)
		},

		openLegacyEdit(item) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'edit-legacy-' + item.name,
				title: this.$t('Edit App'),
				component: 'LegacyAppEditPanel',
				props: { item },
				width: 720,
				height: 530
			})
		},

		doRemoveFromFolder({ item, folderId }) {
			this.$EventBus.$emit(events.REMOVE_FROM_FOLDER, { item, folderId })
		},

		doUpdateState() {
			this.$EventBus.$emit(events.GET_APP_LIST)
		},

		// Click-drag on empty folder background to lasso-select several
		// apps at once, same interaction as the desktop canvas - dragging
		// any one of the selected apps back out of the folder then takes
		// the whole group with it (see handleDragStart/handleDragEnd).
		startMarquee(e) {
			if (e.button !== 0) return
			const container = this.$refs.root
			const startX = e.clientX
			const startY = e.clientY
			const containerRect = container.getBoundingClientRect()
			let moved = false

			const onMove = moveEvent => {
				const curX = moveEvent.clientX
				const curY = moveEvent.clientY
				if (!moved && Math.hypot(curX - startX, curY - startY) > 4) {
					moved = true
				}
				if (!moved) return

				const left = Math.min(startX, curX)
				const top = Math.min(startY, curY)
				this.marquee = {
					x: left - containerRect.left + container.scrollLeft,
					y: top - containerRect.top + container.scrollTop,
					width: Math.abs(curX - startX),
					height: Math.abs(curY - startY)
				}

				const selRect = { left, top, right: Math.max(startX, curX), bottom: Math.max(startY, curY) }
				const names = []
				container.querySelectorAll('[data-app-name]').forEach(el => {
					const r = el.getBoundingClientRect()
					if (r.left < selRect.right && r.right > selRect.left && r.top < selRect.bottom && r.bottom > selRect.top) {
						names.push(el.getAttribute('data-app-name'))
					}
				})
				this.selectedNames = names
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (!moved) {
					this.selectedNames = []
				}
				this.marquee = null
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},

		handleDragStart(evt) {
			this.draggedApp = this.localApps[evt.oldIndex]
			this.draggedGroup = (this.draggedApp && this.selectedNames.includes(this.draggedApp.name) && this.selectedNames.length > 1)
				? this.selectedNames.slice()
				: null
			window.addEventListener('dragover', this.trackPointer)
			window.addEventListener('touchmove', this.trackPointer)
		},

		trackPointer(e) {
			const point = e.touches ? e.touches[0] : e
			this.lastDragPointer = { x: point.clientX, y: point.clientY }
		},

		handleDragEnd() {
			window.removeEventListener('dragover', this.trackPointer)
			window.removeEventListener('touchmove', this.trackPointer)
			if (this.draggedApp && this.lastDragPointer) {
				const rect = this.$el.getBoundingClientRect()
				const { x, y } = this.lastDragPointer
				const isOutside = x < rect.left || x > rect.right || y < rect.top || y > rect.bottom
				if (isOutside) {
					if (this.draggedGroup) {
						const items = this.localApps.filter(a => this.draggedGroup.includes(a.name))
						this.$EventBus.$emit(events.REMOVE_MULTIPLE_FROM_FOLDER, { items, folderId: this.folderId, clientX: x, clientY: y })
						this.selectedNames = []
					} else {
						this.$EventBus.$emit(events.REMOVE_FROM_FOLDER, { item: this.draggedApp, folderId: this.folderId, clientX: x, clientY: y })
					}
				}
			}
			this.draggedApp = null
			this.draggedGroup = null
		}
	}
}
</script>

<style lang="scss" scoped>
.folder-window {
	width: 100%;
	height: 100%;
	padding: var(--space-4);
	overflow-y: auto;
	box-sizing: border-box;
	position: relative;
	scrollbar-width: thin;
	scrollbar-color: var(--theme-input-border, rgba(0, 0, 0, 0.15)) transparent;
	background: var(--theme-bg-window, #fff); color: var(--theme-text-primary, #1e293b);

	// App cards inside the folder window sit on a white background,
	// not over a dark wallpaper, so override the white text/shadow that
	// _card.scss sets globally for the desktop icon look.
	::v-deep .app-card {
		a, p, .app-label {
			color: var(--theme-text-primary, #1a1a1a) !important;
			text-shadow: none !important;
		}

		&:hover .cards-content {
			background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
			border-radius: var(--radius-card);
		}
	}

	// Folder cards inside a folder (rare but possible)
	::v-deep .folder-card {
		.app-label {
			color: var(--theme-text-primary, #1a1a1a) !important;
			text-shadow: none !important;
		}
	}
}

.folder-empty-hint {
	color: var(--theme-text-muted, #94a3b8);
}

.folder-window-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(88px, 1fr));
	gap: var(--space-2);
	min-height: 80px;
}

.folder-item {
	border-radius: var(--radius-card);

	&.selected {
		background: rgba(59, 130, 246, 0.16);
		box-shadow: 0 0 0 1px rgba(59, 130, 246, 0.5) inset;
	}
}

.marquee-box {
	position: absolute;
	border: 1px solid rgba(59, 130, 246, 0.8);
	background: rgba(59, 130, 246, 0.12);
	pointer-events: none;
	z-index: 40;
}
</style>
