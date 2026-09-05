<template>
	<div
		ref="dropdownRoot"
		class="vm-dropdown"
		:class="{
			'is-dark': dark,
			'is-disabled': disabled,
			'is-small': size === 'small',
			'is-compact': size === 'compact',
		}"
	>
		<popper
			ref="popperRef"
			trigger="click"
			append-to-body
			:disabled="disabled"
			transition="dropdown-fade"
			:options="popperOptions"
			root-class="vm-dropdown-popper-root"
			@show="updateTriggerWidth"
		>
			<div
				class="vm-dropdown-menu"
				:class="{ 'is-dark': dark }"
				:style="menuStyle"
				role="listbox"
			>
				<div v-if="!normalizedOptions.length" class="vm-dropdown-empty">
					<b-icon icon="information-outline" size="is-small"></b-icon>
					<span>{{ emptyText || $t('No options available') }}</span>
				</div>
				<button
					v-for="(opt, idx) in normalizedOptions"
					:key="idx"
					type="button"
					class="vm-dropdown-item"
					:class="{
						'is-selected': isSelected(opt.value),
						'is-disabled': opt.disabled,
						'is-rich': !!(opt.specs || opt.sublabel || opt.state),
					}"
					:disabled="opt.disabled"
					:title="opt.label + (opt.specs ? ' · ' + opt.specs : '')"
					role="option"
					:aria-selected="isSelected(opt.value)"
					@click="selectOption(opt)"
				>
					<slot name="option" :option="opt" :is-selected="isSelected(opt.value)">
						<div v-if="opt.specs || opt.sublabel || opt.state" class="item-rich-content">
							<div class="item-icon-box" :class="opt.state ? 'is-' + opt.state : ''">
								<b-icon v-if="opt.icon" :icon="opt.icon" size="is-small"></b-icon>
							</div>
							<div class="item-main">
								<div class="item-head">
									<span class="item-title">{{ opt.label }}</span>
									<span v-if="opt.state" class="item-state-dot" :class="'is-' + opt.state" :title="opt.stateLabel || opt.state"></span>
									<span v-if="opt.stateLabel" class="item-state-text" :class="'is-' + opt.state">{{ opt.stateLabel }}</span>
								</div>
								<div v-if="opt.specs || opt.sublabel" class="item-specs">
									{{ opt.specs || opt.sublabel }}
								</div>
							</div>
							<div class="item-tail">
								<span v-if="opt.meta && !opt.stateLabel" class="item-meta">{{ opt.meta }}</span>
								<b-icon v-if="isSelected(opt.value)" icon="check" size="is-small" class="item-check"></b-icon>
							</div>
						</div>
						<template v-else>
							<div class="item-left">
								<b-icon v-if="opt.icon" :icon="opt.icon" class="item-icon" size="is-small"></b-icon>
								<span class="item-label">{{ opt.label }}</span>
							</div>
							<span v-if="opt.meta" class="item-meta">{{ opt.meta }}</span>
						</template>
					</slot>
				</button>
			</div>

			<button
				slot="reference"
				type="button"
				class="vm-dropdown-trigger"
				:disabled="disabled"
				:title="selectedLabel"
				@keydown.esc="closeMenu"
			>
				<div class="trigger-content">
					<b-icon v-if="selectedOption && selectedOption.icon || icon" :icon="(selectedOption && selectedOption.icon) || icon" class="trigger-icon" size="is-small"></b-icon>
					<div class="trigger-label-group">
						<span class="trigger-label" :class="{ 'is-placeholder': !hasValue }">{{ selectedLabel }}</span>
						<span v-if="selectedOption && selectedOption.state" class="trigger-state-dot" :class="'is-' + selectedOption.state" :title="selectedOption.stateLabel || selectedOption.state"></span>
					</div>
				</div>
				<b-icon icon="chevron-down" class="trigger-chevron" size="is-small"></b-icon>
			</button>
		</popper>
	</div>
</template>

<script>
import Popper from 'vue-popperjs'

export default {
	name: 'vm-dropdown',
	components: {
		Popper,
	},
	props: {
		value: { type: [String, Number, Boolean], default: '' },
		options: { type: Array, default: () => [] },
		placeholder: { type: String, default: '' },
		emptyText: { type: String, default: '' },
		disabled: { type: Boolean, default: false },
		dark: { type: Boolean, default: false },
		icon: { type: String, default: '' },
		size: { type: String, default: 'normal' }, // 'normal', 'small', 'compact'
		align: { type: String, default: 'auto' }, // 'auto', 'left', 'right'
		direction: { type: String, default: 'down' }, // 'down', 'up', 'auto'
	},
	data() {
		return {
			triggerWidth: 0,
		}
	},
	computed: {
		normalizedOptions() {
			return (this.options || []).map((opt) => {
				if (typeof opt === 'object' && opt !== null) {
					return {
						value: opt.value !== undefined ? opt.value : opt.name || opt.id || '',
						label: opt.label !== undefined ? opt.label : opt.name || opt.id || String(opt.value),
						icon: opt.icon || '',
						meta: opt.meta || '',
						specs: opt.specs || '',
						sublabel: opt.sublabel || '',
						state: opt.state || '',
						stateLabel: opt.stateLabel || '',
						disabled: !!opt.disabled,
						raw: opt,
					}
				}
				return {
					value: opt,
					label: String(opt),
					icon: '',
					meta: '',
					specs: '',
					sublabel: '',
					state: '',
					stateLabel: '',
					disabled: false,
					raw: opt,
				}
			})
		},
		hasValue() {
			return this.value !== '' && this.value !== null && this.value !== undefined
		},
		selectedOption() {
			return this.normalizedOptions.find((opt) => opt.value === this.value)
		},
		selectedLabel() {
			if (this.selectedOption) return this.selectedOption.label
			return this.placeholder || this.$t('Select...')
		},
		selectedIcon() {
			return this.selectedOption ? this.selectedOption.icon : ''
		},
		// Popper.js placement string derived from the direction/align props - the
		// 'flip'/'preventOverflow' modifiers let it escape whatever scrollable
		// ancestor the trigger lives in, which position:absolute-in-place couldn't.
		popperOptions() {
			const vertical = this.direction === 'up' ? 'top' : this.direction === 'down' ? 'bottom' : 'auto'
			const suffix = this.align === 'right' ? '-end' : this.align === 'left' ? '-start' : ''
			return {
				placement: `${vertical}${suffix}`,
				modifiers: {
					offset: { offset: '0,4' },
					preventOverflow: { boundariesElement: 'viewport', padding: 8 },
					flip: { enabled: true, boundariesElement: 'viewport' },
				},
			}
		},
		menuStyle() {
			return this.triggerWidth ? { minWidth: `${Math.max(this.triggerWidth, 220)}px` } : null
		},
	},
	methods: {
		updateTriggerWidth() {
			this.triggerWidth = this.$refs.dropdownRoot ? this.$refs.dropdownRoot.offsetWidth : 0
		},
		closeMenu() {
			if (this.$refs.popperRef) this.$refs.popperRef.doClose()
		},
		selectOption(opt) {
			if (opt.disabled) return
			this.$emit('input', opt.value)
			this.$emit('change', opt.value)
			this.closeMenu()
		},
		isSelected(val) {
			return this.value === val
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-dropdown {
	position: relative;
	width: 100%;
	display: block;
	font-family: inherit;
	user-select: none;

	&.is-disabled {
		opacity: 0.55;
		pointer-events: none;
	}
}

.vm-dropdown-trigger {
	width: 100%;
	box-sizing: border-box;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	background: #ffffff;
	border: 1px solid rgba(0, 0, 0, 0.12);
	border-radius: 8px;
	padding: 0.45rem 0.75rem;
	font-family: inherit;
	font-size: 0.82rem;
	font-weight: 500;
	color: #1e293b;
	cursor: pointer;
	outline: none;
	transition: all 0.15s ease;
	box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);

	&:hover:not(:disabled) {
		border-color: rgba(37, 99, 235, 0.45);
		background: #f8fafc;
	}
	&:focus {
		border-color: #2563eb;
		box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.15);
	}
}

.trigger-content {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	min-width: 0;
	flex: 1 1 auto;
	overflow: hidden;
}

.trigger-icon {
	color: #64748b;
	flex-shrink: 0;
}

.trigger-label-group {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	min-width: 0;
	flex: 1 1 auto;
}

.trigger-label {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	text-align: left;

	&.is-placeholder {
		color: #94a3b8;
		font-weight: normal;
	}
}

.trigger-state-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: #94a3b8;
	flex-shrink: 0;

	&.is-running {
		background: #10b981;
		box-shadow: 0 0 5px rgba(16, 185, 129, 0.4);
	}
	&.is-paused {
		background: #f59e0b;
	}
	&.is-crashed {
		background: #ef4444;
	}
}

.trigger-chevron {
	color: #94a3b8;
	flex-shrink: 0;
	transition: transform 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.vm-dropdown-menu {
	width: max-content;
	max-width: min(32rem, calc(100vw - 2rem));
	z-index: 3000;
	background: #ffffff;
	border: 1px solid rgba(0, 0, 0, 0.09);
	border-radius: 10px;
	box-shadow: 0 12px 28px rgba(0, 0, 0, 0.15), 0 4px 10px rgba(0, 0, 0, 0.05);
	padding: 0.35rem;
	max-height: 18rem;
	overflow-y: auto;
	overflow-x: hidden;
	scrollbar-width: thin;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
	box-sizing: border-box;

	&::-webkit-scrollbar {
		width: 5px;
	}
	&::-webkit-scrollbar-thumb {
		background: rgba(0, 0, 0, 0.2);
		border-radius: 4px;
	}
	&::-webkit-scrollbar-track {
		background: transparent;
	}
}

.vm-dropdown-empty {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	padding: 0.6rem 0.75rem;
	font-size: 0.78rem;
	color: #94a3b8;
	justify-content: center;
}

.vm-dropdown-item {
	width: 100%;
	box-sizing: border-box;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	background: transparent;
	border: none;
	border-radius: 6px;
	padding: 0.45rem 0.65rem;
	font-family: inherit;
	font-size: 0.8rem;
	color: #334155;
	cursor: pointer;
	text-align: left;
	outline: none;
	transition: background 0.12s ease, color 0.12s ease;
	min-height: 2.1rem;
	white-space: nowrap;

	&:hover:not(:disabled) {
		background: #f1f5f9;
		color: #0f172a;
	}

	&.is-selected {
		background: #eff6ff;
		color: #2563eb;
		font-weight: 600;
	}

	&.is-disabled {
		opacity: 0.4;
		cursor: default;
	}

	&.is-rich {
		padding: 0.5rem 0.65rem;
		min-height: 2.85rem;
	}
}

.item-rich-content {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	min-width: 0;
}

.item-icon-box {
	flex-shrink: 0;
	width: 2.1rem;
	height: 2.1rem;
	border-radius: 6px;
	display: flex;
	align-items: center;
	justify-content: center;
	background: #f1f5f9;
	color: #64748b;
	transition: all 0.12s ease;

	&.is-running {
		background: #ecfdf5;
		color: #059669;
	}

	&.is-paused {
		background: #fffbeb;
		color: #d97706;
	}

	&.is-crashed {
		background: #fef2f2;
		color: #dc2626;
	}

	.is-selected & {
		background: #dbeafe;
		color: #2563eb;
	}
}

.item-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.item-head {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	min-width: 0;
}

.item-title {
	font-size: 0.825rem;
	font-weight: 600;
	color: #0f172a;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	.is-selected & {
		color: #2563eb;
	}
}

.item-state-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: #94a3b8;
	flex-shrink: 0;

	&.is-running {
		background: #10b981;
		box-shadow: 0 0 5px rgba(16, 185, 129, 0.4);
	}
	&.is-paused {
		background: #f59e0b;
	}
	&.is-crashed {
		background: #ef4444;
	}
}

.item-state-text {
	font-size: 0.68rem;
	font-weight: 500;
	color: #64748b;
	text-transform: capitalize;

	&.is-running {
		color: #059669;
	}
	&.is-paused {
		color: #d97706;
	}
	&.is-crashed {
		color: #dc2626;
	}
}

.item-specs {
	font-size: 0.7rem;
	color: #64748b;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.item-tail {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.4rem;
	margin-left: auto;
}

.item-check {
	color: #2563eb;
}

.item-left {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	min-width: 0;
	flex: 1 1 auto;
	white-space: nowrap;
}

.item-icon {
	color: #64748b;
	flex-shrink: 0;

	.is-selected & {
		color: #2563eb;
	}
}

.item-label {
	white-space: nowrap;
	flex: 1 1 auto;
}

.item-meta {
	font-size: 0.68rem;
	color: #94a3b8;
	font-weight: normal;
	flex-shrink: 0;
	white-space: nowrap;
	background: rgba(0, 0, 0, 0.04);
	padding: 0.1rem 0.35rem;
	border-radius: 4px;
	margin-left: 0.35rem;
}

/* Size Modifiers */
.vm-dropdown.is-small .vm-dropdown-trigger {
	padding: 0.35rem 0.6rem;
	font-size: 0.78rem;
	border-radius: 6px;
}
.vm-dropdown.is-compact .vm-dropdown-trigger {
	padding: 0.25rem 0.5rem;
	font-size: 0.74rem;
	border-radius: 6px;
}

/* Dark Mode (for Console panel and dark themes) */
.vm-dropdown.is-dark .vm-dropdown-trigger {
	background: rgba(255, 255, 255, 0.08);
	border-color: rgba(255, 255, 255, 0.16);
	color: #f8fafc;
	box-shadow: none;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.12);
		border-color: rgba(255, 255, 255, 0.28);
	}
	&:focus {
		border-color: #3b82f6;
		box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.25);
	}
}

.vm-dropdown.is-dark .trigger-icon,
.vm-dropdown.is-dark .trigger-chevron {
	color: rgba(255, 255, 255, 0.55);
}

.vm-dropdown.is-dark .trigger-label.is-placeholder {
	color: rgba(255, 255, 255, 0.4);
}

// The menu itself carries its own .is-dark modifier (bound from the `dark`
// prop) rather than relying on a `.vm-dropdown.is-dark` ancestor selector,
// because vue-popperjs's append-to-body mode moves this element out from
// under .vm-dropdown and into <body> once it opens.
.vm-dropdown-menu.is-dark {
	background: #242424;
	border-color: rgba(255, 255, 255, 0.14);
	box-shadow: 0 16px 40px rgba(0, 0, 0, 0.75);
	scrollbar-color: rgba(255, 255, 255, 0.2) transparent;

	&::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.2);
	}

	.vm-dropdown-empty {
		color: rgba(255, 255, 255, 0.4);
	}

	.vm-dropdown-item {
		color: rgba(255, 255, 255, 0.85);

		&:hover:not(:disabled) {
			background: rgba(255, 255, 255, 0.09);
			color: #ffffff;
		}

		&.is-selected {
			background: rgba(37, 99, 235, 0.22);
			color: #60a5fa;
		}
	}

	.item-rich-content {
		.item-icon-box {
			background: rgba(255, 255, 255, 0.08);
			color: rgba(255, 255, 255, 0.7);

			&.is-running {
				background: rgba(16, 185, 129, 0.2);
				color: #34d399;
			}
		}

		.item-title {
			color: #f8fafc;
		}

		.item-specs {
			color: rgba(255, 255, 255, 0.5);
		}

		.item-check {
			color: #60a5fa;
		}
	}

	.item-icon {
		color: rgba(255, 255, 255, 0.5);

		.is-selected & {
			color: #60a5fa;
		}
	}

	.item-meta {
		color: rgba(255, 255, 255, 0.5);
		background: rgba(255, 255, 255, 0.08);
	}
}
</style>

<style>
/* Unscoped: vue-popperjs renders/animates this element outside VmDropdown's
   own template (appended to <body>), so a scoped selector would never match it. */
.dropdown-fade-enter-active,
.dropdown-fade-leave-active {
	transition: opacity 0.15s ease, transform 0.15s ease;
}

.dropdown-fade-enter,
.dropdown-fade-leave-to {
	opacity: 0;
	transform: scale(0.97) translateY(-4px);
}
</style>
