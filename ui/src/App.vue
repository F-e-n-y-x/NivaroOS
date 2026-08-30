<template>
	<div id="app" :class="{'is-dark-bg':$route.meta.showBackground}" class="is-flex is-flex-direction-column" :style="{'--vh': vh}" >
		<template v-if="$route.meta.showBackground">
			<!-- Background Layer Start -->
			<casa-wallpaper :animate="isWelcome?initAni:noneAni"></casa-wallpaper>
			<!-- Background Layer End -->

		</template>

		<!-- Router View Start -->
		<router-view/>
		<!-- Router View End -->

		<!-- Desktop windows (Files, Settings, Terminal, ...) - Home only,
		not login/welcome (those also set showBackground for the wallpaper) -->
		<window-manager v-if="$route.meta.showWindows"></window-manager>

	</div>
</template>

<script>
import CasaWallpaper from './components/wallpaper/CasaWallpaper.vue'
import WindowManager from './components/desktop/WindowManager.vue'
import {mixin}       from './mixins/mixin';

const customIconConfig = {
	customIconPacks: {
		'casa': {
			sizes: {
				'default': 'is-size-4',
				'is-20': 'is-size-5',
				'is-small': '',
				'is-medium': 'is-size-3',
				'is-large': 'is-size-1'
			},
			iconPrefix: 'casa-',
			internalIcons: {
				'check': 'checkmark',
				'information': 'information',
				'check-circle': 'checkmark-circle-outline',
				'alert': 'alert',
				'alert-circle': 'alert',
				'arrow-up': 'arrow-up',
				'chevron-right': 'arrow-right',
				'chevron-left': 'arrow-back',
				'chevron-down': 'arrow-down',
				'eye': 'eye',
				'eye-off': 'eye-off',
				'menu-down': 'arrow-dropdown',
				'menu-up': 'arrow-dropup',
				'close-circle': 'close-circle-outline'
			}
		},
	}
}

export default {
	components: {
		CasaWallpaper,
		WindowManager
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
		console.log(`%c
_____             _____ _____
|     |___ ___ ___|     |   __|
|   --| .'|_ -| .'|  |  |__   |
|_____|__,|___|__,|_____|_____|
-- Originally made by IceWhale, forked as Recasa by Ayush --
`, `font-family: monospace`);

		this.$buefy.config.setOptions(customIconConfig)
	},
	mounted() {
		this.setInitLang();
		window.addEventListener('resize', this.onWindowResize);
		this.onWindowResize();
		let vh = window.innerHeight * 0.01;
		this["vh"] = `${vh}px`;

		const alpha = localStorage.getItem('uiBackdropAlpha');
		const blur = localStorage.getItem('uiBackdropBlur');
		if (alpha !== null) document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha);
		if (blur !== null) document.documentElement.style.setProperty('--ui-backdrop-blur', `${blur}px`);
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
			const isMobile = document.body.clientWidth < 480
			this.$store.commit('SET_IS_MOBILE', isMobile)
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
	color: #2c3e50;
	overflow-y: hidden;

	&.is-dark-bg {
		background-color: #000;
	}
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
Appearance (see SettingsApp.vue's applyBackdrop). Windows are fully
opaque/white by default (no see-through glass) - lower the alpha
slider there if some transparency is wanted instead. */
:root {
	--ui-backdrop-alpha: 1;
	--ui-backdrop-blur: 0px;
}

</style>
