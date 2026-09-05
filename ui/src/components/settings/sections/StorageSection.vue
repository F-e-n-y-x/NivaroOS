<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>
		<fstab-panel></fstab-panel>
		<disks-panel></disks-panel>
		<storage-pools-panel></storage-pools-panel>
	</section>
</template>

<script>
import DisksPanel from '@/components/settings/DisksPanel.vue'
import StoragePoolsPanel from '@/components/settings/StoragePoolsPanel.vue'
import FstabPanel from '@/components/settings/FstabPanel.vue'

export const ROWS = [
	{ label: 'Persistent Mounts (fstab)' },
	{ label: 'Automount USB Drive' },
	{ label: 'Disks & USB' },
	{ label: 'Storage Pools' }
]

export default {
	name: 'storage-section',
	components: { DisksPanel, StoragePoolsPanel, FstabPanel }
}
</script>
