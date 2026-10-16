<script setup>
import { computed, ref, useAttrs, watch } from 'vue'
import BIcon from './Icon.vue'
import { controlIconSize, fieldProps, splitAttrs, useField } from './field'

// Buefy's b-select: div.control > span.select > select, options from the slot.
defineOptions({ inheritAttrs: false })
const props = defineProps({
	modelValue: { type: null, default: null },
	...fieldProps,
	placeholder: String,
})
const emit = defineEmits(['update:modelValue', 'blur', 'focus'])
const attrs = useAttrs()
const el = ref(null)
const { statusType, checkValidity, recheck } = useField(props, el)
const value = ref(props.modelValue)
const model = computed({
	get: () => value.value,
	set: (v) => {
		value.value = v
		emit('update:modelValue', v)
		recheck()
	},
})
watch(() => props.modelValue, (v) => {
	value.value = v
	recheck()
})
function onBlur(e) {
	emit('blur', e)
	checkValidity()
}
</script>

<template>
	<div class="control" :class="[{ 'is-expanded': expanded, 'has-icons-left': icon }, attrs.class]" :style="attrs.style" :id="attrs.id">
		<span class="select" :class="[size, statusType, { 'is-fullwidth': expanded, 'is-rounded': rounded, 'is-empty': value === null }]">
			<select ref="el" v-model="model" v-bind="splitAttrs(attrs)" @blur="onBlur" @focus="emit('focus', $event)">
				<option v-if="placeholder && model == null" :value="null" disabled hidden>{{ placeholder }}</option>
				<slot />
			</select>
		</span>
		<b-icon v-if="icon" class="is-left" :icon="icon" :pack="iconPack" :size="controlIconSize(size, iconPack)" />
	</div>
</template>
