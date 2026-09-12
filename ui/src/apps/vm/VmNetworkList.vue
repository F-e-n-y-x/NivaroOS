<!-- src/apps/vm/VmNetworkList.vue -->
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
				<b-icon :icon="net.mode === 'bridge' ? 'lan-connect' : 'lan'" :custom-size="net.mode === 'bridge' ? 'mdi-18px' : 'mdi-22px'"></b-icon>
			</div>
			<div class="vm-net-controls">
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
					>Bridge: {{ bridge.name }}</button>
					<button
						v-if="!bridgeNetworks || !bridgeNetworks.length"
						type="button"
						class="segmented-option"
						:class="{ active: net.mode === 'bridge' }"
						@click="setBridge(i, net.bridge_name || 'br0')"
					>{{ $t('Bridge') }} ({{ net.bridge_name || 'br0' }})</button>
				</div>
				<div class="segmented-control vm-net-model" :title="$t('Network adapter emulation model')">
					<button
						type="button"
						class="segmented-option"
						:class="{ active: !net.model || net.model === 'virtio' }"
						@click="setField(i, 'model', 'virtio')"
					>VirtIO</button>
					<button
						type="button"
						class="segmented-option"
						:class="{ active: net.model === 'e1000e' }"
						@click="setField(i, 'model', 'e1000e')"
					>e1000e</button>
					<button
						type="button"
						class="segmented-option"
						:class="{ active: net.model === 'e1000' }"
						@click="setField(i, 'model', 'e1000')"
					>e1000</button>
					<button
						type="button"
						class="segmented-option"
						:class="{ active: net.model === 'rtl8139' }"
						@click="setField(i, 'model', 'rtl8139')"
					>RTL8139</button>
				</div>
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
			this.$emit('input', [...this.networks, { mode: 'nat', model: 'virtio' }])
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
	gap: var(--space-3);
}
.vm-net-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);
	box-shadow: var(--shadow-sm);
}
.vm-net-controls {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--space-2);
}
.vm-net-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	&.is-bridge {
		background: rgba(59, 130, 246, 0.1);
		color: #2563eb;
	}
}
.vm-net-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 2rem;
	height: 2rem;
	border-radius: var(--radius-control);
	transition: background 0.12s ease, color 0.12s ease;

	&:hover {
		color: #dc2626;
		background: rgba(239, 68, 68, 0.1);
	}
}
.vm-net-mode, .vm-net-model {
	flex-wrap: wrap;
	margin-bottom: 0;
}
.vm-net-add {
	align-self: flex-start;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	border: 1px dashed rgba(37, 99, 235, 0.4);
	border-radius: var(--radius-control);
	background: transparent;
	color: #2563eb;
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 500;
	padding: var(--space-2) var(--space-3);
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: rgba(37, 99, 235, 0.08);
	}
}
.vm-net-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}
</style>
