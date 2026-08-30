<template>
	<aside class="settings-nav" :class="{ 'is-compact': compact }">
		<button v-for="s in sections" :key="s.id" class="nav-item hover-effect _is-radius"
			:class="{ active: activeSection === s.id }" :title="compact ? $t(s.label) : null"
			@click="$emit('select', s.id)">
			<b-icon :icon="s.icon" pack="casa" size="is-20"></b-icon>
			<span v-if="!compact" class="nav-label">{{ $t(s.label) }}</span>
		</button>
	</aside>
</template>

<script>
export default {
	name: 'settings-nav',
	props: {
		sections: { type: Array, required: true },
		activeSection: { type: String, required: true },
		compact: { type: Boolean, default: false }
	}
}
</script>

<style lang="scss" scoped>
.settings-nav {
	flex-shrink: 0;
	width: 13.5rem;
	padding: 1.5rem 1rem;
	background: transparent;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
	overflow-y: auto;

	&.is-compact {
		width: 4rem;
		align-items: center;
		padding: 1.5rem 0.5rem;
	}

	&.is-compact .nav-item {
		justify-content: center;
		padding: 0.6rem;
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.7rem;
	border: none;
	background: transparent;
	color: rgba(44, 62, 80, 0.75);
	padding: 0.6rem 0.85rem;
	font-size: 0.85rem;
	border-radius: 9px;
	text-align: left;
	cursor: pointer;
	width: 100%;

	.icon {
		color: rgba(44, 62, 80, 0.5);
	}

	&:hover {
		background: rgba(0, 0, 0, 0.04);
	}

	&.active {
		background: #fff;
		color: #2c3e50;
		font-weight: 600;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);

		.icon {
			color: hsla(208, 100%, 50%, 1);
		}
	}
}

.nav-label {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
