<template>
	<transition name="fade">
		<div v-if="active" class="settings-overlay" @click.self="$emit('close')">
			<div class="settings-overlay-backdrop" @click="$emit('close')"></div>
			<div class="settings-overlay-card" :style="cardStyle">
				<header class="settings-overlay-head" @pointerdown="startDrag">
					<span class="settings-overlay-title">{{ title }}</span>
					<button type="button" class="settings-overlay-close" @pointerdown.stop @click.stop="$emit('close')">
						<b-icon icon="close" size="is-small" pack="mdi"></b-icon>
					</button>
				</header>
				<div class="settings-overlay-body" :class="bodyClass">
					<slot></slot>
				</div>
				<footer v-if="$slots.footer" class="settings-overlay-foot">
					<slot name="footer"></slot>
				</footer>
			</div>
		</div>
	</transition>
</template>

<script>
export default {
	name: 'settings-overlay',
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		width: { type: [String, Number], default: '32rem' },
		bodyClass: { type: String, default: '' }
	},
	data() {
		return {
			dragOffset: { x: 0, y: 0 }
		}
	},
	watch: {
		active(val) {
			if (val) {
				this.dragOffset = { x: 0, y: 0 }
			}
		}
	},
	computed: {
		cardWidth() {
			return typeof this.width === 'number' ? `${this.width}px` : this.width
		},
		cardStyle() {
			const style = {
				width: this.cardWidth,
				maxWidth: 'calc(100% - 2rem)'
			}
			if (this.dragOffset.x || this.dragOffset.y) {
				style.transform = `translate(${this.dragOffset.x}px, ${this.dragOffset.y}px)`
			}
			return style
		}
	},
	methods: {
		startDrag(e) {
			if (e.target.closest('button, input, select, textarea, a, .settings-overlay-close')) return
			const startX = e.clientX
			const startY = e.clientY
			const originX = this.dragOffset.x
			const originY = this.dragOffset.y
			document.body.style.userSelect = 'none'

			const onMove = moveEvent => {
				this.dragOffset = {
					x: originX + (moveEvent.clientX - startX),
					y: originY + (moveEvent.clientY - startY)
				}
			}
			const onUp = () => {
				window.removeEventListener('pointermove', onMove)
				window.removeEventListener('pointerup', onUp)
				document.body.style.userSelect = ''
			}
			window.addEventListener('pointermove', onMove)
			window.addEventListener('pointerup', onUp)
		}
	}
}
</script>

<style lang="scss" scoped>
.settings-overlay {
	position: fixed;
	inset: 0;
	z-index: 2000;
	display: flex;
	align-items: center;
	justify-content: center;
}

.settings-overlay-backdrop {
	position: absolute;
	inset: 0;
	background: rgba(15, 23, 42, 0.45);
	backdrop-filter: blur(3px);
}

.settings-overlay-card {
	position: relative;
	z-index: 1;
	background: var(--theme-card-bg, #ffffff);
	border-color: var(--theme-card-border, #e2e8f0);
	color: var(--theme-text-primary, #334155);
	border-radius: var(--radius-modal);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	box-shadow: var(--shadow-lg);
	display: flex;
	flex-direction: column;
	max-height: calc(100% - 2.5rem);
	overflow: hidden;
	animation: popIn 0.18s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes popIn {
	from {
		opacity: 0;
		transform: scale(0.96) translateY(6px);
	}
	to {
		opacity: 1;
		transform: scale(1) translateY(0);
	}
}

.settings-overlay-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-5);
	border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
	background: var(--theme-card-bg, #ffffff);
	border-color: var(--theme-card-border, #f1f5f9);
	color: var(--theme-text-primary, #1e293b);
	cursor: grab;
	user-select: none;
	touch-action: none;

	&:active {
		cursor: grabbing;
	}
}

.settings-overlay-title {
	font-size: var(--font-md);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
}

.settings-overlay-close {
	border: none;
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	color: var(--theme-text-muted, #64748b);
	cursor: pointer;
	border-radius: 50%;
	width: 1.6rem;
	height: 1.6rem;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: background 0.15s ease, color 0.15s ease;

	&:hover {
		background: var(--theme-card-border, rgba(0, 0, 0, 0.09));
		color: var(--theme-text-primary, #1e293b);
	}
}

.settings-overlay-body {
	padding: var(--space-5);
	overflow-y: auto;
	color: var(--theme-text-secondary, #334155);
	font-size: var(--font-base);
}

.settings-overlay-foot {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, #f1f5f9);
	background: var(--theme-input-bg, #f8fafc);
	border-color: var(--theme-card-border, #f1f5f9);
}
</style>
