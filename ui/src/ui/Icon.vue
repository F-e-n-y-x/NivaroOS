<script setup>
import { computed } from 'vue'
import { iconPacks } from './icons'

// Same DOM as Buefy's b-icon: <span class="icon [size]"><i class="[pack]
// [prefix+icon] [size class] [customClass]"></i></span>.
const props = defineProps({
	icon: { type: String, required: true },
	pack: String,
	size: String,
	customSize: String,
	customClass: String,
})
// `|| 'mdi'` as Buefy did: a null or empty pack is mdi too.
const packName = computed(() => props.pack || 'mdi')
const packOf = computed(() => iconPacks[packName.value] || {})
const sizeClass = computed(() => {
	if (props.customSize) return props.customSize
	const sizes = packOf.value.sizes
	if (!sizes) return null
	return props.size && sizes[props.size] !== undefined ? sizes[props.size] : sizes.default
})
</script>

<template>
	<span class="icon" :class="size">
		<i :class="[packName, `${packOf.iconPrefix || ''}${icon}`, sizeClass, customClass]" />
	</span>
</template>
