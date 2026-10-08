// Field checks for the sign-in and first-run forms (they used vee-validate).
// A rule returns the i18n key of its message, or '' when the value passes.
// The component defines computed `fieldErrors` ({ field: firstError(value, rules) })
// and calls markChecked(field) when a value changes; a field's message and
// colour show once it was checked (or validateAll() ran on submit).
export const required = v => (v === undefined || v === null || String(v).trim() === '' ? 'This field is required' : '')
export const minLength = n => v => (String(v || '').length < n ? `This field must have more than ${n} characters` : '')
export const sameAs = other => v => (v !== other ? 'This field confirmation does not match' : '')

export function firstError(value, rules) {
	for (const rule of rules) {
		const e = rule(value)
		if (e) return e
	}
	return ''
}

export default {
	data() {
		return { checkedFields: {} }
	},
	methods: {
		markChecked(...fields) {
			fields.forEach(f => (this.checkedFields[f] = true))
		},
		fieldType(f) {
			if (!this.checkedFields[f]) return ''
			return this.fieldErrors[f] ? 'is-danger' : 'is-success'
		},
		fieldMessage(f) {
			return this.checkedFields[f] && this.fieldErrors[f] ? this.$t(this.fieldErrors[f]) : ''
		},
		// Shows every field's result; true when all pass.
		validateAll() {
			this.markChecked(...Object.keys(this.fieldErrors))
			return !Object.values(this.fieldErrors).some(Boolean)
		},
	},
}
