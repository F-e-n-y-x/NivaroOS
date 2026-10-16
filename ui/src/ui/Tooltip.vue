<script setup>
import { ref } from 'vue'

// Buefy's b-tooltip: div.b-tooltip > div.tooltip-content (shown on hover
// of the trigger, or always) + div.tooltip-trigger > slot. The CSS
// (Buefy's _tooltip.scss) draws and places it.
const props = defineProps({
	label: String,
	type: { type: String, default: 'is-primary' },
	position: { type: String, default: 'is-top' },
	always: Boolean,
	triggers: { type: Array, default: () => ['hover'] },
	animated: { type: Boolean, default: true },
	animation: { type: String, default: 'fade' },
})
const active = ref(false)
const open = (trigger) => {
	if (props.triggers.includes(trigger)) active.value = true
}
</script>

<template>
	<div class="b-tooltip is-medium" :class="[type, position, { 'is-always': always }]">
		<transition :name="animated ? animation : undefined">
			<div v-show="active || always" class="tooltip-content">{{ label }}</div>
		</transition>
		<div class="tooltip-trigger" @mouseenter="open('hover')" @focus.capture="open('focus')" @blur.capture="active = false" @mouseleave="active = false">
			<slot />
		</div>
	</div>
</template>
