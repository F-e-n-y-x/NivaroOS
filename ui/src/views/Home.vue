<template>
	<div v-if="!isLoading" class="relative h-full flex flex-col">
		<!-- Content Start -->
		<div class="overflow-y-auto overflow-x-hidden flex-1 contextmenu-canvas" @contextmenu.prevent="openHomeContaxtMenu">
			<div class="container home-container pt-4">
				<div class="main-content">
					<!-- MainContent Start -->
					<div class="contextmenu-canvas">
						<!-- Apps Grid Start -->
						<section>
							<app-section ref="apps"></app-section>
						</section>
						<!-- Apps Grid End -->
					</div>
					<!-- MainContent End -->
				</div>

				<!-- Right-side hardware widgets floating over the desktop -->
				<side-bar v-if="!hardwareInfoLoading"></side-bar>
			</div>
		</div>
		<!-- Content End -->
	</div>
</template>

<script>
import SideBar from '@/components/SideBar.vue'
import AppSection from '@/components/Apps/AppSection.vue'
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { nanoid } from 'nanoid'

const wallpaperConfig = 'wallpaper'

export default {
	name: 'home-page',
	mixins: [mixin],
	components: {
		SideBar,
		AppSection
	},
	data() {
		return {
			isLoading: true,
			hardwareInfoLoading: true,
			user_id: localStorage.getItem('user_id') ? localStorage.getItem('user_id') : 1,
			barData: {}
		}
	},
	provide() {
		return {
			homeShowFiles: this.showFiles
		}
	},
	computed: {
		sidebarOpen() {
			return this.$store.state.sidebarOpen
		}
	},
	created() {
		this.getHardwareInfo()
		this.getWallpaperConfig()
		this.getAppearanceConfig()
		this.getConfig()

		this.$store.commit('SET_ACCESS_ID', nanoid())
	},
	mounted() {
		window.addEventListener('resize', this.onResize)
		this.onResize()
		if (sessionStorage.getItem('fromWelcome')) {
			this.$messageBus('global_newvisit')
			this.rssConfirm()
			sessionStorage.removeItem('fromWelcome')
		}
		this.$messageBus('global_visit')

		this.$EventBus.$on('casaUI:openStorageManager', () => {
			this.showStorageManagerPanelModal()
		})
	},
	methods: {
		async getConfig() {
			let systemConfig = await this.$api.users.getCustomStorage('system')
			if (systemConfig.data.success != 200 || systemConfig.data.data == '') {
				const barData = {
					lang: this.getLangFromBrowser(),
					recommend_switch: true,
					existing_apps_switch: true,
					rss_switch: this.barData.rss_switch
				}
				const saveRes = await this.$api.users.setCustomStorage('system', barData)
				if (saveRes.data.success === 200) {
					systemConfig = saveRes
					this.barData = saveRes.data.data
				}
			}

			this.$store.commit('SET_RECOMMEND_SWITCH', systemConfig.data.data.recommend_switch)
			this.$store.commit('SET_RSS_SWITCH', systemConfig.data.data.rss_switch)
			this.barData = systemConfig.data.data
			this.isLoading = false
		},

		showFiles(path) {
			if (path) this.$store.commit('SET_CURRENT_PATH', path)
			this.$store.commit('OPEN_WINDOW', {
				id: 'files',
				title: this.$t('Files'),
				component: 'FilesApp',
				width: 960,
				height: 620
			})
		},

		onResize() {
			if (window.innerWidth > 480 && this.sidebarOpen) {
				this.$store.commit('SET_SIDEBAR_CLOSE')
			}
		},

		getHardwareInfo() {
			this.$api.sys.getUtilization().then(res => {
				if (res.data.success === 200) {
					this.hardwareInfoLoading = false
					this.$store.commit('SET_HARDWARE_INFO', res.data.data)
				}
			})
		},

		openHomeContaxtMenu(e) {
			this.$EventBus.$emit(events.SHOW_HOME_CONTEXT_MENU, e)
		},

		getWallpaperConfig() {
			this.$api.users.getCustomStorage(wallpaperConfig).then(res => {
				if (res.data.success === 200 && res.data.data != '') {
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

		rssConfirm() {
			this.$buefy.dialog.confirm({
				title: this.$t('Show news feed from NivaroOS Blog'),
				message: this.$t('NivaroOS dashboard will get the the latest news feed of https://blog.casaos.io via Internet, which might leave your visit records to the site. Do you accept?'),
				type: 'is-dark',
				confirmText: this.$t('Accept'),
				cancelText: this.$t('Cancel'),
				onConfirm: async () => {
					let systemConfig = await this.$api.users.getCustomStorage('system')
					let barData = systemConfig.data.data
					barData.rss_switch = true
					const saveRes = await this.$api.users.setCustomStorage('system', barData)
					this.barData = saveRes.data.data
				},
				onCancel: () => {
					this.barData.rss_switch = false
				}
			})
		},

		async showStorageManagerPanelModal() {
			this.$messageBus('widget_storagemanager')
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'storage' }
			})
		}
	},
	sockets: {
		'local-storage:disk:added'(res) {
			const props = res.Properties || {}
			const model = props.model || 'External Storage'
			const mountPoint = props.mount_point || '/DATA'
			this.$buefy.snackbar.open({
				message: this.$t('Storage drive connected: {model}', { model }),
				type: 'is-info',
				position: 'is-top',
				actionText: this.$t('Open in Files'),
				onAction: () => this.showFiles(mountPoint),
				duration: 6000
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.main-content {
	flex: 1;
	min-width: 0;
}
</style>
