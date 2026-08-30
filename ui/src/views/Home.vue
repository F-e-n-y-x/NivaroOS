<template>
	<div v-if="!isLoading" class="relative h-full flex flex-col">
		<!-- Content Start -->
		<div class=" overflow-y-auto overflow-x-hidden flex-1 contextmenu-canvas" @contextmenu.prevent="openHomeContaxtMenu">
			<div class="container home-container pt-4">
				<div class="main-content">
					<!-- MainContent Start -->
					<div class=" contextmenu-canvas">
						<!-- core-service Start -->
						<section>
							<transition name="fade">
								<core-service></core-service>
							</transition>
						</section>
						<!-- core-service End -->

						<!-- Apps Start -->
						<section>
							<app-section ref="apps"></app-section>
						</section>
						<!-- Apps End -->
					</div>
					<!-- MainContent End -->
				</div>

				<!-- Right-side widget column, after .main-content in the
				markup so it lands on the right in the row flex layout below
				(see .container.home-container) - .side-bar itself has no
				background, so this reads as widgets floating over the
				desktop rather than a distinct bar. -->
				<side-bar v-if="!hardwareInfoLoading"></side-bar>
			</div>
		</div>
		<!-- Content End -->

		<!-- Files now opens as a desktop window (see showFiles) instead of
		a full-screen modal - see window-manager in App.vue -->
	</div>
</template>

<script>

import SideBar             from '@/components/SideBar.vue';
import CoreService         from '@/components/CoreService.vue';
import AppSection          from '@/components/Apps/AppSection.vue';
import {mixin}             from '@/mixins/mixin';
import events              from '@/events/events';
import {nanoid}            from 'nanoid';


const wallpaperConfig = "wallpaper"

export default {
	name: "home-page",
	mixins: [mixin],
	components: {
		SideBar,
		AppSection,
		CoreService,
	},
	data() {
		return {
			isLoading: true,
			hardwareInfoLoading: true,
			user_id: localStorage.getItem("user_id") ? localStorage.getItem("user_id") : 1,
			barData: {},
		}
	},
	provide() {
		return {
			homeShowFiles: this.showFiles,
		};
	},

	computed: {
		sidebarOpen() {
			return this.$store.state.sidebarOpen
		},
	},
	created() {
		this.getHardwareInfo();
		this.getWallpaperConfig();
		this.getAppearanceConfig();
		this.getConfig();

		this.$store.commit('SET_ACCESS_ID', nanoid());
	},
	mounted() {
		window.addEventListener("resize", this.onResize);
		this.onResize()
		if (sessionStorage.getItem('fromWelcome')) {
			this.$messageBus('global_newvisit')
			this.rssConfirm()
			// one-off consumption
			sessionStorage.removeItem('fromWelcome')
		}
		this.$messageBus('global_visit')

		this.$EventBus.$on('casaUI:openStorageManager', () => {
			this.showStorageManagerPanelModal();
		});

	},
	methods: {

		/**
		 * @description: Get Recasa Configs
		 * @param {*}
		 * @return {*}
		 */
		async getConfig() {
			let systemConfig = await this.$api.users.getCustomStorage("system")
			if (systemConfig.data.success != 200 || systemConfig.data.data == "") {
				const barData = {
					lang: this.getLangFromBrowser(),
					recommend_switch: true,
					existing_apps_switch: true,
					rss_switch: this.barData.rss_switch,
				}
				// save
				const saveRes = await this.$api.users.setCustomStorage("system", barData)
				if (saveRes.data.success === 200) {
					systemConfig = saveRes
					this.barData = saveRes.data.data
				}
			}

			this.$store.commit('SET_RECOMMEND_SWITCH', systemConfig.data.data.recommend_switch);
			this.$store.commit('SET_RSS_SWITCH', systemConfig.data.data.rss_switch);
			this.barData = systemConfig.data.data
			this.isLoading = false

		},

		/**
		 * @description: Show Files
		 * @param {*}
		 * @return {*} void
		 */
		showFiles(path) {
			// Files now opens as a real desktop window (FilesApp, the
			// rewritten Files app - the legacy FilePanel/filebrowser tree
			// this used to open is gone). FilesApp has no path prop of its
			// own (it seeds from $store.state.currentPath on mount), so a
			// specific path from CoreService's disk-widget click still
			// isn't honored if a Files window is already open (OPEN_WINDOW
			// just refocuses an existing window by id, it doesn't re-drive
			// it to a new path) - same pre-existing limitation as before
			// this cutover, not a new gap.
			if (path) this.$store.commit('SET_CURRENT_PATH', path)
			this.$store.commit('OPEN_WINDOW', {
				id: 'files',
				title: this.$t('Files'),
				component: 'FilesApp',
				width: 960,
				height: 620
			})
		},

		/**
		 * @description: Window Resize Handler
		 * @param {*}
		 * @return {*} void
		 */
		onResize() {
			if (window.innerWidth > 480 && this.sidebarOpen) {
				this.$store.commit('SET_SIDEBAR_CLOSE');
			}
		},

		/**
		 * @description: Get Hardware info and save to store
		 * @param {*}
		 * @return {*} void
		 */

		getHardwareInfo() {
			this.$api.sys.getUtilization().then(res => {
				if (res.data.success === 200) {
					this.hardwareInfoLoading = false
					this.$store.commit('SET_HARDWARE_INFO', res.data.data);
				}
			})
		},

		openHomeContaxtMenu(e) {
			// console.log(e.target);
			this.$EventBus.$emit(events.SHOW_HOME_CONTEXT_MENU, e);
		},

		getWallpaperConfig() {
			this.$api.users.getCustomStorage(wallpaperConfig).then(res => {
				if (res.data.success === 200 && res.data.data != "") {
					this.$store.commit('SET_WALLPAPER', {
						path: res.data.data.path,
						from: res.data.data.from
					})
				}
			})
		},

		getAppearanceConfig() {
			this.$api.users.getCustomStorage('appearance').then(res => {
				if (res.data.success === 200 && res.data.data) {
					const { alpha, blur } = res.data.data
					if (alpha !== undefined && alpha !== null) {
						document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha)
						localStorage.setItem('uiBackdropAlpha', alpha)
					}
					if (blur !== undefined && blur !== null) {
						document.documentElement.style.setProperty('--ui-backdrop-blur', `${blur}px`)
						localStorage.setItem('uiBackdropBlur', blur)
					}
				}
			}).catch(() => {})
		},

		// one-off
		rssConfirm() {
			this.$buefy.dialog.confirm({
				title: this.$t('Show news feed from Recasa Blog'),
				message: this.$t('Recasa dashboard will get the the latest news feed of https://blog.casaos.io via Internet, which might leave your visit records to the site. Do you accept?'),
				type: 'is-dark',
				confirmText: this.$t('Accept'),
				cancelText: this.$t('Cancel'),
				onConfirm: async () => {
					let systemConfig = await this.$api.users.getCustomStorage("system")
					let barData = systemConfig.data.data
					barData.rss_switch = true
					const saveRes = await this.$api.users.setCustomStorage("system", barData)
					this.barData = saveRes.data.data
				},
				onCancel: () => {
					this.barData.rss_switch = false
				}
			})
		},

		// Opens Settings' own (windowed, movable) Storage section instead
		// of the old StorageManagerPanel modal, which floated outside any
		// window - triggered from the "found a new drive" notification's
		// "Storage Manager" action (see CoreService.vue's transformLocalStorage).
		async showStorageManagerPanelModal() {
			this.$messageBus('widget_storagemanager');
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540,
				props: { section: 'storage' }
			})
		},

	},
	beforeDestroy() {
		window.removeEventListener("resize", this.onResize);
		this.$EventBus.$off('casaUI:openStorageManager');
	},

}
</script>

<style lang="scss" scoped>
.out-container {
	position: relative;
	height: 100%;
}

// Bulma's .container caps out at a flat 1344px past the fullhd
// breakpoint and just centers with empty margins forever after that -
// wastes most of the screen on any 1920x1080+/ultrawide monitor.
// Scale it with viewport width instead, capped so it doesn't stretch
// absurdly thin/wide on super-ultrawide displays.
.container.home-container {
	// Row flex so .main-content and .side-bar sit side by side (widget
	// column on the right, per markup order above) instead of .side-bar
	// floating over .main-content as a separate fixed layer.
	display: flex;
	align-items: flex-start;
	// Left as Bulma's own `margin: 0 auto` (not overridden here) - auto
	// margins always split the leftover space evenly, so the app-grid's
	// left gap and the widget column's right gap stay identical by
	// construction, at whatever moderate distance the max-width rules
	// below settle on. An explicit fixed margin was tried instead and
	// reverted - it only stays symmetric until max-width (below) becomes
	// the binding constraint, at which point a fixed non-auto margin on
	// both sides over-constrains the box and CSS resolves it by dumping
	// all the slack onto one side, breaking the exact symmetry this is
	// meant to guarantee.

	// Previously 90%/92%/2400px - scaled with viewport width, which meant
	// a big absolute gap on anything wider than ~1920px (the actual
	// complaint). max-width: none + an explicit small fixed margin below
	// keeps the gap the same small size on every screen instead of
	// growing with it.
	max-width: none;
	margin: 0 1.25rem;
}

.contents {
	flex: 1;
	overflow-y: auto;
	overflow-x: hidden;
	// Was 7rem, sized to leave room below the (now-removed) base-bar
	// overlay. That space is unused now, so reclaim most of it.
	height: calc(100% - 4rem);
}

.main-content {
	z-index: 10;
	// Takes whatever's left after .side-bar's fixed width (see SideBar.vue)
	// - min-width: 0 keeps a flex item from refusing to shrink below its
	// content's natural width, which AppSection's absolutely-positioned
	// canvas doesn't otherwise constrain on its own.
	flex: 1 1 auto;
	min-width: 0;
}

@media screen and (max-width: 480px) {
	// .side-bar hides below 480px (see its own style block) - reclaim the
	// row instead of leaving a phantom flex sibling's gap.
	.main-content {
		width: 100%;
	}
}

.dark-bg {
	position: fixed;
	transition: all 0.3s ease;
	left: 0;
	top: 0;
	width: 100%;
	height: 100%;
	background-color: rgba(0, 0, 0, 1);
	z-index: 19;
	opacity: 0;
	visibility: hidden;

	&.open {
		opacity: 1;
		visibility: visible;
	}
}

@media screen and (max-width: 480px) {
	.contents {
		height: calc(100vh - 4rem) !important;
	}

	.container {
		height: 100%;
	}
}
</style>
