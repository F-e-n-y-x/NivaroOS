<script setup>
import { computed } from 'vue'

// Buefy's b-progress: div.progress-wrapper > progress.progress; no value
// means indeterminate.
const props = defineProps({
	type: { type: String, default: 'is-darkgrey' },
	size: String,
	value: Number,
	max: { type: Number, default: 100 },
})
const text = computed(() => (props.value == null || isNaN(props.value) ? undefined : new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(props.value)))
</script>

<template>
	<div class="progress-wrapper">
		<progress class="progress" :class="[size, type, { 'is-more-than-half': value && value > max / 2 }]" :max="max" :value="value"> {{ text }} </progress>
	</div>
</template>
