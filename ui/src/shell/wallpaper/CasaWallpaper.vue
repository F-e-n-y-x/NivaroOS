<template>
	<div>
		<div id="background" v-animate-css="animate" :style="backgroundStyleObj"></div>
		<context-menu></context-menu>
	</div>

</template>

<script>
import ContextMenu from './ContextMenu.vue'

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
			isWelcome: false
		}
	},
	computed: {
		wallpaperPath() {
			return (this.$store.state.wallpaperObject && this.$store.state.wallpaperObject.path) || localStorage.getItem("wallpaper") || require('@/assets/background/wallpaper01.jpg')
		},
		backgroundStyleObj() {
			const path = this.wallpaperPath
			return {
				backgroundImage: `url("${this.parseUrl(path)}")`
			}
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
			// Gallery, uploaded, or server-hosted files use the unauthenticated public wallpaper endpoint
			if (serverUrl.includes('/v3/file') || serverUrl.includes('/DATA/') || serverUrl.includes('/var/lib/') || serverUrl.includes('/users/image') || serverUrl.includes('/v1/users/wallpaper')) {
				return `${this.$protocol}//${this.$baseURL}/v1/users/wallpaper?t=${Date.now()}`;
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
	transition: background-image 0.3s ease;
}
</style>
