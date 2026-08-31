<template>
	<div class="folder-window">
		<div v-if="!localApps.length" class="has-text-centered has-text-grey-400 py-4">
			{{ $t('No apps in this folder yet.') }}
		</div>
		<draggable v-model="localApps" class="folder-window-grid" tag="div"
			:group="{ name: 'apps', pull: true, put: false }"
			@end="handleDragEnd" @start="handleDragStart">
			<app-card v-for="app in localApps" :key="app.name" :folder-id="folderId" :item="app"
				@configApp="openConfig" @importApp="openImport"
				@removeFromFolder="doRemoveFromFolder" @updateState="doUpdateState"
				@editLegacyApp="openLegacyEdit">
			</app-card>
		</draggable>
	</div>
</template>

<script>
import AppCard from '@/components/Apps/AppCard.vue'
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
			lastDragPointer: null
		}
	},

	computed: {
		folderId() {
			return this.folder.id
		}
	},

	watch: {
		'folder.apps': {
			deep: true,
			handler(apps) {
				this.localApps = apps ? [...apps] : []
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

		handleDragStart(evt) {
			this.draggedApp = this.localApps[evt.oldIndex]
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
					this.doRemoveFromFolder({ item: this.draggedApp, folderId: this.folderId })
				}
			}
			this.draggedApp = null
		}
	}
}
</script>

<style lang="scss" scoped>
.folder-window {
	width: 100%;
	height: 100%;
	padding: 1rem;
	overflow-y: auto;
	box-sizing: border-box;
	scrollbar-width: thin;
	scrollbar-color: rgba(0, 0, 0, 0.15) transparent;
	background: #fff;

	// App cards inside the folder window sit on a white background,
	// not over a dark wallpaper, so override the white text/shadow that
	// _card.scss sets globally for the desktop icon look.
	::v-deep .app-card {
		a, p, .app-label {
			color: #1a1a1a !important;
			text-shadow: none !important;
		}

		&:hover .cards-content {
			background: rgba(0, 0, 0, 0.05);
			border-radius: 10px;
		}
	}

	// Folder cards inside a folder (rare but possible)
	::v-deep .folder-card {
		.app-label {
			color: #1a1a1a !important;
			text-shadow: none !important;
		}
	}
}

.folder-window-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(88px, 1fr));
	gap: 0.5rem;
	min-height: 80px;
}
</style>
