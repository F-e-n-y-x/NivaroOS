<script setup>
import { computed, provide, reactive, ref, watch } from 'vue'
import BIcon from './Icon.vue'

// Buefy's b-tabs: nav.tabs > ul[role=tablist] > li > a for each
// b-tab-item (ui/TabItem.vue), then section.tab-content with the panels.
// v-model is the active tab's value, or its index when a number is bound.
// ponytail: no slide animation between panels (every use sets
// :animated="false"); the prop is accepted and ignored.
const props = defineProps({
	modelValue: { type: [String, Number, null], default: undefined },
	type: [String, Object],
	size: String,
	expanded: Boolean,
	animated: Boolean,
})
const emit = defineEmits(['update:modelValue'])
const items = reactive([])
const activeId = ref(props.modelValue)
const activeItem = computed(() => (activeId.value === undefined ? items[0] : activeId.value === null ? null : items.find((i) => i.uniqueValue === activeId.value)))
const fromModel = (v) => (typeof v === 'number' ? items[Math.min(Math.max(v, 0), items.length - 1)]?.uniqueValue : v)

watch(() => props.modelValue, (v) => (activeId.value = fromModel(v)))
watch(activeId, () => {
	const a = activeItem.value
	const v = a ? (typeof props.modelValue === 'number' ? items.indexOf(a) : a.uniqueValue) : undefined
	if (v !== props.modelValue) emit('update:modelValue', v)
})
provide('BTabs', {
	items,
	activeItem,
	register: (item) => {
		items.push(item)
		// A number v-model points at a tab that exists only now.
		if (typeof props.modelValue === 'number') activeId.value = fromModel(props.modelValue)
	},
	unregister: (item) => items.splice(items.indexOf(item), 1),
})

const links = ref([])
const focusAt = ref(null)
function onTablistKey(e) {
	const step = { ArrowLeft: -1, Left: -1, ArrowRight: 1, Right: 1 }[e.key]
	if (!step) return
	e.preventDefault()
	const usable = items.map((i, n) => n).filter((n) => items[n].visible && !items[n].disabled)
	if (!usable.length) return
	const at = usable.indexOf(focusAt.value)
	const next = usable[(at === -1 ? (step > 0 ? -1 : 0) : at) + step] ?? usable[step > 0 ? 0 : usable.length - 1]
	links.value[next]?.focus()
}
function onTabKey(e, item) {
	if ([' ', 'Space', 'Spacebar', 'Enter'].includes(e.key)) {
		activeId.value = item.uniqueValue
		e.preventDefault()
	}
}
</script>

<template>
	<div class="b-tabs" :class="{ 'is-fullwidth': expanded }">
		<nav class="tabs" :class="[type, size, { 'is-fullwidth': expanded, 'is-toggle': type === 'is-toggle-rounded' }]" @keydown="onTablistKey">
			<ul aria-orientation="horizontal" role="tablist">
				<li
					v-for="(item, n) in items"
					v-show="item.visible"
					:key="item.uniqueValue"
					:class="[item.headerClass, { 'is-active': item === activeItem, 'is-disabled': item.disabled }]"
					role="tab"
					:aria-controls="`${item.uniqueValue}-content`"
					:aria-selected="`${item === activeItem}`"
				>
					<a
						:ref="(el) => (links[n] = el)"
						:id="`${item.uniqueValue}-label`"
						:tabindex="item === activeItem ? 0 : -1"
						@focus="focusAt = n"
						@click="activeId = item.uniqueValue"
						@keydown="onTabKey($event, item)"
					>
						<b-icon v-if="item.icon" :icon="item.icon" :pack="item.iconPack" :size="size" />
						<span>{{ item.label }}</span>
					</a>
				</li>
			</ul>
		</nav>
		<section class="tab-content"><slot /></section>
	</div>
</template>
