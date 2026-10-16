<script setup>
import { ref, useAttrs, watch } from 'vue'
import { splitAttrs } from './field'

// Buefy's b-upload, single file: label.upload.control wrapping the slot
// (or a drop zone with drag-drop) and a hidden input[type=file]. A file
// that doesn't match `accept` (".iso,application/x-iso9660-image") is
// refused and `invalid` emitted.
defineOptions({ inheritAttrs: false })
const props = defineProps({
	modelValue: null,
	accept: String,
	disabled: Boolean,
	dragDrop: Boolean,
	expanded: Boolean,
	type: { type: String, default: 'is-primary' },
})
const emit = defineEmits(['update:modelValue', 'invalid'])
const attrs = useAttrs()
const input = ref(null)
const value = ref(props.modelValue)
const hovered = ref(false)
watch(() => props.modelValue, (v) => {
	value.value = v
	if (!v && input.value) input.value.value = ''
})

function accepted(file) {
	const types = (props.accept || '').split(',').map((t) => t.trim()).filter(Boolean)
	const ok = !types.length || types.some((t) => (t.startsWith('.') ? file.name.toLowerCase().endsWith(t.toLowerCase()) : file.type.match(t)))
	if (!ok) emit('invalid')
	return ok
}
function onFiles(e) {
	if (props.disabled) return
	if (props.dragDrop) hovered.value = false
	const files = (e.target && e.target.files) || (e.dataTransfer && e.dataTransfer.files) || []
	if (!files.length || (props.dragDrop && files.length !== 1)) return
	if (accepted(files[0])) value.value = files[0]
	else {
		input.value.value = ''
		if (!value.value) return
		value.value = null
	}
	emit('update:modelValue', value.value)
}
const hover = (on) => !props.disabled && (hovered.value = on)
</script>

<template>
	<label class="upload control" :class="[{ 'is-expanded': expanded }, attrs.class]" :style="attrs.style" :id="attrs.id">
		<div
			v-if="dragDrop"
			class="upload-draggable"
			:class="[type, { 'is-disabled': disabled, 'is-hovered': hovered, 'is-expanded': expanded }]"
			@dragover.prevent="hover(true)"
			@dragleave.prevent="hover(false)"
			@dragenter.prevent="hover(true)"
			@drop.prevent="onFiles"
		><slot /></div>
		<slot v-else />
		<input ref="input" type="file" v-bind="splitAttrs(attrs)" :accept="accept" :disabled="disabled || undefined" @change="onFiles" />
	</label>
</template>
