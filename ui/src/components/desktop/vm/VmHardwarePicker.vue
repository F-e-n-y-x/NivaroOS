<!-- src/components/desktop/vm/VmHardwarePicker.vue -->
<!--
	USB and PCI host-device passthrough picker, shared by CreateVmModal and
	EditVmModal. PCI passthrough needs IOMMU (VT-d/AMD-Vi) enabled on the
	host - when it isn't, the PCI section explains why instead of offering
	a picker that would just fail at VM start.
-->
<template>
	<div class="vm-hw-picker">
		<div class="vm-hw-section">
			<h3 class="setting-card-title">{{ $t('USB Devices') }}</h3>
			<p v-if="loading" class="vm-hw-hint">{{ $t('Loading host devices...') }}</p>
			<p v-else-if="!usbDevices.length" class="vm-hw-hint">{{ $t('No USB devices found on the host.') }}</p>
			<label v-for="dev in usbDevices" :key="dev.vendor_id + ':' + dev.product_id" class="vm-hw-row" :class="{ active: isUsbSelected(dev) }">
				<div class="vm-hw-icon" :class="{ active: isUsbSelected(dev) }">
					<b-icon icon="usb" custom-size="mdi-20px"></b-icon>
				</div>
				<div class="vm-hw-main">
					<span class="vm-hw-desc">{{ dev.description || (dev.vendor_id + ':' + dev.product_id) }}</span>
					<span class="vm-hw-id">{{ dev.vendor_id }}:{{ dev.product_id }}</span>
				</div>
				<input type="checkbox" class="vm-hw-checkbox" :checked="isUsbSelected(dev)" @change="toggleUsb(dev, $event.target.checked)" />
			</label>
		</div>

		<div class="vm-hw-section">
			<h3 class="setting-card-title">{{ $t('PCI Devices') }}</h3>
			<p v-if="!iommuEnabled" class="vm-hw-warning">
				{{ $t('PCI passthrough needs IOMMU (VT-d/AMD-Vi) enabled on the host - it isn\'t right now, so this is unavailable.') }}
			</p>
			<template v-else>
				<p v-if="!pciDevices.length" class="vm-hw-hint">{{ $t('No PCI devices found on the host.') }}</p>
				<label v-for="dev in pciDevices" :key="dev.address" class="vm-hw-row" :class="{ active: isPciSelected(dev) }">
					<div class="vm-hw-icon" :class="{ active: isPciSelected(dev) }">
						<b-icon icon="expansion-card" custom-size="mdi-20px"></b-icon>
					</div>
					<div class="vm-hw-main">
						<span class="vm-hw-desc">{{ dev.description }}</span>
						<span class="vm-hw-id">{{ dev.address }}</span>
					</div>
					<input type="checkbox" class="vm-hw-checkbox" :checked="isPciSelected(dev)" @change="togglePci(dev, $event.target.checked)" />
				</label>
			</template>
		</div>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'

export default {
	name: 'vm-hardware-picker',
	props: {
		usbValue: { type: Array, default: () => [] },
		pciValue: { type: Array, default: () => [] },
	},
	data() {
		return {
			loading: true,
			usbDevices: [],
			pciDevices: [],
			iommuEnabled: false,
		}
	},
	created() {
		this.load()
	},
	methods: {
		async load() {
			try {
				const caps = await vmSidecar.getHostCapabilities()
				this.usbDevices = caps.usb_devices || []
				this.pciDevices = caps.pci_devices || []
				this.iommuEnabled = !!caps.iommu_enabled
			} catch (e) {
				this.usbDevices = []
				this.pciDevices = []
			} finally {
				this.loading = false
			}
		},
		isUsbSelected(dev) {
			return this.usbValue.some((d) => d.vendor_id === dev.vendor_id && d.product_id === dev.product_id)
		},
		isPciSelected(dev) {
			return this.pciValue.some((d) => d.address === dev.address)
		},
		toggleUsb(dev, checked) {
			if (checked) {
				this.$emit('update:usbValue', [...this.usbValue, { vendor_id: dev.vendor_id, product_id: dev.product_id }])
			} else {
				this.$emit('update:usbValue', this.usbValue.filter((d) => !(d.vendor_id === dev.vendor_id && d.product_id === dev.product_id)))
			}
		},
		togglePci(dev, checked) {
			if (checked) {
				this.$emit('update:pciValue', [...this.pciValue, { address: dev.address }])
			} else {
				this.$emit('update:pciValue', this.pciValue.filter((d) => d.address !== dev.address))
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-hw-picker {
	display: flex;
	flex-direction: column;
	gap: 1.25rem;
}
.vm-hw-section {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;

	// The parent (EditVmModal/CreateVmModal) zeroes .setting-card-title's
	// own padding for its OWN direct children via a scoped rule that can't
	// reach into this component's template - repeating it here keeps this
	// heading's spacing identical to the Disks/Network Adapters ones
	// beside it instead of falling back to padding meant for a heading
	// that's the first thing inside a .setting-card.
	> .setting-card-title {
		padding: 0;
	}
}
.vm-hw-hint {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
}
.vm-hw-warning {
	font-size: 0.78rem;
	color: #b5651d;
	background: rgba(181, 101, 29, 0.08);
	border-radius: 8px;
	padding: 0.6rem 0.75rem;
}
// Same icon-badge row treatment as VmDiskList's .vm-disk-row / VmNetworkList's
// .vm-net-row - a passthrough device is a selectable row, not an editable
// one, so the trailing control is a checkbox instead of remove/options.
.vm-hw-row {
	display: flex;
	align-items: center;
	gap: 0.85rem;
	padding: 0.65rem 0.85rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
	color: rgba(0, 0, 0, 0.6);
	cursor: pointer;

	&:hover {
		border-color: rgba(50, 115, 220, 0.4);
	}
	&.active {
		border-color: rgba(50, 115, 220, 0.35);
		background: rgba(50, 115, 220, 0.04);
	}
}
.vm-hw-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.04);
	color: rgba(0, 0, 0, 0.4);

	&.active {
		background: rgba(50, 115, 220, 0.1);
		color: #3273dc;
	}
}
.vm-hw-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}
.vm-hw-desc {
	font-size: 0.82rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.vm-hw-id {
	font-size: 0.7rem;
	color: rgba(0, 0, 0, 0.4);
	font-family: monospace;
}
.vm-hw-checkbox {
	flex-shrink: 0;
	width: 1.1rem;
	height: 1.1rem;
	cursor: pointer;
}
</style>
