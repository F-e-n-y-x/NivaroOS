<template>
	<div class="modal-card">
		<!-- Modal-Card Header Start -->
		<header class="modal-card-head">
			<div class="is-flex-grow-1">
				<h3 class="title is-3">{{ $t('Share NivaroOS') }}</h3>
			</div>
			<div>
				<button class="delete" type="button" @click="$emit('close')"/>
			</div>
		</header>
		<!-- Modal-Card Header End -->
		<!-- Modal-Card Body Start -->
		<section class="modal-card-body ">
			<div class="node-card  mt-5 mb-5">

				<div>
					<div class=" is-size-14px">{{
						$t('Please invite more friends who are concerned about family and data privacy to join and use NivaroOS.')
						}}
					</div>

					<b-image :src="require('@/assets/img//social/share_img.png')"
							 class="share-img-shadow share-img"></b-image>
				</div>

				<div class="buttons is-justify-content-center mb-6 mt-4">
					<ShareNetwork v-for="site in shareSites" :key="site" :description="shareTitle" :img="githubUrl"
								  :network="site"
								  :title="shareTitle" :url="githubUrl" hashtags="homecloud,opensource">
						<b-button :icon-left="site" :type="`is-${site}`" class="ml-3 mr-3" icon-pack="casa">
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
			githubUrl: `https://raw.githubusercontent.com/IceWhaleTech/logo/main/casaos/0.4/casaos_social_share.png`,
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
.share-img-shadow {
	box-shadow: var(--shadow-lg);
}

.share-img {
	margin: var(--space-12) 6rem var(--space-16) 6rem;
}

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
		font-size: 2em;
	}

	h2 {
		font-size: 1.5em;
	}

	h3 {
		font-size: 1.17em;
	}

	h4 {
		font-size: 1em;
	}

	h5 {
		font-size: 0.83em;
	}

	h6 {
		font-size: 0.67em;
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
