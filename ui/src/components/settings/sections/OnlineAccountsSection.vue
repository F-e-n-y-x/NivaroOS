<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Online Accounts') }}</h2>
		<p class="section-desc">
			{{ $t('Connect Google Drive, Dropbox, OneDrive, iCloud, S3-compatible storage, and more - each one shows up as a location in Files.') }}
		</p>

		<h3 class="setting-card-title">{{ $t('Accounts') }}</h3>
		<div class="setting-card">
			<cloud-accounts-panel></cloud-accounts-panel>
		</div>
	</section>
</template>

<script>
import CloudAccountsPanel from '@/components/settings/CloudAccountsPanel.vue'

export const ROWS = [{ label: 'Accounts' }]

export default {
	name: 'online-accounts-section',
	components: { CloudAccountsPanel }
}
</script>

<style lang="scss" scoped>
.section-desc {
	font-size: 0.8rem;
	color: rgba(0, 0, 0, 0.55);
	margin: -0.5rem 0 1rem;
}
</style>
