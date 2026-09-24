<template>
	<!-- On phone/tablet, WindowManager's MobileScreenHost/MobileTabBar (a
	     sibling of this view, mounted in App.vue) fully replace this
	     desktop surface - its own "Apps" screen reuses the same
	     AppSection this renders below, so showing both at once would be
	     two overlapping, redundant icon grids rather than one. -->
	<div v-if="!isLoading && !isMobileShell" class="desktop-viewport contextmenu-canvas" @contextmenu.prevent="openHomeContaxtMenu">
		<div class="desktop-workspace">
			<div class="desktop-canvas-area contextmenu-canvas">
				<!-- Apps Grid Start -->
				<app-section ref="apps"></app-section>
				<!-- Apps Grid End -->
			</div>

			<!-- Right-side floating hardware widgets -->
			<div class="desktop-sidebar-area">
				<side-bar v-if="!hardwareInfoLoading"></side-bar>
			</div>
		</div>
	</div>
</template>

<script>
import SideBar from '@/shell/SideBar.vue'
import AppSection from '@/apps/app-store/AppSection.vue'
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { THEME_MODES, getStoredThemeMode, applyTheme } from '@/utils/theme'
import { escapeHtml } from '@/utils/escapeHtml'
import { formatSize } from '@/utils/formatSize'
import activityService from '@/service/activity'
import { isFormatting } from '@/apps/storage/storageJobs'

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
			barData: {
				recommend_switch: true
			},
			tokens: {
				token: '',
				refresh_token: ''
			},
			isLoading: true,
			hardwareInfoLoading: true
		}
	},
	computed: {
		sidebarOpen() {
			return this.$store.state.sidebarOpen
		},
		isMobileShell() {
			return this.$store.state.isMobile || this.$store.state.isTablet
		}
	},
	provide() {
		return {
			homeShowFiles: this.showFiles
		}
	},
	created() {
		this.isLoading = true
		this.hardwareInfoLoading = true
		this.getHardwareInfo()
		this.getConfig()
		this.getWallpaperConfig()
		this.getAppearanceConfig()
		this.getDateTimeConfig()
	},
	mounted() {
		window.addEventListener('resize', this.onResize)
		this.onResize()
		sessionStorage.removeItem('fromWelcome')
	},
	methods: {
		async getConfig() {
			let systemConfig = await this.$api.users.getCustomStorage('system')
			if (systemConfig.data.success != 200 || systemConfig.data.data == '') {
				const barData = {
					lang: this.getLangFromBrowser(),
					recommend_switch: true,
					existing_apps_switch: true
				}
				const saveRes = await this.$api.users.setCustomStorage('system', barData)
				if (saveRes.data.success === 200) {
					systemConfig = saveRes
					this.barData = saveRes.data.data
				}
			}

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

		// The widgets only mount once this succeeds, so a transient failure
		// (core still starting, network blip) must not leave the sidebar
		// empty until a reload: retry with backoff 2s, 4s ... capped at 60s.
		getHardwareInfo(attempt = 0) {
			const retry = () => {
				if (this._isDestroyed) return
				setTimeout(() => {
					if (!this._isDestroyed) this.getHardwareInfo(attempt + 1)
				}, Math.min(60000, 2000 * Math.pow(2, attempt)))
			}
			this.$api.sys.getUtilization().then(res => {
				if (res.data.success === 200) {
					this.hardwareInfoLoading = false
					this.$store.commit('SET_HARDWARE_INFO', res.data.data)
				} else {
					retry()
				}
			}).catch(retry)
		},

		openHomeContaxtMenu(e) {
			this.$EventBus.$emit(events.SHOW_HOME_CONTEXT_MENU, e)
		},

		getWallpaperConfig() {
			this.$api.users.getCustomStorage(wallpaperConfig).then(res => {
				if (res.data.success === 200 && res.data.data != '') {
					this.$store.commit('SET_WALLPAPER', res.data.data)
				}
			})
		},

		getAppearanceConfig() {
			this.$api.users.getCustomStorage('appearance').then(res => {
				if (res.data.success === 200 && res.data.data) {
					const { alpha, blur, theme } = res.data.data
					// The theme was saved per user but never read back, so a new
					// device (or cleared storage) fell back to Auto.
					if (theme && Object.values(THEME_MODES).includes(theme) && theme !== getStoredThemeMode()) {
						applyTheme(theme)
					}
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

		getDateTimeConfig() {
			this.$api.users.getCustomStorage('datetime_format').then(res => {
				if (res.data && res.data.success === 200 && res.data.data) {
					const { timeFormat, showSeconds, dateFormatStyle, customDateTimeFormat } = res.data.data
					if (timeFormat) this.$store.commit('SET_TIME_FORMAT', timeFormat)
					if (showSeconds !== undefined) this.$store.commit('SET_SHOW_SECONDS', showSeconds)
					if (dateFormatStyle) this.$store.commit('SET_DATE_FORMAT_STYLE', dateFormatStyle)
					if (customDateTimeFormat !== undefined) this.$store.commit('SET_CUSTOM_DATETIME_FORMAT', customDateTimeFormat)
				}
			}).catch(() => {})
		},

		openStorageSettings() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'storage' }
			})
		},

		// Normalises a local-storage:disk:* event. Hot-plug events carry the
		// udev keys (local-storage:model / local-storage:path); the backend
		// adds lsblk details (model, path, mount_point as a comma-joined list
		// with empty entries for unmounted partitions, size, children:num)
		// when it can still read the disk.
		diskEventInfo(res) {
			const p = (res && (res.Properties || res.properties)) || {}
			const model = (p['local-storage:model'] || p.model || '').replace(/_/g, ' ').trim()
			const path = p['local-storage:path'] || p.path || ''
			const mountPoints = String(p.mount_point || '').split(',').map(m => m.trim()).filter(Boolean)
			const tran = String(p.tran || p['local-storage:bus'] || '').toLowerCase()
			const size = Number(p.size) || 0
			const partitions = p['children:num'] !== undefined ? Number(p['children:num']) : null
			return { model, path, mountPoints, isUsb: tran === 'usb', size, partitions }
		}
	},
	beforeDestroy() {
		window.removeEventListener('resize', this.onResize)
	},
	sockets: {
		'local-storage:disk:added'(res) {
			const info = this.diskEventInfo(res)
			// Our own format repartitions the disk, which udev reports as a
			// remove + add - that's not a newly connected drive.
			if (isFormatting(info.path)) return
			const name = info.model || info.path || this.$t('External storage')
			const detail = [info.path, info.size ? formatSize(info.size) : ''].filter(Boolean).join(' · ')
			const mountPoint = info.mountPoints[0] || ''
			const needsSetup = !mountPoint && info.partitions === 0
			const title = needsSetup
				? this.$t('New unformatted disk detected')
				: (info.isUsb ? this.$t('USB drive connected') : this.$t('Storage drive connected'))
			const action = mountPoint
				? { label: this.$t('Open in Files'), path: mountPoint }
				: { label: this.$t('Storage settings'), window: { id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540, props: { section: 'storage' } } }
			activityService.add({
				title,
				message: [name, detail, info.mountPoints.join(', ')].filter(Boolean).join(' - '),
				type: info.isUsb ? 'usb' : 'storage',
				status: needsSetup ? 'warning' : 'info',
				action
			})
			this.$buefy.snackbar.open({
				message: `${escapeHtml(title)}: <b>${escapeHtml(name)}</b>`,
				type: 'is-info',
				position: 'is-top',
				actionText: action.label,
				onAction: () => (mountPoint ? this.showFiles(mountPoint) : this.openStorageSettings()),
				duration: 6000
			})
		},
		'local-storage:disk:removed'(res) {
			const info = this.diskEventInfo(res)
			if (isFormatting(info.path)) return
			activityService.add({
				title: info.isUsb ? this.$t('USB drive disconnected') : this.$t('Storage drive removed'),
				message: info.model || info.path || this.$t('External storage'),
				type: info.isUsb ? 'usb' : 'storage',
				status: 'info'
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.desktop-viewport {
	position: fixed;
	inset: 0;
	width: 100vw;
	height: 100vh;
	overflow: hidden;
	user-select: none;
	-webkit-user-select: none;
}

.desktop-workspace {
	display: flex;
	align-items: flex-start;
	width: 100%;
	height: 100%;
	padding: var(--space-5) var(--space-6) calc(80px + var(--space-4)) var(--space-6);
	overflow: hidden;
	gap: var(--space-6);
}

.desktop-canvas-area {
	flex: 1 1 auto;
	height: 100%;
	position: relative;
	overflow: hidden;
	min-width: 0;
}

.desktop-sidebar-area {
	flex: 0 0 auto;
	height: 100%;
	overflow: visible;
	z-index: 10;

	@media screen and (max-width: 480px) {
		display: none;
	}
}
</style>
