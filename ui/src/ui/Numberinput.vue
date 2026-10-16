<script setup>
import { computed, onBeforeUnmount, ref, useAttrs, watch } from 'vue'
import BIcon from './Icon.vue'
import BInput from './Input.vue'
import { controlIconSize, splitAttrs } from './field'

// Buefy's b-numberinput: [-] number input [+], holding a button repeats
// every 250 ms. Steps are whole numbers here (step defaults to 1).
defineOptions({ inheritAttrs: false })
const props = defineProps({
	modelValue: [Number, null],
	min: [Number, String],
	max: [Number, String],
	step: { type: [Number, String], default: 1 },
	type: { type: String, default: 'is-primary' },
	size: String,
	disabled: Boolean,
	expanded: Boolean,
	controlsPosition: String,
	ariaMinusLabel: String,
	ariaPlusLabel: String,
})
const emit = defineEmits(['update:modelValue', 'focus', 'blur'])
const attrs = useAttrs()
const value = ref(props.modelValue)
watch(() => props.modelValue, (v) => (value.value = v))
const num = (v) => (typeof v === 'string' ? parseFloat(v) : v)
const min = computed(() => num(props.min))
const max = computed(() => num(props.max))
const step = computed(() => num(props.step))
const stepDecimals = computed(() => (String(step.value).split('.')[1] || '').length)

const model = computed({
	get: () => value.value,
	set: (v) => {
		const n = v === '' || v == null ? null : Number(v) === 0 ? 0 : Number(v) || null
		value.value = n
		if (n === null || !isNaN(n)) emit('update:modelValue', n === null ? n : Number(n))
	},
})
const isDisabled = (plus) => props.disabled || (plus ? +value.value + step.value > max.value : +value.value - step.value < min.value)
function change(plus) {
	if (value.value == null || (plus && +value.value < min.value)) {
		const start = plus ? min.value : max.value
		if (start != null) return (model.value = start)
		model.value = 0
	}
	const next = +value.value + (plus ? step.value : -step.value)
	if (plus ? max.value === undefined || next <= max.value : min.value === undefined || next >= min.value) model.value = parseFloat(next.toFixed(stepDecimals.value))
}
let timer
function startPress(e, plus) {
	if (e.button !== 0 && e.type !== 'touchstart') return
	clearTimeout(timer)
	const tick = () => {
		change(plus)
		timer = setTimeout(tick, 250)
	}
	tick()
}
const stopPress = () => {
	clearTimeout(timer)
	timer = undefined
}
// Keyboard (Enter/Space) clicks; mouse clicks went through mousedown.
const keyClick = (e, plus) => e.detail === 0 && !isDisabled(plus) && change(plus)
onBeforeUnmount(stopPress)
const iconSize = computed(() => controlIconSize(props.size))
</script>

<template>
	<div
		class="b-numberinput field"
		:class="[{ 'has-addons': controlsPosition === 'compact', 'is-grouped': controlsPosition !== 'compact', 'is-expanded': expanded }, attrs.class]"
		:style="attrs.style"
		:id="attrs.id"
	>
		<p class="control minus" @mouseup="stopPress" @mouseleave="stopPress" @touchend="stopPress" @touchcancel="stopPress">
			<button
				type="button"
				class="button"
				:class="[type, size]"
				:disabled="isDisabled(false) || undefined"
				:aria-label="ariaMinusLabel"
				@mousedown="!isDisabled(false) && startPress($event, false)"
				@touchstart.prevent="!isDisabled(false) && startPress($event, false)"
				@click="keyClick($event, false)"
			>
				<b-icon icon="minus" :size="iconSize" />
			</button>
		</p>
		<b-input
			v-model="model"
			type="number"
			v-bind="splitAttrs(attrs)"
			:step="step"
			:max="max"
			:min="min"
			:size="size"
			:disabled="disabled || undefined"
			:readonly="false"
			:expanded="expanded"
			@focus="emit('focus', $event)"
			@blur="emit('blur', $event)"
		/>
		<p class="control plus" @mouseup="stopPress" @mouseleave="stopPress" @touchend="stopPress" @touchcancel="stopPress">
			<button
				type="button"
				class="button"
				:class="[type, size]"
				:disabled="isDisabled(true) || undefined"
				:aria-label="ariaPlusLabel"
				@mousedown="!isDisabled(true) && startPress($event, true)"
				@touchstart.prevent="!isDisabled(true) && startPress($event, true)"
				@click="keyClick($event, true)"
			>
				<b-icon icon="plus" :size="iconSize" />
			</button>
		</p>
	</div>
</template>
