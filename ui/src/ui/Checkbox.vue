<script setup>
import { computed, ref, watch } from 'vue'

// Buefy's b-checkbox: label.b-checkbox.checkbox > input + span.check + span.control-label.
const props = defineProps({
	modelValue: null,
	// Boolean among the types, as Buefy: an absent value is false (value="false").
	nativeValue: [String, Number, Boolean, Function, Object, Array, Date],
	disabled: Boolean,
	type: String,
	name: String,
	required: Boolean,
	size: String,
	indeterminate: Boolean,
	trueValue: { type: null, default: true },
	falseValue: { type: null, default: false },
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
</script>

<template>
	<label
		ref="label"
		class="b-checkbox checkbox"
		:class="[size, { 'is-disabled': disabled }]"
		:disabled="disabled || undefined"
		@click="input.focus()"
		@keydown.prevent.enter="label.click()"
		@keydown.prevent.space="label.click()"
	>
		<input
			ref="input"
			v-model="model"
			:indeterminate.prop="indeterminate"
			type="checkbox"
			autocomplete="on"
			:disabled="disabled || undefined"
			:required="required || undefined"
			:name="name"
			:value="nativeValue"
			:true-value="trueValue"
			:false-value="falseValue"
			@click.stop
		/>
		<span class="check" :class="type" />
		<span class="control-label"><slot /></span>
	</label>
</template>
