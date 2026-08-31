<template>
	<div :class="{ 'drag-target': isDragTarget }" :data-folder-id="folder.id"
		class="common-card is-flex is-align-items-center is-justify-content-center app-card folder-card"
		@contextmenu.prevent.stop="handleCardContextMenu">
		<div class="action-btn">
			<b-dropdown ref="dro" :mobile-modal="false" append-to-body aria-role="list" class="app-card-drop"
				:triggers="['contextmenu']" animation="fade1" position="is-bottom-left">
				<template #trigger>
					<p role="button"></p>
				</template>
				<b-dropdown-item :focusable="false" aria-role="menu-item" custom>
					<b-button expanded type="is-text" @click="closeMenuThen('rename', folder)">
						<i class="mdi mdi-pencil-outline mr-2"></i>
						{{ $t('Rename') }}
					</b-button>
					<b-button expanded type="is-text" @click="closeMenuThen('editIcon', folder)">
						<i class="mdi mdi-image-edit-outline mr-2"></i>
						{{ $t('Edit icon') }}
					</b-button>
					<b-button class="has-text-red" expanded type="is-text" @click="closeMenuThen('delete', folder)">
						<i class="mdi mdi-trash-can-outline mr-2"></i>
						{{ $t('Delete folder') }}
					</b-button>
				</b-dropdown-item>
			</b-dropdown>
		</div>

		<div class="blur-background"></div>
		<div class="cards-content" @click="handleFolderClick" @dblclick="handleFolderDblClick">
			<div class="has-text-centered is-flex is-justify-content-center is-flex-direction-column icon-cell">
				<div class="is-flex is-justify-content-center">
					<div v-if="folder.icon" class="folder-custom-icon is-52x52" :style="{ borderRadius: (folder.iconRadius || 0) + '%' }">
						<img :src="folder.icon">
					</div>
					<div v-else class="folder-icon-grid is-52x52">
						<div v-for="i in 4" :key="i" class="folder-icon-cell">
							<b-image v-if="previewApps[i - 1]" :src="previewApps[i - 1].icon"
								:src-fallback="require('@/assets/img/app/default.svg')" webp-fallback=".jpg"></b-image>
						</div>
					</div>
				</div>
				<p class="app-label one-line">
					<a class="one-line" style="cursor:default">{{ folder.name }}</a>
				</p>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'folder-card',
	props: {
		folder: {
			type: Object,
			required: true
		},
		isDragTarget: {
			type: Boolean,
			default: false
		}
	},
	computed: {
		previewApps() {
			return (this.folder.apps || []).slice(0, 4)
		}
	},
	methods: {
		handleFolderClick() {
			if (this.$store.state.isMobile) {
				this.$emit('open', this.folder)
			}
		},
		handleFolderDblClick() {
			this.$emit('open', this.folder)
		},
		handleCardContextMenu() {
			if (!this.$refs.dro) return
			this.$refs.dro.isActive = true
		},

		closeMenuThen(eventName, ...args) {
			if (this.$refs.dro) {
				this.$refs.dro.isActive = false
			}
			this.$emit(eventName, ...args)
		}
	}
}
</script>

<style lang="scss" scoped>
.folder-card.drag-target {
	transform: scale(1.12);
	transition: transform 0.15s ease;

	.blur-background {
		opacity: 1;
		background-color: rgba(255, 255, 255, 0.25);
	}

	.folder-icon-grid {
		animation: folder-drag-pulse 0.6s ease-in-out infinite;
	}
}

@keyframes folder-drag-pulse {
	0%, 100% {
		box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.4);
	}
	50% {
		box-shadow: 0 0 0 6px rgba(255, 255, 255, 0.15);
	}
}

.folder-custom-icon {
	overflow: hidden;
	border-radius: 12px;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
}

.folder-icon-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	grid-template-rows: repeat(2, 1fr);
	gap: 2px;
	border-radius: 12px;
	overflow: hidden;
	background: rgba(255, 255, 255, 0.1);
	padding: 2px;
}

.folder-icon-cell {
	background: rgba(255, 255, 255, 0.08);
	border-radius: 4px;
	overflow: hidden;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
}

.icon-cell {
	width: 100%;
	height: 100%;
	padding: 6px 4px 4px;
	box-sizing: border-box;
	justify-content: center;
	gap: 0;
}

.app-label {
	margin-top: 4px;
	font-size: 0.72rem;
	font-weight: 500;
	color: #fff;
	text-shadow: 0 1px 3px rgba(0, 0, 0, 0.85);
	line-height: 1.2;
	max-width: 84px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
</style>
