<!--
	A VM app dialog shown as a real desktop window.

	It used to be an overlay with a dark backdrop covering the whole VM app
	window, so nothing else in the app could be used until it was closed.
	Now it is a thin wrapper around the same windowed dialog Settings uses
	(SettingsOverlay: opens a PortalWindow and moves this content into it),
	so every VM dialog can be moved, stacked and used side by side with the
	VM list, a console or any other window. Same API as before: `active`,
	`title`, `width`, `height`, `@close`, default + `footer` slots.
-->
<template>
	<settings-overlay :active="active" :title="title" :width="width" body-class="vm-dialog-body" @close="$emit('close')">
		<div class="vm-dialog" :style="height !== 'auto' ? { height } : null">
			<slot></slot>
		</div>
		<template v-if="$slots.footer" #footer>
			<div class="vm-dialog-foot">
				<slot name="footer"></slot>
			</div>
		</template>
	</settings-overlay>
</template>

<script>
import SettingsOverlay from '@/apps/settings/SettingsOverlay.vue'

export default {
	name: 'vm-overlay-panel',
	components: { SettingsOverlay },
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		width: { type: String, default: '24rem' },
		// 'auto' (default) sizes the window to its content. A fixed height
		// is only for a child that flex-fills (the file picker's list).
		height: { type: String, default: 'auto' }
	}
}
</script>

<style lang="scss" scoped>
.vm-dialog {
	display: flex;
	flex-direction: column;
	min-height: 0;
}

.vm-dialog,
.vm-dialog-foot {
	// Flat app-style buttons instead of Bulma's stock bordered white ones,
	// for body buttons (Browse/Clear) and footer buttons alike.
	::v-deep .button {
		border: none;
		border-radius: var(--radius-sm);
		font-weight: 500;
		font-size: var(--font-sm);
		padding: 0 var(--space-3);
		height: 2rem;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.045));
		color: var(--theme-text-primary, #1e293b);
		box-shadow: none;
		transition: background 0.15s ease, color 0.15s ease;

		&:hover {
			background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
		}
		&.is-primary {
			background: var(--color-primary);
			color: #fff;
			&:hover { background: #1d4ed8; }
		}
		// Darker shades than the usual amber/red so white text passes
		// WCAG AA (4.5:1); #f59e0b / #ef4444 were 2.1:1 / 3.8:1.
		&.is-warning {
			background: #b45309;
			color: #fff;
			&:hover { background: #92400e; }
		}
		&.is-danger {
			background: #dc2626;
			color: #fff;
			&:hover { background: #b91c1c; }
		}
		&[disabled] {
			opacity: 0.5;
		}
	}
}

.vm-dialog-foot {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
}
</style>
