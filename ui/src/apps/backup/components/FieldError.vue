<!-- The error text under one wizard field, referenced by the field's
     aria-describedby (see wizard/fields.js). -->
<template>
	<p v-if="code" :id="elId" class="field-error">
		<b-icon icon="alert-circle-outline" custom-size="mdi-14px" aria-hidden="true"></b-icon>
		<span>{{ $t(key_) }}</span>
	</p>
</template>

<script>
import { fieldErrorKey } from '../errorCodes'
import { errorId } from '../wizard/fields'

export default {
	name: 'FieldError',
	props: {
		idp: { type: String, required: true },
		field: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	computed: {
		code() {
			return this.errors ? this.errors[this.field] : ''
		},
		key_() {
			return fieldErrorKey(this.code)
		},
		elId() {
			return errorId(this.idp, this.field)
		}
	}
}
</script>

<style lang="scss" scoped>
.field-error {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	margin: 0;
	font-size: var(--font-xs);
	font-weight: 600;
	color: var(--color-danger-fg);
}
</style>
