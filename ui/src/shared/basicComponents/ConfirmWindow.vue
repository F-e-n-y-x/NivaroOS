<template>
	<div v-if="active" class="confirm-window">
		<div class="confirm-window-backdrop" @click="$emit('cancel')"></div>
		<div class="confirm-window-card" :style="cardStyle">
			<button type="button" class="confirm-window-close" :title="$t('Close')" @pointerdown.stop @click.stop="$emit('cancel')">
				<b-icon icon="close" size="is-small" pack="mdi"></b-icon>
			</button>
			<div class="confirm-window-header" @pointerdown="startDrag">
				<div v-if="hasIcon" class="confirm-window-icon" :class="type">
					<b-icon :icon="icon" :pack="iconPack" custom-size="mdi-24px"></b-icon>
				</div>
				<div class="confirm-window-title">{{ title }}</div>
			</div>
			<div class="confirm-window-message" v-html="message"></div>
			<div class="confirm-window-actions">
				<b-button @click="$emit('cancel')">{{ cancelText }}</b-button>
				<b-button :type="type" @click="$emit('confirm')">{{ confirmText }}</b-button>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'confirm-window',
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		message: { type: String, default: '' },
		confirmText: { type: String, default: 'OK' },
		cancelText: { type: String, default: 'Cancel' },
		type: { type: String, default: 'is-primary' },
		hasIcon: { type: Boolean, default: false },
		icon: { type: String, default: 'alert' },
		iconPack: { type: String, default: 'mdi' },
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
		cardStyle() {
			if (this.dragOffset.x || this.dragOffset.y) {
				return { transform: `translate(${this.dragOffset.x}px, ${this.dragOffset.y}px)` }
			}
			return {}
		}
	},
	methods: {
		startDrag(e) {
			if (e.target.closest('button, input, select, textarea, a')) return
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
.confirm-window {
	position: fixed;
	inset: 0;
	z-index: 2000;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: var(--space-4);
}

.confirm-window-backdrop {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.45);
	backdrop-filter: blur(2px);
}

.confirm-window-card {
	position: relative;
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-primary, #1e293b);
	border-radius: var(--radius-card);
	box-shadow: var(--shadow-xl);
	width: min(24rem, 100%);
	max-height: calc(100% - 1.5rem);
	padding: var(--space-5);
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
	gap: var(--space-2);
}

.confirm-window-close {
	position: absolute;
	top: var(--space-3);
	right: var(--space-3);
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-xs);
	display: flex;
	align-items: center;
	z-index: 2;

	&:hover {
		color: var(--theme-text-primary, #0f172a);
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	}
}

.confirm-window-header {
	display: flex;
	flex-direction: column;
	align-items: center;
	width: 100%;
	cursor: grab;
	user-select: none;
	touch-action: none;

	&:active {
		cursor: grabbing;
	}
}

.confirm-window-icon {
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	margin-bottom: var(--space-1);
	background: var(--theme-danger-soft, rgba(239, 68, 68, 0.12));
	color: var(--color-danger, #ef4444);

	&.is-primary {
		background: var(--theme-info-soft, rgba(37, 99, 235, 0.12));
		color: var(--color-primary, #2563eb);
	}
	&.is-warning {
		background: var(--theme-warning-soft, rgba(217, 119, 6, 0.12));
		color: var(--color-warning, #d97706);
	}
	&.is-success {
		background: var(--theme-success-soft, rgba(22, 163, 74, 0.12));
		color: var(--color-success, #16a34a);
	}
}

.confirm-window-title {
	font-weight: 600;
	font-size: var(--font-md);
}

.confirm-window-message {
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-sm);
	line-height: 1.5;
	word-break: break-word;
}

.confirm-window-actions {
	display: flex;
	gap: var(--space-2);
	margin-top: var(--space-3);
	width: 100%;

	.button {
		flex: 1 1 0;
	}
}
</style>
