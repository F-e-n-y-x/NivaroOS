<template>
	<b-dropdown ref="dd" position="is-bottom-left" append-to-body :close-on-click="false" aria-role="menu" class="widget-fields-menu" @active-change="onActive">
		<template #trigger>
			<button type="button" class="widget-icon-btn" :title="$t('Choose what this widget shows')" :aria-label="$t('Choose what this widget shows')">
				<i class="mdi mdi-tune-variant" aria-hidden="true"></i>
			</button>
		</template>
		<div class="widget-fields-title">{{ $t('Show') }}</div>
		<b-dropdown-item
			v-for="f in fields"
			:key="f.key"
			aria-role="menuitemcheckbox"
			:aria-checked="f.on ? 'true' : 'false'"
			class="widget-fields-item"
			@click="$emit('toggle', f.key)"
		>
			<i class="mdi" :class="f.on ? 'mdi-checkbox-marked' : 'mdi-checkbox-blank-outline'" aria-hidden="true"></i>
			<span>{{ f.raw ? f.label : $t(f.label) }}</span>
		</b-dropdown-item>
		<div v-if="!fields.length" class="widget-fields-title">{{ $t('Nothing to choose yet') }}</div>
	</b-dropdown>
</template>

<script>
// fields: [{ key, label, on, raw? }] - raw labels (drive names) skip $t.
export default {
	name: 'widget-fields-menu',
	props: { fields: { type: Array, required: true } },
	emits: ['toggle'],
	beforeUnmount() {
		this.onActive(false)
	},
	methods: {
		// The menu lives on <body> at a fixed spot: when the widget column
		// (or anything else) scrolls, close it instead of leaving it behind.
		onActive(open) {
			window[open ? 'addEventListener' : 'removeEventListener']('scroll', this.onScroll, true)
		},
		onScroll(e) {
			const menu = this.$refs.dd && this.$refs.dd.$refs.dropdownMenu
			if (menu && e.target instanceof Node && menu.contains(e.target)) return
			this.$refs.dd.isActive = false
		}
	}
}
</script>

<style lang="scss">
.widget-fields-title {
	padding: var(--space-1) var(--space-3);
	font-size: var(--font-xs);
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	opacity: 0.6;
}

.widget-fields-item {
	display: flex !important;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-sm);

	.mdi {
		font-size: 1rem;
		color: var(--color-primary-fg, #1d4ed8);
	}
}
</style>
