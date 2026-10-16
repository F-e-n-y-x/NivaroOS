<script setup>
import { onBeforeUnmount, onMounted } from 'vue'

// Buefy's b-loading: div.loading-overlay over the page (or its positioned
// parent with is-full-page false) while v-model is true.
const props = defineProps({
	modelValue: Boolean,
	isFullPage: { type: Boolean, default: true },
	canCancel: Boolean,
	animation: { type: String, default: 'fade' },
})
const emit = defineEmits(['update:modelValue', 'close'])
function cancel() {
	if (!props.canCancel || !props.modelValue) return
	emit('close')
	emit('update:modelValue', false)
}
const onKey = (e) => (e.key === 'Escape' || e.key === 'Esc') && cancel()
onMounted(() => document.addEventListener('keyup', onKey))
onBeforeUnmount(() => document.removeEventListener('keyup', onKey))
</script>

<template>
	<transition :name="animation">
		<div v-if="modelValue" class="loading-overlay is-active" :class="{ 'is-full-page': isFullPage }">
			<div class="loading-background" @click="cancel" />
			<slot><div class="loading-icon" /></slot>
		</div>
	</transition>
</template>
