import { computed, inject, nextTick, ref } from 'vue'

// What a form control (b-input, b-select) shares with the b-field around it
// (ui/Field.vue): the field's type as the control's status class, and the
// control's HTML5 validity reported to the field on blur - unless the
// caller set the field's type/message itself.
export function useField(props, el) {
	const field = inject('BField', null)
	const valid = ref(true)
	const statusType = computed(() => {
		const t = field && field.newType
		return !t || typeof t === 'string' ? t || undefined : Object.keys(t).find((k) => t[k])
	})
	const hasMessage = computed(() => !!(field && field.newMessage))
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
	// Once invalid, every change re-checks (so the message goes as soon as it's fixed).
	const recheck = () => !valid.value && checkValidity()
	return { statusType, hasMessage, checkValidity, recheck }
}

export const fieldProps = {
	size: String,
	expanded: Boolean,
	rounded: Boolean,
	icon: String,
	iconPack: String,
	useHtml5Validation: { type: Boolean, default: true },
	validationMessage: String,
}

// Icon size for a control's size, as Buefy's FormElementMixin.
export const controlIconSize = (size, pack) => ({ 'is-small': 'is-small', 'is-large': (pack || 'mdi') === 'mdi' ? 'is-medium' : '' })[size]

// class/style/id on the .control wrapper, everything else on the native control.
export function splitAttrs(attrs) {
	const { class: _c, style: _s, id: _i, ...rest } = attrs
	return rest
}
