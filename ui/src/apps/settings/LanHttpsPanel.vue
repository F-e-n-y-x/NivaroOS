<template>
	<div class="setting-card">
		<div class="setting-row">
			<b-icon class="row-icon" icon="lock-outline" pack="mdi" size="is-20"></b-icon>
			<div class="row-label">
				<div class="setting-title">{{ $t('Serve HTTPS on your network') }}</div>
				<div class="setting-desc">{{ $t('Encrypts the dashboard and the app on your home network with a certificate this server makes for itself. http keeps working. Each browser needs the certificate installed once.') }}</div>
			</div>
			<div class="row-control">
				<b-switch :key="switchKey" :model-value="!!(status && status.enabled)" class="is-flex-direction-row-reverse mr-0" type="is-primary"
					:loading="busy" :disabled="!status" :aria-label="$t('Serve HTTPS on your network')" @update:modelValue="save($event, false)"></b-switch>
			</div>
		</div>

		<template v-if="status && status.enabled">
			<div v-if="status.running" class="setting-row">
				<b-icon class="row-icon" icon="link-variant" pack="mdi" size="is-20"></b-icon>
				<div class="row-label setting-desc">
					<a :href="secureUrl" target="_blank" rel="noopener noreferrer">{{ secureUrl }}</a>
				</div>
			</div>
			<div class="setting-row">
				<b-icon class="row-icon" icon="certificate-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Trust this server') }}</div>
					<div class="setting-desc">
						{{ $t('Install the certificate on each device: open') }}
						<a href="/trust" target="_blank" rel="noopener noreferrer">{{ trustUrl }}</a>
						{{ $t('there. The app asks by itself.') }}
					</div>
					<div class="setting-desc fingerprint">SHA-256 {{ status.fingerprint }}</div>
				</div>
				<div class="row-control">
					<button class="icon-button" type="button" :title="$t('Copy fingerprint')" :aria-label="$t('Copy fingerprint')" @click="copyFingerprint">
						<b-icon icon="content-copy" pack="mdi" size="is-16"></b-icon>
					</button>
				</div>
			</div>
			<div class="setting-row">
				<b-icon class="row-icon" icon="arrow-decision-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Send browsers to HTTPS') }}</div>
					<div class="setting-desc">{{ canRedirect || status.redirect
						? $t('Pages opened with http:// switch to https://. The app, scripts and remote access keep working over http.')
						: $t('Open the secure address above first, so you know this browser trusts the certificate.') }}</div>
				</div>
				<div class="row-control">
					<b-switch :key="switchKey" :model-value="!!status.redirect" class="is-flex-direction-row-reverse mr-0" type="is-primary"
						:disabled="busy || (!status.redirect && !canRedirect)" :aria-label="$t('Send browsers to HTTPS')" @update:modelValue="save(true, $event)"></b-switch>
				</div>
			</div>
		</template>
		<p v-if="error || (status && status.error)" class="error-note">{{ error || status.error }}</p>
	</div>
</template>

<script>
import { copyText } from '@/apps/terminal/termClipboard'
import { httpsUrl, canEnableRedirect } from '@/apps/settings/lanHttps'

export default {
	name: 'lan-https-panel',
	data() {
		return { status: null, busy: false, error: '', switchKey: 0 }
	},
	computed: {
		secureUrl() {
			return httpsUrl(window.location, this.status && this.status.port)
		},
		trustUrl() {
			return `${window.location.host}/trust`
		},
		canRedirect() {
			return canEnableRedirect(window.location, this.status)
		}
	},
	created() {
		this.$api.sys.getLanHttps().then(res => {
			if (res.data.success === 200) this.status = res.data.data
		}).catch(() => {
			this.error = this.$t("Couldn't read the HTTPS settings")
		})
	},
	methods: {
		save(enabled, redirect) {
			this.busy = true
			this.error = ''
			this.$api.sys.setLanHttps({ enabled, redirect }).then(res => {
				if (res.data.success === 200) this.status = res.data.data
			}).catch(e => {
				const d = e && e.response && e.response.data
				if (d && d.data) this.status = d.data
				this.error = (d && d.message) || this.$t("Couldn't change HTTPS")
				this.switchKey++
			}).finally(() => {
				this.busy = false
			})
		},
		copyFingerprint() {
			copyText(this.status.fingerprint).then(done => {
				this.$buefy.toast.open({ message: done ? this.$t('Fingerprint copied') : this.$t('Copy failed - select it instead'), type: done ? 'is-info' : 'is-warning', duration: 1500 })
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.fingerprint {
	font-family: ui-monospace, Menlo, Consolas, monospace;
	font-size: var(--font-xs);
	word-break: break-all;
	user-select: all;
}
</style>
