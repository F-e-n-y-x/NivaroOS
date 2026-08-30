<!-- src/components/desktop/vm/VmDiskList.vue -->
<!--
	Shared disk-list editor for CreateVmModal and EditVmModal - a VM can
	have zero disks (PXE boot, or storage attached later - matching
	VirtualBox's "Do not add a virtual hard disk" and Unraid's "no primary
	vdisk" option) or several, each independently sized/typed. `existing`
	marks which rows are already-attached disks (identified by path) -
	those can only grow, and their bus/type can't be changed without
	recreating them (the same restriction VirtualBox/VMware apply), so
	those fields lock once a disk is real.
-->
<template>
	<div class="vm-disk-list">
		<div v-for="(disk, i) in disks" :key="i" class="vm-disk-row">
			<div class="vm-disk-icon" :class="{ 'is-ssd': disk.ssd }">
				<b-icon :icon="disk.ssd ? 'harddisk' : 'database'" custom-size="mdi-22px"></b-icon>
			</div>
			<div class="vm-disk-main">
				<div class="vm-disk-size-row">
					<b-numberinput
						class="vm-disk-size"
						:value="disk.gib"
						@input="setField(i, 'gib', $event)"
						:min="minGiB(disk)"
						:max="2000"
						size="is-small"
						type="is-light"
						controls-position="compact"
					></b-numberinput>
					<span class="vm-disk-unit">{{ $t('GiB') }}</span>
					<!-- An existing disk's bus/type can't change without recreating
					     it, so it's shown as a plain fact here, not a picker that
					     looks interactive but is actually locked. -->
					<span v-if="isExisting(disk)" class="vm-disk-type-badge">
						{{ disk.bus.toUpperCase() }}<template v-if="disk.ssd"> &middot; SSD</template>
					</span>
					<span v-if="isExisting(disk)" class="vm-disk-existing-tag">{{ $t('Existing') }}</span>
				</div>
				<div v-if="!isExisting(disk)" class="vm-disk-options">
					<div class="segmented-control vm-disk-bus">
						<button
							v-for="bus in ['virtio', 'sata', 'ide']"
							:key="bus"
							type="button"
							class="segmented-option"
							:class="{ active: disk.bus === bus }"
							@click="setField(i, 'bus', bus)"
						>{{ bus.toUpperCase() }}</button>
					</div>
					<label class="vm-disk-ssd">
						<input type="checkbox" :checked="disk.ssd" @change="setField(i, 'ssd', $event.target.checked)" />
						{{ $t('SSD') }}
					</label>
				</div>
			</div>
			<button v-if="!isExisting(disk)" type="button" class="vm-disk-remove" :title="$t('Remove')" @click="removeDisk(i)">
				<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
			</button>
		</div>

		<button v-if="allowAdd" type="button" class="vm-disk-add" @click="addDisk">
			<b-icon icon="plus" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Add Disk') }}</span>
		</button>

		<p v-if="!disks.length" class="vm-disk-hint">
			{{ $t('No disks - the VM has no storage until one is attached, unless it boots entirely from an ISO or the network.') }}
		</p>
	</div>
</template>

<script>
export default {
	name: 'vm-disk-list',
	props: {
		value: { type: Array, default: () => [] },
		// Paths (from the VM's current, already-attached disks) that lock
		// bus/SSD and floor the size at their current GiB - empty for a
		// brand-new VM where every disk is new.
		existing: { type: Array, default: () => [] },
		// Basic mode hides the ability to add further disks - a single
		// disk (grow-only if it's an existing one) is all that mode edits.
		allowAdd: { type: Boolean, default: true },
	},
	computed: {
		disks() {
			return this.value
		},
	},
	methods: {
		isExisting(disk) {
			return this.existing.some((e) => e.path === disk.path)
		},
		minGiB(disk) {
			const match = this.existing.find((e) => e.path === disk.path)
			return match ? match.gib : 1
		},
		setField(index, field, value) {
			const next = this.disks.map((d, i) => (i === index ? { ...d, [field]: value } : d))
			this.$emit('input', next)
		},
		addDisk() {
			this.$emit('input', [...this.disks, { gib: 10, bus: 'virtio', ssd: false }])
		},
		removeDisk(index) {
			this.$emit('input', this.disks.filter((_, i) => i !== index))
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-disk-list {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}
.vm-disk-row {
	display: flex;
	align-items: center;
	gap: 0.85rem;
	padding: 0.65rem 0.85rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
	color: rgba(0, 0, 0, 0.6);
}
// Matches VmStorage.vue's .iso-icon / VmNetworks.vue's .network-icon
// exactly - the same round icon-badge treatment used everywhere else in
// this app's list rows.
.vm-disk-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.04);
	color: rgba(0, 0, 0, 0.4);

	&.is-ssd {
		background: rgba(50, 115, 220, 0.1);
		color: #3273dc;
	}
}
.vm-disk-main {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}
.vm-disk-size-row {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-wrap: wrap;
}
.vm-disk-size {
	width: 8rem;
	flex-shrink: 0;

	// Buefy's b-numberinput defaults its +/- buttons to type="is-primary"
	// (solid filled blue) - a jarring mismatch next to an input this app
	// styles flat/neutral everywhere else. type="is-light" on the
	// component (see template) gets partway there; !important here
	// guarantees this wins outright rather than depending on a
	// specificity tie against Bulma's own .is-light/.is-primary rules,
	// which is fragile to get right by inspection alone. Explicit height
	// keeps the input and its flanking buttons the exact same size - a
	// mismatch there was the second half of what looked "wrong" here.
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
.vm-disk-unit {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
	flex-shrink: 0;
}
.vm-disk-type-badge {
	font-size: 0.72rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.5);
	flex-shrink: 0;
}
.vm-disk-existing-tag {
	font-size: 0.7rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.4);
	background: rgba(0, 0, 0, 0.05);
	padding: 0.1rem 0.5rem;
	border-radius: 999px;
	flex-shrink: 0;
}
.vm-disk-options {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	flex-wrap: wrap;
}
// .segmented-control (see common/_settings.scss, global) has its own
// bottom margin meant for standalone use as a tab bar - inline here it
// just needs to sit level with the SSD checkbox beside it.
.vm-disk-bus {
	flex-shrink: 0;
	margin-bottom: 0;
}
.vm-disk-ssd {
	display: flex;
	align-items: center;
	gap: 0.3rem;
	font-size: 0.78rem;
	white-space: nowrap;
	cursor: pointer;
	flex-shrink: 0;
}
.vm-disk-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: rgba(0, 0, 0, 0.35);
	cursor: pointer;
	display: flex;
	align-items: center;
	padding: 0.35rem;
	border-radius: 6px;

	&:hover {
		color: #f2534a;
		background: rgba(242, 83, 74, 0.08);
	}
}
.vm-disk-add {
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
.vm-disk-hint {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
}
</style>
