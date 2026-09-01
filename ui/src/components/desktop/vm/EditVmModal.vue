<!-- src/components/desktop/vm/EditVmModal.vue -->
<!--
	Editing has far fewer decisions than creating (no name, no OS
	template, no fresh disk to provision) - a single form fits comfortably
	without needing CreateVmModal's step-by-step wizard treatment.
-->
<template>
	<div class="edit-vm-window">
		<div class="edit-vm-body">
		<template v-if="vm">
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="chip" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('vCPUs') }}</div>
					<div class="row-control slider-control">
						<span class="slider-hint">1</span>
						<input class="pretty-range" v-model.number="form.vcpus" type="range" min="1" max="32" step="1"
							:style="rangeStyle(form.vcpus, 1, 32)" />
						<span class="slider-hint">32</span>
						<input type="number" class="slider-value-input" v-model.number="form.vcpus" min="1" max="64" @change="clampVcpus" />
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="memory" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Memory') }}</div>
					<div class="row-control slider-control">
						<span class="slider-hint">512 MB</span>
						<input class="pretty-range" v-model.number="form.memory_mib" type="range" min="512" max="65536" step="512"
							:style="rangeStyle(form.memory_mib, 512, 65536)" />
						<span class="slider-hint">64 GB</span>
						<input type="number" class="slider-value-input" v-model.number="memoryGiB" min="0.5" max="128" step="0.5" />
						<span class="slider-value-unit">{{ $t('GB') }}</span>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="chip" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Firmware') }}</div>
					<div class="row-control">
						<div class="segmented-control">
							<button type="button" class="segmented-option" :class="{ active: form.firmware === 'bios' }" @click="form.firmware = 'bios'">{{ $t('BIOS') }}</button>
							<button type="button" class="segmented-option" :class="{ active: form.firmware === 'uefi' }" @click="form.firmware = 'uefi'">{{ $t('UEFI') }}</button>
						</div>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="monitor" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Display Resolution') }}</div>
					<div class="row-control">
						<b-select v-model="displayResolution" size="is-small" expanded class="display-res-select">
							<option value="">{{ $t('Default (let the guest decide)') }}</option>
							<option value="1920x1080">1920 × 1080</option>
							<option value="1280x720">1280 × 720</option>
							<option value="1024x768">1024 × 768</option>
							<option value="800x600">800 × 600</option>
						</b-select>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="disc" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Boot ISO') }}</div>
					<div class="row-control">
						<button type="button" class="iso-picker" @click="showIsoPicker = true">
							<span class="iso-picker-name" :class="{ 'is-empty': !form.iso_path }">{{ isoFileName(form.iso_path) || $t('None (boot from disk)') }}</span>
							<span v-if="form.iso_path" class="iso-picker-clear" :title="$t('Clear')" @click.stop="form.iso_path = ''">
								<b-icon icon="close" size="is-small"></b-icon>
							</span>
							<b-icon v-else icon="chevron-right" size="is-small" class="iso-picker-chevron"></b-icon>
						</button>
					</div>
				</div>
			</div>
			<vm-file-picker-dialog
				:active="showIsoPicker"
				:title="$t('ISO')"
				start-path="/DATA/VMs/isos"
				:extensions="['iso', 'img']"
				@selected="form.iso_path = $event"
				@close="showIsoPicker = false"
			></vm-file-picker-dialog>
			<div class="edit-section">
				<h3 class="setting-card-title">{{ $t('Disks') }}</h3>
				<vm-disk-list v-model="form.disks" :existing="existingDisks"></vm-disk-list>
			</div>
			<div class="edit-section">
				<h3 class="setting-card-title">{{ $t('Network Adapters') }}</h3>
				<vm-network-list v-model="form.networks" :bridge-networks="bridgeNetworks"></vm-network-list>
			</div>
			<vm-hardware-picker
				:usb-value="form.usb_devices"
				:pci-value="form.pci_devices"
				@update:usbValue="form.usb_devices = $event"
				@update:pciValue="form.pci_devices = $event"
			></vm-hardware-picker>
			<b-message v-if="error" type="is-danger" :closable="false">{{ error }}</b-message>
		</template>
		</div>

		<footer class="window-foot">
			<b-button @click="close">{{ $t('Cancel') }}</b-button>
			<b-button type="is-primary" :loading="saving" @click="submit">{{ $t('Save') }}</b-button>
		</footer>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmFilePickerDialog from './VmFilePickerDialog.vue'
import VmDiskList from './VmDiskList.vue'
import VmNetworkList from './VmNetworkList.vue'
import VmHardwarePicker from './VmHardwarePicker.vue'

export default {
	name: 'edit-vm-modal',
	components: { VmFilePickerDialog, VmDiskList, VmNetworkList, VmHardwarePicker },
	props: {
		// The VM (from VmList's poll) being edited - only its identity/
		// current values matter here, re-read fresh from the sidecar on
		// open rather than trusting this prop's possibly-stale snapshot.
		vm: { type: Object, default: null },
	},
	data() {
		return {
			form: { vcpus: 1, memory_mib: 512, iso_path: '', firmware: 'bios', display_width: 0, display_height: 0, disks: [], networks: [], usb_devices: [], pci_devices: [] },
			// The VM's disks as they were when loaded - VmDiskList uses this
			// to floor each existing disk's size at its current GiB (grow
			// only) and lock its bus/SSD, since those can't change on an
			// already-attached disk without recreating it.
			existingDisks: [],
			networks: [],
			showIsoPicker: false,
			saving: false,
			error: null,
		}
	},
	computed: {
		bridgeNetworks() {
			return this.networks.filter((n) => n.mode === 'bridge')
		},
		// The slider steps in raw MiB (matching form.memory_mib directly),
		// but typing an exact value is far more natural in GB - this
		// converts both ways, snapping back to the slider's own 512MiB
		// step so the two controls never disagree with each other.
		memoryGiB: {
			get() {
				return this.form.memory_mib / 1024
			},
			set(gib) {
				const mib = Math.round((Number(gib) || 0) * 1024)
				const snapped = Math.round(mib / 512) * 512
				this.form.memory_mib = Math.min(65536, Math.max(512, snapped))
			},
		},
		displayResolution: {
			get() {
				if (!this.form.display_width || !this.form.display_height) return ''
				return `${this.form.display_width}x${this.form.display_height}`
			},
			set(value) {
				if (!value) {
					this.form.display_width = 0
					this.form.display_height = 0
					return
				}
				const [w, h] = value.split('x').map(Number)
				this.form.display_width = w
				this.form.display_height = h
			},
		},
	},
	created() {
		if (this.vm) this.load()
	},
	methods: {
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		clampVcpus() {
			this.form.vcpus = Math.min(64, Math.max(1, Math.round(this.form.vcpus) || 1))
		},
		// Matches AppearanceSection.vue's own range inputs exactly - sets
		// the --pct custom property .pretty-range's filled-track gradient
		// reads (see common/_settings.scss).
		rangeStyle(value, min, max) {
			return { '--pct': `${((value - min) / (max - min)) * 100}%` }
		},
		isoFileName(path) {
			return path ? path.slice(path.lastIndexOf('/') + 1) : ''
		},
		async load() {
			this.error = null
			this.networks = await vmSidecar.listNetworks().catch(() => [])
			// Re-fetch rather than trusting the prop - VmList's poll snapshot
			// could be a couple seconds stale, and disk sizes in particular
			// need a fresh qemu-img read.
			const fresh = await vmSidecar.getVM(this.vm.name).catch(() => this.vm)
			this.form = {
				vcpus: fresh.vcpus,
				memory_mib: fresh.memory_mib,
				iso_path: fresh.iso_path || '',
				firmware: fresh.firmware || 'bios',
				display_width: fresh.display_width || 0,
				display_height: fresh.display_height || 0,
				disks: (fresh.disks || []).map((d) => ({ path: d.path, gib: d.gib, bus: d.bus, ssd: !!d.ssd })),
				networks: (fresh.networks || []).map((n) => ({ mode: n.mode, bridge_name: n.bridge_name })),
				usb_devices: fresh.usb_devices || [],
				pci_devices: fresh.pci_devices || [],
			}
			this.existingDisks = (fresh.disks || []).map((d) => ({ path: d.path, gib: d.gib }))
		},
		close() {
			this.$emit('close')
		},
		async submit() {
			this.error = null
			this.saving = true
			try {
				const payload = {
					vcpus: Number(this.form.vcpus),
					memory_mib: Number(this.form.memory_mib),
					firmware: this.form.firmware,
					disks: this.form.disks.map((d) => ({ path: d.path, gib: Number(d.gib), bus: d.bus, ssd: !!d.ssd })),
					networks: this.form.networks,
					usb_devices: this.form.usb_devices,
					pci_devices: this.form.pci_devices,
				}
				if (this.form.iso_path) payload.iso_path = this.form.iso_path
				if (this.form.display_width && this.form.display_height) {
					payload.display_width = Number(this.form.display_width)
					payload.display_height = Number(this.form.display_height)
				}
				// VmList polls every 2s on its own, so the change shows up
				// there shortly after this window closes - no event to wire up.
				await vmSidecar.updateVM(this.vm.name, payload)
				this.close()
			} catch (e) {
				this.error = e.message
			} finally {
				this.saving = false
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.edit-vm-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	padding: 1rem;
	background: #fff;

	// Same reasoning as CreateVmModal's own copy of this: it used to come
	// from VmOverlayPanel's card-wide override, which no longer wraps
	// this window.
	::v-deep .button {
		border: none;
		border-radius: 8px;
		font-weight: 600;
		font-size: 0.85rem;
		padding: 0.55rem 1rem;
		height: auto;
		background: rgba(0, 0, 0, 0.045);
		color: #2c3e50;
		box-shadow: none;

		&:hover {
			background: rgba(0, 0, 0, 0.08);
		}
		&.is-primary {
			background: #3273dc;
			color: #fff;
			&:hover { background: #2366d1; }
		}
		&[disabled] {
			opacity: 0.5;
		}
	}
}
.edit-vm-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
	// Still scrolls (overflow-y: auto above) - just no visible scrollbar
	// track/thumb cluttering a page that's mostly form controls.
	scrollbar-width: none;
	-ms-overflow-style: none;
	&::-webkit-scrollbar {
		display: none;
	}
	// Every direct child (the Resources card, each .edit-section) is one
	// section of the page - a single uniform gap between them, instead of
	// each relying on its own margin, is what makes the rhythm between
	// sections consistent throughout.
	display: flex;
	flex-direction: column;
	gap: 1.25rem;

	// A flex item's automatic minimum size drops to 0 the moment it has
	// any overflow other than visible - .setting-card's own overflow:
	// hidden (for its rounded corners) means that WITHOUT this, it's the
	// one section flexbox is allowed to crush down to zero height to make
	// everything "fit" instead of properly scrolling, while the
	// .edit-section blocks (no overflow set) keep their natural size and
	// stay visible - exactly the "everything above Disks disappears" bug.
	> * {
		flex-shrink: 0;
	}
}
// A section's own heading-to-content gap is deliberately smaller than
// the between-sections gap above, and set here ONCE so every section
// uses the exact same value - relying on .setting-card-title's own
// padding (meant for a different context - see common/_settings.scss)
// for this instead is what produced two visibly different gaps earlier.
.edit-section {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;

	> .setting-card-title {
		padding: 0;
	}
}
.window-foot {
	flex-shrink: 0;
	display: flex;
	justify-content: flex-end;
	gap: 0.5rem;
	padding-top: 0.75rem;
	margin-top: 0.75rem;
	border-top: 1px solid rgb(228 233 237);
}
.iso-picker {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	width: 100%;
	max-width: 18rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 8px;
	background: #fff;
	padding: 0.4rem 0.65rem;
	font-family: inherit;
	font-size: 0.8rem;
	color: #2c3e50;
	cursor: pointer;

	&:hover {
		border-color: rgba(50, 115, 220, 0.4);
	}
}
.iso-picker-name {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	text-align: left;

	&.is-empty {
		color: rgba(0, 0, 0, 0.4);
	}
}
.iso-picker-clear {
	flex-shrink: 0;
	display: flex;
	color: rgba(0, 0, 0, 0.35);
	border-radius: 4px;

	&:hover {
		color: #f2534a;
	}
}
.iso-picker-chevron {
	flex-shrink: 0;
	color: rgba(0, 0, 0, 0.3);
}
// Matches .iso-picker's own flat-bordered look right below it, instead of
// Buefy/Bulma's stock select (a heavier border, a mismatched blue focus
// ring, and a taller default height than every other row control here).
.display-res-select {
	max-width: 18rem;

	::v-deep select {
		width: 100%;
		height: 2.2rem;
		border: 1px solid rgb(228 233 237);
		border-radius: 8px;
		background: #fff;
		padding: 0 2rem 0 0.65rem;
		font-family: inherit;
		font-size: 0.8rem;
		color: #2c3e50;
		box-shadow: none;

		&:hover {
			border-color: rgba(50, 115, 220, 0.4);
		}
		&:focus {
			border-color: #3273dc;
			box-shadow: none;
		}
	}
	::v-deep .select:not(.is-multiple)::after {
		border-color: rgba(0, 0, 0, 0.35);
		right: 0.9em;
	}
}
// Same pill look as the base .slider-value (see common/_settings.scss),
// but editable - the plain read-only span didn't make clear (or allow)
// typing an exact value instead of dragging the slider.
.slider-value-input {
	min-width: 2.6rem;
	width: 3.4rem;
	padding: 0.15rem 0.4rem;
	border: 1px solid transparent;
	border-radius: 999px;
	background: rgba(0, 0, 0, 0.045);
	color: rgba(44, 62, 80, 0.85);
	font-family: inherit;
	font-size: 0.7rem;
	font-weight: 600;
	text-align: center;
	white-space: nowrap;
	-moz-appearance: textfield;

	&::-webkit-outer-spin-button,
	&::-webkit-inner-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	&:hover {
		background: rgba(0, 0, 0, 0.08);
	}
	&:focus {
		outline: none;
		border-color: #3273dc;
		background: #fff;
	}
}
.slider-value-unit {
	font-size: 0.72rem;
	color: rgba(0, 0, 0, 0.45);
	flex-shrink: 0;
}
// The base .segmented-control (see common/_settings.scss, global) has its
// own bottom margin meant for standalone use as a tab bar - inline inside
// a .row-control it just needs to sit level with the label.
.setting-row .segmented-control {
	margin-bottom: 0;
}
</style>
