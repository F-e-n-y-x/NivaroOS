<template>
	<div class="app-editor-container">
		<!-- Top Hero: Link Identity & Live Icon Preview -->
		<div class="editor-hero-section">
			<div class="hero-icon-preview">
				<b-image :key="icon" :src="icon" :src-fallback="require('@/assets/img/app-icons/default.svg')"
					:alt="$t('Icon preview')" class="hero-img" ratio="1by1"></b-image>
			</div>

			<div class="hero-form-fields">
				<div class="field-row">
					<label class="field-lbl" for="weblink-name">{{ $t('App Name') }}<span class="required-mark" aria-hidden="true">*</span></label>
					<b-input id="weblink-name" v-model="name" :disabled="disableEditName" :placeholder="$t('Customize your APP name')"
						size="is-small" icon="tag-outline" expanded required maxlength="60"></b-input>
				</div>

				<div class="field-row mt-2">
					<label class="field-lbl" for="weblink-address">{{ $t('Address') }}<span class="required-mark" aria-hidden="true">*</span></label>
					<b-autocomplete id="weblink-address" ref="inputs" v-model="hostname" :data="filteredDataObj" :open-on-focus="true"
						:placeholder="$t('Local or public URL')" append-to-body field="hostname" max-height="120px"
						size="is-small" icon="link-variant" expanded required
						aria-describedby="weblink-address-hint"
						@keyup.native.enter="connect">
					</b-autocomplete>
					<p v-if="hostname && !isHostValid" id="weblink-address-hint" class="field-hint is-error" role="alert">
						<i class="mdi mdi-alert-circle-outline mr-1" aria-hidden="true"></i>
						{{ $t('Enter a full address with a host, e.g. {example}', { example: exampleUrl }) }}
					</p>
					<p v-else id="weblink-address-hint" class="field-hint">
						<i class="mdi mdi-information-outline mr-1" aria-hidden="true"></i>
						{{ $t('Example: {example}', { example: exampleUrl }) }}
					</p>
				</div>
			</div>
		</div>

		<!-- Body -->
		<section class="editor-body">
			<div class="field-row">
				<label class="field-lbl" for="weblink-icon">{{ $t('Icon URL') }}</label>
				<b-input id="weblink-icon" v-model="icon" :placeholder="$t('Your custom icon URL')" size="is-small"
					icon="image-outline" expanded></b-input>
			</div>
		</section>

		<!-- Footer Actions -->
		<footer class="editor-footer-bar">
			<div class="is-flex-grow-1"></div>
			<div class="footer-btn-group">
				<b-button rounded @click="$emit('close')">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" rounded :loading="isLoading" :disabled="!canConnect" @click="connect">
					<i class="mdi mdi-check mr-1" aria-hidden="true"></i>{{ isEditing ? $t('Save') : $t('Connect') }}
				</b-button>
			</div>
		</footer>

		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import Business_ShowNewAppTag from "@/mixins/app/Business_ShowNewAppTag";
import Business_LinkApp from "@/mixins/app/Business_LinkApp";
import { confirmWindowMixin } from '@/mixins/confirmWindow';
import { apiErrorHtml } from '@/mixins/app/apiError';
import { escapeHtml } from '@/utils/escapeHtml';
import events from '@/events/events'

const EXAMPLE_URL = 'http://192.168.1.10:8080'

// Scheme-less input ("192.168.1.10:8080", "//nas:5000") is treated as http.
function normalizeLinkUrl(value) {
	const raw = String(value || '').trim()
	if (!raw) return ''
	if (/^[a-z][a-z0-9+.-]*:\/\//i.test(raw)) return raw
	return raw.startsWith('//') ? `http:${raw}` : `http://${raw}`
}

// A usable link needs a host: "http://" alone (the prefilled value) or
// "https://:8080" must not pass.
function isValidLinkHost(value) {
	const withScheme = normalizeLinkUrl(value)
	if (!withScheme) return false
	try {
		const url = new URL(withScheme)
		if (!/^https?:$/.test(url.protocol)) return false
		return !!url.hostname && url.hostname.length > 0
	} catch (e) {
		return false
	}
}

export default {
	mixins: [Business_ShowNewAppTag, Business_LinkApp, confirmWindowMixin],
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
			exampleUrl: EXAMPLE_URL,
		}
	},
	computed: {
		filteredDataObj() {
			return this.$store.state.networkStorage
		},
		isHostValid() {
			return isValidLinkHost(this.hostname)
		},
		trimmedName() {
			return String(this.name || '').trim()
		},
		canConnect() {
			return !!this.trimmedName && this.isHostValid && !this.isLoading
		},
		isEditing() {
			return !!this.linkName
		},
		disableEditName() {
			return !!this.linkName
		},
	},
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
		async connect() {
			if (!this.canConnect) return
			this.isLoading = true
			const name = this.trimmedName
			const hostname = normalizeLinkUrl(this.hostname)
			let listLinkApp
			try {
				listLinkApp = await this.getLinkAppList()
			} catch (err) {
				// Never save after a failed read - it would drop every other link.
				this.isLoading = false
				this.$buefy.toast.open({
					message: apiErrorHtml(err, this.$t('Unable to load the web links')),
					type: 'is-danger'
				})
				return
			}
			const existing = listLinkApp.find(item => item.name === name)
			const apply = () => {
				let next
				if (existing) {
					next = listLinkApp.map(item => item.name === name ? { ...item, hostname, icon: this.icon } : item)
				} else {
					next = listLinkApp.concat({
						hostname,
						name,
						icon: this.icon,
						app_type: "LinkApp",
						status: "running",
					})
				}
				return this.saveLinkApp(next, !existing ? name : null)
			}
			if (existing && !this.isEditing) {
				// Adding a new link whose name is already taken: ask first
				// instead of silently overwriting the other link.
				this.isLoading = false
				this.confirmWindow({
					title: this.$t('Replace web link'),
					message: this.$t('A web link named {name} already exists. Replace its address and icon?', { name: `<b>${escapeHtml(name)}</b>` }),
					type: 'is-warning',
					confirmText: this.$t('Replace'),
					cancelText: this.$t('Cancel'),
					onConfirm: () => {
						this.isLoading = true
						return apply()
					}
				})
				return
			}
			await apply()
		},

		saveLinkApp(data, newName) {
			let json = JSON.stringify(data)
			return this.$api.users.saveLinkAppDetail(json).then((res) => {
				if (res.data.success == 200) {
					if (newName) this.addIdToSessionStorage(newName);
					this.$messageBus('apps_external')
					this.$EventBus.$emit(events.GET_APP_LIST)
					this.$emit('updateState')
					this.$emit('close')
				} else {
					this.$buefy.toast.open({
						message: escapeHtml(res.data.message || this.$t('Unable to save the web link')),
						type: 'is-warning'
					})
				}
			}).catch((err) => {
				this.$buefy.toast.open({
					message: apiErrorHtml(err, this.$t('Unable to save the web link')),
					type: 'is-danger'
				})
			}).finally(() => {
				this.isLoading = false;
			})
		},

	},
}
</script>

<style lang="scss" scoped>
.app-editor-container {
	position: relative;
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
		color: var(--color-danger-fg);
		margin-left: 2px;
	}
}

.field-hint {
	display: flex;
	align-items: center;
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	margin: 0;

	&.is-error {
		color: var(--color-danger-fg);
	}
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
