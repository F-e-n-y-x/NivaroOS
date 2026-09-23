<!--
	A plain desktop window whose content is supplied by another component
	(SettingsOverlay moves its dialog content in here). Dialogs used to be
	full-screen overlays that blurred and blocked the whole desktop; as a
	window they move, stack and sit beside everything else.
-->
<template>
	<div :id="'portal-' + portalId" class="portal-window scrollbars-light"></div>
</template>

<script>
export default {
	name: 'PortalWindow',
	props: {
		portalId: { type: String, required: true }
	}
}
</script>

<style scoped>
.portal-window {
	height: 100%;
	overflow: auto;
	display: flex;
	flex-direction: column;
	background: var(--theme-bg-window-opaque, #fff);
}
</style>
