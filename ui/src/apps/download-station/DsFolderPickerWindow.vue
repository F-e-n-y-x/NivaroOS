<!-- src/apps/download-station/DsFolderPickerWindow.vue -->
<!-- Folder chooser as its own desktop window (not an overlay inside the
     window that asked for it). Browses with the same $api.folder.getList()
     the Files app and VmFilePickerDialog use. -->
<template>
	<div class="ds-folder-picker">
		<nav class="picker-crumbs" :aria-label="$t('Folder path')">
			<button class="crumb" :class="{ current: !path }" @click="navigate('')">{{ $t('Storage') }}</button>
			<template v-for="c in crumbs">
				<span :key="c.path + '-sep'" class="crumb-sep" aria-hidden="true">/</span>
				<button :key="c.path" class="crumb" :class="{ current: c.path === path }" @click="navigate(c.path)">{{ c.name }}</button>
			</template>
		</nav>
		<div class="picker-list scrollbars-light" role="listbox" :aria-label="$t('Folders')">
			<div v-if="loading" class="picker-status">{{ $t('Loading...') }}</div>
			<div v-else-if="loadError" class="picker-status ds-error-text" role="alert">{{ loadError }}</div>
			<div v-else-if="!folders.length" class="picker-status">{{ path ? $t('No subfolders') : $t('No storage available') }}</div>
			<div v-for="f in folders" v-else :key="f.path" class="picker-row" :class="{ selected: f.path === selected }"
				role="option" tabindex="0" :aria-selected="f.path === selected ? 'true' : 'false'"
				@click="selected = f.path" @dblclick="navigate(f.path)" @keydown.enter.prevent="navigate(f.path)" @keydown.space.prevent="selected = f.path">
				<b-icon :icon="path ? 'folder' : 'harddisk'" custom-size="mdi-18px" class="folder-glyph"></b-icon>
				<span class="one-line">{{ f.name }}</span>
				<button class="enter-btn" :title="$t('Open folder')" :aria-label="$t('Open folder {name}', { name: f.name })" @click.stop="navigate(f.path)">
					<b-icon icon="chevron-right" custom-size="mdi-18px"></b-icon>
				</button>
			</div>
		</div>
		<form v-if="creating" class="new-folder-row" @submit.prevent="createFolder">
			<b-icon icon="folder-plus-outline" custom-size="mdi-18px" class="folder-glyph"></b-icon>
			<input ref="newName" v-model="newName" class="ds-input" spellcheck="false" maxlength="200"
				:aria-label="$t('New folder name')" :placeholder="$t('New folder name')" @keydown.esc.prevent="creating = false" />
			<button class="ds-primary-btn" type="submit" :disabled="!newName.trim() || busy">{{ $t('Create') }}</button>
			<button class="ds-secondary-btn" type="button" @click="creating = false">{{ $t('Cancel') }}</button>
		</form>
		<p v-if="createError" class="create-error ds-error-text" role="alert">{{ createError }}</p>
		<div class="picker-foot">
			<button class="ds-secondary-btn" :disabled="!path || creating" :title="path ? '' : $t('Open a storage location first')" @click="startCreate">
				<b-icon icon="folder-plus-outline" custom-size="mdi-18px"></b-icon><span>{{ $t('New folder') }}</span>
			</button>
			<span class="picker-selected one-line" :title="target">{{ target }}</span>
			<button class="ds-secondary-btn" @click="close">{{ $t('Cancel') }}</button>
			<button class="ds-primary-btn" :disabled="!target" @click="choose">{{ $t('Select') }}</button>
		</div>
	</div>
</template>

<script>
import { downloadSidecar, rootOf } from '@/api/downloadSidecar'

// Downloads may only be saved inside the sidecar's storage roots (/DATA,
// /media, /mnt and mounted data drives - the sidecar enforces it), so the
// picker starts from those and never lets you climb above them.
export default {
	name: 'DsFolderPickerWindow',
	props: {
		winId: { type: String, default: '' },
		startPath: { type: String, default: '/DATA' },
		onSelect: { type: Function, default: null }
	},
	data() {
		return {
			roots: [],
			// '' = the list of storage roots
			path: '',
			items: [],
			loading: false,
			loadError: '',
			selected: '',
			creating: false,
			newName: '',
			createError: '',
			busy: false
		}
	},
	computed: {
		root() {
			return rootOf(this.path, this.roots)
		},
		folders() {
			if (!this.path) return this.roots.map(r => ({ name: r, path: r, is_dir: true }))
			return this.items.filter(i => i.is_dir && !i.name.startsWith('.')).sort((a, b) => a.name.localeCompare(b.name))
		},
		crumbs() {
			const root = this.root
			if (!root) return []
			const out = [{ name: root, path: root }]
			let acc = root
			for (const p of this.path.slice(root.length).split('/').filter(Boolean)) {
				acc += '/' + p
				out.push({ name: p, path: acc })
			}
			return out
		},
		target() {
			return this.selected || this.path
		}
	},
	async created() {
		this.loading = true
		try {
			const res = await downloadSidecar.storageRoots()
			this.roots = (res && res.roots) || []
		} catch (e) {
			this.roots = []
			this.loadError = this.$t('Could not load the storage locations: {error}', { error: e.message })
			this.loading = false
			return
		}
		const start = (this.startPath || '').replace(/\/+$/, '')
		this.navigate(rootOf(start, this.roots) ? start : '')
	},
	methods: {
		async load() {
			this.loadError = ''
			if (!this.path) {
				this.items = []
				this.loading = false
				return
			}
			this.loading = true
			try {
				const res = await this.$api.folder.getList(this.path)
				this.items = (res.data.data && res.data.data.content) || []
			} catch (e) {
				this.items = []
				this.loadError = this.$t('Could not open this folder.')
			} finally {
				this.loading = false
			}
		},
		navigate(p) {
			// Anything outside the storage roots goes back to the root list.
			this.path = p && rootOf(p, this.roots) ? p : ''
			this.selected = ''
			this.creating = false
			this.createError = ''
			this.load()
		},
		startCreate() {
			if (!this.path) return
			this.creating = true
			this.newName = ''
			this.createError = ''
			this.$nextTick(() => this.$refs.newName && this.$refs.newName.focus())
		},
		async createFolder() {
			const name = this.newName.trim()
			if (!name || !this.path) return
			this.busy = true
			this.createError = ''
			try {
				const res = await downloadSidecar.createFolder(this.path, name)
				this.creating = false
				await this.load()
				this.selected = (res && res.path) || `${this.path}/${name}`
			} catch (e) {
				this.createError = this.$t('Could not create the folder: {error}', { error: e.message })
			} finally {
				this.busy = false
			}
		},
		choose() {
			if (!this.target) return
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
	&.current {
		font-weight: 600;
		color: var(--theme-text-primary, #1e293b);
	}
	&:focus-visible {
		outline: 2px solid var(--theme-input-focus, #2563eb);
	}
}

.crumb-sep {
	color: var(--theme-text-muted, #5b6779);
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
	&:focus-visible {
		outline: 2px solid var(--theme-input-focus, #2563eb);
		outline-offset: -2px;
	}
}

.folder-glyph {
	color: var(--color-primary-fg, #1d4ed8);
}

.new-folder-row {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-4);
	border-top: 1px solid var(--theme-card-border, rgb(228 233 237));

	.ds-primary-btn,
	.ds-secondary-btn {
		height: 2.1rem;
	}
}

.create-error {
	margin: 0;
	padding: 0 var(--space-4) var(--space-2);
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
