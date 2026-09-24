<!-- A status as icon + text + colour (spec §13: never colour alone). -->
<template>
	<span class="bk-pill" :class="'is-' + tone">
		<b-icon :icon="icon" pack="mdi" custom-size="mdi-14px" aria-hidden="true"></b-icon>
		<span>{{ label }}</span>
	</span>
</template>

<script>
export default {
	name: 'StatusPill',
	props: {
		tone: { type: String, default: 'muted' },
		icon: { type: String, required: true },
		label: { type: String, required: true }
	}
}
</script>
