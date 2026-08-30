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
					<b-dropdown-item v-if="singleItem" aria-role="menuitem" @click="$emit('rename-selection')">{{ $t('Rename') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('copy-selection')">{{ $t('Copy') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('move-selection')">{{ $t('Cut') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('download-selection')">{{ $t('Download') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('compress-selection')">{{ $t('Compress to Zip') }}</b-dropdown-item>
					<b-dropdown-item v-if="singleArchiveItem" aria-role="menuitem" @click="$emit('extract-selection')">{{ $t('Extract') }}</b-dropdown-item>
					<b-dropdown-item v-if="singleItem" aria-role="menuitem" @click="$emit('open-selection-window')">{{ $t('New Window') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasSelection" aria-role="menuitem" @click="$emit('delete-selection')">{{ $t('Delete') }}</b-dropdown-item>
					<b-dropdown-item v-if="hasClipboard" aria-role="menuitem" @click="$emit('paste')">{{ $t('Paste') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-folder')">{{ $t('New Folder') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-file')">{{ $t('New File') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('upload')">{{ $t('Upload') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('set-view', nextViewMode)">{{ viewModeLabel }}</b-dropdown-item>
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
	padding: 0.5rem 0.75rem;
	border-bottom: 1px solid rgb(228 233 237);
	min-width: 0;
}
.breadcrumb-bar {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	overflow: hidden;
	position: relative;
}
.shadow-crumbs {
	position: absolute;
	visibility: hidden;
	white-space: nowrap;
	pointer-events: none;
}
.crumb {
	cursor: pointer;
	padding: 0 0.35rem;
	white-space: nowrap;
	color: rgba(0, 0, 0, 0.55);
	&:hover { text-decoration: underline; }
	// A visual separator between segments, and bold/dark styling for the
	// current folder (the last visible crumb) - previously every segment
	// looked identical with no separator at all, making it genuinely hard
	// to tell which folder you were in at a glance.
	&:not(:first-child)::before {
		content: '/';
		margin-right: 0.35rem;
		color: rgba(0, 0, 0, 0.3);
	}
	&.current {
		cursor: default;
		font-weight: 600;
		color: #2c3e50;
		&:hover { text-decoration: none; }
	}
}
.actions {
	flex-shrink: 0;
	display: flex;
	gap: 0.5rem;
	align-items: center;
}
.action-group {
	display: flex;
	align-items: center;
	gap: 0.15rem;
	background: rgba(0, 0, 0, 0.04);
	border-radius: 8px;
	padding: 0.2rem;
}
.action-btn {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	background: transparent;
	color: #2c3e50;
	font-family: inherit;
	font-size: 0.78rem;
	padding: 0.3rem 0.6rem;
	border-radius: 6px;
	cursor: pointer;
	white-space: nowrap;

	&:hover {
		background: #fff;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.12);
	}
}
.paste-btn {
	background: rgba(50, 115, 220, 0.12);
	color: #3273dc;

	&:hover {
		background: rgba(50, 115, 220, 0.2);
		box-shadow: none;
	}
}
.delete-btn {
	color: #f2534a;

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

	&:hover {
		background: rgba(0, 0, 0, 0.06);
		color: #2c3e50;
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
