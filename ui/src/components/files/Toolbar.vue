<!-- src/components/files/Toolbar.vue -->
<template>
	<header class="files-toolbar">
		<div v-if="hasSelection" class="selection-bar">
			<b-icon icon="close" class="is-clickable" @click.native="$emit('clear-selection')"></b-icon>
			<span class="selection-label">{{ selectionSummary.count }} {{ $t('selected') }}</span>
		</div>
		<div v-else ref="breadContainer" class="breadcrumb-bar">
			<span ref="liveCrumbs" class="live-crumbs">
				<b-dropdown v-if="hiddenCrumbs.length" aria-role="list">
					<template #trigger>
						<b-icon icon="dots-horizontal" custom-size="mdi-18px"></b-icon>
					</template>
					<b-dropdown-item v-for="c in hiddenCrumbs" :key="c.path" @click="go(c)">
						{{ $t(c.name) }}
					</b-dropdown-item>
				</b-dropdown>
				<span
					v-for="(c, index) in visibleCrumbs"
					:key="c.path"
					class="crumb"
					:class="{ current: index === visibleCrumbs.length - 1 }"
					@click="index === visibleCrumbs.length - 1 ? null : go(c)"
					>{{ $t(c.name) }}</span>
			</span>
			<span ref="shadowCrumbs" class="shadow-crumbs">
				<span v-for="c in crumbs" :key="'shadow-' + c.path" class="crumb">{{ $t(c.name) }}</span>
			</span>
		</div>

		<!-- New Folder/New File/Upload/Paste/view-mode stay visible at all times
		     (not just when nothing is selected) - the selection-specific actions
		     (Rename/Copy/Cut/Download/New Window/Delete) are added alongside
		     them, not swapped in for them. -->
		<div class="actions" :class="{ overflow: filesController.breakpoints.toolbarCollapsed }">
			<template v-if="filesController.breakpoints.toolbarCollapsed">
				<b-dropdown aria-role="list" position="is-bottom-left">
					<template #trigger>
						<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
					</template>
					<b-dropdown-item v-if="singleItem" aria-role="menuitem" @click="$emit('rename-selection')">
						<i class="mdi mdi-pencil-outline mr-2"></i>{{ $t('Rename') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('copy-selection')">
						<i class="mdi mdi-content-copy mr-2"></i>{{ $t('Copy') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('move-selection')">
						<i class="mdi mdi-content-cut mr-2"></i>{{ $t('Cut') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('download-selection')">
						<i class="mdi mdi-download-outline mr-2"></i>{{ $t('Download') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('compress-selection')">
						<i class="mdi mdi-folder-zip-outline mr-2"></i>{{ $t('Compress to Zip') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="singleArchiveItem" aria-role="menuitem" @click="$emit('extract-selection')">
						<i class="mdi mdi-archive-arrow-down-outline mr-2"></i>{{ $t('Extract') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="singleItem" aria-role="menuitem" @click="$emit('open-selection-window')">
						<i class="mdi mdi-open-in-new mr-2"></i>{{ $t('New Window') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" class="has-text-danger" @click="$emit('delete-selection')">
						<i class="mdi mdi-trash-can-outline mr-2"></i>{{ $t('Delete') }}
					</b-dropdown-item>
					<b-dropdown-item v-if="hasClipboard" aria-role="menuitem" @click="$emit('paste')">
						<i class="mdi mdi-content-paste mr-2"></i>{{ $t('Paste') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-folder')">
						<i class="mdi mdi-folder-plus-outline mr-2"></i>{{ $t('New Folder') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-file')">
						<i class="mdi mdi-file-plus-outline mr-2"></i>{{ $t('New File') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('upload')">
						<i class="mdi mdi-upload-outline mr-2"></i>{{ $t('Upload') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="toggleHidden">
						<i class="mdi mr-2" :class="showHidden ? 'mdi-eye-off-outline' : 'mdi-eye-outline'"></i>{{ showHidden ? $t('Hide Hidden Files') : $t('Show Hidden Files') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('set-view', nextViewMode)">
						<i class="mdi mdi-view-grid-outline mr-2"></i>{{ viewModeLabel }}
					</b-dropdown-item>
				</b-dropdown>
			</template>
			<template v-else>
				<div v-if="hasSelection" class="action-group">
					<button v-if="singleItem" class="action-btn" @click="$emit('rename-selection')">
						<b-icon icon="pencil-outline" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Rename') }}</span>
					</button>
					<button class="action-btn" @click="$emit('copy-selection')">
						<b-icon icon="content-copy" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Copy') }}</span>
					</button>
					<button class="action-btn" @click="$emit('move-selection')">
						<b-icon icon="content-cut" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Cut') }}</span>
					</button>
					<button class="action-btn" @click="$emit('download-selection')">
						<b-icon icon="download-outline" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Download') }}</span>
					</button>
					<button class="action-btn" @click="$emit('compress-selection')">
						<b-icon icon="folder-zip-outline" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Compress to Zip') }}</span>
					</button>
					<button v-if="singleArchiveItem" class="action-btn" @click="$emit('extract-selection')">
						<b-icon icon="archive-arrow-down-outline" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Extract') }}</span>
					</button>
					<button v-if="singleItem" class="action-btn" @click="$emit('open-selection-window')">
						<b-icon icon="open-in-new" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('New Window') }}</span>
					</button>
				</div>
				<button v-if="hasSelection" class="action-btn delete-btn" @click="$emit('delete-selection')">
					<b-icon icon="trash-can-outline" custom-size="mdi-16px"></b-icon>
					<span>{{ $t('Delete') }}</span>
				</button>
				<div class="action-group">
					<button v-if="hasClipboard" class="action-btn paste-btn" @click="$emit('paste')">
						<b-icon icon="content-paste" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Paste') }}</span>
					</button>
					<!-- Hidden during a selection - showing both action groups at once
					     overflowed/clipped the toolbar. -->
					<template v-if="!hasSelection">
						<button class="action-btn" @click="$emit('new-folder')">
							<b-icon icon="folder-plus-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('New Folder') }}</span>
						</button>
						<button class="action-btn" @click="$emit('new-file')">
							<b-icon icon="file-plus-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('New File') }}</span>
						</button>
						<button class="action-btn" @click="$emit('upload')">
							<b-icon icon="upload-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Upload') }}</span>
						</button>
					</template>
				</div>
				<b-tooltip :label="showHidden ? $t('Hide Hidden Files') : $t('Show Hidden Files')" position="is-left" type="is-dark">
					<button class="view-trigger" :class="{ 'is-active': showHidden }" @click="toggleHidden">
						<b-icon :icon="showHidden ? 'eye-off-outline' : 'eye-outline'" custom-size="mdi-18px"></b-icon>
					</button>
				</b-tooltip>
				<b-tooltip :label="viewModeLabel" position="is-left" type="is-dark">
					<button class="view-trigger" @click="$emit('set-view', nextViewMode)">
						<b-icon :icon="viewModeIcon" custom-size="mdi-18px"></b-icon>
					</button>
				</b-tooltip>
			</template>
		</div>
	</header>
</template>

<script>
import { buildBreadcrumb } from '@/utils/files/breadcrumb'
import { isArchive } from '@/utils/files/archive'

export default {
	name: 'files-toolbar',
	inject: ['filesController'],
	props: {
		selectionSummary: {
			type: Object,
			default: null,
		},
		selectedItems: {
			type: Array,
			default: () => [],
		},
	},
	data() {
		return { hiddenCount: 0, resizeObserver: null }
	},
	computed: {
		hasSelection() {
			return !!(this.selectionSummary && this.selectionSummary.count > 0)
		},
		hasClipboard() {
			return this.$store.state.operateObject != null
		},
		viewMode() {
			return this.$store.state.viewMode
		},
		// A single button cycling Thumbnails -> Large Thumbnails -> Details
		// -> Thumbnails - by request, no dropdown menu for this.
		nextViewMode() {
			const order = ['grid', 'grid-large', 'list']
			return order[(order.indexOf(this.viewMode) + 1) % order.length]
		},
		viewModeIcon() {
			return { grid: 'view-grid-outline', 'grid-large': 'view-dashboard-outline', list: 'view-list-outline' }[this.viewMode]
		},
		viewModeLabel() {
			return { grid: this.$t('Thumbnails'), 'grid-large': this.$t('Large Thumbnails'), list: this.$t('Details') }[this.viewMode]
		},
		// Rename and Open in New Window only make sense for exactly one
		// selected item - a batch has no single new-window destination, and
		// nothing to rename them all "to". FilesApp branches New Window's
		// actual behavior by whether that one item is a file or a folder.
		singleItem() {
			return this.selectedItems.length === 1
		},
		singleArchiveItem() {
			return this.selectedItems.length === 1 && isArchive(this.selectedItems[0])
		},
		crumbs() {
			return buildBreadcrumb(this.filesController.currentPath)
		},
		visibleCrumbs() {
			return this.hiddenCount === 0 ? this.crumbs : this.crumbs.slice(this.hiddenCount)
		},
		hiddenCrumbs() {
			return this.hiddenCount === 0 ? [] : this.crumbs.slice(0, this.hiddenCount)
		},
		showHidden() {
			return !!this.$store.state.showHidden
		},
	},
	watch: {
		'filesController.currentPath'() {
			this.hiddenCount = 0
			this.$nextTick(this.measure)
		},
		// The breadcrumb bar (and its ResizeObserver target) is torn down and
		// recreated by the v-if/v-else toggle whenever the toolbar switches in
		// and out of selection mode - re-attach the observer to the fresh DOM
		// node and re-measure whenever the breadcrumb bar comes back.
		hasSelection(value) {
			if (value) return
			this.$nextTick(() => {
				if (this.resizeObserver && this.$refs.breadContainer) {
					this.resizeObserver.disconnect()
					this.resizeObserver.observe(this.$refs.breadContainer)
				}
				this.measure()
			})
		},
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(() => this.measure())
		this.resizeObserver.observe(this.$refs.breadContainer)
		this.$nextTick(this.measure)
	},
	beforeDestroy() {
		this.resizeObserver && this.resizeObserver.disconnect()
	},
	methods: {
		go(crumb) {
			this.filesController.navigate(crumb.path)
		},
		toggleHidden() {
			this.$store.commit('SET_SHOW_HIDDEN', !this.showHidden)
		},
		measure() {
			const container = this.$refs.breadContainer
			const shadow = this.$refs.shadowCrumbs
			if (!container || !shadow) return
			const containerWidth = container.clientWidth
			if (shadow.scrollWidth <= containerWidth || this.crumbs.length <= 1) {
				this.hiddenCount = 0
				return
			}
			const children = Array.from(shadow.children)
			const DROPDOWN_RESERVE = 32 // approx width of the "…" dropdown trigger icon
			let hidden = this.crumbs.length - 1 // fallback: hide everything except the last crumb
			for (let hideCount = 1; hideCount < this.crumbs.length; hideCount++) {
				const visibleWidth = children.slice(hideCount).reduce((sum, el) => sum + el.offsetWidth, 0)
				if (visibleWidth + DROPDOWN_RESERVE <= containerWidth) {
					hidden = hideCount
					break
				}
			}
			this.hiddenCount = hidden
		},
	},
}
</script>

<style lang="scss" scoped>
.files-toolbar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	height: 2.75rem;
	padding: 0 0.85rem;
	background: #fff;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
}
.breadcrumb-bar {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	position: relative;
	height: 1.5rem;
	display: flex;
	align-items: center;
}
.live-crumbs {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	white-space: nowrap;
}
.shadow-crumbs {
	position: absolute;
	visibility: hidden;
	pointer-events: none;
	white-space: nowrap;
}
.crumb {
	font-size: 0.8125rem;
	color: #64748b;
	cursor: pointer;

	&:hover:not(.current) {
		color: #1e293b;
	}

	&.current {
		color: #1e293b;
		font-weight: 600;
		cursor: default;
	}

	&:not(:last-child)::after {
		content: '/';
		margin-left: 0.35rem;
		color: #cbd5e1;
	}
}
.actions {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	flex-shrink: 0;
}
.action-group {
	display: flex;
	align-items: center;
	gap: 0.25rem;
}
.action-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	padding: 0.3rem 0.6rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 6px;
	font-size: 0.75rem;
	font-weight: 500;
	color: #334155;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: rgba(0, 0, 0, 0.04);
		color: #0f172a;
	}
}
.paste-btn {
	background: rgba(50, 115, 220, 0.1);
	border-color: rgba(50, 115, 220, 0.3);
	color: #3273dc;

	&:hover {
		background: rgba(50, 115, 220, 0.2);
		box-shadow: none;
	}
}
.delete-btn {
	color: #f2534a;
	border-color: rgba(242, 83, 74, 0.2);

	&:hover {
		background: rgba(242, 83, 74, 0.1);
		box-shadow: none;
	}
}
.view-trigger {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.8rem;
	height: 1.8rem;
	border: none;
	background: transparent;
	border-radius: 6px;
	color: rgba(0, 0, 0, 0.55);
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: rgba(0, 0, 0, 0.06);
		color: #2c3e50;
	}

	&.is-active {
		background: rgba(37, 99, 235, 0.12);
		color: #2563eb;
	}
}
.selection-bar {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	gap: 0.5rem;
}
.selection-label {
	font-weight: 600;
	white-space: nowrap;
}
</style>
