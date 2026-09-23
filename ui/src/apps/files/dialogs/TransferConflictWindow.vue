<!-- src/apps/files/dialogs/TransferConflictWindow.vue -->
<!--
	"Some of these already exist here" - asked before a paste / drop that
	would otherwise silently overwrite (every paste used to send
	style: overwrite). Its own movable desktop window, like the other
	dialogs; closing it counts as Cancel.
-->
<template>
	<div class="conflict-window">
		<div class="cw-body">
			<div class="cw-icon"><b-icon icon="file-replace-outline" custom-size="mdi-24px"></b-icon></div>
			<div class="cw-main">
				<p class="cw-title">
					{{ names.length === 1 ? $t('“{name}” already exists in {dest}', { name: names[0], dest: destName }) : $t('{n} items already exist in {dest}', { n: names.length, dest: destName }) }}
				</p>
				<ul v-if="names.length > 1" class="cw-list">
					<li v-for="n in shown" :key="n" class="one-line" :title="n">{{ n }}</li>
					<li v-if="names.length > shown.length" class="cw-more">{{ $t('…and {n} more', { n: names.length - shown.length }) }}</li>
				</ul>
				<p class="cw-hint">{{ $t('Other items are copied as usual.') }}</p>
			</div>
		</div>
		<div class="cw-actions">
			<b-button rounded size="is-small" @click="choose(null)">{{ $t('Cancel') }}</b-button>
			<b-button rounded size="is-small" @click="choose('skip')">{{ $t('Skip these') }}</b-button>
			<b-button rounded size="is-small" @click="choose('rename')">{{ $t('Keep both') }}</b-button>
			<b-button rounded size="is-small" type="is-danger" @click="choose('overwrite')">{{ $t('Replace') }}</b-button>
		</div>
	</div>
</template>

<script>
export default {
	name: 'TransferConflictWindow',
	props: {
		winId: { type: String, default: '' },
		names: { type: Array, default: () => [] },
		destName: { type: String, default: '' },
		onChoose: { type: Function, default: null },
	},
	data() {
		return { answered: false }
	},
	computed: {
		shown() {
			return this.names.slice(0, 8)
		},
	},
	beforeDestroy() {
		// Closed with the window's own close button = Cancel.
		if (!this.answered && this.onChoose) this.onChoose(null)
	},
	methods: {
		choose(style) {
			this.answered = true
			if (this.onChoose) this.onChoose(style)
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id) this.$store.commit('CLOSE_WINDOW', id)
		},
	},
}
</script>

<style lang="scss" scoped>
.conflict-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window-opaque, #ffffff);
	color: var(--theme-text-primary, #1e293b);
}

.cw-body {
	flex: 1 1 auto;
	display: flex;
	gap: var(--space-4);
	padding: var(--space-5) var(--space-5) var(--space-3);
	overflow-y: auto;
}

.cw-icon {
	flex-shrink: 0;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-warning-soft, rgba(217, 119, 6, 0.12));
	color: var(--color-warning, #d97706);
}

.cw-main {
	flex: 1 1 auto;
	min-width: 0;
}

.cw-title {
	font-size: var(--font-base, 0.9rem);
	font-weight: 600;
	line-height: 1.45;
	word-break: break-word;
}

.cw-list {
	margin: var(--space-2) 0 0;
	padding: var(--space-2) var(--space-3);
	border-radius: 8px;
	background: var(--theme-card-subtle, #f1f5f9);
	font-size: var(--font-xs);
	max-height: 9rem;
	overflow-y: auto;

	li + li {
		margin-top: 0.15rem;
	}
}

.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.cw-more,
.cw-hint {
	color: var(--theme-text-muted, #64748b);
	font-size: var(--font-xs);
}

.cw-hint {
	margin-top: var(--space-2);
}

.cw-actions {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	justify-content: flex-end;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-input-bg, #f8fafc);
}
</style>
