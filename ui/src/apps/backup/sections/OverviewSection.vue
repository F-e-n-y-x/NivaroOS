<!-- Overview (spec §12.2): the state in one line, what needs attention,
     what's running and what's coming up - or, with no jobs yet, the
     first-run explanation leading to "Create your first backup". -->
<template>
	<div class="bk-section">
		<status-header :overall="overall" :fmt="fmt" @new-job="app.openWizard({})"></status-header>

		<empty-state v-if="overall.state === 'empty'" @start="opts => app.openWizard(opts)"></empty-state>

		<template v-else>
			<attention-list
				v-if="attention.length"
				:items="attention"
				:busy-job-id="app.busyJobId"
				@open-job="job => app.showJob(job.id)"
				@action="(a, item) => app.runAction(a, item.job, item.runId)"
			></attention-list>

			<section v-if="running.length" class="bk-card" :aria-labelledby="runningId">
				<h3 :id="runningId" class="bk-card-title">
					<b-icon icon="progress-upload" pack="mdi" custom-size="mdi-20px" aria-hidden="true"></b-icon>
					<span>{{ $t('backup.overview.running_title') }}</span>
				</h3>
				<ul class="bk-plain-list">
					<running-job-card
						v-for="job in running"
						:key="job.id"
						:job="job"
						:live="app.live[job.active_run.id] || null"
						:fmt="fmt"
						@open="j => app.openRun(j.active_run.id, j)"
						@review="j => app.runAction('review', j, j.active_run.id)"
					></running-job-card>
				</ul>
			</section>

			<upcoming-list :items="upcoming" :fmt="fmt" :caps="app.caps" @open-job="job => app.showJob(job.id)"></upcoming-list>
		</template>
	</div>
</template>

<script>
import StatusHeader from '../components/StatusHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import AttentionList from '../components/AttentionList.vue'
import RunningJobCard from '../components/RunningJobCard.vue'
import UpcomingList from '../components/UpcomingList.vue'
import { overallState, attentionItems, runningItems, upcomingItems } from '../state'

export default {
	name: 'OverviewSection',
	components: { StatusHeader, EmptyState, AttentionList, RunningJobCard, UpcomingList },
	inject: { app: 'backupApp' },
	props: {
		jobs: { type: Array, required: true },
		fmt: { type: Object, required: true }
	},
	data() {
		return { runningId: 'bk-overview-running' }
	},
	computed: {
		overall() {
			return overallState(this.jobs)
		},
		attention() {
			return attentionItems(this.jobs)
		},
		running() {
			return runningItems(this.jobs)
		},
		upcoming() {
			return upcomingItems(this.jobs)
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-plain-list {
	list-style: none;
	margin: 0;
	padding: 0;
}
</style>
