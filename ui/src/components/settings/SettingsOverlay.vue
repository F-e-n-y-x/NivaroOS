<template>
	<transition name="fade">
		<div v-if="active" class="settings-overlay" @click.self="$emit('close')">
			<div class="settings-overlay-backdrop" @click="$emit('close')"></div>
			<div class="settings-overlay-card" :style="{ width: cardWidth, maxWidth: 'calc(100% - 2rem)' }">
				<header class="settings-overlay-head">
					<span class="settings-overlay-title">{{ title }}</span>
					<button type="button" class="settings-overlay-close" @click="$emit('close')">
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
	computed: {
		cardWidth() {
			return typeof this.width === 'number' ? `${this.width}px` : this.width
		}
	}
}
</script>

<style lang="scss" scoped>
.settings-overlay {
	position: absolute;
	inset: 0;
	z-index: 100;
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
	background: #ffffff;
	border-radius: 14px;
	border: 1px solid #e2e8f0;
	box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.15), 0 10px 10px -5px rgba(0, 0, 0, 0.04);
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
	padding: 0.85rem 1.25rem;
	border-bottom: 1px solid #f1f5f9;
	background: #ffffff;
}

.settings-overlay-title {
	font-size: 0.9375rem;
	font-weight: 600;
	color: #1e293b;
}

.settings-overlay-close {
	border: none;
	background: rgba(0, 0, 0, 0.04);
	color: #64748b;
	cursor: pointer;
	border-radius: 50%;
	width: 1.6rem;
	height: 1.6rem;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: background 0.15s ease, color 0.15s ease;

	&:hover {
		background: rgba(0, 0, 0, 0.09);
		color: #1e293b;
	}
}

.settings-overlay-body {
	padding: 1.25rem;
	overflow-y: auto;
	color: #334155;
	font-size: 0.875rem;
}

.settings-overlay-foot {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.6rem;
	padding: 0.85rem 1.25rem;
	border-top: 1px solid #f1f5f9;
	background: #f8fafc;
}
</style>
