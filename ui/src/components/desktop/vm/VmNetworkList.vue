<!-- src/components/desktop/vm/VmNetworkList.vue -->
<!--
	Shared network-adapter list editor for CreateVmModal and EditVmModal -
	a VM can have zero NICs (fully isolated) or several, each independently
	NAT or bridged - matching VMware Workstation/VirtualBox's per-adapter
	settings rather than one network choice for the whole VM.
-->
<template>
	<div class="vm-net-list">
		<div v-for="(net, i) in networks" :key="i" class="vm-net-row">
			<div class="vm-net-icon" :class="{ 'is-bridge': net.mode === 'bridge' }">
				<b-icon :icon="net.mode === 'bridge' ? 'lan-connect' : 'lan'" custom-size="mdi-22px"></b-icon>
			</div>
			<div class="segmented-control vm-net-mode">
				<button
					type="button"
					class="segmented-option"
					:class="{ active: net.mode === 'nat' }"
					@click="setField(i, 'mode', 'nat')"
				>{{ $t('NAT') }}</button>
				<button
					v-for="bridge in bridgeNetworks"
					:key="bridge.name"
					type="button"
					class="segmented-option"
					:class="{ active: net.mode === 'bridge' && net.bridge_name === bridge.name }"
					@click="setBridge(i, bridge.name)"
				>{{ bridge.name }}</button>
			</div>
			<button type="button" class="vm-net-remove" :title="$t('Remove')" @click="removeNet(i)">
				<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
			</button>
		</div>

		<button v-if="allowAdd" type="button" class="vm-net-add" @click="addNet">
			<b-icon icon="plus" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Add Network Adapter') }}</span>
		</button>

		<p v-if="!networks.length" class="vm-net-hint">{{ $t('No network adapters - the VM will be fully isolated.') }}</p>
	</div>
</template>

<script>
export default {
	name: 'vm-network-list',
	props: {
		value: { type: Array, default: () => [] },
		bridgeNetworks: { type: Array, default: () => [] },
		// Basic mode hides the ability to add further adapters - a single
		// NAT/bridge choice is all that mode edits.
		allowAdd: { type: Boolean, default: true },
	},
	computed: {
		networks() {
			return this.value
		},
	},
	methods: {
		setField(index, field, val) {
			this.$emit('input', this.networks.map((n, i) => (i === index ? { ...n, [field]: val } : n)))
		},
		setBridge(index, bridgeName) {
			this.$emit('input', this.networks.map((n, i) => (i === index ? { ...n, mode: 'bridge', bridge_name: bridgeName } : n)))
		},
		addNet() {
			this.$emit('input', [...this.networks, { mode: 'nat' }])
		},
		removeNet(index) {
			this.$emit('input', this.networks.filter((_, i) => i !== index))
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-net-list {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}
.vm-net-row {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: 0.65rem;
	padding: 0.65rem 0.85rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
	color: rgba(0, 0, 0, 0.6);
}
// Matches VmDiskList.vue's .vm-disk-icon / VmStorage.vue's .iso-icon -
// the same round icon-badge treatment used everywhere else in this app's
// list rows.
.vm-net-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.04);
	color: rgba(0, 0, 0, 0.4);

	&.is-bridge {
		background: rgba(50, 115, 220, 0.1);
		color: #3273dc;
	}
}
// .segmented-control (see common/_settings.scss, global) has its own
// bottom margin meant for standalone use as a tab bar - inline here it
// just needs to sit level with the row's icon/remove button, and wrap
// (rather than overflow) once there are several bridges to choose from.
.vm-net-mode {
	flex: 1 1 auto;
	flex-wrap: wrap;
	margin-bottom: 0;
}
.vm-net-remove {
	border: none;
	background: transparent;
	color: rgba(0, 0, 0, 0.35);
	cursor: pointer;
	display: flex;
	align-items: center;
	flex-shrink: 0;
	padding: 0.35rem;
	border-radius: 6px;

	&:hover {
		background: rgba(242, 83, 74, 0.08);
		color: #f2534a;
	}
}
.vm-net-add {
	align-self: flex-start;
	display: flex;
	align-items: center;
	gap: 0.35rem;
	border: 1px dashed rgb(200 207 214);
	border-radius: 8px;
	background: transparent;
	color: #3273dc;
	font-family: inherit;
	font-size: 0.8rem;
	font-weight: 600;
	padding: 0.4rem 0.75rem;
	cursor: pointer;

	&:hover {
		background: rgba(50, 115, 220, 0.06);
	}
}
.vm-net-hint {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
}
</style>
