<!-- src/views/HostDesktopStandalone.vue -->
<!--
	Standalone full-tab view for Host Desktop Console.
	Opened via window.open() into its own browser tab so users can use
	the host PC / server desktop on a dedicated monitor, full window, or
	separate browser tab without being constrained to the desktop shell window.
-->
<template>
	<div class="host-desktop-standalone-page">
		<host-desktop-panel :show-close="true" @close="closeTab"></host-desktop-panel>
	</div>
</template>

<script>
import HostDesktopPanel from '@/components/desktop/HostDesktopPanel.vue'

export default {
	name: 'host-desktop-standalone',
	components: { HostDesktopPanel },
	mounted() {
		document.title = `${this.$t('Host Desktop')} - NivaroOS`
	},
	methods: {
		closeTab() {
			window.close()
		},
	},
}
</script>

<style lang="scss" scoped>
.host-desktop-standalone-page {
	position: fixed;
	inset: 0;
	width: 100vw;
	height: 100vh;
	overflow: hidden;
	font-family: $family-sans-serif;
	background: #000000;
}
</style>
