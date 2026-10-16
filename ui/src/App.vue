<template>
	<div id="app" :class="{'is-dark-bg':$route.meta.showBackground}" class="is-flex is-flex-direction-column" :style="{'--vh': vh}" >
		<template v-if="$route.meta.showBackground">
			<!-- Background Layer Start -->
			<casa-wallpaper :animate="isWelcome?initAni:noneAni"></casa-wallpaper>
			<!-- Background Layer End -->

		</template>

		<div v-if="$store.state.serverDown" class="reconnect-banner" role="status">{{ $t('Reconnecting to your server…') }}</div>

		<!-- Router View Start -->
		<router-view/>
		<!-- Router View End -->

		<!-- Desktop windows (Files, Settings, Terminal, ...) - Home only,
		not login/welcome (those also set showBackground for the wallpaper) -->
		<window-manager v-if="$route.meta.showWindows"></window-manager>

	</div>
</template>

<script>
import CasaWallpaper from '@/shell/wallpaper/CasaWallpaper.vue'
import { defineAsyncComponent } from 'vue'
import {mixin}       from './mixins/mixin';
import { initTheme, applyTheme, THEME_MODES } from './utils/theme';
import { iconPacks } from '@/ui/icons';


export default {
	components: {
		CasaWallpaper,
		// Home only: the desktop shell loads with it, not on the login screen.
		WindowManager: defineAsyncComponent(() => import('@/shell/desktop/WindowManager.vue'))
	},
	mixins: [mixin],
	data() {
		return {
			//isLoading: true,
			steps: [],
			noneAni: {
				classes: 'fadeIn',
				duration: 500
			},
			initAni: {
				classes: 'zoomOutIn',
				duration: 2500
			},
			"vh": "0px"
		}
	},


	computed: {
		isLoading() {
			return this.$store.state.siteLoading
		},
		isWelcome() {
			return this.$store.state.needInitialization
		}
	},

	created() {
		initTheme();
		console.log(`%c
_____             _____ _____
|     |___ ___ ___|     |   __|
|   --| .'|_ -| .'|  |  |__   |
|_____|__,|___|__,|_____|_____|
-- Originally made by IceWhale, forked as NivaroOS by Ayush --
`, `font-family: monospace`);

		this.$buefy.config.setOptions({ customIconPacks: iconPacks })
	},
	mounted() {
		this.setInitLang();
		window.addEventListener('resize', this.onWindowResize);
		this.onWindowResize();
		// A capability check, not a viewport-size one - unlike isMobile/
		// isTablet this never changes after load, so it's set once here
		// rather than inside the resize handler.
		this.$store.commit('SET_IS_TOUCH_DEVICE', 'ontouchstart' in window || navigator.maxTouchPoints > 0);
		let vh = window.innerHeight * 0.01;
		this["vh"] = `${vh}px`;

		const alpha = localStorage.getItem('uiBackdropAlpha');
		const blur = localStorage.getItem('uiBackdropBlur');
		if (alpha !== null) document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha);
		if (blur !== null) document.documentElement.style.setProperty('--ui-backdrop-blur', `${blur}px`);

		this.$api.users.getUserStatus().then(res => {
			if (res.data.success === 200 && res.data.data) {
				const data = res.data.data
				this.$store.commit('SET_PUBLIC_WALLPAPER', data.wallpaper)
				if (data.wallpaper && data.wallpaper.path) {
					this.$store.commit('SET_WALLPAPER', data.wallpaper)
				}
				if (data.appearance) {
					if (data.appearance.alpha !== undefined && data.appearance.alpha !== null) {
						document.documentElement.style.setProperty('--ui-backdrop-alpha', data.appearance.alpha)
						localStorage.setItem('uiBackdropAlpha', data.appearance.alpha)
					}
					if (data.appearance.blur !== undefined && data.appearance.blur !== null) {
						document.documentElement.style.setProperty('--ui-backdrop-blur', `${data.appearance.blur}px`)
						localStorage.setItem('uiBackdropBlur', data.appearance.blur)
					}
					if (data.appearance.theme && Object.values(THEME_MODES).includes(data.appearance.theme)) {
						applyTheme(data.appearance.theme)
					}
				}
			}
		}).catch(() => {})
	},
	methods: {
		/**
		 * @description: Get and Set default language
		 * @return {*} void
		 */
		setInitLang() {
			let lang = localStorage.getItem('lang') ? localStorage.getItem('lang') : this.getLangFromBrowser()
			lang = lang.includes("_") ? lang : "en_us";
			this.setLang(lang);
		},
		/**
		 * @description: Handle on Window reize
		 * @return {*}
		 */
		onWindowResize() {
			const width = document.body.clientWidth
			// isMobile's own threshold/consumers (the App Store's own
			// mobile layout - AppPanel/AppSection/Dropdown) are untouched;
			// isTablet is a second, wider tier only the desktop shell's
			// window-sizing (see OPEN_WINDOW) reacts to.
			this.$store.commit('SET_IS_MOBILE', width < 480)
			this.$store.commit('SET_IS_TABLET', width >= 480 && width < 1024)
		},
	},
	sockets: {
		connect() {
			console.log('socket connected');
		},

	},
}
</script>

<style lang="scss" scoped>
#app {
	width: 100vw;
	height: 100dvh;
	font-weight: 400;
	font-size: 0.875rem;
	-webkit-font-smoothing: antialiased;
	-moz-osx-font-smoothing: grayscale;
	color: var(--theme-text-primary, #2c3e50);
	overflow-y: hidden;

	&.is-dark-bg {
		background-color: #000;
	}
}

.reconnect-banner {
	position: fixed;
	top: var(--space-3, 0.75rem);
	left: 50%;
	transform: translateX(-50%);
	z-index: 10000;
	max-width: calc(100vw - 2rem);
	padding: var(--space-2, 0.5rem) var(--space-4, 1rem);
	border-radius: var(--radius-pill, 999px);
	background: var(--theme-desktop-glass-bg, rgba(15, 23, 42, 0.85));
	color: var(--theme-desktop-glass-text, #fff);
	box-shadow: var(--theme-desktop-glass-shadow);
	backdrop-filter: blur(10px);
	pointer-events: none;
}
</style>

<!-- Unscoped: html isn't part of this component's own rendered tree,
so a scoped rule targeting it would never match anything. Almost all
spacing/typography here is rem-based, so scaling the root font-size
shrinks it proportionally everywhere - unlike CSS zoom (tried once
already), this doesn't change the CSS pixel scale factor pointer
events are measured against, so click coordinates stay correct. -->
<style>
html.is-compact-view {
	font-size: 85%;
}

/* Window/panel backdrop transparency + blur - adjustable from Settings >
Appearance (see AppearanceSection.vue's restoreBackdropSettings/
resetToDefaults, which these two values must match). 0.9 alpha is
mostly-opaque "glass" rather than fully solid - lower it there for more
see-through, or raise to 1 for fully opaque. */
:root {
	--ui-backdrop-alpha: 0.9;
	--ui-backdrop-blur: 10px;
}

</style>
