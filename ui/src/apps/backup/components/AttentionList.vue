<!-- "Needs attention" (spec §12.2): one row per job with a problem - the
     job, the problem and its cause, and its direct fixes. -->
<template>
	<section class="bk-card bk-attention" :aria-labelledby="headingId">
		<h3 :id="headingId" class="bk-card-title">
			<b-icon icon="alert-outline" pack="mdi" custom-size="mdi-20px" class="bk-tone-warn" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.overview.attention_title', { count: items.length }) }}</span>
		</h3>
		<ul class="bk-attention-list">
			<li v-for="item in items" :key="item.job.id + item.reason + item.code" class="bk-attention-row">
				<button type="button" class="bk-btn is-link bk-attention-job" @click="$emit('open-job', item.job)">{{ item.job.name }}</button>
				<error-explain
					:code="item.code"
					:reason="item.code ? '' : item.reason"
					:actions="item.actions"
					:tone="item.severity === 'problem' ? 'danger' : 'warn'"
					:busy="busyJobId === item.job.id"
					@action="a => $emit('action', a, item)"
				></error-explain>
			</li>
		</ul>
	</section>
</template>

<script>
import ErrorExplain from './ErrorExplain.vue'

let seq = 0

export default {
	name: 'AttentionList',
	components: { ErrorExplain },
	props: {
		// attentionItems() from state.js
		items: { type: Array, required: true },
		busyJobId: { type: String, default: '' }
	},
	data() {
		return { headingId: `bk-attention-${++seq}` }
	}
}
</script>

<style lang="scss" scoped>
.bk-attention-list {
	list-style: none;
	margin: 0;
	padding: 0;
	display: flex;
	flex-direction: column;
}
.bk-attention-row {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: var(--space-1);
	padding: var(--space-3) 0;
	border-top: 1px solid var(--theme-table-divider);
	&:first-child {
		border-top: none;
		padding-top: 0;
	}
}
.bk-attention-job {
	padding-left: 0;
	font-size: var(--font-base);
	font-weight: 600;
	white-space: normal;
	text-align: left;
}
</style>
