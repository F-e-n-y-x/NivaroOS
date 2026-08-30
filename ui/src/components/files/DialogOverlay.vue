<!-- src/components/files/DialogOverlay.vue -->
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
	background: #fff;
	border-radius: 8px;
	width: min(22rem, calc(100% - 2rem));
	max-height: calc(100% - 2rem);
	overflow: auto;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
}
.dialog-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1rem;
	border-bottom: 1px solid rgb(228 233 237);
}
.dialog-body { padding: 1rem; }
</style>
