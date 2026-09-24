<!-- "Coming up" (spec §12.2): the next scheduled runs and the plug-in
     jobs, with the server time zone they're in. -->
<template>
	<section class="bk-card" :aria-labelledby="headingId">
		<h3 :id="headingId" class="bk-card-title">
			<b-icon icon="calendar-clock" pack="mdi" custom-size="mdi-20px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.overview.upcoming_title') }}</span>
		</h3>
		<p v-if="!items.length" class="bk-secondary bk-upcoming-none">{{ $t('backup.overview.upcoming_none') }}</p>
		<ul v-else class="bk-upcoming">
			<li v-for="item in items" :key="item.kind + item.job.id">
				<span class="bk-upcoming-when">
					<b-icon :icon="item.kind === 'plug' ? 'usb' : 'clock-outline'" pack="mdi" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<time v-if="item.at" :datetime="item.at">{{ fmt.when(item.at) }}</time>
					<span v-else>{{ $t('backup.overview.on_plug_in') }}</span>
				</span>
				<button type="button" class="bk-btn is-link bk-upcoming-job" @click="$emit('open-job', item.job)">{{ item.job.name }}</button>
			</li>
		</ul>
		<p v-if="zoneText" class="bk-muted bk-upcoming-zone">{{ zoneText }}</p>
	</section>
</template>

<script>
let seq = 0

export default {
	name: 'UpcomingList',
	props: {
		items: { type: Array, required: true },
		fmt: { type: Object, required: true },
		caps: { type: Object, default: null }
	},
	data() {
		return { headingId: `bk-upcoming-${++seq}` }
	},
	computed: {
		zoneText() {
			const c = this.caps
			if (!c || !c.timezone) return ''
			return this.$t('backup.overview.server_time', { zone: c.timezone, offset: c.utc_offset || '' })
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-upcoming {
	list-style: none;
	margin: 0;
	padding: 0;
	li {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
		padding: var(--space-1) 0;
	}
}
.bk-upcoming-when {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	min-width: 9rem;
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
	font-variant-numeric: tabular-nums;
}
.bk-upcoming-job {
	white-space: normal;
	text-align: left;
}
.bk-upcoming-none,
.bk-upcoming-zone {
	margin: var(--space-2) 0 0;
	font-size: var(--font-xs);
}
</style>
