
<template>
	<div class="modal-card">
		<!-- Modal-Card Header Start -->
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-header">{{$t('Update completed')}}</h3>
			</div>
			<b-icon class="close-button" icon="close-outline" pack="casa" @click.native="$emit('close');" />
		</header>
		<!-- Modal-Card Header End -->
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body ">
			<div class="node-card  mt-5 mb-5">
				<div class="update-info-container  is-size-14px " v-dompurify-html="markdownToHtml"></div>
				<div class="mt-2rem">
					<h3 class="title is-5 mb-2">{{ $t('Let more friends know') }}</h3>
					<div class=" is-size-14px">{{ $t('Please share to friends who are concerned about family and data privacy to join and use NivaroOS.') }}
					</div>
				</div>

				<div class="buttons is-justify-content-center mb-6 mt-4">
					<ShareNetwork v-for="site in shareSites" :network="site" :key="site" :url="githubUrl"
						:title="shareTitle" hashtags="homecloud,opensource">
						<b-button icon-pack="casa" :icon-left="site" :type="`is-${site}`" class="ml-3 mr-3">
							Share
						</b-button>
					</ShareNetwork>
				</div>

			</div>
		</section>
		<!-- Modal-Card Body End -->
	</div>
</template>

<script>
import { marked } from 'marked'

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
			markdown: ``,
			githubUrl: `https://github.com/F-e-n-y-x/NivaroOS`,
			shareTitle: `I'm using NivaroOS, a simple, easy-to-use, elegant open-source home cloud system, try it like me.`,
			shareSites: [
				'facebook',
				'twitter',
				'reddit'
			]
		};
	},
	computed: {
		markdownToHtml() {
			return marked.parse(this.changeLog);
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
						clearInterval(this.updateTimer);
						setTimeout(() => {
							location.reload();
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
.update-info-container {
	overflow: hidden;
	min-height: 20rem;
	background: var(--theme-card-subtle, #f8f8f8);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.1));
	border-radius: var(--radius-card);
	padding: var(--space-6);

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