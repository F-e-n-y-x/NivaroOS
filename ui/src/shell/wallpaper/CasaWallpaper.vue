<template>
	<div>
		<div id="background" v-animate-css="animate" :style="backgroundStyleObj"></div>
		<context-menu></context-menu>
	</div>
</template>

<script>
// require() of an image can give { default: url }; callers need the URL.
const __assetUrl = (m) => (m && typeof m === 'object' && m.default) || m
import ContextMenu from './ContextMenu.vue'
import { getEffectiveTheme, getStoredThemeMode } from '@/utils/theme'

const DEFAULT_LIGHT_WALLPAPER = __assetUrl(require('@/assets/background/wallpaper01.jpg'))
const DEFAULT_DARK_WALLPAPER = __assetUrl(require('@/assets/background/wallpaper02.jpg'))

export default {
	name: "casa-background",
	components: {
		ContextMenu,
	},
	props: {
		animate: {
			type: Object,
			default: null
		},
	},
	data() {
		return {
			isWelcome: false,
			effectiveTheme: getEffectiveTheme(getStoredThemeMode()),
			mql: null
		}
	},
	computed: {
		isDualMode() {
			const obj = this.$store.state.wallpaperObject
			if (obj && obj.dualMode !== undefined) {
				return Boolean(obj.dualMode)
			}
			const stored = localStorage.getItem('wallpaper_dual_mode')
			return stored !== 'false' // default to true
		},
		wallpaperPath() {
			const obj = this.$store.state.wallpaperObject || {}
			const currentTheme = this.effectiveTheme || getEffectiveTheme(getStoredThemeMode())
			if (this.isDualMode) {
				if (currentTheme === 'light') {
					return (obj.light && obj.light.path) || localStorage.getItem('wallpaper_light') || DEFAULT_LIGHT_WALLPAPER
				} else {
					return (obj.dark && obj.dark.path) || localStorage.getItem('wallpaper_dark') || DEFAULT_DARK_WALLPAPER
				}
			}
			return obj.path || localStorage.getItem('wallpaper') || (currentTheme === 'light' ? DEFAULT_LIGHT_WALLPAPER : DEFAULT_DARK_WALLPAPER)
		},
		backgroundStyleObj() {
			const path = this.wallpaperPath
			return {
				backgroundImage: `url("${this.parseUrl(path)}")`
			}
		}
	},
	watch: {
		'$store.state.wallpaperObject': {
			deep: true,
			handler() {
				this.$forceUpdate()
			}
		}
	},
	mounted() {
		this.onThemeChange = (e) => {
			if (e && e.detail && e.detail.effective) {
				this.effectiveTheme = e.detail.effective
			} else {
				this.effectiveTheme = getEffectiveTheme(getStoredThemeMode())
			}
			this.$forceUpdate()
		}
		window.addEventListener('nivaroos:theme-change', this.onThemeChange)

		if (typeof window !== 'undefined' && window.matchMedia) {
			this.mql = window.matchMedia('(prefers-color-scheme: dark)')
			this.onMqlChange = () => {
				const mode = getStoredThemeMode()
				if (mode === 'auto') {
					this.effectiveTheme = getEffectiveTheme('auto')
					this.$forceUpdate()
				}
			}
			if (this.mql.addEventListener) {
				this.mql.addEventListener('change', this.onMqlChange)
			} else if (this.mql.addListener) {
				this.mql.addListener(this.onMqlChange)
			}
		}

		this.onWallpaperChange = () => {
			this.$forceUpdate()
		}
		this.$EventBus.$on('desktop:wallpaper-change', this.onWallpaperChange)
	},
	beforeDestroy() {
		if (this.onThemeChange) {
			window.removeEventListener('nivaroos:theme-change', this.onThemeChange)
		}
		if (this.mql && this.onMqlChange) {
			if (this.mql.removeEventListener) {
				this.mql.removeEventListener('change', this.onMqlChange)
			} else if (this.mql.removeListener) {
				this.mql.removeListener(this.onMqlChange)
			}
		}
		if (this.onWallpaperChange) {
			this.$EventBus.$off('desktop:wallpaper-change', this.onWallpaperChange)
		}
	},
	methods: {
		parseUrl(serverUrl) {
			if (!serverUrl) return '';
			if (serverUrl.startsWith('data:') || serverUrl.startsWith('blob:')) {
				return serverUrl;
			}
			if (serverUrl.startsWith('http://') || serverUrl.startsWith('https://')) {
				return serverUrl;
			}
			// Built-in assets bundled in the UI
			if (serverUrl.startsWith('/img/') || serverUrl.startsWith('img/') || serverUrl.startsWith('./') || serverUrl.startsWith('assets/')) {
				return serverUrl;
			}
			// Local, gallery, or mounted storage files
			if (serverUrl.includes('/DATA/') || serverUrl.includes('/var/lib/') || serverUrl.includes('/mnt/')) {
				const token = this.$store.state.access_token || localStorage.getItem('access_token') || ''
				return `${this.$protocol}//${this.$baseURL}/v1/image?path=${encodeURIComponent(serverUrl)}${token ? '&token=' + token : ''}`;
			}
			// Public wallpaper endpoint with theme context
			if (serverUrl.includes('/v1/users/wallpaper')) {
				return `${this.$protocol}//${this.$baseURL}/v1/users/wallpaper?theme=${this.effectiveTheme}&t=${Date.now()}`;
			}
			let newUrl = serverUrl.replace('SERVER_URL', `${this.$protocol}//${this.$baseURL}`);
			newUrl = newUrl.replace('/ui', '').replace('/user/', '/users/');
			return newUrl;
		},
	},
}
</script>

<style lang="scss">
#background {
	position: fixed;
	z-index: 0;
	width: 100%;
	height: 100%;
	background-size: cover;
	background-repeat: no-repeat;
	background-position: center center;
	overflow: hidden;
	transition: background-image 0.4s cubic-bezier(0.16, 1, 0.3, 1);
}
</style>
