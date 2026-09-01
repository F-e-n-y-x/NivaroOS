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
	<vm-overlay-panel :active="active" :title="title" width="28rem" height="28rem" @close="$emit('close')">
		<div class="picker-breadcrumb">
			<template v-for="(crumb, i) in crumbs">
				<button :key="crumb.path" class="crumb" @click="navigate(crumb.path)">{{ crumb.name }}</button>
				<span v-if="i < crumbs.length - 1" :key="crumb.path + '-sep'" class="crumb-sep">/</span>
			</template>
		</div>
		<div class="picker-list">
			<div v-if="loading" class="picker-status">{{ $t('Loading...') }}</div>
			<div v-else-if="!items.length" class="picker-status">{{ $t('This folder is empty') }}</div>
			<template v-else>
				<button
					v-for="item in sortedItems"
					:key="item.path"
					class="picker-row"
					:class="{ selected: item.path === selectedPath, dimmed: !item.is_dir && !matchesExtension(item) }"
					@click="onRowClick(item)"
				>
					<b-icon :icon="item.is_dir ? 'folder-outline' : 'files-outline'" pack="casa" custom-size="casa-18px" class="casa-color-blue"></b-icon>
					<span class="one-line">{{ item.name }}</span>
				</button>
			</template>
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
		directoryMode: { type: Boolean, default: false }
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
			const parts = this.currentPath.split('/').filter(Boolean)
			const crumbs = [{ name: this.$t('Root'), path: '/' }]
			let acc = ''
			parts.forEach(part => {
				acc += '/' + part
				crumbs.push({ name: part, path: acc })
			})
			return crumbs
		},
		sortedItems() {
			return [...this.items].sort((a, b) => {
				if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
				return a.name.localeCompare(b.name)
			})
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
			this.currentPath = path
			this.load()
		},
		onRowClick(item) {
			if (item.is_dir) {
				this.navigate(item.path)
			} else {
				this.selectedPath = item.path
			}
		},
		confirm() {
			if (!this.selectedPath) return
			this.$emit('selected', this.selectedPath)
			this.$emit('close')
		},
		confirmFolder() {
			this.$emit('selected', this.selectedPath || this.currentPath)
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
	gap: 0.25rem;
	margin-bottom: 0.5rem;
	font-size: 0.8rem;
}

.crumb {
	border: none;
	background: none;
	color: #3273dc;
	font-family: inherit;
	font-size: inherit;
	cursor: pointer;
	padding: 0.1rem 0.2rem;
	border-radius: 4px;

	&:hover {
		background: rgba(50, 115, 220, 0.1);
	}

	&:last-child {
		color: #2c3e50;
		font-weight: 600;
		cursor: default;

		&:hover {
			background: none;
		}
	}
}

.crumb-sep {
	color: rgba(0, 0, 0, 0.3);
}

.picker-list {
	// Fills whatever room the dialog actually has instead of a fixed
	// height - a fixed height taller than the available space forced the
	// overlay body to scroll too, nesting a second scrollbar around this
	// one's own.
	flex: 1 1 auto;
	min-height: 8rem;
	overflow-y: auto;
	border: 1px solid rgb(228 233 237);
	border-radius: 6px;
	padding: 0.25rem;
}

.picker-status {
	padding: 1rem;
	text-align: center;
	color: rgba(0, 0, 0, 0.45);
	font-size: 0.85rem;
}

.picker-row {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	width: 100%;
	border: none;
	background: none;
	text-align: left;
	padding: 0.4rem 0.5rem;
	border-radius: 4px;
	cursor: pointer;
	font-family: inherit;
	font-size: 0.85rem;

	&:hover {
		background: rgba(0, 0, 0, 0.05);
	}

	&.selected {
		background: rgba(50, 115, 220, 0.15);
		color: #3273dc;
		font-weight: 600;
	}

	&.dimmed {
		opacity: 0.45;
	}
}

.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
