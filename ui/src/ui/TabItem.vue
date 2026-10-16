<script setup>
import { computed, getCurrentInstance, inject, onBeforeUnmount, reactive, toRefs } from 'vue'

// Buefy's b-tab-item: its panel (div.tab-item) inside ui/Tabs.vue, which
// draws the tab itself from what this registers.
const props = defineProps({
	label: String,
	value: { type: String, default: null },
	icon: String,
	iconPack: String,
	visible: { type: Boolean, default: true },
	disabled: Boolean,
	headerClass: { type: [String, Array, Object], default: null },
})
const tabs = inject('BTabs')
// No value: the component uid, as Buefy did (the ids hang off it).
const uid = getCurrentInstance().uid
const item = reactive({ ...toRefs(props), uniqueValue: computed(() => (props.value != null ? props.value : uid)) })
tabs.register(item)
onBeforeUnmount(() => tabs.unregister(item))
const active = computed(() => tabs.activeItem.value === item)
</script>

<template>
	<div
		v-show="active && visible"
		class="tab-item"
		role="tabpanel"
		:id="`${item.uniqueValue}-content`"
		:aria-labelledby="`${item.uniqueValue}-label`"
		:tabindex="active ? 0 : -1"
	><slot /></div>
</template>
