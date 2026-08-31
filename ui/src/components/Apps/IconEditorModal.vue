<template>
	<div class="modal-card icon-editor-card">
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ $t('Edit Icon') }}</h3>
			</div>
			<b-icon class="close-button" icon="close-outline" pack="casa" @click.native="$emit('close')" />
		</header>
		<section class="modal-card-body">
			<div ref="viewport" class="icon-editor-viewport" @mousedown="startDrag" @touchstart="startDrag">
				<div class="icon-editor-crop" :style="{ borderRadius: radius + '%' }">
					<img
						ref="img"
						:src="src"
						:style="imgTransformStyle"
						draggable="false"
						class="icon-preview-img"
						@load="onImageLoaded"
					/>
				</div>
			</div>

			<div class="editor-control">
				<label><i class="mdi mdi-magnify-plus-outline mr-1"></i>{{ $t('Zoom') }}</label>
				<input v-model.number="zoom" max="3" min="1" step="0.02" type="range" class="slider-control" />
				<span class="zoom-value">{{ Math.round(zoom * 100) }}%</span>
			</div>
			<div class="editor-control">
				<label><i class="mdi mdi-rounded-corner mr-1"></i>{{ $t('Roundness') }}</label>
				<input v-model.number="radius" max="50" min="0" step="1" type="range" class="slider-control" />
				<span class="zoom-value">{{ radius }}%</span>
			</div>
		</section>
		<footer class="modal-card-foot is-flex is-align-items-center">
			<b-button rounded size="is-small" @click="resetTransforms">
				<i class="mdi mdi-restore mr-1"></i>
				{{ $t('Reset') }}
			</b-button>
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Apply')" rounded type="is-primary" @click="apply" />
			</div>
		</footer>
	</div>
</template>

<script>
const VIEWPORT_SIZE = 220
const OUTPUT_SIZE = 256

export default {
	name: 'IconEditorModal',
	props: {
		src: {
			type: String,
			required: true
		},
		initialRadius: {
			type: Number,
			default: 0
		}
	},
	data() {
		return {
			zoom: 1,
			offsetX: 0,
			offsetY: 0,
			radius: this.initialRadius,
			imageLoaded: false,
			naturalWidth: 0,
			naturalHeight: 0,
			dragging: false,
			dragStart: null
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
			img.crossOrigin = 'anonymous'
			img.onload = () => {
				this.naturalWidth = img.naturalWidth || VIEWPORT_SIZE
				this.naturalHeight = img.naturalHeight || VIEWPORT_SIZE
				this.imageLoaded = true
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
		apply() {
			const canvas = document.createElement('canvas')
			canvas.width = OUTPUT_SIZE
			canvas.height = OUTPUT_SIZE
			const ctx = canvas.getContext('2d')

			try {
				const img = this.$refs.img
				const ratio = OUTPUT_SIZE / VIEWPORT_SIZE

				ctx.save()
				ctx.translate(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2)
				ctx.translate(this.offsetX * ratio, this.offsetY * ratio)
				ctx.scale(this.zoom, this.zoom)
				ctx.drawImage(img, -OUTPUT_SIZE / 2, -OUTPUT_SIZE / 2, OUTPUT_SIZE, OUTPUT_SIZE)
				ctx.restore()

				this.$emit('apply', { dataUrl: canvas.toDataURL('image/png'), radius: this.radius })
			} catch (e) {
				// Cross-origin image fallback
				this.$emit('apply', { dataUrl: null, radius: this.radius })
			}
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.icon-editor-card {
	max-width: 24rem;
	border-radius: 14px;
	overflow: hidden;
}

.icon-editor-viewport {
	position: relative;
	width: 220px;
	height: 220px;
	margin: 0.5rem auto 1.25rem;
	overflow: hidden;
	background: repeating-conic-gradient(#0000000d 0% 25%, transparent 0% 50%) 50% / 16px 16px;
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 12px;
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
}

.icon-preview-img {
	width: 100% !important;
	height: 100% !important;
	max-width: none !important;
	max-height: none !important;
	object-fit: cover;
	user-select: none;
	pointer-events: none;
	display: block;
}

.editor-control {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	margin-bottom: 0.85rem;

	label {
		flex-shrink: 0;
		width: 7.5rem;
		font-size: 0.8125rem;
		font-weight: 500;
		color: #334155;
		display: flex;
		align-items: center;

		i {
			font-size: 1.1rem;
			color: #64748b;
		}
	}

	.slider-control {
		flex-grow: 1;
		accent-color: #2563eb;
		cursor: pointer;
	}

	.zoom-value {
		flex-shrink: 0;
		width: 2.5rem;
		font-size: 0.75rem;
		font-weight: 600;
		color: #64748b;
		text-align: right;
	}
}
</style>
