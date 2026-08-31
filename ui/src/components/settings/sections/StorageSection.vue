<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>
		<disks-panel></disks-panel>
		<storage-pools-panel></storage-pools-panel>
	</section>
</template>

<script>
import DisksPanel from '@/components/settings/DisksPanel.vue'
import StoragePoolsPanel from '@/components/settings/StoragePoolsPanel.vue'

export const ROWS = [
	{ label: 'Automount USB Drive' },
	{ label: 'Disks & USB' },
	{ label: 'Storage Pools' }
]

export default {
	name: 'storage-section',
	components: { DisksPanel, StoragePoolsPanel }
}
</script>
