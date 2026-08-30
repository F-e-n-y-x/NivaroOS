<template>
	<div class="modal-card folder-modal-card">
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ folder.name }}</h3>
			</div>
			<b-icon class="close-button" icon="close-outline" pack="casa" @click.native="$emit('close')" />
		</header>
		<section class="modal-card-body">
			<div v-if="localApps.length === 0" class="has-text-centered has-text-grey-400 py-4">
				{{ $t('No apps in this folder yet.') }}
			</div>
			<draggable v-model="localApps" class="folder-modal-grid" tag="div" :group="{ name: 'apps', pull: true, put: false }"
				@end="handleDragEnd" @start="handleDragStart">
				<app-card v-for="app in localApps" :key="app.name" :folder-id="folder.id" :item="app"
					@configApp="$emit('configApp', $event)" @importApp="$emit('importApp', $event)"
					@removeFromFolder="$emit('removeFromFolder', $event)" @updateState="$emit('updateState')"
					@editLegacyApp="$emit('editLegacyApp', $event)">
				</app-card>
			</draggable>
		</section>
	</div>
</template>

<script>
import AppCard from './AppCard.vue'
import draggable from 'vuedraggable'

export default {
	name: 'folder-modal',
	components: {
		AppCard,
		draggable
	},
	props: {
		folder: {
			type: Object,
			required: true
		}
	},
	data() {
		return {
			localApps: this.folder.apps || [],
			draggedApp: null,
			lastDragPointer: null
		}
	},
	watch: {
		folder: {
			deep: true,
			handler(folder) {
				this.localApps = folder.apps || []
			}
		}
	},
	methods: {
		handleDragStart(evt) {
			this.draggedApp = this.localApps[evt.oldIndex]
			// mousemove is suppressed by the browser during a native HTML5
			// drag (which Sortable uses by default on desktop) - dragover
			// is what actually fires with live coordinates.
			window.addEventListener('dragover', this.trackPointer)
			window.addEventListener('touchmove', this.trackPointer)
		},

		trackPointer(e) {
			const point = e.touches ? e.touches[0] : e
			this.lastDragPointer = { x: point.clientX, y: point.clientY }
		},

		// Dropping outside the modal's own bounds = take it out of the
		// folder (which puts it back in the main grid) - simpler and more
		// reliable than trying to drag live across the modal boundary
		// into the (dimmed, overlaid) grid behind it.
		handleDragEnd() {
			window.removeEventListener('dragover', this.trackPointer)
			window.removeEventListener('touchmove', this.trackPointer)
			if (this.draggedApp && this.lastDragPointer) {
				const rect = this.$el.getBoundingClientRect()
				const { x, y } = this.lastDragPointer
				const isOutside = x < rect.left || x > rect.right || y < rect.top || y > rect.bottom
				if (isOutside) {
					this.$emit('removeFromFolder', { item: this.draggedApp, folderId: this.folder.id })
				}
			}
			this.draggedApp = null
		}
	}
}
</script>

<style lang="scss" scoped>
// Same frosted-glass look as the desktop widgets/app tiles, rather
// than a plain opaque dialog - and always-light text since it sits
// over a blurred wallpaper regardless of the system light/dark
// preference (matching how widgets/app labels already work).
.folder-modal-card {
	background: $backDropColor;
	backdrop-filter: $backDropBlur;
	border: $backDropBorder;
	box-shadow: $backDropShadow;
	color: $white;

	::v-deep .title,
	::v-deep .has-text-grey-400 {
		color: rgba(255, 255, 255, 0.7) !important;
	}

	::v-deep .modal-card-head {
		border-bottom-color: rgba(255, 255, 255, 0.12);
	}
}

.folder-modal-grid {
	display: grid;
	// Fixed 5 columns instead of auto-fill(96-116px) - auto-fill was
	// leaving a partial-track's worth of width unused on the right
	// whenever the modal was wider than an exact multiple of ~104px.
	// minmax(96px, 1fr) keeps the same minimum icon size while letting
	// all 5 columns stretch to fill the actual available width.
	grid-template-columns: repeat(5, minmax(96px, 1fr));
	gap: 0.5rem;
	min-height: 96px;
}
</style>
