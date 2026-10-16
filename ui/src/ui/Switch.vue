<script setup>
import { computed, ref, watch } from 'vue'

// Buefy's b-switch: label.switch > input[type=checkbox] + span.check.
const props = defineProps({
	modelValue: null,
	// Boolean among the types, as Buefy: an absent value is false (value="false").
	nativeValue: [String, Number, Boolean, Function, Object, Array, Date],
	disabled: Boolean,
	type: String,
	name: String,
	required: Boolean,
	size: String,
	trueValue: { type: null, default: true },
	falseValue: { type: null, default: false },
	rounded: { type: Boolean, default: true },
})
const emit = defineEmits(['update:modelValue'])
// Local copy, as Buefy: it shows the click even if the parent only listens.
const value = ref(props.modelValue)
watch(() => props.modelValue, (v) => (value.value = v))
const model = computed({
	get: () => value.value,
	set: (v) => {
		value.value = v
		emit('update:modelValue', v)
	},
})
const label = ref(null)
const input = ref(null)
const pressed = ref(false)
</script>

<template>
	<label
		ref="label"
		class="switch"
		:class="[size, { 'is-disabled': disabled, 'is-rounded': rounded }]"
		:disabled="disabled || undefined"
		@click="input.focus()"
		@keydown.prevent.enter="label.click()"
		@mousedown="pressed = true"
		@mouseup="pressed = false"
		@mouseout="pressed = false"
		@blur="pressed = false"
	>
		<input
			ref="input"
			v-model="model"
			type="checkbox"
			:disabled="disabled || undefined"
			:name="name"
			:required="required"
			:value="nativeValue"
			:true-value="trueValue"
			:false-value="falseValue"
			@click.stop
		/>
		<span class="check" :class="[{ 'is-elastic': pressed && !disabled }, type]" />
		<span v-if="$slots.default" class="control-label"><slot /></span>
	</label>
</template>
