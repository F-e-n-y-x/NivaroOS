<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>
		<fstab-panel></fstab-panel>
		<disks-panel></disks-panel>
		<storage-pools-panel></storage-pools-panel>
	</section>
</template>

<script>
import DisksPanel from '@/apps/settings/DisksPanel.vue'
import StoragePoolsPanel from '@/apps/settings/StoragePoolsPanel.vue'
import FstabPanel from '@/apps/settings/FstabPanel.vue'

// Search index: labels are the titles this section renders (the search
// jumps to them); keywords are other words people type for them.
export const ROWS = [
	{ label: 'Mounted Drives', keywords: 'fstab persistent mounts ntfs drive boot' },
	{ label: 'Available Disks', keywords: 'format new disk' },
	{ label: 'Mounted Disks', keywords: 'disk remove smart health' },
	{ label: 'USB Drives & Automount', keywords: 'usb eject automount' },
	{ label: 'Combined Storage Pools (MergerFS)', keywords: 'mergerfs pool' }
]

export default {
	name: 'storage-section',
	components: { DisksPanel, StoragePoolsPanel, FstabPanel }
}
</script>
