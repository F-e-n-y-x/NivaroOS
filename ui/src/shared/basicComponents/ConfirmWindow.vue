<!--
	A confirm dialog confined to the app's own window bounds, not Buefy's
	$buefy.dialog.confirm() - that renders as position:fixed against the
	whole viewport, but window chrome elsewhere in this app sets
	backdrop-filter for its glass effect, which makes that ancestor the
	containing block for any position:fixed descendant. In practice this
	means a "confirm" prompt from inside one small app window freezes the
	entire simulated desktop behind a dimmed backdrop - every other window,
	the dock, the taskbar - instead of just that one window.
	This is the same position:absolute-confined-to-the-window pattern
	VmOverlayPanel.vue and Files' DialogOverlay.vue already use
	successfully - render this as a child of the calling app's own root
	element (which needs `position: relative`, matching the convention
	already used throughout this codebase) rather than at the document root,
	and it naturally covers only that window.
-->
<template>
	<div v-if="active" class="confirm-window">
		<div class="confirm-window-backdrop" @click="$emit('cancel')"></div>
		<div class="confirm-window-card">
			<div v-if="hasIcon" class="confirm-window-icon" :class="type">
				<b-icon :icon="icon" :pack="iconPack" custom-size="mdi-24px"></b-icon>
			</div>
			<div class="confirm-window-title">{{ title }}</div>
			<div class="confirm-window-message">{{ message }}</div>
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
		// Matches Buefy's own `type` values ('is-danger', 'is-primary', ...)
		// so existing $buefy.dialog.confirm call sites need no remapping.
		type: { type: String, default: 'is-primary' },
		hasIcon: { type: Boolean, default: false },
		icon: { type: String, default: 'alert' },
		iconPack: { type: String, default: 'mdi' },
	},
}
</script>

<style lang="scss" scoped>
.confirm-window {
	position: absolute;
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
	width: min(22rem, 100%);
	max-height: calc(100% - 1.5rem);
	padding: var(--space-5);
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
	gap: var(--space-2);
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
