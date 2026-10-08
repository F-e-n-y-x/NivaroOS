<template>
	<div class="avatar-editor">
		<div v-if="!img" class="avatar-drop" :class="{ 'is-over': dragOver }" role="button" tabindex="0"
			@click="pick" @keydown.enter.prevent="pick" @keydown.space.prevent="pick"
			@dragover.prevent="dragOver = true" @dragleave="dragOver = false" @drop.prevent="onDrop">
			<b-icon icon="image-outline" pack="mdi" custom-size="mdi-24px"></b-icon>
			<span>{{ $t('Drop a picture here or click to choose one') }}</span>
			<span class="avatar-hint">{{ $t('PNG, JPEG or WebP') }}</span>
		</div>

		<div v-else class="avatar-crop-row">
			<div ref="view" class="avatar-view" :style="{ width: VIEW + 'px', height: VIEW + 'px' }"
				@pointerdown="onDown" @pointermove="onMove" @pointerup="onUp" @pointercancel="onUp" @wheel.prevent="onWheel">
				<img :src="img.src" alt="" draggable="false" :style="imgStyle" />
				<div class="avatar-mask"></div>
			</div>
			<div class="avatar-side">
				<div class="avatar-previews">
					<canvas ref="preview" width="96" height="96" class="avatar-preview" :aria-label="$t('Preview')"></canvas>
					<canvas ref="previewSmall" width="40" height="40" class="avatar-preview is-small" aria-hidden="true"></canvas>
				</div>
				<label class="avatar-zoom">
					<span>{{ $t('Zoom') }}</span>
					<input v-model.number="zoom" type="range" min="1" max="4" step="0.01" :aria-label="$t('Zoom')" />
				</label>
				<p class="avatar-hint">{{ $t('Drag the picture to move it') }}</p>
			</div>
		</div>

		<p v-if="error" class="error-note">{{ $t(error) }}</p>

		<div class="avatar-actions">
			<b-button v-if="hasAvatar && !img" rounded size="is-small" type="is-danger" outlined :loading="busy" @click="remove">{{ $t('Remove picture') }}</b-button>
			<b-button v-if="img" rounded size="is-small" @click="pick">{{ $t('Choose another') }}</b-button>
			<span class="avatar-spacer"></span>
			<b-button rounded size="is-small" @click="$emit('close')">{{ $t('Cancel') }}</b-button>
			<b-button v-if="img" rounded size="is-small" type="is-primary" :loading="busy" @click="save">{{ $t('Save') }}</b-button>
		</div>
		<input ref="input" type="file" :accept="types" class="avatar-input" @change="onPicked" />
	</div>
</template>

<script>
import { AVATAR_OUT, AVATAR_TYPES, checkAvatarFile, clampOffset, coverScale, cropRect } from '@/utils/avatar'

const VIEW = 240

export default {
	emits: ['close', 'saved'],
	name: 'avatar-editor',
	props: {
		hasAvatar: { type: Boolean, default: false }
	},
	data() {
		return { VIEW, types: AVATAR_TYPES.join(','), img: null, zoom: 1, x: 0, y: 0, drag: null, dragOver: false, error: '', busy: false }
	},
	computed: {
		imgStyle() {
			if (!this.img) return {}
			const s = coverScale(this.img.naturalWidth, this.img.naturalHeight, VIEW, this.zoom)
			return {
				width: this.img.naturalWidth * s + 'px',
				height: this.img.naturalHeight * s + 'px',
				transform: `translate(-50%, -50%) translate(${this.x}px, ${this.y}px)`
			}
		}
	},
	watch: {
		zoom() {
			this.move(this.x, this.y)
		}
	},
	beforeUnmount() {
		if (this.img) URL.revokeObjectURL(this.img.src)
	},
	methods: {
		pick() {
			this.$refs.input.click()
		},
		onPicked(e) {
			this.load(e.target.files && e.target.files[0])
			e.target.value = ''
		},
		onDrop(e) {
			this.dragOver = false
			this.load(e.dataTransfer.files && e.dataTransfer.files[0])
		},
		load(file) {
			this.error = checkAvatarFile(file)
			if (this.error) return
			const img = new Image()
			img.onload = () => {
				if (this.img) URL.revokeObjectURL(this.img.src)
				this.img = img
				this.zoom = 1
				this.x = this.y = 0
				this.$nextTick(this.drawPreview)
			}
			img.onerror = () => {
				URL.revokeObjectURL(img.src)
				this.error = 'That file is not a picture this browser can open'
			}
			img.src = URL.createObjectURL(file)
		},
		move(x, y) {
			const o = clampOffset(x, y, this.img.naturalWidth, this.img.naturalHeight, VIEW, this.zoom)
			this.x = o.x
			this.y = o.y
			this.$nextTick(this.drawPreview)
		},
		onDown(e) {
			this.drag = { id: e.pointerId, px: e.clientX, py: e.clientY, x: this.x, y: this.y }
			e.currentTarget.setPointerCapture(e.pointerId)
		},
		onMove(e) {
			if (!this.drag || this.drag.id !== e.pointerId) return
			this.move(this.drag.x + e.clientX - this.drag.px, this.drag.y + e.clientY - this.drag.py)
		},
		onUp() {
			this.drag = null
		},
		onWheel(e) {
			this.zoom = Math.min(4, Math.max(1, this.zoom - e.deltaY * 0.002))
		},
		render(canvas) {
			const { sx, sy, side } = cropRect(this.x, this.y, this.img.naturalWidth, this.img.naturalHeight, VIEW, this.zoom)
			const ctx = canvas.getContext('2d')
			ctx.imageSmoothingQuality = 'high'
			ctx.clearRect(0, 0, canvas.width, canvas.height)
			ctx.drawImage(this.img, sx, sy, side, side, 0, 0, canvas.width, canvas.height)
			return canvas
		},
		drawPreview() {
			if (!this.img || !this.$refs.preview) return
			this.render(this.$refs.preview)
			this.render(this.$refs.previewSmall)
		},
		async save() {
			const canvas = document.createElement('canvas')
			canvas.width = canvas.height = AVATAR_OUT
			this.render(canvas)
			this.busy = true
			try {
				const res = await this.$api.users.saveAvatar({ file: canvas.toDataURL('image/png') })
				this.$emit('saved', res.data.data, canvas)
			} catch (e) {
				this.error = (e.response && e.response.data && e.response.data.message) || 'Could not save the picture'
			} finally {
				this.busy = false
			}
		},
		async remove() {
			this.busy = true
			try {
				const res = await this.$api.users.deleteAvatar()
				this.$emit('saved', res.data.data, null)
			} catch (e) {
				this.error = 'Could not remove the picture'
			} finally {
				this.busy = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.avatar-editor {
	padding: 0 var(--space-5) var(--space-5);
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.avatar-drop {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	min-height: 9rem;
	padding: var(--space-4);
	border: 2px dashed var(--theme-card-border);
	border-radius: var(--radius-control);
	color: var(--theme-text-secondary);
	text-align: center;
	cursor: pointer;

	&.is-over,
	&:hover,
	&:focus-visible {
		border-color: #2563eb;
		color: var(--theme-text-primary);
	}
}

.avatar-crop-row {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-start;
}

.avatar-view {
	position: relative;
	flex-shrink: 0;
	overflow: hidden;
	border-radius: var(--radius-control);
	background: #000;
	cursor: grab;
	touch-action: none;

	&:active {
		cursor: grabbing;
	}

	img {
		position: absolute;
		left: 50%;
		top: 50%;
		max-width: none;
		pointer-events: none;
		user-select: none;
	}
}

.avatar-mask {
	position: absolute;
	inset: 0;
	border-radius: 50%;
	box-shadow: 0 0 0 999px rgba(0, 0, 0, 0.55);
	outline: 2px solid rgba(255, 255, 255, 0.9);
	pointer-events: none;
}

.avatar-side {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	min-width: 10rem;
}

.avatar-previews {
	display: flex;
	align-items: flex-end;
	gap: var(--space-3);
}

.avatar-preview {
	width: 96px;
	height: 96px;
	border-radius: 50%;
	background: var(--theme-card-subtle);

	&.is-small {
		width: 40px;
		height: 40px;
	}
}

.avatar-zoom {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	font-size: var(--font-sm);

	input {
		width: 100%;
		accent-color: #2563eb;
	}
}

.avatar-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.avatar-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.avatar-spacer {
	flex: 1;
}

.avatar-input {
	display: none;
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
}
</style>
