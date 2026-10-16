<script>
import { trapTab } from './focus'

// Buefy's b-dropdown, the parts this app uses: click to toggle, v-model
// selection through b-dropdown-item, close on outside click / Esc / pick,
// Tab kept inside the menu, the mobile-modal look, and append-to-body
// (the menu moves to a body-level wrapper placed under the trigger).
// Options API on purpose: callers reach in as Buefy allowed
// ($refs.dd.isActive = false, $refs.dd.$refs.dropdownMenu).
export default {
	name: 'BDropdown',
	provide() {
		return { BDropdown: this }
	},
	props: {
		modelValue: { type: null, default: null },
		disabled: Boolean,
		position: String,
		mobileModal: { type: Boolean, default: true },
		ariaRole: { type: String, default: null },
		animation: { type: String, default: 'fade' },
		closeOnClick: { type: Boolean, default: true },
		appendToBody: Boolean,
	},
	emits: ['active-change', 'change', 'update:modelValue'],
	data() {
		return { selected: this.modelValue, isActive: false, style: {} }
	},
	computed: {
		// The object part also goes on the body-level wrapper, as Buefy did.
		stateClasses() {
			return { 'is-disabled': this.disabled, 'is-active': this.isActive, 'is-mobile-modal': this.mobileModal }
		},
	},
	watch: {
		modelValue(v) {
			this.selected = v
		},
		isActive(v) {
			this.$emit('active-change', v)
			if (this.mobileModal) document.documentElement.classList.toggle('is-clipped-touch', v)
			if (this.appendToBody) this.$nextTick(this.place)
		},
	},
	created() {
		document.addEventListener('click', this.clickedOutside)
		document.addEventListener('keyup', this.keyPress)
	},
	mounted() {
		if (this.appendToBody) this.place()
	},
	beforeUnmount() {
		document.removeEventListener('click', this.clickedOutside)
		document.removeEventListener('keyup', this.keyPress)
		clearTimeout(this.openTimer)
	},
	methods: {
		selectItem(value) {
			if (this.selected !== value) {
				this.selected = value
				this.$emit('change', value)
			}
			this.$emit('update:modelValue', value)
			this.isActive = !this.closeOnClick
		},
		toggle() {
			if (this.disabled) return
			// Opened on the next tick, so the click that opens it isn't
			// also the outside click that closes it.
			if (!this.isActive) this.openTimer = setTimeout(() => (this.isActive = true))
			else this.isActive = false
		},
		inside(el) {
			const { dropdownMenu, trigger } = this.$refs
			return !!el && ((dropdownMenu && dropdownMenu.contains(el)) || (trigger && trigger.contains(el)))
		},
		clickedOutside(e) {
			if (!this.inside(e.target)) this.isActive = false
		},
		keyPress(e) {
			if (this.isActive && (e.key === 'Escape' || e.key === 'Esc')) this.isActive = false
		},
		trap(e) {
			trapTab(e, this.$refs.dropdownMenu)
		},
		// Under the trigger (above it for is-top-*), right-aligned for *-left.
		place() {
			const { dropdownMenu: menu, trigger } = this.$refs
			if (!menu || !trigger) return
			const rect = trigger.getBoundingClientRect()
			let top = rect.top + window.scrollY
			let left = rect.left + window.scrollX
			if (!this.position || this.position.includes('bottom')) top += trigger.clientHeight
			else top -= menu.clientHeight
			if (this.position && this.position.includes('left')) left -= menu.clientWidth - trigger.clientWidth
			this.style = { position: 'absolute', top: `${top}px`, left: `${left}px`, zIndex: '99', width: undefined }
		},
	},
}
</script>

<template>
	<div class="dropdown dropdown-menu-animation" :class="[position, stateClasses]">
		<div ref="trigger" :tabindex="disabled ? undefined : 0" class="dropdown-trigger" aria-haspopup="true" @click="toggle">
			<slot name="trigger" :active="isActive" />
		</div>
		<transition :name="animation">
			<div v-if="mobileModal" v-show="isActive" class="background" :aria-hidden="!isActive" />
		</transition>
		<Teleport to="body" :disabled="!appendToBody">
			<div v-if="appendToBody" style="position: absolute; left: 0px; top: 0px; width: 100%">
				<div class="dropdown dropdown-menu-animation" :class="stateClasses">
					<transition :name="animation">
						<div v-show="!disabled && isActive" ref="dropdownMenu" class="dropdown-menu" :style="style" :aria-hidden="!isActive" @keydown="trap">
							<div class="dropdown-content" :role="ariaRole" aria-modal="true"><slot /></div>
						</div>
					</transition>
				</div>
			</div>
			<transition v-else :name="animation">
				<div v-show="!disabled && isActive" ref="dropdownMenu" class="dropdown-menu" :style="style" :aria-hidden="!isActive" @keydown="trap">
					<div class="dropdown-content" :role="ariaRole" aria-modal="true"><slot /></div>
				</div>
			</transition>
		</Teleport>
	</div>
</template>
