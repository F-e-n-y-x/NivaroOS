<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

// Buefy's b-image: figure.b-image-wrapper.image > img, natively lazy,
// with a fallback src when the image fails to load.
// ponytail: no webp-fallback (every browser we support shows webp); the
// prop is accepted and ignored.
const props = defineProps({
	src: String,
	alt: String,
	srcFallback: String,
	webpFallback: String,
	lazy: { type: Boolean, default: true },
	ratio: String,
	rounded: Boolean,
})
const failed = ref(false)
const width = ref(0)
const figure = ref(null)
const currentSrc = computed(() => (failed.value && props.srcFallback ? props.srcFallback : props.src))
const hasRatio = computed(() => !!props.ratio && /([0-9]+)by([0-9]+)/.test(props.ratio))
const setWidth = () => (width.value = figure.value ? figure.value.clientWidth : 0)
onMounted(() => {
	setWidth()
	window.addEventListener('resize', setWidth)
})
onBeforeUnmount(() => window.removeEventListener('resize', setWidth))
</script>

<template>
	<figure ref="figure" class="b-image-wrapper image" :class="hasRatio ? `is-${ratio}` : null">
		<transition name="fade">
			<img
				:src="currentSrc"
				:alt="alt"
				:class="{ 'is-rounded': rounded, 'has-ratio': hasRatio }"
				:width="width > 0 ? width : undefined"
				:loading="lazy ? 'lazy' : undefined"
				@error="failed = true"
			/>
		</transition>
	</figure>
</template>
