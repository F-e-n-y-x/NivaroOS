<!-- src/views/VmConsoleStandalone.vue -->
<!--
	Full-page wrapper for VmConsolePanel, opened via window.open() into
	its own browser tab (VmList's "Open in New Tab" action). A real
	remote console lives in its own tab in every VM app this was modeled
	after (VMware Workstation, VirtualBox) - useful for a second monitor,
	or just not losing the console when the NivaroOS desktop tab is closed.
-->
<template>
	<div class="vm-console-page">
		<vm-console-panel :vm-name="name" :show-close="true" @close="closeTab"></vm-console-panel>
	</div>
</template>

<script>
import VmConsolePanel from '@/components/desktop/vm/VmConsolePanel.vue'

export default {
	name: 'vm-console-standalone',
	components: { VmConsolePanel },
	props: {
		name: { type: String, required: true },
	},
	mounted() {
		document.title = `${this.name} - ${this.$t('VM Console')}`
	},
	methods: {
		closeTab() {
			window.close()
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-console-page {
	position: fixed;
	inset: 0;
	font-family: $family-sans-serif;
}
</style>
