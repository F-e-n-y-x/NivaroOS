<template>
	<span :class="rootClass">{{ $t(content) }}</span>
</template>

<script>
export default {
	name: "tooltip-vue",
	props: {
		modal: {
			type: String,
			default: "is-success",
			validator(v) {
				return ["is-warning", "is-success", "is-danger", "is-info"].includes(v);
			}
		},
		'isBlock': {
			type: Boolean,
			default: false
		},
		content: {
			type: String,
			default: "Beta"
		}
	},
	computed: {
		rootClass() {
			return ['has-text-white', '_is-normal', {
				'_has-background-green': this.modal === 'is-success',
				'_has-background-red': this.modal === 'is-danger',
				'_tooltip-right-inline': this['isBlock'] === false,
				'_tooltip-right-block': this['isBlock'] === true
			}];
		},
	},
}
</script>

<style scoped>
._has-background-green {
	background: var(--color-success);
}

._has-background-red {
	background: var(--color-danger);
}

._is-normal {
	/* Text 400Regular/Text04 */

	font-family: $family-sans-serif;
	font-style: normal;
	font-weight: 400;
	font-size: var(--font-xs);
	line-height: 1rem;
	/* identical to box height, or 133% */

	font-feature-settings: 'pnum' on, 'lnum' on;
	height: 1.125rem;
}

span._tooltip-right-inline {
	position: relative;
	padding: 1px var(--space-1);
	gap: var(--space-2);
	border-radius: var(--radius-xs);
	top: -1rem;
	left: -0.875rem;
}

span._tooltip-right-block {
	position: relative;
	padding: 1px var(--space-1);
	gap: var(--space-2);
	border-radius: var(--radius-xs);
	top: -0.375rem;
	right: -0.5rem;
}


</style>