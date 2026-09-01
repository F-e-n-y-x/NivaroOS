<!-- src/components/desktop/vm/CreateVmModal.vue -->
<!-- A real desktop window (opened via OPEN_WINDOW), not an overlay popup -
     the window chrome itself provides the titlebar/drag/close, so this is
     just the wizard's own content filling the window body. -->
<template>
	<div class="create-vm-window">
		<div class="wizard-steps">
			<div class="wizard-dots">
				<span
					v-for="(s, i) in steps"
					:key="s.id"
					class="wizard-dot"
					:class="{ active: i === step, done: i < step }"
					:title="s.label"
				>
					<b-icon v-if="i < step" icon="check" custom-size="mdi-12px"></b-icon>
					<template v-else>{{ i + 1 }}</template>
				</span>
			</div>
			<div class="wizard-current-label">{{ $t('Step') }} {{ step + 1 }} {{ $t('of') }} {{ steps.length }} &middot; {{ steps[step].label }}</div>
		</div>

		<div class="wizard-body scrollbars-light">
		<!-- Step: name + OS template -->
		<div v-if="currentStepId === 'os'" class="wizard-pane">
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="tag-outline" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Name') }}</div>
					<div class="row-control">
						<b-input v-model="form.name" placeholder="my-vm" ref="nameInput" size="is-small"></b-input>
					</div>
				</div>
			</div>
			<div class="wizard-section">
				<h3 class="setting-card-title">{{ $t('Operating System') }}</h3>
				<div class="os-template-grid">
					<button
						v-for="tpl in osTemplates"
						:key="tpl.id"
						class="os-template"
						:class="{ active: selectedTemplate === tpl.id }"
						@click="applyTemplate(tpl)"
					>
						<b-icon :icon="tpl.icon" custom-size="mdi-28px"></b-icon>
						<span>{{ tpl.label }}</span>
					</button>
				</div>
			</div>
			<p class="wizard-hint">{{ $t('Picking an OS just fills in sensible defaults below - everything stays fully editable.') }}</p>
		</div>

		<!-- Step: resources -->
		<div v-if="currentStepId === 'resources'" class="wizard-pane">
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="chip" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('vCPUs') }}</div>
					<div class="row-control slider-control">
						<span class="slider-hint">1</span>
						<input class="pretty-range" v-model.number="form.vcpus" type="range" min="1" :max="maxVcpus" step="1"
							:style="rangeStyle(form.vcpus, 1, maxVcpus)" />
						<span class="slider-hint">{{ maxVcpus }}</span>
						<input type="number" class="slider-value-input" v-model.number="form.vcpus" min="1" :max="maxVcpus" @change="clampVcpus" />
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="memory" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Memory') }}</div>
					<div class="row-control slider-control">
						<span class="slider-hint">512 MB</span>
						<input class="pretty-range" v-model.number="form.memory_mib" type="range" min="512" :max="maxMemoryMiB" step="512"
							:style="rangeStyle(form.memory_mib, 512, maxMemoryMiB)" />
						<span class="slider-hint">{{ maxMemoryGiB }} GB</span>
						<input type="number" class="slider-value-input" v-model.number="memoryGiB" min="0.5" :max="maxMemoryGiB" step="0.5" />
						<span class="slider-value-unit">{{ $t('GB') }}</span>
					</div>
				</div>
				<div class="setting-row">
					<b-icon class="row-icon" icon="monitor" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Display Resolution') }}</div>
					<div class="row-control">
						<vm-dropdown
							v-model="displayResolution"
							:options="resolutionOptions"
							:placeholder="$t('Default (let the guest decide)')"
							icon="monitor"
							size="small"
							style="max-width: 18rem;"
						></vm-dropdown>
					</div>
				</div>
				<div class="setting-row" v-if="mode === 'advanced'">
					<b-icon class="row-icon" icon="chip" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Firmware') }}</div>
					<div class="row-control">
						<div class="segmented-control">
							<button type="button" class="segmented-option" :class="{ active: form.firmware === 'bios' }" @click="form.firmware = 'bios'">{{ $t('BIOS') }}</button>
							<button type="button" class="segmented-option" :class="{ active: form.firmware === 'uefi' }" @click="form.firmware = 'uefi'">{{ $t('UEFI') }}</button>
						</div>
					</div>
				</div>
			</div>
			<p class="wizard-hint">{{ recommendedHint }}</p>
		</div>

		<!-- Step: storage (boot media + disks) -->
		<div v-if="currentStepId === 'storage'" class="wizard-pane">
			<div class="setting-card">
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
				<div class="setting-row" v-if="mode === 'basic'">
					<b-icon class="row-icon" icon="harddisk" custom-size="mdi-20px"></b-icon>
					<div class="row-label">{{ $t('Disk size') }}</div>
					<div class="row-control">
						<b-numberinput :value="form.disks[0].gib" @input="form.disks[0].gib = $event" :min="1" :max="2000" size="is-small" type="is-light" controls-position="compact" class="basic-disk-size"></b-numberinput>
						<span class="basic-disk-unit">{{ $t('GiB') }}</span>
					</div>
				</div>
			</div>
			<p class="wizard-hint">{{ $t('Pick an installer ISO to install an OS from scratch, or skip this if the disk already has one.') }}</p>
			<vm-file-picker-dialog
				:active="showIsoPicker"
				:title="$t('ISO')"
				start-path="/DATA/VMs/isos"
				:extensions="['iso', 'img']"
				@selected="form.iso_path = $event"
				@close="showIsoPicker = false"
			></vm-file-picker-dialog>
			<div class="wizard-section" v-if="mode === 'advanced'">
				<h3 class="setting-card-title">{{ $t('Disks') }}</h3>
				<vm-disk-list v-model="form.disks"></vm-disk-list>
			</div>
		</div>

		<!-- Step: network -->
		<div v-if="currentStepId === 'network'" class="wizard-pane">
			<template v-if="mode === 'basic'">
				<div class="setting-card">
					<div class="setting-row">
						<b-icon class="row-icon" :icon="form.networks[0].mode === 'bridge' ? 'lan-connect' : 'lan'" custom-size="mdi-20px"></b-icon>
						<div class="row-label">{{ $t('Network') }}</div>
						<div class="row-control">
							<div class="segmented-control">
								<button type="button" class="segmented-option" :class="{ active: form.networks[0].mode === 'nat' }" @click="form.networks[0] = { mode: 'nat' }">{{ $t('NAT') }}</button>
								<button
									v-for="bridge in bridgeNetworks"
									:key="bridge.name"
									type="button"
									class="segmented-option"
									:class="{ active: form.networks[0].mode === 'bridge' && form.networks[0].bridge_name === bridge.name }"
									@click="form.networks[0] = { mode: 'bridge', bridge_name: bridge.name }"
								>{{ bridge.name }}</button>
							</div>
						</div>
					</div>
				</div>
				<p class="wizard-hint">{{ $t('Recommended - the VM shares this machine\'s internet connection.') }}</p>
			</template>
			<div class="wizard-section" v-else>
				<h3 class="setting-card-title">{{ $t('Network Adapters') }}</h3>
				<vm-network-list v-model="form.networks" :bridge-networks="bridgeNetworks"></vm-network-list>
			</div>
		</div>

		<!-- Step: hardware (advanced only) -->
		<div v-if="currentStepId === 'hardware'" class="wizard-pane">
			<vm-hardware-picker
				:usb-value="form.usb_devices"
				:pci-value="form.pci_devices"
				@update:usbValue="form.usb_devices = $event"
				@update:pciValue="form.pci_devices = $event"
			></vm-hardware-picker>
		</div>

		<!-- Step: review -->
		<div v-if="currentStepId === 'review'" class="wizard-pane">
			<div class="setting-card">
				<div class="setting-row"><div class="row-label">{{ $t('Name') }}</div><div class="row-control review-value">{{ form.name }}</div></div>
				<div class="setting-row"><div class="row-label">{{ $t('vCPUs') }}</div><div class="row-control review-value">{{ form.vcpus }}</div></div>
				<div class="setting-row"><div class="row-label">{{ $t('Memory') }}</div><div class="row-control review-value">{{ formatMib(form.memory_mib) }}</div></div>
				<div class="setting-row"><div class="row-label">{{ $t('Firmware') }}</div><div class="row-control review-value">{{ form.firmware.toUpperCase() }}</div></div>
				<div class="setting-row" v-if="displayResolution"><div class="row-label">{{ $t('Display Resolution') }}</div><div class="row-control review-value">{{ displayResolution }}</div></div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Disks') }}</div>
					<div class="row-control review-value">{{ form.disks.length ? form.disks.map((d) => d.gib + ' GiB ' + d.bus.toUpperCase() + (d.ssd ? ' SSD' : '')).join(', ') : $t('None') }}</div>
				</div>
				<div class="setting-row"><div class="row-label">{{ $t('ISO') }}</div><div class="row-control review-value">{{ form.iso_path || $t('None') }}</div></div>
				<div class="setting-row">
					<div class="row-label">{{ $t('Network') }}</div>
					<div class="row-control review-value">{{ form.networks.length ? form.networks.map((n) => (n.mode === 'nat' ? $t('NAT') : n.bridge_name)).join(', ') : $t('None') }}</div>
				</div>
				<div class="setting-row" v-if="form.usb_devices.length"><div class="row-label">{{ $t('USB Devices') }}</div><div class="row-control review-value">{{ form.usb_devices.length }}</div></div>
				<div class="setting-row" v-if="form.pci_devices.length"><div class="row-label">{{ $t('PCI Devices') }}</div><div class="row-control review-value">{{ form.pci_devices.length }}</div></div>
			</div>
		</div>

		<b-message v-if="error" type="is-danger" :closable="false">{{ error }}</b-message>
		</div>

		<footer class="window-foot">
			<div class="segmented-control wizard-mode-toggle" :title="mode === 'basic' ? $t('One disk, one network, sensible defaults.') : $t('Multiple disks/adapters, USB & PCI passthrough.')">
				<button type="button" class="segmented-option" :class="{ active: mode === 'basic' }" @click="setMode('basic')">{{ $t('Basic') }}</button>
				<button type="button" class="segmented-option" :class="{ active: mode === 'advanced' }" @click="setMode('advanced')">{{ $t('Advanced') }}</button>
			</div>
			<div class="window-foot-actions">
				<b-button v-if="step > 0" @click="step--">{{ $t('Back') }}</b-button>
				<b-button @click="close">{{ $t('Cancel') }}</b-button>
				<b-button v-if="step < steps.length - 1" type="is-primary" :disabled="!canAdvance" @click="step++">
					{{ $t('Next') }}
				</b-button>
				<b-button v-else type="is-primary" :loading="creating" @click="submit">
					{{ $t('Create') }}
				</b-button>
			</div>
		</footer>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmFilePickerDialog from './VmFilePickerDialog.vue'
import VmDiskList from './VmDiskList.vue'
import VmNetworkList from './VmNetworkList.vue'
import VmHardwarePicker from './VmHardwarePicker.vue'
import VmDropdown from './VmDropdown.vue'

// Picking a template only pre-fills these fields - it's a starting point
// for beginners, not a locked-in mode; every field stays editable right
// after, matching Unraid's "template just fills defaults" behavior.
const OS_TEMPLATES = [
	{ id: 'linux', label: 'Linux', icon: 'linux', vcpus: 2, memory_mib: 2048, disk_gib: 20, firmware: 'bios', net_model: 'virtio' },
	// Windows 11 requires UEFI and standard Intel e1000e driver out-of-the-box
	{ id: 'windows', label: 'Windows', icon: 'microsoft-windows', vcpus: 4, memory_mib: 4096, disk_gib: 60, firmware: 'uefi', net_model: 'e1000e' },
	{ id: 'custom', label: 'Other', icon: 'cog-outline', vcpus: 2, memory_mib: 2048, disk_gib: 20, firmware: 'bios', net_model: 'e1000e' },
]

const RESOLUTION_OPTIONS = [
	{ value: '', label: 'Default (let the guest decide)', icon: 'monitor' },
	{ value: '1920x1080', label: '1920 × 1080', meta: 'Full HD', icon: 'monitor' },
	{ value: '1280x720', label: '1280 × 720', meta: 'HD', icon: 'monitor' },
	{ value: '1024x768', label: '1024 × 768', meta: '4:3', icon: 'monitor' },
	{ value: '800x600', label: '800 × 600', meta: 'SVGA', icon: 'monitor' },
]

export default {
	name: 'create-vm-modal',
	components: { VmFilePickerDialog, VmDiskList, VmNetworkList, VmHardwarePicker, VmDropdown },
	data() {
		return {
			mode: 'basic',
			step: 0,
			osTemplates: OS_TEMPLATES,
			resolutionOptions: RESOLUTION_OPTIONS,
			selectedTemplate: 'linux',
			form: {
				name: '',
				vcpus: 2,
				memory_mib: 2048,
				iso_path: '',
				firmware: 'bios',
				display_width: 0,
				display_height: 0,
				disks: [{ gib: 20, bus: 'virtio', ssd: false }],
				networks: [{ mode: 'nat' }],
				usb_devices: [],
				pci_devices: [],
			},
			hostCaps: null,
			showIsoPicker: false,
			networks: [],
			creating: false,
			error: null,
		}
	},
	computed: {
		maxVcpus() {
			if (this.hostCaps && this.hostCaps.cpu_cores) {
				return Math.max(1, this.hostCaps.cpu_cores)
			}
			return 16
		},
		maxMemoryMiB() {
			if (this.hostCaps && this.hostCaps.total_memory_mib) {
				return Math.max(1024, Math.floor(this.hostCaps.total_memory_mib / 512) * 512)
			}
			return 16384
		},
		maxMemoryGiB() {
			return Math.round(this.maxMemoryMiB / 1024)
		},
		bridgeNetworks() {
			return this.networks.filter((n) => n.mode === 'bridge')
		},
		// Basic mode collapses to one disk, one adapter, no USB/PCI step -
		// Unraid and VirtualBox 7.1 both use exactly this two-tier
		// approach (Basic/Advanced, Basic/Expert) to stay approachable for
		// a first VM while keeping everything advanced users need one
		// toggle away, rather than always showing every field to everyone.
		steps() {
			const list = [
				{ id: 'os', label: this.$t('Name & OS') },
				{ id: 'resources', label: this.$t('Resources') },
				{ id: 'storage', label: this.$t('Storage') },
				{ id: 'network', label: this.$t('Network') },
			]
			if (this.mode === 'advanced') list.push({ id: 'hardware', label: this.$t('Hardware') })
			list.push({ id: 'review', label: this.$t('Review') })
			return list
		},
		currentStepId() {
			return this.steps[this.step] && this.steps[this.step].id
		},
		canAdvance() {
			if (this.currentStepId === 'os') return !!this.form.name
			return true
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
				this.form.memory_mib = Math.min(this.maxMemoryMiB, Math.max(512, snapped))
			},
		},
		// The guest's actual display resolution (a libvirt video hint the
		// guest OS reads like a monitor's EDID) - not the console view's
		// own client-side render scaling, which is a separate, unrelated
		// control living in the console toolbar instead.
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
		recommendedHint() {
			return this.selectedTemplate === 'windows'
				? this.$t('Windows generally wants at least 4 GB of memory and 60 GB of disk.')
				: this.$t('2 GB of memory and 20 GB of disk is enough for most lightweight Linux distros.')
		},
	},
	created() {
		this.refresh()
	},
	mounted() {
		this.$nextTick(() => this.$refs.nameInput && this.$refs.nameInput.focus())
	},
	methods: {
		setMode(mode) {
			if (this.mode === mode) return
			this.mode = mode
			this.step = 0
			// Switching to Basic collapses down to exactly what its simpler
			// UI actually edits (disks[0]/networks[0]) - anything beyond
			// that would otherwise keep being submitted invisibly. Nothing
			// to reconcile going the other way: Advanced's UI can only ever
			// add to what Basic already has.
			if (mode === 'basic') {
				this.form.disks = this.form.disks.slice(0, 1)
				this.form.networks = this.form.networks.slice(0, 1)
				this.form.usb_devices = []
				this.form.pci_devices = []
			}
		},
		applyTemplate(tpl) {
			this.selectedTemplate = tpl.id
			this.form.vcpus = Math.min(this.maxVcpus, tpl.vcpus)
			this.form.memory_mib = Math.min(this.maxMemoryMiB, tpl.memory_mib)
			this.form.firmware = tpl.firmware
			if (this.form.disks.length === 1) {
				this.form.disks = [{ ...this.form.disks[0], gib: tpl.disk_gib }]
			}
			if (this.form.networks.length === 1) {
				this.form.networks = [{ ...this.form.networks[0], model: tpl.net_model || 'virtio' }]
			}
		},
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		clampVcpus() {
			this.form.vcpus = Math.min(this.maxVcpus, Math.max(1, Math.round(this.form.vcpus) || 1))
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
		close() {
			this.$emit('close')
		},
		async refresh() {
			const [nets, caps] = await Promise.all([
				vmSidecar.listNetworks().catch(() => []),
				vmSidecar.getHostCapabilities().catch(() => null),
			])
			this.networks = nets || []
			if (caps) {
				this.hostCaps = caps
				if (this.form.vcpus > this.maxVcpus) {
					this.form.vcpus = this.maxVcpus
				}
				if (this.form.memory_mib > this.maxMemoryMiB) {
					this.form.memory_mib = this.maxMemoryMiB
				}
			}
		},
		async submit() {
			this.error = null
			this.creating = true
			try {
				const payload = {
					name: this.form.name,
					vcpus: Number(this.form.vcpus),
					memory_mib: Number(this.form.memory_mib),
					firmware: this.form.firmware,
					disks: this.form.disks.map((d) => ({ gib: Number(d.gib), bus: d.bus, ssd: !!d.ssd })),
					networks: this.form.networks,
					usb_devices: this.form.usb_devices,
					pci_devices: this.form.pci_devices,
				}
				if (this.form.iso_path) payload.iso_path = this.form.iso_path
				if (this.form.display_width && this.form.display_height) {
					payload.display_width = Number(this.form.display_width)
					payload.display_height = Number(this.form.display_height)
				}
				// VmList polls every 2s on its own, so the new VM shows up there
				// shortly after this window closes - no event to wire up.
				await vmSidecar.createVM(payload)
				this.close()
			} catch (e) {
				this.error = e.message
				// A failure belongs on the step whose fields caused it - back
				// to Resources covers disk/name/network errors the sidecar
				// reports, keeping the review step from silently swallowing them.
				this.step = this.steps.length - 1
			} finally {
				this.creating = false
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.create-vm-window {
	display: flex;
	flex-direction: column;
	height: 100%;
	padding: 1rem;
	background: #fff;
	// Belt-and-suspenders: this window's content must never scroll
	// horizontally regardless of what's inside it - the step header
	// overflow above is the real fix, this just guarantees nothing else
	// can reintroduce the same class of bug later.
	overflow-x: hidden;

	// Every button in this window (Browse/Clear next to the ISO field,
	// the wizard's own Back/Cancel/Next/Create footer) used to inherit
	// this from VmOverlayPanel's own card-wide override - now that this
	// is a real window instead of an overlay dialog, it needs its own
	// copy so raw <b-button>s don't fall back to Bulma's stock
	// bordered/white look next to the rest of this app's flat design.
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
.wizard-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
}
.wizard-pane {
	// Each step's own children (a setting-card, a hint paragraph, a
	// .wizard-section) otherwise only had their OWN margin to space
	// themselves from whatever came before - inconsistent depending on
	// what type of element that happened to be. A uniform gap means the
	// rhythm is the same regardless, in every step.
	display: flex;
	flex-direction: column;
	gap: 1rem;

	// A flex item's automatic minimum size drops to 0 the moment it has
	// any overflow other than visible - .setting-card's own overflow:
	// hidden (for its rounded corners) means that WITHOUT this, it's the
	// section flexbox is allowed to crush down to zero height to make
	// everything "fit" in cramped space instead of properly scrolling
	// (see EditVmModal.vue's .edit-vm-body for where this actually bit).
	> * {
		flex-shrink: 0;
	}
}
// A section's own heading-to-content gap is deliberately smaller than
// the between-sections gap above, and set here ONCE so every section
// uses the exact same value - relying on .setting-card-title's own
// padding (meant for a different context - see common/_settings.scss)
// for this instead produced two visibly different gaps.
.wizard-section {
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
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	padding-top: 0.75rem;
	margin-top: 0.75rem;
	border-top: 1px solid rgb(228 233 237);
}
.window-foot-actions {
	display: flex;
	gap: 0.5rem;
}
.wizard-mode-toggle {
	margin-bottom: 0;
}
.basic-disk-size {
	width: 8rem;

	::v-deep input,
	::v-deep .button {
		height: 2.25rem !important;
	}
	::v-deep input {
		text-align: center;
		border-color: rgb(228 233 237) !important;
	}
	::v-deep .button {
		border-color: rgb(228 233 237) !important;
		background: rgba(0, 0, 0, 0.04) !important;
		color: rgba(0, 0, 0, 0.55) !important;
		box-shadow: none !important;

		&:hover {
			background: rgba(0, 0, 0, 0.08) !important;
		}
	}
}
.basic-disk-unit {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
	margin-left: 0.5rem;
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
.wizard-steps {
	margin-bottom: 1rem;
	// A row of N inline text labels (one per step) kept overflowing this
	// window horizontally no matter how the flex/shrink rules were tuned -
	// with 6+ steps there's just no width small enough to guarantee they
	// all fit. Dots (fixed-size, always compact) plus a single current-
	// step label below them can't reproduce that bug by construction:
	// there's only ever one line of text on screen.
	overflow: hidden;
}
.wizard-dots {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 0.4rem;
	flex-wrap: wrap;
}
.wizard-dot {
	flex-shrink: 0;
	width: 1.4rem;
	height: 1.4rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 0.7rem;
	background: rgba(0, 0, 0, 0.06);
	color: rgba(0, 0, 0, 0.4);

	&.active {
		background: #3273dc;
		color: #fff;
		font-weight: 600;
	}
	&.done {
		background: rgba(50, 115, 220, 0.15);
		color: #3273dc;
	}
}
.wizard-current-label {
	margin-top: 0.5rem;
	text-align: center;
	font-size: 0.78rem;
	font-weight: 600;
	color: #2c3e50;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
.wizard-hint {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
	margin-top: 0.5rem;
}
.os-template-grid {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 0.5rem;
	width: 100%;
}
.os-template {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.4rem;
	padding: 0.85rem 0.5rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
	color: rgba(0, 0, 0, 0.6);
	cursor: pointer;
	font-family: inherit;
	font-size: 0.8rem;

	// Buefy's <b-icon> wraps every glyph in a Bulma .icon span fixed at
	// 1.5rem (24px) by default - custom-size only scales the glyph's own
	// font-size, so anything bigger than 24px overflows its own wrapper
	// unless the wrapper itself is resized to match here.
	::v-deep .icon {
		width: 2rem;
		height: 2rem;
	}

	&:hover {
		border-color: rgba(50, 115, 220, 0.4);
	}
	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.06);
		color: #3273dc;
	}
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
// Matches .iso-picker's own flat-bordered look, instead of Buefy/Bulma's
// stock select (a heavier border, a mismatched blue focus ring, and a
// taller default height than every other row control here).
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
// The base .segmented-control (see common/_settings.scss, global) has its
// own bottom margin meant for standalone use as a tab bar - inline inside
// a .row-control it just needs to sit level with the label.
.setting-row .segmented-control {
	margin-bottom: 0;
}
.review-value {
	font-weight: 600;
	color: #2c3e50;
	font-size: 0.85rem;
	text-align: right;
	overflow-wrap: anywhere;
}
</style>
