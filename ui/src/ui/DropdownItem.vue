<script setup>
import { computed, inject } from 'vue'

// Buefy's b-dropdown-item: a.dropdown-item that picks its value in the
// b-dropdown around it (ui/Dropdown.vue).
const props = defineProps({
	value: { type: null, default: null },
	disabled: Boolean,
	ariaRole: { type: String, default: '' },
})
const emit = defineEmits(['click'])
const dropdown = inject('BDropdown')
const active = computed(() => dropdown.selected !== null && props.value === dropdown.selected)
function select() {
	if (dropdown.disabled || props.disabled) return
	dropdown.selectItem(props.value)
	emit('click')
}
</script>

<template>
	<a
		class="dropdown-item"
		:class="{ 'is-disabled': dropdown.disabled || disabled, 'is-active': active }"
		:role="ariaRole === 'menuitem' || ariaRole === 'listitem' ? ariaRole : undefined"
		tabindex="0"
		@click="select"
	><slot /></a>
</template>
