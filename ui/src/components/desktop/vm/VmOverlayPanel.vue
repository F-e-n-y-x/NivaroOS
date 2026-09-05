<!--
	A dialog confined to the app's own window bounds, instead of Buefy's
	<b-modal>. b-modal renders as position:fixed against the viewport,
	but DesktopWindow.vue's window chrome sets backdrop-filter (for the
	glass effect other apps use) - a backdrop-filter on an ancestor makes
	it the containing block for any position:fixed descendant, which
	clipped the modal to the small window instead of covering the
	viewport. This overlay is position:absolute against VmManagerApp's
	own bounds instead, so it always renders correctly contained within
	the window - the same approach VmConsole.vue already uses.
-->
<template>
	<div v-if="active" class="vm-overlay">
		<div class="vm-overlay-backdrop" @click="$emit('close')"></div>
		<div class="vm-overlay-card" :style="{ width, height }">
			<header class="vm-overlay-head">
				<span class="vm-overlay-title">{{ title }}</span>
				<button class="vm-overlay-close" @click="$emit('close')">
					<b-icon icon="close" size="is-small"></b-icon>
				</button>
			</header>
			<div class="vm-overlay-body">
				<slot></slot>
			</div>
			<footer class="vm-overlay-foot">
				<slot name="footer"></slot>
			</footer>
		</div>
	</div>
</template>

<script>
export default {
	name: 'vm-overlay-panel',
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		width: { type: String, default: '24rem' },
		// 'auto' (default) lets the card size to its content, as every
		// existing dialog wants. A fixed height is only needed when a
		// child needs real space to flex-fill into (e.g. the file picker's
		// scrolling list) - without it, flex: 1 1 auto on that child has
		// no leftover space to grow into at all, since the card itself is
		// just wrapping its content's natural size.
		height: { type: String, default: 'auto' }
	}
}
</script>

<style lang="scss" scoped>
.vm-overlay {
	position: absolute;
	inset: 0;
	z-index: 2000;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: 1rem;
}

.vm-overlay-backdrop {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.45);
	backdrop-filter: blur(2px);
}

.vm-overlay-card {
	position: relative;
	background: #fff;
	border-radius: 12px;
	box-shadow: 0 16px 40px rgba(0, 0, 0, 0.35);
	max-height: calc(100% - 1.5rem);
	max-width: calc(100% - 1.5rem);
	display: flex;
	flex-direction: column;
	overflow: hidden;

	// Every dialog (Create VM, Create bridged network, the file picker)
	// uses plain <b-button> both in its body (Browse/Clear next to an ISO
	// field) and its footer, which renders Bulma's stock bordered/white
	// button - flat, borderless, colored buttons everywhere else in this
	// app made those look like an unstyled default sitting next to
	// custom-designed ones. Scoped to the whole card (not just the
	// footer) so body buttons get it too. Overriding Buefy's own classes
	// here instead of touching every dialog individually keeps every
	// current and future dialog consistent for free.
	::v-deep .button {
		border: none;
		border-radius: 6px;
		font-weight: 500;
		font-size: 0.8125rem;
		padding: 0 0.85rem;
		height: 2rem;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: rgba(0, 0, 0, 0.045);
		color: #1e293b;
		box-shadow: none;
		transition: background 0.15s ease, color 0.15s ease;

		&:hover {
			background: rgba(0, 0, 0, 0.08);
		}
		&.is-primary {
			background: #2563eb;
			color: #fff;
			&:hover { background: #1d4ed8; }
		}
		&.is-warning {
			background: #f59e0b;
			color: #fff;
			&:hover { background: #d97706; }
		}
		&.is-danger {
			background: #ef4444;
			color: #fff;
			&:hover { background: #dc2626; }
		}
		&[disabled] {
			opacity: 0.5;
		}
	}
}

.vm-overlay-head {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1rem;
	border-bottom: 1px solid rgb(228 233 237);
	font-weight: 600;
	color: #2c3e50;
}

.vm-overlay-close {
	border: none;
	background: transparent;
	cursor: pointer;
	color: #7a7a7a;
	display: flex;
	align-items: center;
}

.vm-overlay-body {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	overflow: hidden;
	min-height: 0;
	padding: 0.85rem 1rem;
}

.vm-overlay-foot {
	flex-shrink: 0;
	display: flex;
	justify-content: flex-end;
	gap: 0.5rem;
	padding: 0.75rem 1rem;
	border-top: 1px solid rgb(228 233 237);
}
</style>
