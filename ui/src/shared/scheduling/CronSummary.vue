<!-- src/shared/scheduling/CronSummary.vue -->
<!-- A cron expression as a sentence ("Every day at 3:00 AM") in the user's
     language and clock format, worked out on this device (no request), for
     lists and cards. Invalid expressions show as themselves. -->
<template>
	<span class="cron-summary" :title="expr">{{ text }}</span>
</template>

<script>
import { describeCronText } from './cronText'

export default {
	name: 'CronSummary',
	props: {
		expr: { type: String, default: '' }
	},
	computed: {
		text() {
			const f = this.$store && this.$store.state && this.$store.state.timeFormat
			const hour12 = typeof f === 'string' ? f.startsWith('h') : undefined
			return describeCronText(this.$t.bind(this), this.expr, { locale: this.$i18n && this.$i18n.locale, hour12 }) || this.expr
		}
	}
}
</script>
