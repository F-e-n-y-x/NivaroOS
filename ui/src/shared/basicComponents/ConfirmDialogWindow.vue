<template>
	<div class="confirm-dialog-window">
		<div class="confirm-dialog-body">
			<div v-if="hasIcon" class="confirm-dialog-icon" :class="iconClass">
				<b-icon :icon="resolvedIcon" :pack="iconPack" custom-size="mdi-24px"></b-icon>
			</div>
			<div class="confirm-dialog-main">
				<div class="confirm-dialog-message" v-html="message"></div>
			</div>
		</div>
		<div class="confirm-dialog-actions">
			<b-button rounded size="is-small" @click="handleCancel">
				{{ cancelText }}
			</b-button>
			<b-button rounded size="is-small" :type="type" :loading="loading" @click="handleConfirm">
				{{ confirmText }}
			</b-button>
		</div>
	</div>
</template>

<script>
export default {
	name: 'ConfirmDialogWindow',
	props: {
		id: { type: String, default: '' },
		title: { type: String, default: '' },
		message: { type: String, default: '' },
		confirmText: { type: String, default: 'Confirm' },
		cancelText: { type: String, default: 'Cancel' },
		type: { type: String, default: 'is-primary' },
		hasIcon: { type: Boolean, default: true },
		icon: { type: String, default: '' },
		iconPack: { type: String, default: 'mdi' },
		onConfirm: { type: Function, default: null },
		onCancel: { type: Function, default: null }
	},
	data() {
		return {
			loading: false,
			responded: false
		}
	},
	computed: {
		resolvedIcon() {
			if (this.icon) return this.icon
			if (this.type === 'is-danger') return 'trash-can-outline'
			if (this.type === 'is-warning') return 'alert'
			if (this.type === 'is-success') return 'check-circle-outline'
			return 'alert-circle-outline'
		},
		iconClass() {
			return this.type || 'is-primary'
		}
	},
	methods: {
		async handleConfirm() {
			if (this.loading) return
			this.responded = true
			const winId = this.id || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (typeof this.onConfirm === 'function') {
				try {
					const res = this.onConfirm()
					if (res && typeof res.then === 'function') {
						this.loading = true
						await res
					}
				} catch (err) {
					console.error('Error during onConfirm in ConfirmDialogWindow:', err)
				} finally {
					this.loading = false
					if (winId && this.$store) {
						this.$store.commit('CLOSE_WINDOW', winId)
					}
					this.$emit('close')
				}
				return
			}
			if (winId && this.$store) {
				this.$store.commit('CLOSE_WINDOW', winId)
			}
			this.$emit('close')
		},
		handleCancel() {
			this.responded = true
			if (typeof this.onCancel === 'function') {
				try {
					this.onCancel()
				} catch (err) {
					console.error('Error during onCancel in ConfirmDialogWindow:', err)
				}
			}
			const winId = this.id || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (winId && this.$store) {
				this.$store.commit('CLOSE_WINDOW', winId)
			}
			this.$emit('close')
		}
	},
	beforeDestroy() {
		if (!this.responded && typeof this.onCancel === 'function') {
			try {
				this.onCancel()
			} catch (err) {
				console.error('Error during onCancel beforeDestroy in ConfirmDialogWindow:', err)
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.confirm-dialog-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 100%;
	background: var(--theme-bg-window-opaque, #ffffff);
	color: var(--theme-text-primary, #1e293b);
	user-select: text;
}

.confirm-dialog-body {
	flex: 1 1 auto;
	display: flex;
	align-items: flex-start;
	gap: var(--space-4);
	padding: var(--space-5) var(--space-5) var(--space-4);
	overflow-y: auto;
}

.confirm-dialog-icon {
	flex-shrink: 0;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-info-soft, rgba(37, 99, 235, 0.12));
	color: var(--color-primary, #2563eb);

	&.is-danger {
		background: var(--theme-danger-soft, rgba(239, 68, 68, 0.12));
		color: var(--color-danger, #ef4444);
	}
	&.is-warning {
		background: var(--theme-warning-soft, rgba(217, 119, 6, 0.12));
		color: var(--color-warning, #d97706);
	}
	&.is-success {
		background: var(--theme-success-soft, rgba(22, 163, 74, 0.12));
		color: var(--color-success, #16a34a);
	}
}

.confirm-dialog-main {
	flex: 1 1 auto;
	min-width: 0;
}

.confirm-dialog-message {
	font-size: var(--font-base, 0.9rem);
	line-height: 1.5;
	color: var(--theme-text-primary, #1e293b);
	word-break: break-word;

	::v-deep b,
	::v-deep strong {
		color: var(--theme-text-primary, #0f172a);
		font-weight: 600;
	}

	::v-deep .text-muted {
		color: var(--theme-text-muted, #64748b);
	}
}

.confirm-dialog-actions {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-input-bg, #f8fafc);

	.button {
		min-width: 4.5rem;
	}
}
</style>
