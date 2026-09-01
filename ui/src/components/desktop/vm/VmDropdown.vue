<template>
	<div
		ref="dropdownRoot"
		class="vm-dropdown"
		:class="{
			'is-open': isOpen,
			'is-dark': dark,
			'is-disabled': disabled,
			'is-small': size === 'small',
			'is-compact': size === 'compact',
			'align-right': align === 'right',
		}"
	>
		<button
			type="button"
			class="vm-dropdown-trigger"
			:disabled="disabled"
			:title="selectedLabel"
			@click="toggle"
			@keydown.esc="close"
		>
			<div class="trigger-content">
				<b-icon v-if="selectedIcon || icon" :icon="selectedIcon || icon" class="trigger-icon" size="is-small"></b-icon>
				<span class="trigger-label" :class="{ 'is-placeholder': !hasValue }">{{ selectedLabel }}</span>
			</div>
			<b-icon icon="chevron-down" class="trigger-chevron" size="is-small"></b-icon>
		</button>

		<transition name="dropdown-fade">
			<div v-if="isOpen" class="vm-dropdown-menu" :style="menuStyle" role="listbox">
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
					}"
					:disabled="opt.disabled"
					role="option"
					:aria-selected="isSelected(opt.value)"
					@click="selectOption(opt)"
				>
					<div class="item-left">
						<b-icon v-if="opt.icon" :icon="opt.icon" class="item-icon" size="is-small"></b-icon>
						<span class="item-label">{{ opt.label }}</span>
						<span v-if="opt.meta" class="item-meta">{{ opt.meta }}</span>
					</div>
					<b-icon v-if="isSelected(opt.value)" icon="check" class="item-check" size="is-small"></b-icon>
				</button>
			</div>
		</transition>
	</div>
</template>

<script>
export default {
	name: 'vm-dropdown',
	props: {
		value: { type: [String, Number, Boolean], default: '' },
		options: { type: Array, default: () => [] },
		placeholder: { type: String, default: '' },
		emptyText: { type: String, default: '' },
		disabled: { type: Boolean, default: false },
		dark: { type: Boolean, default: false },
		icon: { type: String, default: '' },
		size: { type: String, default: 'normal' }, // 'normal', 'small', 'compact'
		align: { type: String, default: 'left' }, // 'left', 'right'
		menuMinWidth: { type: String, default: '' },
	},
	data() {
		return {
			isOpen: false,
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
						disabled: !!opt.disabled,
					}
				}
				return {
					value: opt,
					label: String(opt),
					icon: '',
					meta: '',
					disabled: false,
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
		menuStyle() {
			if (this.menuMinWidth) {
				return { minWidth: this.menuMinWidth }
			}
			return {}
		},
	},
	mounted() {
		document.addEventListener('mousedown', this.handleOutsideClick)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.handleOutsideClick)
	},
	methods: {
		toggle() {
			if (this.disabled) return
			this.isOpen = !this.isOpen
		},
		close() {
			this.isOpen = false
		},
		selectOption(opt) {
			if (opt.disabled) return
			this.$emit('input', opt.value)
			this.$emit('change', opt.value)
			this.close()
		},
		isSelected(val) {
			return this.value === val
		},
		handleOutsideClick(e) {
			if (this.isOpen && this.$refs.dropdownRoot && !this.$refs.dropdownRoot.contains(e.target)) {
				this.close()
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-dropdown {
	position: relative;
	display: inline-block;
	width: 100%;
	font-family: inherit;
	user-select: none;

	&.is-disabled {
		opacity: 0.55;
		pointer-events: none;
	}
}

.vm-dropdown-trigger {
	width: 100%;
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

.trigger-label {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	&.is-placeholder {
		color: #94a3b8;
		font-weight: normal;
	}
}

.trigger-chevron {
	color: #94a3b8;
	flex-shrink: 0;
	transition: transform 0.2s cubic-bezier(0.4, 0, 0.2, 1);

	.is-open & {
		transform: rotate(180deg);
		color: #2563eb;
	}
}

.vm-dropdown-menu {
	position: absolute;
	top: calc(100% + 4px);
	left: 0;
	min-width: 100%;
	width: max-content;
	max-width: min(34rem, calc(100vw - 2rem));
	z-index: 100;
	background: #ffffff;
	border: 1px solid rgba(0, 0, 0, 0.09);
	border-radius: 10px;
	box-shadow: 0 12px 28px rgba(0, 0, 0, 0.12), 0 4px 10px rgba(0, 0, 0, 0.04);
	padding: 0.35rem;
	max-height: 16rem;
	overflow-y: auto;
	scrollbar-width: thin;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;

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

.vm-dropdown.align-right .vm-dropdown-menu {
	left: auto;
	right: 0;
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
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.85rem;
	background: transparent;
	border: none;
	border-radius: 6px;
	padding: 0.45rem 0.75rem;
	font-family: inherit;
	font-size: 0.8rem;
	color: #334155;
	cursor: pointer;
	text-align: left;
	outline: none;
	transition: background 0.12s ease, color 0.12s ease;
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
}

.item-left {
	display: flex;
	align-items: center;
	gap: 0.55rem;
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
	font-size: 0.7rem;
	color: #94a3b8;
	font-weight: normal;
	flex-shrink: 0;
	white-space: nowrap;
	background: rgba(0, 0, 0, 0.04);
	padding: 0.12rem 0.4rem;
	border-radius: 4px;
}

.item-check {
	color: #2563eb;
	flex-shrink: 0;
	margin-left: 0.25rem;
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

/* Dark Mode (for Console panel) */
.vm-dropdown.is-dark {
	.vm-dropdown-trigger {
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

	.trigger-icon,
	.trigger-chevron {
		color: rgba(255, 255, 255, 0.55);
	}

	.trigger-label.is-placeholder {
		color: rgba(255, 255, 255, 0.4);
	}

	&.is-open .trigger-chevron {
		color: #60a5fa;
	}

	.vm-dropdown-menu {
		background: #242424;
		border-color: rgba(255, 255, 255, 0.14);
		box-shadow: 0 14px 36px rgba(0, 0, 0, 0.7);
		scrollbar-color: rgba(255, 255, 255, 0.2) transparent;

		&::-webkit-scrollbar-thumb {
			background: rgba(255, 255, 255, 0.2);
		}
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

	.item-icon {
		color: rgba(255, 255, 255, 0.5);

		.is-selected & {
			color: #60a5fa;
		}
	}

	.item-meta {
		color: rgba(255, 255, 255, 0.4);
	}

	.item-check {
		color: #60a5fa;
	}
}

/* Animations */
.dropdown-fade-enter-active,
.dropdown-fade-leave-active {
	transition: opacity 0.15s ease, transform 0.15s ease;
	transform-origin: top center;
}

.dropdown-fade-enter {
	opacity: 0;
	transform: scaleY(0.95) translateY(-4px);
}

.dropdown-fade-leave-to {
	opacity: 0;
	transform: scaleY(0.95) translateY(-2px);
}
</style>
