<!-- src/apps/files/DialogOverlay.vue -->
<template>
	<div class="dialog-overlay" @click.self="$emit('close')">
		<div class="dialog-card">
			<header class="dialog-header">
				<span class="dialog-title">{{ title }}</span>
				<b-icon icon="close-outline" pack="casa" class="is-clickable" @click.native="$emit('close')"></b-icon>
			</header>
			<div class="dialog-body">
				<slot></slot>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'files-dialog-overlay',
	props: { title: { type: String, required: true } },
}
</script>

<style lang="scss" scoped>
.dialog-overlay {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.25);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 40;
}
.dialog-card {
	background: var(--theme-card-bg, #fff); border: 1px solid var(--theme-card-border, rgb(228 233 237)); color: var(--theme-text-primary, #2c3e50);
	border-radius: var(--radius-control);
	width: min(22rem, calc(100% - 2rem));
	max-height: calc(100% - 2rem);
	overflow: auto;
	box-shadow: var(--shadow-xl);
}
.dialog-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, rgb(228 233 237)); color: var(--theme-text-primary, #2c3e50);
}
.dialog-body { padding: var(--space-4); }
</style>
