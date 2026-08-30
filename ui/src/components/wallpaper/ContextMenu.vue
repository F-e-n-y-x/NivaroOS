<template>
	<div>
		<div class="home-context-menu" :style="{ top: y + 'px', left: x + 'px' }">
			<b-dropdown ref="dropDown" id="dr2" aria-role="list" close-on-click class="file-dropdown"
				:position="'is-' + verticalPos + '-' + horizontalPos" :animation="ani" :mobile-modal="false">
				<template>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center context-menu-item" key="system-context-install"
						@click="showCustomInstall">
						<i class="mdi mdi-cube-plus mr-3 menu-icon"></i>
						<span>{{ $t('Custom Install APP') }}</span>
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center context-menu-item" key="system-context-link"
						@click="showExternalLinkPanel">
						<i class="mdi mdi-link-variant-plus mr-3 menu-icon"></i>
						<span>{{ $t('Add external link/APP') }}</span>
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center context-menu-item" key="system-context-folder"
						@click="showCreateFolderPrompt">
						<i class="mdi mdi-folder-plus-outline mr-3 menu-icon"></i>
						<span>{{ $t('Create folder') }}</span>
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center context-menu-item" key="system-context11"
						@click="openWallpaperSettings">
						<i class="mdi mdi-image-outline mr-3 menu-icon"></i>
						<span>{{ $t('Change wallpaper') }}</span>
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center context-menu-item" key="system-context-arrange-apps"
						@click="arrangeApps">
						<i class="mdi mdi-view-grid-outline mr-3 menu-icon"></i>
						<span>{{ $t('Arrange icons') }}</span>
					</b-dropdown-item>
				</template>
			</b-dropdown>
		</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'

export default {
	mixins: [mixin],
	data() {
		return {
			verticalPos: 'bottom',
			horizontalPos: 'right',
			x: 0,
			y: 0,
			ani: 'fade1'
		}
	},
	mounted() {
		this.$EventBus.$on(events.SHOW_HOME_CONTEXT_MENU, data => {
			this.open(data)
		})
	},
	methods: {
		open(event) {
			const target = event.target
			const className = target?.getAttribute ? target.getAttribute('class') || '' : ''
			const isDesktopCanvas =
				className.includes('contextmenu-canvas') ||
				className.includes('desktop-') ||
				target.tagName === 'MAIN' ||
				target.id === 'app' ||
				(target.classList && target.classList.contains('desktop-viewport'))

			if (isDesktopCanvas) {
				this.$refs.dropDown.isActive = false
				this.$nextTick(() => {
					const menuWidth = 220
					const menuHeight = 220
					const maxLeft = Math.max(12, window.innerWidth - menuWidth - 16)
					const maxTop = Math.max(12, window.innerHeight - menuHeight - 88) // Leaves room above the bottom dock

					this.x = Math.max(12, Math.min(maxLeft, event.clientX))
					this.y = Math.max(12, Math.min(maxTop, event.clientY))
					this.verticalPos = 'bottom'
					this.horizontalPos = 'right'
					this.$refs.dropDown.isActive = true
				})
			}
		},
		openWallpaperSettings() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'appearance' }
			})
		},
		showCustomInstall() {
			this.$EventBus.$emit(events.SHOW_CUSTOM_INSTALL)
		},
		showExternalLinkPanel() {
			this.$EventBus.$emit(events.SHOW_EXTERNAL_LINK_PANEL)
		},
		showCreateFolderPrompt() {
			this.$EventBus.$emit(events.SHOW_CREATE_FOLDER_PROMPT)
		},
		arrangeApps() {
			this.$EventBus.$emit(events.ARRANGE_APPS)
		}
	}
}
</script>

<style lang="scss" scoped>
.home-context-menu {
	position: fixed;
	z-index: 800;
}

.context-menu-item {
	font-size: 0.875rem;
	padding: 0.5rem 0.85rem;
	border-radius: 6px;
	transition: all 0.15s ease;

	.menu-icon {
		font-size: 1.15rem;
		color: #4b5563;
	}

	&:hover {
		background: rgba(59, 130, 246, 0.1);
		.menu-icon {
			color: #2563eb;
		}
	}
}
</style>
