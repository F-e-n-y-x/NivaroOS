<template>
	<div>
		<div class="home-context-menu" :style="{top:y + 'px',left:x+'px'}">
			<b-dropdown aria-role="list" close-on-click ref="dropDown" id="dr2" class="file-dropdown"
						:position="'is-'+verticalPos+'-'+horizontalPos" :animation="ani" :mobile-modal="false">
				<!-- Blank Start -->
				<template>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center" key="system-context-install"
									 @click="showCustomInstall">
						<b-icon pack="casa" icon="add-outline" size="is-small" class="mr-3"></b-icon>
						{{ $t('Custom Install APP') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center" key="system-context-link"
									 @click="showExternalLinkPanel">
						<b-icon pack="casa" icon="internet-outline" size="is-small" class="mr-3"></b-icon>
						{{ $t('Add external link/APP') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center" key="system-context-folder"
									 @click="showCreateFolderPrompt">
						<b-icon pack="casa" icon="folder-plus-outline" size="is-small" class="mr-3"></b-icon>
						{{ $t('Create folder') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center" key="system-context11"
									 @click="openWallpaperSettings">
						<b-icon pack="casa" icon="wallpaper-outline" size="is-small" class="mr-3"></b-icon>
						{{ $t('Change wallpaper') }}
					</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" class="is-flex is-align-items-center" key="system-context-arrange-apps"
									 @click="arrangeApps">
						<b-icon pack="casa" icon="view-grid-outline" size="is-small" class="mr-3"></b-icon>
						{{ $t('Arrange icons') }}
					</b-dropdown-item>
				</template>
				<!-- Blank End -->
			</b-dropdown>
		</div>
	</div>
</template>

<script>
import {mixin} from '@/mixins/mixin';
import events  from '@/events/events';

export default {
	mixins: [mixin],
	data() {
		return {
			verticalPos: "bottom",
			horizontalPos: "right",
			x: Number,
			y: Number,
			ani: "fade1",
		}
	},

	computed: {
		close() {
			return this.item == undefined
		}
	},
	mounted() {
		this.$EventBus.$on(events.SHOW_HOME_CONTEXT_MENU, (data) => {
			this.open(data)
		});


	},
	methods: {
		open(event) {
			const bounced = event.target.getAttribute('class').includes('contextmenu-canvas')
			if (bounced) {
				this.$refs.dropDown.isActive = false
				this.$nextTick(() => {
					this.x = event.clientX
					this.y = event.clientY
					const rightOffset = window.innerWidth - event.clientX - 184
					this.horizontalPos = rightOffset > 0 ? "right" : "left"
					this.$refs.dropDown.isActive = true;
				})
			}
		},
		openWallpaperSettings() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540,
				props: { section: 'appearance' }
			})
		},
		showCustomInstall() {
			this.$EventBus.$emit(events.SHOW_CUSTOM_INSTALL);
		},
		showExternalLinkPanel() {
			this.$EventBus.$emit(events.SHOW_EXTERNAL_LINK_PANEL);
		},
		showCreateFolderPrompt() {
			this.$EventBus.$emit(events.SHOW_CREATE_FOLDER_PROMPT);
		},
		arrangeApps() {
			this.$EventBus.$emit(events.ARRANGE_APPS);
		},
	},
}
</script>

<style lang="scss" scoped>
.home-context-menu {
	position: fixed;
	z-index: 800;
}
</style>