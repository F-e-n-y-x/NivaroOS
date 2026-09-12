<!--
	A general filesystem browser for picking a single file (an ISO/IMG
	install disc, or any other path) - unlike the old fixed dropdown this
	replaces (populated only from the vm-sidecar's own dedicated
	/DATA/VMs/isos folder via vmSidecar.listISOs()), this uses the same
	general-purpose $api.folder.getList() the main Files app uses, so it
	can browse anywhere the filesystem actually has, not just one
	sidecar-managed directory. VmStorage.vue's own "Upload ISO" flow
	(which still populates that dedicated folder) is untouched - this is
	just an additional, more general way to point at a file, wherever it
	actually lives.
-->
<template>
	<vm-overlay-panel :active="active" :title="title || (directoryMode ? $t('Select Folder') : $t('Select File'))" width="30rem" height="26rem" @close="$emit('close')">
		<div class="picker-breadcrumb">
			<template v-for="(crumb, i) in crumbs">
				<button :key="crumb.path" class="crumb" @click="navigate(crumb.path)">{{ crumb.name }}</button>
				<span v-if="i < crumbs.length - 1" :key="crumb.path + '-sep'" class="crumb-sep">/</span>
			</template>
		</div>
		<div class="picker-list">
			<div v-if="loading" class="picker-status">{{ $t('Loading...') }}</div>
			<div v-else-if="!sortedItems.length" class="picker-status">{{ $t('This folder is empty') }}</div>
			<template v-else>
				<div
					v-for="item in sortedItems"
					:key="item.path"
					class="picker-row"
					:class="{ selected: item.path === effectiveSelection, dimmed: !item.is_dir && !matchesExtension(item) }"
					@click="onRowClick(item)"
					@dblclick="onRowDblClick(item)"
				>
					<b-icon :icon="item.is_dir ? 'folder' : 'file-outline'" :class="item.is_dir ? 'folder-glyph' : 'file-glyph'" size="is-small"></b-icon>
					<span class="one-line item-name">{{ item.name }}</span>
					<button v-if="item.is_dir" type="button" class="enter-dir-btn" :title="$t('Open folder')" @click.stop="navigate(item.path)">
						<b-icon icon="chevron-right" size="is-small"></b-icon>
					</button>
				</div>
			</template>
		</div>
		<div v-if="directoryMode" class="picker-selected-summary">
			<span class="selected-label">{{ $t('Selected:') }}</span>
			<span class="selected-path-text" :title="effectiveSelection">{{ effectiveSelection }}</span>
		</div>
		<template #footer>
			<b-button @click="$emit('close')">{{ $t('Cancel') }}</b-button>
			<b-button v-if="directoryMode" type="is-primary" @click="confirmFolder">{{ $t('Select This Folder') }}</b-button>
			<b-button v-else type="is-primary" :disabled="!selectedPath" @click="confirm">{{ $t('Select') }}</b-button>
		</template>
	</vm-overlay-panel>
</template>

<script>
import VmOverlayPanel from './VmOverlayPanel.vue'

export default {
	name: 'vm-file-picker-dialog',
	components: { VmOverlayPanel },
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		// Where the browser opens by default - the caller can point this at
		// wherever files of this kind usually live, but every folder above
		// and beside it is still reachable via the breadcrumb/rows below.
		startPath: { type: String, default: '/DATA' },
		// File extensions (no dot, lowercase) relevant to what's being
		// picked - non-matching files are shown but dimmed, not hidden
		// entirely, since the destination filesystem may use a different
		// extension convention than expected. Empty = no filtering at all.
		extensions: { type: Array, default: () => [] },
		directoryMode: { type: Boolean, default: false },
			// When set, browsing is confined to this directory and below - the
			// "Root" breadcrumb and everything above this point are hidden
			// entirely rather than merely discouraged. Used for VM shared
			// folders, which the backend only accepts from /DATA anyway (see
			// vm-sidecar's validateShareSource) - confining the picker to
			// match means a folder picked here can never be rejected afterwards.
			confineToRoot: { type: String, default: '' }
	},
	data() {
		return {
			currentPath: this.startPath,
			items: [],
			loading: false,
			selectedPath: null
		}
	},
	computed: {
		crumbs() {
			const root = this.confineToRoot || '/'
			const rootParts = root.split('/').filter(Boolean)
			const parts = this.currentPath.split('/').filter(Boolean)
			// When confined, start the breadcrumb at the root itself (named
			// after its last segment, e.g. "DATA") instead of a global "Root"
			// that would otherwise invite navigating above it.
			const crumbs = this.confineToRoot
				? [{ name: rootParts[rootParts.length - 1] || root, path: root }]
				: [{ name: this.$t('Root'), path: '/' }]
			let acc = ''
			parts.forEach((part, i) => {
				acc += '/' + part
				if (this.confineToRoot && i < rootParts.length) return
				crumbs.push({ name: part, path: acc })
			})
			return crumbs
		},
		sortedItems() {
			const list = this.directoryMode
				? this.items.filter(item => item.is_dir)
				: this.items
			return [...list].sort((a, b) => {
				if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
				return a.name.localeCompare(b.name)
			})
		},
		effectiveSelection() {
			return this.selectedPath || this.currentPath
		}
	},
	watch: {
		active(isActive) {
			if (isActive) {
				this.currentPath = this.startPath
				this.selectedPath = null
				this.load()
			}
		}
	},
	methods: {
		matchesExtension(item) {
			if (!this.extensions.length) return true
			const ext = item.name.slice(item.name.lastIndexOf('.') + 1).toLowerCase()
			return this.extensions.includes(ext)
		},
		async load() {
			this.loading = true
			this.selectedPath = null
			try {
				const res = await this.$api.folder.getList(this.currentPath)
				this.items = (res.data.data.content || []).filter(item => !item.name.startsWith('.'))
			} catch (e) {
				this.items = []
			} finally {
				this.loading = false
			}
		},
		navigate(path) {
			if (this.confineToRoot && path !== this.confineToRoot && !path.startsWith(this.confineToRoot + '/')) {
				return
			}
			this.currentPath = path
			this.selectedPath = null
			this.load()
		},
		onRowClick(item) {
			this.selectedPath = item.path
		},
		onRowDblClick(item) {
			if (item.is_dir) {
				this.navigate(item.path)
			} else if (!this.directoryMode && this.matchesExtension(item)) {
				this.confirm()
			}
		},
		confirm() {
			if (!this.selectedPath) return
			this.$emit('selected', this.selectedPath)
			this.$emit('close')
		},
		confirmFolder() {
			this.$emit('selected', this.effectiveSelection)
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
.picker-breadcrumb {
	flex-shrink: 0;
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--space-1);
	margin-bottom: var(--space-2);
	padding: var(--space-1) var(--space-2);
	background: var(--theme-card-subtle, rgba(0, 0, 0, 0.04));
	border-radius: var(--radius-sm);
	font-size: var(--font-sm);
}

.crumb {
	border: none;
	background: none;
	color: var(--color-primary, #3273dc);
	font-family: inherit;
	font-size: inherit;
	font-weight: 500;
	cursor: pointer;
	padding: var(--space-1) var(--space-1);
	border-radius: var(--radius-xs);
	transition: background 0.12s ease;

	&:hover {
		background: rgba(50, 115, 220, 0.15);
	}

	&:last-child {
		color: var(--theme-text-primary, #2c3e50);
		font-weight: 600;
		cursor: default;

		&:hover {
			background: none;
		}
	}
}

.crumb-sep {
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.25));
	font-size: var(--font-xs);
}

.picker-list {
	flex: 1 1 auto;
	min-height: 0;
	overflow-y: auto;
	border: 1px solid var(--theme-card-border, rgb(228 233 237));
	border-radius: var(--radius-control);
	padding: var(--space-1);
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.picker-status {
	padding: var(--space-8);
	text-align: center;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.45));
	font-size: var(--font-base);
}

.picker-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	border: none;
	background: none;
	text-align: left;
	padding: var(--space-2) var(--space-2);
	border-radius: var(--radius-sm);
	cursor: pointer;
	font-family: inherit;
	font-size: var(--font-base);
	color: var(--theme-text-primary, #2c3e50);
	user-select: none;
	transition: all 0.12s ease;

	&:hover {
		background: var(--theme-card-subtle, rgba(0, 0, 0, 0.04));
	}

	&.selected {
		background: rgba(50, 115, 220, 0.12);
		color: #1d4ed8;
		font-weight: 600;

		.folder-glyph {
			color: #1d4ed8;
		}
	}

	&.dimmed {
		opacity: 0.45;
	}
}

.folder-glyph {
	color: #ca8a04;
}

.file-glyph {
	color: var(--theme-text-muted, #64748b);
}

.item-name {
	flex: 1 1 auto;
}

.enter-dir-btn {
	border: none;
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.5));
	border-radius: var(--radius-xs);
	padding: var(--space-1) var(--space-1);
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.12s ease;

	&:hover {
		background: rgba(50, 115, 220, 0.2);
		color: var(--color-primary, #3273dc);
	}
}

.picker-selected-summary {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	margin-top: var(--space-2);
	padding: var(--space-1) var(--space-2);
	background: rgba(50, 115, 220, 0.06);
	border-radius: var(--radius-sm);
	font-size: var(--font-xs);
}

.selected-label {
	font-weight: 600;
	color: var(--theme-text-secondary, rgba(0, 0, 0, 0.6));
}

.selected-path-text {
	font-family: monospace;
	color: #1d4ed8;
	font-weight: 600;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
