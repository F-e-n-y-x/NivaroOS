<template>
	<div class="prompt-dialog-window">
		<div class="prompt-dialog-body">
			<div class="prompt-dialog-message" v-html="message"></div>
			<b-input ref="input" v-model="value" :type="inputType" :placeholder="placeholder" :maxlength="maxlength"
				size="is-small" expanded @keyup.native.enter="handleConfirm"></b-input>
		</div>
		<div class="prompt-dialog-actions">
			<b-button rounded size="is-small" @click="handleCancel">
				{{ cancelText }}
			</b-button>
			<b-button rounded size="is-small" type="is-primary" :loading="loading" :disabled="!value"
				@click="handleConfirm">
				{{ confirmText }}
			</b-button>
		</div>
	</div>
</template>

<script>
export default {
	name: 'PromptDialogWindow',
	props: {
		id: { type: String, default: '' },
		title: { type: String, default: '' },
		message: { type: String, default: '' },
		initialValue: { type: String, default: '' },
		placeholder: { type: String, default: '' },
		maxlength: { type: Number, default: null },
		inputType: { type: String, default: 'text' },
		confirmText: { type: String, default: 'Confirm' },
		cancelText: { type: String, default: 'Cancel' },
		onConfirm: { type: Function, default: null },
		onCancel: { type: Function, default: null }
	},
	data() {
		return {
			value: this.initialValue,
			loading: false,
			responded: false
		}
	},
	mounted() {
		this.$nextTick(() => {
			if (this.$refs.input) this.$refs.input.focus()
		})
	},
	methods: {
		winId() {
			return this.id || (this.$parent && this.$parent.win && this.$parent.win.id)
		},
		async handleConfirm() {
			if (this.loading || !this.value) return
			this.responded = true
			if (typeof this.onConfirm === 'function') {
				try {
					const res = this.onConfirm(this.value)
					if (res && typeof res.then === 'function') {
						this.loading = true
						await res
					}
				} catch (err) {
					console.error('Error during onConfirm in PromptDialogWindow:', err)
				} finally {
					this.loading = false
				}
			}
			const winId = this.winId()
			if (winId && this.$store) this.$store.commit('CLOSE_WINDOW', winId)
			this.$emit('close')
		},
		handleCancel() {
			this.responded = true
			if (typeof this.onCancel === 'function') {
				try {
					this.onCancel()
				} catch (err) {
					console.error('Error during onCancel in PromptDialogWindow:', err)
				}
			}
			const winId = this.winId()
			if (winId && this.$store) this.$store.commit('CLOSE_WINDOW', winId)
			this.$emit('close')
		}
	},
	beforeDestroy() {
		if (!this.responded && typeof this.onCancel === 'function') {
			try {
				this.onCancel()
			} catch (err) {
				console.error('Error during onCancel beforeDestroy in PromptDialogWindow:', err)
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.prompt-dialog-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 100%;
	background: var(--theme-bg-window-opaque, #ffffff);
	color: var(--theme-text-primary, #1e293b);
	user-select: text;
}

.prompt-dialog-body {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	padding: var(--space-5) var(--space-5) var(--space-4);
	overflow-y: auto;
}

.prompt-dialog-message {
	font-size: var(--font-base, 0.9rem);
	line-height: 1.5;
	color: var(--theme-text-primary, #1e293b);
	word-break: break-word;
}

.prompt-dialog-actions {
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
