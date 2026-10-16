<script setup>
import { computed, inject, nextTick, ref, useAttrs, watch } from 'vue'
import BIcon from './Icon.vue'

// Buefy's b-input: div.control > input.input (or textarea.textarea) with
// the same classes. class/style/id go on the .control, every other attribute
// and listener on the input. Inside a b-field it takes the field's type
// (is-danger: red border + alert icon) and reports HTML5 validity on blur.
defineOptions({ inheritAttrs: false })
const props = defineProps({
	modelValue: [Number, String],
	type: { type: String, default: 'text' },
	size: String,
	expanded: Boolean,
	rounded: Boolean,
	icon: String,
	iconPack: String,
	maxlength: [Number, String],
	passwordReveal: Boolean,
	autocomplete: { type: String, default: 'on' },
	customClass: { type: String, default: '' },
	hasCounter: { type: Boolean, default: true },
	useHtml5Validation: { type: Boolean, default: true },
	validationMessage: String,
})
const emit = defineEmits(['update:modelValue', 'blur', 'focus'])
const attrs = useAttrs()
const field = inject('BField', null)

const el = ref(null)
const value = ref(props.modelValue)
const revealed = ref(false)
const focused = ref(false)
const valid = ref(true)

const inputType = computed(() => (props.passwordReveal && revealed.value ? 'text' : props.type))
const statusType = computed(() => {
	const t = field && field.newType
	return !t || typeof t === 'string' ? t || undefined : Object.keys(t).find((k) => t[k])
})
const statusIcon = computed(() => ({ 'is-success': 'check', 'is-danger': 'alert-circle', 'is-info': 'information', 'is-warning': 'alert' })[statusType.value])
const rightIcon = computed(() => (props.passwordReveal ? (revealed.value ? 'eye-off' : 'eye') : statusIcon.value))
const iconSize = computed(() => ({ 'is-small': 'is-small', 'is-large': (props.iconPack || 'mdi') === 'mdi' ? 'is-medium' : '' })[props.size])
const rootClasses = computed(() => [
	[props.icon && 'has-icons-left', rightIcon.value && 'has-icons-right'].filter(Boolean).join(' '),
	props.size,
	{ 'is-expanded': props.expanded, 'is-clearfix': !(field && field.newMessage) },
])
const inputAttrs = computed(() => {
	const { class: _c, style: _s, id: _i, ...rest } = attrs
	return rest
})
const length = computed(() => (typeof value.value === 'string' ? Array.from(value.value).length : value.value == null ? 0 : String(value.value).length))

// HTML5 validity goes to the field unless the caller set its type/message.
function setValidity(type, message) {
	nextTick(() => {
		if (!field) return
		if (!field.props.type) field.newType = type
		if (!field.props.message) field.newMessage = message
	})
}
function checkValidity() {
	if (!props.useHtml5Validation || !el.value) return false
	valid.value = el.value.checkValidity()
	if (valid.value) setValidity(null, null)
	else setValidity('is-danger', props.validationMessage || el.value.validationMessage)
	return valid.value
}
watch(() => props.modelValue, (v) => {
	const outside = value.value != v // eslint-disable-line eqeqeq
	value.value = v
	if (outside) nextTick(() => !valid.value && checkValidity())
})

function onInput(e) {
	value.value = e.target.value
	emit('update:modelValue', e.target.value)
	if (!valid.value) checkValidity()
}
function onBlur(e) {
	focused.value = false
	emit('blur', e)
	checkValidity()
}
function onFocus(e) {
	focused.value = true
	emit('focus', e)
}
function focus() {
	nextTick(() => el.value && el.value.focus())
}
function onRightIcon() {
	if (!props.passwordReveal) return
	revealed.value = !revealed.value
	focus()
}
defineExpose({ focus, getElement: () => el.value, checkHtml5Validity: checkValidity })
</script>

<template>
	<div class="control" :class="[rootClasses, attrs.class]" :style="attrs.style" :id="attrs.id">
		<textarea
			v-if="type === 'textarea'"
			ref="el"
			class="textarea"
			:class="[statusType, size, { 'is-rounded': rounded }, customClass]"
			:maxlength="maxlength"
			:value="value === null ? undefined : value"
			v-bind="inputAttrs"
			@input="onInput"
			@blur="onBlur"
			@focus="onFocus"
		/>
		<input
			v-else
			ref="el"
			class="input"
			:class="[statusType, size, { 'is-rounded': rounded }, customClass]"
			:type="inputType"
			:autocomplete="autocomplete"
			:maxlength="maxlength"
			:value="value"
			v-bind="inputAttrs"
			@input="onInput"
			@blur="onBlur"
			@focus="onFocus"
		/>
		<b-icon v-if="icon" class="is-left" :icon="icon" :pack="iconPack" :size="iconSize" />
		<b-icon
			v-if="rightIcon"
			class="is-right"
			:class="{ 'is-clickable': passwordReveal }"
			:icon="rightIcon"
			:pack="iconPack"
			:size="iconSize"
			:type="passwordReveal ? 'is-primary' : statusType"
			@click="onRightIcon"
		/>
		<small v-if="maxlength && hasCounter && type !== 'number'" class="help counter" :class="{ 'is-invisible': !focused }">
			{{ length }} / {{ maxlength }}
		</small>
	</div>
</template>
