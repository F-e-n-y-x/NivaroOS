<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useAttrs, useSlots, watch } from 'vue'
import BInput from './Input.vue'
import { fieldProps, splitAttrs, useField } from './field'

// Buefy's b-autocomplete, the parts this app uses: a b-input with a
// .dropdown-menu of matching options under it (above it when there's no
// room), opened on focus or typing, Up/Down to move, Enter/Tab or a click
// to pick (emits `select`), the #empty slot, and append-to-body.
// ponytail: no groups, header/footer, keep-first, clearable or infinite
// scroll - nothing here uses them.
defineOptions({ inheritAttrs: false })
const props = defineProps({
	modelValue: [Number, String, null],
	...fieldProps,
	data: { type: Array, default: () => [] },
	field: { type: String, default: 'value' },
	openOnFocus: Boolean,
	maxHeight: [String, Number],
	appendToBody: Boolean,
	maxlength: [Number, String],
	type: { type: String, default: 'text' },
})
const emit = defineEmits(['update:modelValue', 'select', 'typing', 'focus', 'blur', 'active'])
const attrs = useAttrs()
const slots = useSlots()

const input = ref(null)
const menu = ref(null)
const inputEl = computed(() => (input.value ? input.value.getElement() : null))
const { checkValidity } = useField(props, inputEl)

const value = ref(props.modelValue)
const selected = ref(null)
const hovered = ref(null)
const active = ref(false)
const focused = ref(false)
const openedTop = ref(false)
const style = ref({})

const valueOf = (o) => (o === null ? undefined : typeof o === 'object' ? props.field.split('.').reduce((v, k) => (v == null ? v : v[k]), o) : o)
const isEmpty = computed(() => !props.data.length)
const maxH = computed(() => (props.maxHeight == null ? undefined : /^\d+$/.test(String(props.maxHeight)) ? `${props.maxHeight}px` : props.maxHeight))

watch(() => props.modelValue, (v) => (value.value = v))
watch(value, (v) => {
	emit('update:modelValue', v)
	const current = valueOf(selected.value)
	if (current && current !== v) select(null, false)
	if (focused.value && (!props.openOnFocus || v)) active.value = !!v
})
watch(() => props.data, () => {
	if (hovered.value && !props.data.some((d) => valueOf(d) === valueOf(hovered.value))) hovered.value = null
})
watch(active, (on) => {
	// Room below? Otherwise open above (re-measured once it has closed).
	if (on) measure()
	else setTimeout(measure, 100)
	nextTick(() => emit('active', on))
})

function select(option, close = true, event) {
	if (option === undefined) return
	selected.value = option
	emit('select', option, event)
	if (option !== null) {
		value.value = valueOf(option)
		hovered.value = null
	}
	if (close) nextTick(() => (active.value = false))
	if (props.useHtml5Validation) nextTick(checkValidity)
}
function onKeydown(e) {
	if (e.key === 'Enter') e.preventDefault()
	if (e.key === 'Escape' || e.key === 'Tab') active.value = false
	if ((e.key === 'Enter' || e.key === 'Tab') && hovered.value !== null) select(hovered.value, true, e)
}
function arrows(dir) {
	if (!active.value) return (active.value = true)
	const list = props.data
	const i = Math.min(Math.max(list.indexOf(hovered.value) + dir, 0), list.length - 1)
	hovered.value = list[i] !== undefined ? list[i] : null
	// Keep the hovered option in view.
	const box = menu.value && menu.value.querySelector('.dropdown-content')
	const el = box && box.querySelectorAll('a.dropdown-item:not(.is-disabled)')[i]
	if (!el) return
	if (el.offsetTop < box.scrollTop) box.scrollTop = el.offsetTop
	else if (el.offsetTop >= box.scrollTop + box.clientHeight - el.clientHeight) box.scrollTop = el.offsetTop - box.clientHeight + el.clientHeight
}
function onFocus(e) {
	if (valueOf(selected.value) === value.value && inputEl.value) inputEl.value.select()
	if (props.openOnFocus) active.value = true
	focused.value = true
	emit('focus', e)
}
function onBlur(e) {
	focused.value = false
	emit('blur', e)
}
function onInput() {
	const current = valueOf(selected.value)
	if (current && current === value.value) return
	emit('typing', value.value)
	if (props.useHtml5Validation) nextTick(checkValidity)
}
function clickedOutside(e) {
	if (focused.value) return
	if (e.target === inputEl.value || (menu.value && menu.value.contains(e.target))) return
	active.value = false
}
function measure() {
	nextTick(() => {
		if (!menu.value) return
		const r = menu.value.getBoundingClientRect()
		openedTop.value = !(r.top >= 0 && r.bottom <= (window.innerHeight || document.documentElement.clientHeight))
		if (props.appendToBody) place()
	})
}
function place() {
	const trigger = input.value && input.value.$el
	if (!menu.value || !trigger) return
	const rect = trigger.getBoundingClientRect()
	let top = rect.top + window.scrollY
	if (!openedTop.value) top += trigger.clientHeight
	else top -= menu.value.clientHeight
	const w = `${trigger.clientWidth}px`
	style.value = { position: 'absolute', top: `${top}px`, left: `${rect.left + window.scrollX}px`, width: w, maxWidth: w, zIndex: '99' }
}
onMounted(() => {
	document.addEventListener('click', clickedOutside)
	window.addEventListener('resize', measure)
	if (props.appendToBody) place()
})
onBeforeUnmount(() => {
	document.removeEventListener('click', clickedOutside)
	window.removeEventListener('resize', measure)
})
defineExpose({ focus: () => input.value && input.value.focus(), getElement: () => inputEl.value })
const showMenu = computed(() => active.value && (!isEmpty.value || !!slots.empty))
</script>

<template>
	<div class="autocomplete control" :class="[{ 'is-expanded': expanded }, attrs.class]" :style="attrs.style" :id="attrs.id">
		<b-input
			ref="input"
			v-model="value"
			:type="type"
			:size="size"
			:rounded="rounded"
			:icon="icon"
			:icon-pack="iconPack"
			:maxlength="maxlength"
			autocomplete="off"
			:use-html5-validation="false"
			aria-autocomplete="list"
			v-bind="splitAttrs(attrs)"
			@update:model-value="onInput"
			@focus="onFocus"
			@blur="onBlur"
			@keydown="onKeydown"
			@keydown.up.prevent="arrows(-1)"
			@keydown.down.prevent="arrows(1)"
		/>
		<Teleport to="body" :disabled="!appendToBody">
			<div v-if="appendToBody" class="autocomplete control" :class="{ 'is-expanded': expanded }" style="position: absolute; left: 0px; top: 0px; width: 100%">
				<div>
					<transition name="fade">
						<div v-show="showMenu" ref="menu" class="dropdown-menu" :style="style">
							<div v-show="active" class="dropdown-content" :style="{ maxHeight: maxH }">
								<a v-for="(option, i) in data" :key="i" class="dropdown-item" role="button" tabindex="0" :class="{ 'is-hovered': option === hovered }" @click.stop="select(option, true, $event)">
									<slot v-if="$slots.default" :option="option" :index="i" />
									<span v-else>{{ valueOf(option) }}</span>
								</a>
								<div v-if="isEmpty && $slots.empty" class="dropdown-item is-disabled"><slot name="empty" /></div>
							</div>
						</div>
					</transition>
				</div>
			</div>
			<transition v-else name="fade">
				<div v-show="showMenu" ref="menu" class="dropdown-menu" :class="{ 'is-opened-top': openedTop }" :style="style">
					<div v-show="active" class="dropdown-content" :style="{ maxHeight: maxH }">
						<a v-for="(option, i) in data" :key="i" class="dropdown-item" role="button" tabindex="0" :class="{ 'is-hovered': option === hovered }" @click.stop="select(option, true, $event)">
							<slot v-if="$slots.default" :option="option" :index="i" />
							<span v-else>{{ valueOf(option) }}</span>
						</a>
						<div v-if="isEmpty && $slots.empty" class="dropdown-item is-disabled"><slot name="empty" /></div>
					</div>
				</div>
			</transition>
		</Teleport>
	</div>
</template>
