<template>
	<div class="free-memory-window">
		<div class="free-memory-body">
			<div class="free-memory-icon" :class="{ 'is-success': result }">
				<b-icon :icon="result ? 'check-circle-outline' : 'broom'" pack="mdi" custom-size="mdi-24px"></b-icon>
			</div>
			<div class="free-memory-main">
				<template v-if="!result">
					<p class="free-memory-title">{{ $t("Free up memory?") }}</p>
					<p class="free-memory-text">
						{{ $t("Linux uses spare memory as a cache to speed things up; clearing it frees memory now, but things may be slower for a moment while the cache refills.") }}
					</p>
					<div v-if="swapUsed > 0" class="free-memory-swap">
						<b-checkbox v-model="swap" size="is-small" :disabled="running">{{ $t("Also empty swap") }}</b-checkbox>
						<p class="free-memory-hint">
							{{ $t("Moves {size} from swap back into memory. The server skips it if there isn't enough room. It can take a minute or two.", { size: formatBytes(swapUsed) }) }}
						</p>
					</div>
					<p v-if="running" class="free-memory-status" role="status">
						{{ swap ? $t("Freeing memory and emptying swap…") : $t("Freeing memory…") }}
					</p>
					<p v-if="error" class="free-memory-error" role="alert">{{ error }}</p>
				</template>
				<template v-else>
					<p class="free-memory-title" role="status">
						{{ result.alreadyClear ? $t("Memory was already clear") : $t("Freed {size}", { size: result.freedText }) }}
					</p>
					<p class="free-memory-text">{{ $t("The cache fills up again as the server reads files.") }}</p>
					<dl class="free-memory-rows">
						<div v-for="row in result.rows" :key="row.key" class="free-memory-row">
							<dt>{{ $t(row.label) }}</dt>
							<dd>
								{{ row.before }} <span class="free-memory-arrow" aria-hidden="true">→</span><span class="is-sr-only">{{ $t("to") }}</span> {{ row.after }}
							</dd>
						</div>
					</dl>
					<p v-if="result.note" class="free-memory-hint">{{ result.note }}</p>
				</template>
			</div>
		</div>
		<div class="free-memory-actions">
			<template v-if="!result">
				<b-button rounded size="is-small" :disabled="running" @click="close">{{ $t("Cancel") }}</b-button>
				<b-button rounded size="is-small" type="is-primary" :loading="running" @click="run">{{ $t("Free up memory") }}</b-button>
			</template>
			<b-button v-else rounded size="is-small" type="is-primary" @click="close">{{ $t("Done") }}</b-button>
		</div>
	</div>
</template>

<script>
import { apiError } from "@/utils/apiError"
import { describeClear, formatBytes } from "@/utils/freeMemory.js"

export default {
	name: "FreeMemoryWindow",
	props: {
		id: { type: String, default: "" },
		// The server's swap in use when the window opened; the swap choice
		// only shows when there is some.
		swapUsed: { type: Number, default: 0 },
	},
	data() {
		return {
			swap: false,
			running: false,
			error: "",
			result: null,
		}
	},
	methods: {
		formatBytes,
		async run() {
			if (this.running) return
			this.running = true
			this.error = ""
			try {
				const res = await this.$api.sys.clearMemory({ swap: this.swap && this.swapUsed > 0 })
				this.result = describeClear(res && res.data && res.data.data)
			} catch (e) {
				this.error = apiError(e, this.$t("Memory could not be freed"))
			} finally {
				this.running = false
			}
		},
		close() {
			const winId = this.id || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (winId && this.$store) this.$store.commit("CLOSE_WINDOW", winId)
			this.$emit("close")
		},
	},
}
</script>

<style lang="scss" scoped>
.free-memory-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 100%;
	background: var(--theme-bg-window-opaque, #ffffff);
	color: var(--theme-text-primary, #1e293b);
	user-select: text;
}

.free-memory-body {
	flex: 1 1 auto;
	display: flex;
	align-items: flex-start;
	gap: var(--space-4);
	padding: var(--space-5) var(--space-5) var(--space-4);
	overflow-y: auto;
}

.free-memory-icon {
	flex-shrink: 0;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-info-soft, rgba(37, 99, 235, 0.12));
	color: var(--color-primary, #2563eb);

	&.is-success {
		background: var(--theme-success-soft, rgba(22, 163, 74, 0.12));
		color: var(--color-success, #16a34a);
	}
}

.free-memory-main {
	flex: 1 1 auto;
	min-width: 0;
}

.free-memory-title {
	font-size: var(--font-md, 1rem);
	font-weight: 600;
	margin: 0 0 var(--space-1);
}

.free-memory-text {
	font-size: var(--font-base, 0.9rem);
	line-height: 1.5;
	color: var(--theme-text-secondary, #475569);
	margin: 0;
}

.free-memory-swap {
	margin-top: var(--space-3);

	::v-deep .b-checkbox.checkbox {
		color: var(--theme-text-primary, #1e293b);
		font-size: var(--font-sm, 0.85rem);
	}
}

.free-memory-hint {
	margin: var(--space-1) 0 0;
	font-size: var(--font-xs, 0.75rem);
	color: var(--theme-text-secondary, #475569);
}

.free-memory-status {
	margin: var(--space-3) 0 0;
	font-size: var(--font-sm, 0.85rem);
	color: var(--theme-text-secondary, #475569);
}

.free-memory-error {
	margin: var(--space-3) 0 0;
	font-size: var(--font-sm, 0.85rem);
	color: var(--color-danger-fg, #b91c1c);
}

.free-memory-rows {
	margin: var(--space-3) 0 0;
}

.free-memory-row {
	display: flex;
	justify-content: space-between;
	gap: var(--space-3);
	padding: var(--space-1) 0;
	font-size: var(--font-sm, 0.85rem);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));

	&:last-child {
		border-bottom: 0;
	}

	dt {
		color: var(--theme-text-secondary, #475569);
	}

	dd {
		margin: 0;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
}

.free-memory-arrow {
	color: var(--theme-text-muted, #64748b);
	padding: 0 0.15rem;
}

.free-memory-actions {
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
