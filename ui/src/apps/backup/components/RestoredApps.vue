<!-- The apps of a restored settings backup while app-management recreates
     them (services/backup POST /config-restore/status): icon + text, never
     colour alone. -->
<template>
	<ul class="restored-apps" :aria-busy="String(status.state !== 'done')">
		<li v-for="a in status.apps" :key="a.name" class="mt-2">
			<b-icon :icon="look(a.status).icon" :class="look(a.status).cls" pack="mdi" size="is-small" aria-hidden="true"></b-icon>
			<strong class="ml-1">{{ a.name }}</strong> <span :class="look(a.status).cls">{{ $t('settings_restore.app_' + look(a.status).key) }}</span><span v-if="a.reason" class="__op60">: {{ a.reason }}</span>
			<p v-if="a.data_missing" class="has-text-warning-dark ml-5">{{ $t('settings_restore.data_missing') }}</p>
		</li>
	</ul>
</template>

<script>
const LOOKS = {
	waiting: { icon: 'clock-outline', cls: '__op60' },
	installing: { icon: 'progress-download', cls: 'has-text-info' },
	ok: { icon: 'check-circle', cls: 'has-text-success' },
	failed: { icon: 'alert-circle', cls: 'has-text-danger' },
	skipped: { icon: 'skip-next-circle', cls: '__op60' }
}

export default {
	name: 'RestoredApps',
	props: {
		status: { type: Object, required: true }
	},
	methods: {
		look: s => ({ key: LOOKS[s] ? s : 'waiting', ...(LOOKS[s] || LOOKS.waiting) })
	}
}
</script>

<style scoped>
.restored-apps {
	list-style: none;
	text-align: left;
}
</style>
