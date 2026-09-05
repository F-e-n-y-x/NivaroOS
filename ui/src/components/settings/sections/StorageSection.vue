<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>
		<disks-panel></disks-panel>
		<storage-pools-panel></storage-pools-panel>
		<fstab-panel></fstab-panel>
	</section>
</template>

<script>
import DisksPanel from '@/components/settings/DisksPanel.vue'
import StoragePoolsPanel from '@/components/settings/StoragePoolsPanel.vue'
import FstabPanel from '@/components/settings/FstabPanel.vue'

export const ROWS = [
	{ label: 'Automount USB Drive' },
	{ label: 'Disks & USB' },
	{ label: 'Storage Pools' },
	{ label: 'Persistent Mounts (fstab)' }
]

export default {
	name: 'storage-section',
	components: { DisksPanel, StoragePoolsPanel, FstabPanel }
}
</script>
