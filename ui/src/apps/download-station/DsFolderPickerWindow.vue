<!-- src/apps/download-station/DsFolderPickerWindow.vue -->
<!-- Folder chooser as its own desktop window (not an overlay inside the
     window that asked for it). Browses with the same $api.folder.getList()
     the Files app and VmFilePickerDialog use. -->
<template>
	<div class="ds-folder-picker">
		<div class="picker-crumbs">
			<template v-for="(c, i) in crumbs">
				<button :key="c.path" class="crumb" @click="navigate(c.path)">{{ c.name }}</button>
				<span v-if="i < crumbs.length - 1" :key="c.path + '-sep'" class="crumb-sep">/</span>
			</template>
		</div>
		<div class="picker-list scrollbars-light">
			<div v-if="loading" class="picker-status">{{ $t('Loading...') }}</div>
			<div v-else-if="!folders.length" class="picker-status">{{ $t('No subfolders') }}</div>
			<div v-for="f in folders" v-else :key="f.path" class="picker-row" :class="{ selected: f.path === selected }"
				@click="selected = f.path" @dblclick="navigate(f.path)">
				<b-icon icon="folder" custom-size="mdi-18px" class="folder-glyph"></b-icon>
				<span class="one-line">{{ f.name }}</span>
				<button class="enter-btn" :title="$t('Open folder')" @click.stop="navigate(f.path)">
					<b-icon icon="chevron-right" custom-size="mdi-18px"></b-icon>
				</button>
			</div>
		</div>
		<div class="picker-foot">
			<span class="picker-selected one-line" :title="target">{{ target }}</span>
			<button class="ds-secondary-btn" @click="close">{{ $t('Cancel') }}</button>
			<button class="ds-primary-btn" @click="choose">{{ $t('Select') }}</button>
		</div>
	</div>
</template>

<script>
export default {
	name: 'DsFolderPickerWindow',
	props: {
		winId: { type: String, default: '' },
		startPath: { type: String, default: '/DATA' },
		onSelect: { type: Function, default: null }
	},
	data() {
		return { path: this.startPath || '/DATA', items: [], loading: false, selected: '' }
	},
	computed: {
		folders() {
			return this.items.filter(i => i.is_dir && !i.name.startsWith('.')).sort((a, b) => a.name.localeCompare(b.name))
		},
		crumbs() {
			const parts = this.path.split('/').filter(Boolean)
			const out = [{ name: this.$t('Root'), path: '/' }]
			let acc = ''
			for (const p of parts) {
				acc += '/' + p
				out.push({ name: p, path: acc })
			}
			return out
		},
		target() {
			return this.selected || this.path
		}
	},
	created() {
		this.load()
	},
	methods: {
		async load() {
			this.loading = true
			try {
				const res = await this.$api.folder.getList(this.path)
				this.items = (res.data.data && res.data.data.content) || []
			} catch (e) {
				this.items = []
			} finally {
				this.loading = false
			}
		},
		navigate(p) {
			this.path = p
			this.selected = ''
			this.load()
		},
		choose() {
			if (typeof this.onSelect === 'function') this.onSelect(this.target)
			this.close()
		},
		close() {
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id) this.$store.commit('CLOSE_WINDOW', id)
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-folder-picker {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--theme-bg-window, #fff);
	color: var(--theme-text-primary, #1e293b);
}

.picker-crumbs {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: 0.15rem;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.07));
	font-size: var(--font-sm);
}

.crumb {
	border: none;
	background: transparent;
	color: var(--theme-text-secondary, #475569);
	font-family: inherit;
	font-size: inherit;
	padding: 0.1rem 0.3rem;
	border-radius: var(--radius-xs);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
		color: var(--theme-text-primary, #1e293b);
	}
	&:last-of-type {
		font-weight: 600;
		color: var(--theme-text-primary, #1e293b);
	}
}

.crumb-sep {
	color: var(--theme-text-muted, #cbd5e1);
}

.picker-list {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-2);
}

.picker-status {
	padding: var(--space-6);
	text-align: center;
	font-size: var(--font-sm);
	color: var(--theme-text-muted, #94a3b8);
}

.picker-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: 0.4rem var(--space-2);
	border-radius: var(--radius-sm);
	font-size: var(--font-sm);
	cursor: pointer;
	user-select: none;

	.one-line {
		flex: 1 1 auto;
		min-width: 0;
	}

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}
	&.selected {
		background: var(--theme-card-selected, rgba(37, 99, 235, 0.1));
	}
}

.folder-glyph {
	color: #3b82f6;
}

.enter-btn {
	display: inline-flex;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	border-radius: var(--radius-xs);
	cursor: pointer;
	padding: 0;

	&:hover {
		color: var(--theme-text-primary, #1e293b);
	}
}

.picker-foot {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));
}

.picker-selected {
	flex: 1 1 auto;
	min-width: 0;
	font-size: var(--font-xs);
	font-family: $family-monospace;
	color: var(--theme-text-muted, #64748b);
}
</style>
