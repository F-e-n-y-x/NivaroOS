<template>
	<div class="modal-card icon-editor-card">
		<section class="modal-card-body">
			<div ref="viewport" class="icon-editor-viewport" @mousedown="startDrag" @touchstart="startDrag">
				<div class="icon-editor-crop" :style="{ borderRadius: radius + '%' }">
					<img
						ref="img"
						:src="src"
						:style="imgTransformStyle"
						draggable="false"
						class="icon-preview-img"
						:alt="$t('Icon preview')"
						@load="onImageLoaded"
					/>
				</div>
			</div>

			<p v-if="corsBlocked" class="icon-editor-notice" role="status">
				<i class="mdi mdi-information-outline mr-1" aria-hidden="true"></i>
				{{ $t("This image's server doesn't allow editing it here. Only the roundness will be saved - upload the image instead to crop or zoom it.") }}
			</p>
			<div class="editor-control">
				<label :for="uid + '-zoom'"><i class="mdi mdi-magnify-plus-outline mr-1" aria-hidden="true"></i>{{ $t('Zoom') }}</label>
				<input :id="uid + '-zoom'" v-model.number="zoom" :disabled="corsBlocked" max="3" min="1" step="0.02" type="range" class="slider-control" />
				<span class="zoom-value" aria-hidden="true">{{ Math.round(zoom * 100) }}%</span>
			</div>
			<div class="editor-control">
				<label :for="uid + '-radius'"><i class="mdi mdi-rounded-corner mr-1" aria-hidden="true"></i>{{ $t('Roundness') }}</label>
				<input :id="uid + '-radius'" v-model.number="radius" max="50" min="0" step="1" type="range" class="slider-control" />
				<span class="zoom-value" aria-hidden="true">{{ radius }}%</span>
			</div>
		</section>
		<footer class="modal-card-foot is-flex is-align-items-center">
			<b-button rounded size="is-small" @click="resetTransforms">
				<i class="mdi mdi-restore mr-1"></i>
				{{ $t('Reset') }}
			</b-button>
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Apply')" :loading="isApplying" rounded type="is-primary" @click="apply" />
			</div>
		</footer>
	</div>
</template>

<script>
import events from '@/events/events'
import business_Folders from '@/mixins/app/Business_Folders'
import { apiErrorHtml } from '@/mixins/app/apiError'

const VIEWPORT_SIZE = 220
const OUTPUT_SIZE = 256

export default {
	name: 'IconEditorModal',
	mixins: [business_Folders],
	props: {
		src: {
			type: String,
			required: true
		},
		initialZoom: {
			type: Number,
			default: 1
		},
		initialOffsetX: {
			type: Number,
			default: 0
		},
		initialOffsetY: {
			type: Number,
			default: 0
		},
		initialRadius: {
			type: Number,
			default: 0
		},
		// When set, apply() saves directly to this folder itself instead of
		// relying on a parent-listened 'apply' event - needed once this
		// component is opened as a standalone desktop window rather than
		// embedded inline, since the shared window chrome only forwards
		// close/minimize/drag-start/status-change, not custom business events.
		folderId: {
			type: String,
			default: null
		}
	},
	data() {
		return {
			zoom: this.initialZoom || 1,
			offsetX: this.initialOffsetX || 0,
			offsetY: this.initialOffsetY || 0,
			radius: this.initialRadius || 0,
			imageLoaded: false,
			naturalWidth: 0,
			naturalHeight: 0,
			dragging: false,
			dragStart: null,
			// A CORS-enabled copy of the image for the canvas; the on-screen
			// <img> has no crossorigin so it always displays.
			canvasImage: null,
			corsBlocked: false,
			isApplying: false,
			uid: 'icon-editor-' + Math.random().toString(36).slice(2, 8)
		}
	},
	watch: {
		zoom() {
			this.clampOffsets()
		}
	},
	computed: {
		imgTransformStyle() {
			return {
				transform: `translate3d(${this.offsetX}px, ${this.offsetY}px, 0) scale(${this.zoom})`,
				transformOrigin: 'center center'
			}
		}
	},
	mounted() {
		this.loadImage()
	},
	beforeDestroy() {
		this.stopDrag()
	},
	methods: {
		loadImage() {
			const img = new Image()
			// Must be set before src, or the canvas gets tainted.
			img.crossOrigin = 'anonymous'
			img.onload = () => {
				this.naturalWidth = img.naturalWidth || VIEWPORT_SIZE
				this.naturalHeight = img.naturalHeight || VIEWPORT_SIZE
				this.imageLoaded = true
				this.canvasImage = img
				this.corsBlocked = false
			}
			img.onerror = () => {
				// Usually the server sends no CORS headers: the picture still
				// shows (plain <img>) but can't be re-drawn into a canvas.
				this.canvasImage = null
				this.corsBlocked = !String(this.src || '').startsWith('data:')
			}
			img.src = this.src
		},
		onImageLoaded(e) {
			const el = e.target
			if (el) {
				this.naturalWidth = el.naturalWidth || VIEWPORT_SIZE
				this.naturalHeight = el.naturalHeight || VIEWPORT_SIZE
				this.imageLoaded = true
			}
		},
		clampOffsets() {
			const maxOffsetX = Math.max(0, ((this.zoom - 1) * VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, ((this.zoom - 1) * VIEWPORT_SIZE) / 2)
			this.offsetX = Math.min(maxOffsetX, Math.max(-maxOffsetX, this.offsetX))
			this.offsetY = Math.min(maxOffsetY, Math.max(-maxOffsetY, this.offsetY))
		},
		resetTransforms() {
			this.zoom = 1
			this.offsetX = 0
			this.offsetY = 0
			this.radius = 0
		},
		startDrag(e) {
			if (this.zoom <= 1) return
			const point = e.touches ? e.touches[0] : e
			this.dragging = true
			this.dragStart = { x: point.clientX, y: point.clientY, offsetX: this.offsetX, offsetY: this.offsetY }
			window.addEventListener('mousemove', this.onDrag)
			window.addEventListener('touchmove', this.onDrag)
			window.addEventListener('mouseup', this.stopDrag)
			window.addEventListener('touchend', this.stopDrag)
		},
		onDrag(e) {
			if (!this.dragging) return
			const point = e.touches ? e.touches[0] : e
			const dx = point.clientX - this.dragStart.x
			const dy = point.clientY - this.dragStart.y
			const maxOffsetX = Math.max(0, ((this.zoom - 1) * VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, ((this.zoom - 1) * VIEWPORT_SIZE) / 2)
			this.offsetX = Math.min(maxOffsetX, Math.max(-maxOffsetX, this.dragStart.offsetX + dx))
			this.offsetY = Math.min(maxOffsetY, Math.max(-maxOffsetY, this.dragStart.offsetY + dy))
		},
		stopDrag() {
			this.dragging = false
			window.removeEventListener('mousemove', this.onDrag)
			window.removeEventListener('touchmove', this.onDrag)
			window.removeEventListener('mouseup', this.stopDrag)
			window.removeEventListener('touchend', this.stopDrag)
		},
		// Draws the image "contained" (aspect ratio kept, never stretched)
		// into the square output with the current zoom/pan.
		renderToDataUrl() {
			const img = this.canvasImage
			if (!img) return null
			const canvas = document.createElement('canvas')
			canvas.width = OUTPUT_SIZE
			canvas.height = OUTPUT_SIZE
			const ctx = canvas.getContext('2d')
			const ratio = OUTPUT_SIZE / VIEWPORT_SIZE
			const nw = img.naturalWidth || OUTPUT_SIZE
			const nh = img.naturalHeight || OUTPUT_SIZE
			const fit = Math.min(OUTPUT_SIZE / nw, OUTPUT_SIZE / nh)
			const dw = nw * fit
			const dh = nh * fit

			ctx.save()
			ctx.translate(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2)
			ctx.translate(this.offsetX * ratio, this.offsetY * ratio)
			ctx.scale(this.zoom, this.zoom)
			ctx.drawImage(img, -dw / 2, -dh / 2, dw, dh)
			ctx.restore()
			// Throws a SecurityError if the canvas is tainted.
			return canvas.toDataURL('image/png')
		},

		async apply() {
			if (this.isApplying) return
			let dataUrl = null
			try {
				dataUrl = this.renderToDataUrl()
			} catch (e) {
				console.warn('IconEditorModal: canvas export blocked', e)
				dataUrl = null
			}
			if (!dataUrl) {
				this.corsBlocked = true
			}
			const payload = {
				dataUrl,
				rawSrc: this.src,
				zoom: this.zoom,
				offsetX: this.offsetX,
				offsetY: this.offsetY,
				radius: this.radius
			}
			this.$emit('apply', payload)

			if (this.folderId) {
				this.isApplying = true
				try {
					await this.setFolderIcon(this.folderId, payload.dataUrl, payload.radius)
					this.$EventBus.$emit(events.GET_APP_LIST)
					if (!dataUrl) {
						this.$buefy.toast.open({
							message: this.$t('Only the roundness was saved - this image cannot be cropped here.'),
							type: 'is-warning',
							position: 'is-top',
							duration: 5000
						})
					}
				} catch (e) {
					this.$buefy.toast.open({
						message: this.$t('Folders could not be updated: {reason}', { reason: apiErrorHtml(e, this.$t('Something went wrong')) }),
						type: 'is-danger',
						position: 'is-top',
						duration: 5000
					})
					return
				} finally {
					this.isApplying = false
				}
			}
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.icon-editor-card {
	max-width: 24rem;
	border-radius: var(--radius-modal);
	overflow: hidden;
}

.icon-editor-viewport {
	position: relative;
	width: 220px;
	height: 220px;
	margin: var(--space-2) auto var(--space-5);
	overflow: hidden;
	background-color: var(--theme-text-primary, #0f172a);
	background-image: repeating-conic-gradient(rgba(255, 255, 255, 0.07) 0% 25%, transparent 0% 50%);
	background-size: 16px 16px;
	background-position: 50% 50%;
	border: 1px solid rgba(15, 23, 42, 0.3);
	border-radius: var(--radius-card);
	box-shadow: inset 0 3px 10px rgba(0, 0, 0, 0.4);
	cursor: grab;
	user-select: none;

	&:active {
		cursor: grabbing;
	}
}

.icon-editor-crop {
	position: absolute;
	inset: 0;
	overflow: hidden;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: border-radius 0.12s ease;
	box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.15), 0 4px 16px rgba(0, 0, 0, 0.35);
}

.icon-preview-img {
	width: 100% !important;
	height: 100% !important;
	max-width: none !important;
	max-height: none !important;
	object-fit: contain;
	user-select: none;
	pointer-events: none;
	display: block;
}

.icon-editor-notice {
	display: flex;
	align-items: flex-start;
	margin: 0 0 var(--space-3);
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);
	background: var(--theme-card-subtle);
	color: var(--color-warning-fg);
	font-size: var(--font-xs);
	line-height: 1.4;
}

.editor-control {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	margin-bottom: var(--space-3);

	label {
		flex-shrink: 0;
		width: 7.5rem;
		font-size: var(--font-sm);
		font-weight: 500;
		color: var(--theme-text-secondary, #334155);
		display: flex;
		align-items: center;

		i {
			font-size: var(--font-lg);
			color: var(--theme-text-muted, #64748b);
		}
	}

	.slider-control {
		flex-grow: 1;
		accent-color: var(--color-primary, #2563eb);
		cursor: pointer;

		&:focus-visible {
			outline: 2px solid var(--color-primary-fg);
			outline-offset: 2px;
		}

		&:disabled {
			cursor: not-allowed;
			opacity: 0.5;
		}
	}

	.zoom-value {
		flex-shrink: 0;
		width: 2.5rem;
		font-size: var(--font-xs);
		font-weight: 600;
		color: var(--theme-text-muted, #64748b);
		text-align: right;
	}
}
</style>
