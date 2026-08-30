<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>

		<h3 class="setting-card-title">{{ $t('Disks & USB') }}</h3>
		<div class="setting-card">
			<disks-panel></disks-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('Storage Pools') }}</h3>
		<div class="setting-card">
			<storage-pools-panel></storage-pools-panel>
		</div>
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
