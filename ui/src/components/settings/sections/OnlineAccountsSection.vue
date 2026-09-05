<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Online Storage') }}</h2>
		<p class="section-subtitle text-muted is-size-7">
			{{ $t('Connect Google Drive, Dropbox, OneDrive, iCloud, S3-compatible storage, and more - each one shows up as a location in Files.') }}
		</p>

		<h3 class="setting-card-title">{{ $t('Connected') }}</h3>
		<div class="setting-card">
			<cloud-accounts-list ref="list"></cloud-accounts-list>
		</div>

		<h3 class="setting-card-title">{{ $t('Add an account') }}</h3>
		<div class="setting-card">
			<cloud-accounts-panel @added="$refs.list.refresh()"></cloud-accounts-panel>
		</div>
	</section>
</template>

<script>
import CloudAccountsList from '@/components/settings/CloudAccountsList.vue'
import CloudAccountsPanel from '@/components/settings/CloudAccountsPanel.vue'

export const ROWS = [{ label: 'Connected' }, { label: 'Add an account' }]

export default {
	name: 'online-accounts-section',
	components: { CloudAccountsList, CloudAccountsPanel }
}
</script>

<style lang="scss" scoped>
// text-muted isn't an actual defined class anywhere in this app (other
// sections use it too, but it's a no-op) - keeping is-size-7 (a real Bulma
// helper) for sizing, but the muted color needs its own rule here.
.section-subtitle {
	color: rgba(0, 0, 0, 0.55);
}
</style>
