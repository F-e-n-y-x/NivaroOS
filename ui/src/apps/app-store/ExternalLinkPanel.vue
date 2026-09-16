<template>
	<div class="app-editor-container">
		<!-- Top Hero: Link Identity & Live Icon Preview -->
		<div class="editor-hero-section">
			<div class="hero-icon-preview">
				<b-image :key="icon" :src="icon" :src-fallback="require('@/assets/img/app-icons/default.svg')"
					class="hero-img" ratio="1by1"></b-image>
			</div>

			<div class="hero-form-fields">
				<div class="field-row">
					<label class="field-lbl">{{ $t('App Name') }}<span class="required-mark">*</span></label>
					<b-input v-model="name" :disabled="disableEditName" :placeholder="$t('Customize your APP name')"
						size="is-small" icon="tag-outline" expanded></b-input>
				</div>

				<div class="field-row mt-2">
					<label class="field-lbl">{{ $t('Address') }}<span class="required-mark">*</span></label>
					<b-autocomplete ref="inputs" v-model="hostname" :data="filteredDataObj" :open-on-focus="true"
						:placeholder="$t('Local URL,Pblic URL')" append-to-body field="hostname" max-height="120px"
						size="is-small" icon="link-variant" expanded>
					</b-autocomplete>
					<p v-if="!state_hostIsExist" class="field-hint">
						<i class="mdi mdi-information-outline mr-1"></i>
						{{ $t('Eg: //192.168.1.1:5000 or https://www.google.com') }}
					</p>
				</div>
			</div>
		</div>

		<!-- Body -->
		<section class="editor-body">
			<div class="field-row">
				<label class="field-lbl">{{ $t('Icon URL') }}</label>
				<b-input v-model="icon" :placeholder="$t('Your custom icon URL')" size="is-small"
					icon="image-outline" expanded></b-input>
			</div>
		</section>

		<!-- Footer Actions -->
		<footer class="editor-footer-bar">
			<div class="is-flex-grow-1"></div>
			<div class="footer-btn-group">
				<b-button rounded @click="$emit('close')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" rounded :loading="isLoading" :disabled="!name || !hostname" @click="connect">
					<i class="mdi mdi-check mr-1"></i>{{ $t('Connect') }}
				</b-button>
			</div>
		</footer>
	</div>
</template>

<script>
import Business_ShowNewAppTag from "@/mixins/app/Business_ShowNewAppTag";
import Business_LinkApp from "@/mixins/app/Business_LinkApp";
import events from '@/events/events'


export default {
	mixins: [Business_ShowNewAppTag, Business_LinkApp],
	props: {
		linkName: {
			type: String,
			default: "",
		},
		linkHost: {
			type: String,
			default: "",
		},
		linkIcon: {
			type: String,
			default: "",
		}
	},
	data() {
		return {
			hostname: "",
			name: "",
			title: {
				en_us: "",
			},
			icon: "",
			isLoading: false,
		}
	},
	computed: {
		filteredDataObj() {
			return this.$store.state.networkStorage
		},
		state_hostIsExist() {
			return this.hostname === "" ? false : true
		},
		disableEditName() {
			return !!this.linkName
		},
	},
	watch: {},
	created() {
		this.hostname = this.linkHost || "http://"
		this.name = this.linkName
		this.icon = this.linkIcon
	},

	mounted() {
		this.$nextTick(() => {
			if (this.$refs.inputs) this.$refs.inputs.focus()
		})
	},
	methods: {
		/**
		 * @description: Validate form async
		 * @param {Object} ref ref of component
		 * @return {Boolean}
		 */
		async checkStep(ref) {
			let isValid = await ref.validate()
			console.log(ref)
			return isValid
		},

		connect() {
			if (!this.name || !this.hostname) return
			this.isLoading = true
			this.getLinkAppList().then(async listLinkApp => {
				if (!listLinkApp.find((item) => {
					if (item.name === this.name) {
						item.hostname = this.hostname
						item.icon = this.icon
						return true
					}
				})) {
					listLinkApp = listLinkApp.concat({
						hostname: this.hostname,
						name: this.name,
						icon: this.icon,
						app_type: "LinkApp",
						status: "running",
					})
					this.addIdToSessionStorage(this.name);
				}
				this.saveLinkApp(listLinkApp)
			})
		},

		getLinkAppByHost() {
			this.$api.sys.getProxyRequestContent(this.hostname).then((res) => {
				this.isLoading = false;
				if (res.status == 200) {
					this.name = ""
					this.icon = "https://avatars.githubusercontent.com/u/91336243?s=200&v=4"
				} else {
					this.$buefy.toast.open({
						message: res.data.message,
						type: 'is-warning'
					})
				}
			}).catch((err) => {
				this.isLoading = false;
				this.$buefy.toast.open({
					message: err.response.data.message || "NOT FOUND",
					type: 'is-danger'
				})
			})
		},

		saveLinkApp(data) {
			let json = JSON.stringify(data)
			this.$api.users.saveLinkAppDetail(json).then((res) => {
				this.isLoading = false;
				if (res.data.success == 200) {
					let stor = res.data.data
					if (stor === "") {
						stor = []
					}
					this.$messageBus('apps_external')
					this.$EventBus.$emit(events.GET_APP_LIST)
					this.$emit('updateState')
					this.$emit('close')
				} else {
					this.$buefy.toast.open({
						message: res.data.message,
						type: 'is-warning'
					})
				}
			}).catch((err) => {
				this.isLoading = false;
				this.$buefy.toast.open({
					message: err.response.data.message || "NOT FOUND",
					type: 'is-danger'
				})
			})
		},

	},
}
</script>

<style lang="scss" scoped>
.app-editor-container {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #0f172a);
	overflow: hidden;
}

.editor-hero-section {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-4);
	padding: var(--space-4) var(--space-5);
	background: var(--theme-titlebar-bg, #ffffff);
	border-bottom: 1px solid var(--theme-card-border, #e2e8f0);
}

.hero-icon-preview {
	position: relative;
	width: 60px;
	height: 60px;
	min-width: 60px;
	min-height: 60px;
	flex-shrink: 0;
	overflow: hidden;
	border-radius: var(--radius-card);
	background: var(--theme-card-subtle, #f8fafc);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	box-shadow: var(--shadow-md);

	.hero-img {
		width: 100%;
		height: 100%;
	}
}

.hero-form-fields {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.editor-body {
	flex: 1 1 auto;
	overflow-y: auto;
	padding: var(--space-4) var(--space-5);
}

.field-row {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.field-lbl {
	font-size: var(--font-xs);
	font-weight: 600;
	color: var(--theme-text-secondary, #475569);

	.required-mark {
		color: #dc2626;
		margin-left: 2px;
	}
}

.field-hint {
	display: flex;
	align-items: center;
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	margin: 0;
}

.editor-footer-bar {
	flex-shrink: 0;
	padding: var(--space-3) var(--space-5);
	background: var(--theme-card-bg, #ffffff);
	border-top: 1px solid var(--theme-card-border, #e2e8f0);
	display: flex;
	align-items: center;
}

.footer-btn-group {
	display: flex;
	gap: var(--space-2);
}
</style>

<style lang="scss">
.network-storage-modal {
	.field-label {
		text-align: left;
	}
}

.smb-media {
	color: var(--theme-text-muted, #999);
}
</style>
