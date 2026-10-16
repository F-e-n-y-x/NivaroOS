<script setup>
import { computed, provide, reactive, useSlots, watch } from 'vue'

// Buefy's b-field: a Bulma .field with a label and a .help message. Its
// type/message can also be set by the b-input inside (HTML5 validation on
// blur), unless the caller passes its own.
const props = defineProps({
	type: [String, Object],
	label: String,
	labelFor: String,
	message: [String, Array, Object],
	expanded: Boolean,
})
const slots = useSlots()
const state = reactive({ newType: props.type, newMessage: props.message, props })
watch(() => props.type, (v) => (state.newType = v))
watch(() => props.message, (v) => (state.newMessage = v))
provide('BField', state)

const messages = computed(() => {
	const m = state.newMessage
	if (!m) return []
	if (typeof m === 'string') return [m]
	const keys = (o) => Object.keys(o).filter((k) => o[k])
	return (Array.isArray(m) ? m.flatMap((x) => (typeof x === 'string' ? [x] : keys(x))) : keys(m)).filter(Boolean)
})
// More than one element in the slot: Bulma addons (input + button).
const hasAddons = () => (slots.default ? slots.default().filter((n) => typeof n.type !== 'symbol').length > 1 : false)
</script>

<template>
	<div class="field" :class="{ 'is-expanded': expanded }">
		<label v-if="label" :for="labelFor" class="label">{{ label }}</label>
		<div v-if="hasAddons()" class="field-body">
			<div class="field has-addons"><slot /></div>
		</div>
		<slot v-else />
		<p v-if="state.newMessage" class="help" :class="state.newType">
			<template v-for="(m, i) in messages" :key="i">{{ m }} <br v-if="i + 1 < messages.length" /></template>
		</p>
	</div>
</template>
