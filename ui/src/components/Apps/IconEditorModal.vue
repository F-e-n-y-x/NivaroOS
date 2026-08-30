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
					<img v-if="imageLoaded" ref="img" :src="src" :style="imgStyle" draggable="false">
				</div>
			</div>

			<div class="editor-control">
				<label>{{ $t('Zoom') }}</label>
				<input v-model.number="zoom" max="3" min="1" step="0.01" type="range">
			</div>
			<div class="editor-control">
				<label>{{ $t('Corner roundness') }}</label>
				<input v-model.number="radius" max="50" min="0" step="1" type="range">
			</div>
		</section>
		<footer class="modal-card-foot is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Apply')" expaned rounded type="is-primary" @click="apply" />
			</div>
		</footer>
	</div>
</template>

<script>
const VIEWPORT_SIZE = 220
const OUTPUT_SIZE = 256

export default {
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
		// The pan offset was only ever clamped while actively dragging -
		// zooming out after panning near an edge left it stuck outside
		// the now-smaller valid range, so the image sat visibly
		// off-center/cut off until the next drag.
		zoom() {
			const maxOffsetX = Math.max(0, (this.displayWidth - VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, (this.displayHeight - VIEWPORT_SIZE) / 2)
			this.offsetX = Math.min(maxOffsetX, Math.max(-maxOffsetX, this.offsetX))
			this.offsetY = Math.min(maxOffsetY, Math.max(-maxOffsetY, this.offsetY))
		}
	},
	computed: {
		baseScale() {
			if (!this.naturalWidth || !this.naturalHeight) return 1
			return Math.max(VIEWPORT_SIZE / this.naturalWidth, VIEWPORT_SIZE / this.naturalHeight)
		},
		totalScale() {
			return this.baseScale * this.zoom
		},
		displayWidth() {
			return this.naturalWidth * this.totalScale
		},
		displayHeight() {
			return this.naturalHeight * this.totalScale
		},
		imgStyle() {
			const left = (VIEWPORT_SIZE - this.displayWidth) / 2 + this.offsetX
			const top = (VIEWPORT_SIZE - this.displayHeight) / 2 + this.offsetY
			return {
				position: 'absolute',
				width: this.displayWidth + 'px',
				height: this.displayHeight + 'px',
				left: left + 'px',
				top: top + 'px'
			}
		}
	},
	created() {
		const img = new Image()
		img.crossOrigin = 'anonymous'
		img.onload = () => {
			this.naturalWidth = img.naturalWidth
			this.naturalHeight = img.naturalHeight
			this.imageLoaded = true
			this.$nextTick(() => {
				this.$refs.img && (this.$refs.img.src = this.src)
			})
		}
		img.src = this.src
	},
	beforeDestroy() {
		this.stopDrag()
	},
	methods: {
		startDrag(e) {
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
			const maxOffsetX = Math.max(0, (this.displayWidth - VIEWPORT_SIZE) / 2)
			const maxOffsetY = Math.max(0, (this.displayHeight - VIEWPORT_SIZE) / 2)
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

		// Only the crop (position + zoom) gets baked into pixels here -
		// roundness is applied live via CSS border-radius wherever the
		// icon is shown instead. Baking roundness into the saved image
		// was destructive: reopening the editor next time would start
		// from an already-rounded (permanently transparent-cornered)
		// image, so adjusting the radius again compounded/couldn't
		// un-round it. Keeping it as separate metadata makes it always
		// re-editable, and means it doesn't silently fail to apply when
		// the source can't be read into a canvas (e.g. a cross-origin
		// URL without CORS headers) - CSS rounding never needs that.
		apply() {
			const canvas = document.createElement('canvas')
			canvas.width = OUTPUT_SIZE
			canvas.height = OUTPUT_SIZE
			const ctx = canvas.getContext('2d')

			const imgLeft = (VIEWPORT_SIZE - this.displayWidth) / 2 + this.offsetX
			const imgTop = (VIEWPORT_SIZE - this.displayHeight) / 2 + this.offsetY
			const sx = -imgLeft / this.totalScale
			const sy = -imgTop / this.totalScale
			const sSize = VIEWPORT_SIZE / this.totalScale

			try {
				ctx.drawImage(this.$refs.img, sx, sy, sSize, sSize, 0, 0, OUTPUT_SIZE, OUTPUT_SIZE)
				this.$emit('apply', { dataUrl: canvas.toDataURL('image/png'), radius: this.radius })
			} catch (e) {
				// Tainted canvas (remote image without CORS headers) - can't
				// re-crop, but the radius still applies fine on its own.
				this.$emit('apply', { dataUrl: null, radius: this.radius })
			}
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.modal-card {
	max-width: 22rem;
}

.icon-editor-viewport {
	position: relative;
	width: 220px;
	height: 220px;
	margin: 0 auto 1.25rem;
	overflow: hidden;
	background: repeating-conic-gradient(#00000010 0% 25%, transparent 0% 50%) 50% / 16px 16px;
	border-radius: 8px;
	cursor: grab;
	user-select: none;
}

.icon-editor-crop {
	position: absolute;
	inset: 0;
	overflow: hidden;
}

.editor-control {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	margin-bottom: 0.75rem;

	label {
		flex-shrink: 0;
		width: 7rem;
		font-size: 0.8rem;
	}

	input[type='range'] {
		flex-grow: 1;
	}
}
</style>
