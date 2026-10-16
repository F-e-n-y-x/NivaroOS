<script setup>
import { computed, useAttrs } from 'vue'
import BIcon from './Icon.vue'

// Buefy's b-button: a Bulma .button with the same modifier classes.
const props = defineProps({
	type: String,
	size: String,
	label: String,
	iconLeft: String,
	rounded: Boolean,
	loading: Boolean,
	outlined: Boolean,
	expanded: Boolean,
	nativeType: { type: String, default: 'button' },
	tag: { type: String, default: 'button' },
})
const attrs = useAttrs()
// A disabled link can't be disabled: it renders as a button, as in Buefy.
const tag = computed(() => (attrs.disabled !== undefined && attrs.disabled !== false ? 'button' : props.tag))
const iconSize = computed(() => {
	if (!props.size || props.size === 'is-medium') return 'is-small'
	return props.size === 'is-large' ? 'is-medium' : props.size
})
</script>

<template>
	<component
		:is="tag"
		class="button"
		:type="tag === 'button' ? nativeType : undefined"
		:class="[size, type, { 'is-rounded': rounded, 'is-loading': loading, 'is-outlined': outlined, 'is-fullwidth': expanded }]"
	>
		<b-icon v-if="iconLeft" :icon="iconLeft" :size="iconSize" />
		<span v-if="label">{{ label }}</span>
		<span v-else-if="$slots.default"><slot /></span>
	</component>
</template>
