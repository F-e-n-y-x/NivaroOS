<script setup>
import { ref, watch } from 'vue'

// Buefy's b-notification: a closable Bulma article.notification.
const props = defineProps({
	modelValue: { type: Boolean, default: true },
	type: String,
	closable: { type: Boolean, default: true },
	ariaCloseLabel: String,
	animation: { type: String, default: 'fade' },
})
const emit = defineEmits(['update:modelValue', 'close'])
const active = ref(props.modelValue)
watch(() => props.modelValue, (v) => (active.value = v))
function close() {
	active.value = false
	emit('close')
	emit('update:modelValue', false)
}
</script>

<template>
	<transition :name="animation">
		<article v-show="active" class="notification" :class="type">
			<button v-if="closable" class="delete" type="button" :aria-label="ariaCloseLabel" @click="close" />
			<div v-if="$slots.default" class="media">
				<div class="media-content"><slot /></div>
			</div>
		</article>
	</transition>
</template>
