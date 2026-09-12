<template>
	<div class="modal-card">
		<!-- Modal-Card Header Start -->
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{ $t('Update') }}</h3>
			</div>
			<b-icon class="close-button" icon="close-outline" pack="casa" @click.native="$emit('close');" />
		</header>
		<!-- Modal-Card Header End -->
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body ">
			<div class="node-card fixed-height">
				<div v-if="!isUpdating" class="update-info-container  is-size-14px" v-dompurify-html="markdownToHtml"></div>
				<div v-else class="update-info-container  is-size-14px" v-dompurify-html="updateMarkdownHtml"></div>
			</div>
		</section>
		<!-- Modal-Card Body End -->
		<!-- Modal-Card Footer Start-->
		<footer class="modal-card-foot is-flex is-align-items-center">
			<div class="is-flex-grow-1"></div>
			<div>
				<b-button :label="$t('Upgrade Now')" :loading="isUpdating" expaned rounded type="is-primary"
						  @click="updateSystem"/>
			</div>
		</footer>
		<!-- Modal-Card Footer End -->
	</div>
</template>

<script>
import {marked} from 'marked'

export default {
	props: {
		changeLog: {
			type: String,
			default: ""
		},
	},
	data() {
		return {
			timer: 0,
			updateTimer: 0,
			isUpdating: false,
			markdown: ``,
			updateLogs: ``
		};
	},
	computed: {
		markdownToHtml() {
			return marked.parse(this.changeLog);
		},
		updateMarkdownHtml() {

			return marked.parse(this.updateLogs);
		}
	},
	methods: {
		/**
		 * @description: Update System Version and check update state
		 * @return {*} void
		 */
		async updateSystem() {
			this.isUpdating = true;
			await this.$api.sys.updateNivaroOS();
			// this.checkUpdateState();
			this.getUpdateLogs()
		},

		/**
		 * @description: Get update logs
		 * @return {*} void
		 */
		getUpdateLogs() {
			this.updateTimer = setInterval(() => {
				this.$api.file.getContent(`/var/log/nivaroos/upgrade.log`).then(res => {

					this.updateLogs = res.data.data;
					if (this.updateLogs.includes(`NivaroOS upgrade successfully`)) {
						localStorage.setItem('is_update', 'true')
						clearInterval(this.updateTimer);
						setTimeout(() => {
							this.$router.replace({
								path: '/logout'
							})
						}, 1000);
					} else if (this.updateLogs.includes(`NivaroOS upgrade failed`)) {
						this.$buefy.toast.open({
							message: this.$t(`There seems to be a problem with the upgrade process, please try again!`),
							type: 'is-danger'
						})
						clearInterval(this.updateTimer);
						setTimeout(() => {
							this.isUpdating = false;
						}, 1000);

					}
				})
			}, 200);
		},
		/**
		 * @description: check update state if is_need is false then reload page
		 * @return {*} void
		 */
		checkUpdateState() {
			this.timer = setInterval(() => {
				this.$api.sys.getVersion().then(res => {
					if (res.data.success == 200) {
						if (!res.data.data.is_need) {
							clearInterval(this.timer);
							location.reload();
						}
					}
				})
			}, 3000)
		},
	},
}
</script>

<style lang="scss">
.fixed-height {
	max-height: 20rem;
	overflow-y: auto;
}

.update-info-container {
	line-height: 1.5rem;
	border-radius: var(--radius-xs);
	overflow: hidden;
	min-height: 20rem;

	h1,
	h2,
	h3,
	h4,
	h5,
	h6 {
		font-weight: bold;
		margin-bottom: var(--space-2);
	}

	h1 {
		font-size: var(--font-3xl);
	}

	h2 {
		font-size: var(--font-2xl);
	}

	h3 {
		font-size: var(--font-lg);
	}

	h4 {
		font-size: var(--font-md);
	}

	h5 {
		font-size: var(--font-sm);
	}

	h6 {
		font-size: var(--font-2xs);
	}

	ul {
		margin-bottom: var(--space-2);

		li {
			list-style: disc;
			margin-left: var(--space-4);
		}
	}
}
</style>
