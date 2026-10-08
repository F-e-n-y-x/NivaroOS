<!-- src/apps/download-station/DsBrowser.vue -->
<!-- Download Station's browser. Normally the full browser: a real Chromium
     on the server, streamed here (rb/DsRemoteBrowser.vue). When it isn't
     installed or can't run on this box - or the user prefers it - Lite mode
     (DsLiteBrowser.vue, the rewriting proxy) takes its place, with a banner
     that says why and offers the way back. -->
<template>
	<div class="ds-browser-host">
		<div v-if="mode === 'checking'" class="browser-checking" role="status">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-24px"></b-icon>
		</div>
		<ds-remote-browser v-else-if="mode === 'full'" ref="impl" :visible="visible" :adblock-enabled="adblockEnabled" :allowed-sites="allowedSites"
			@toggle-adblock="$emit('toggle-adblock')" @trust-site="(h, on) => $emit('trust-site', h, on)" @open-adblock="$emit('open-adblock')"
			@fallback="onFallback"></ds-remote-browser>
		<template v-else>
			<div class="lite-banner" role="status">
				<b-icon :icon="installing ? 'loading' : 'feather'" :custom-class="installing ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
				<span class="lite-text">{{ bannerText }}</span>
				<button v-if="canUseFull" class="ds-secondary-btn" type="button" @click="useFull">{{ $t('Use the full browser') }}</button>
				<button v-else-if="canInstall && !installing" class="ds-primary-btn" type="button" :disabled="installBusy" @click="install">
					<b-icon icon="download" custom-size="mdi-16px"></b-icon>{{ reason === 'no_chrome' || installFailed ? $t('Try installing again') : $t('Install full browser') }}
				</button>
			</div>
			<ds-lite-browser ref="impl" :visible="visible" :adblock-enabled="adblockEnabled" @toggle-adblock="$emit('toggle-adblock')"></ds-lite-browser>
		</template>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { escapeHtml } from '@/utils/escapeHtml'
import DsLiteBrowser from './DsLiteBrowser.vue'
import DsRemoteBrowser from './rb/DsRemoteBrowser.vue'

const PREF_KEY = 'ds.browser.lite'
const INSTALL_POLL_MS = 4000

function readPref() {
	try {
		return localStorage.getItem(PREF_KEY) === '1'
	} catch (e) {
		return false
	}
}

function writePref(lite) {
	try {
		if (lite) localStorage.setItem(PREF_KEY, '1')
		else localStorage.removeItem(PREF_KEY)
	} catch (e) {}
}

export default {
	emits: ['open-adblock', 'toggle-adblock', 'trust-site'],
	name: 'ds-browser',
	components: { DsLiteBrowser, DsRemoteBrowser },
	props: {
		visible: { type: Boolean, default: false },
		adblockEnabled: { type: Boolean, default: false },
		allowedSites: { type: Array, default: () => [] }
	},
	data() {
		return {
			mode: 'checking',
			status: null,
			reason: '',
			installBusy: false,
			installing: false,
			installFailed: false,
			pending: null
		}
	},
	computed: {
		fullAvailable() {
			return !!(this.status && this.status.available)
		},
		canUseFull() {
			return this.fullAvailable && (this.reason === 'user' || this.reason === 'crashed')
		},
		canInstall() {
			return !!(this.status && this.status.can_install) && ['not_installed', 'no_chrome', 'unreachable'].includes(this.reason)
		},
		bannerText() {
			if (this.installing) return this.$t('Installing the full browser (Chromium and uBlock Origin Lite) - this takes a minute or two. Lite mode works in the meantime.')
			if (this.installFailed) return this.$t('The full browser could not be installed - see the install log with: journalctl -u nivaroos-ds-browser-install. Lite mode is used for now.')
			switch (this.reason) {
				case 'user':
					return this.$t('Lite mode: pages go through a rewriting proxy. It is lighter on the server, but many sites (Google, sign-ins, video) work badly in it.')
				case 'crashed':
					return this.$t('The full browser kept crashing, so Lite mode is on for now.')
				case 'sandbox':
					return this.$t('This system can\'t give Chromium its security sandbox, so the full browser is off and Lite mode is used.')
				case 'low_memory':
					return this.$t('This box has less than 2 GB of memory, so Download Station uses its Lite browser.')
				case 'no_chrome':
					return this.$t('Chromium could not be installed on this box, so Download Station uses its Lite browser.')
				case 'unreachable':
					return this.$t('The full browser isn\'t responding right now, so Lite mode is used.')
				default:
					return this.$t('The full browser isn\'t installed on this box yet, so Download Station uses its Lite browser.')
			}
		}
	},
	async created() {
		await this.check()
	},
	beforeUnmount() {
		clearTimeout(this.pollTimer)
	},
	methods: {
		async check() {
			try {
				this.status = await downloadSidecar.rbStatus()
			} catch (e) {
				this.status = { available: false, reason: 'unreachable' }
			}
			this.installing = this.status.install === 'installing'
			this.installFailed = this.status.install === 'failed'
			if (this.status.available && !readPref()) {
				this.setMode('full', '')
			} else {
				this.setMode('lite', this.status.available ? 'user' : this.status.reason || 'not_installed')
			}
			if (this.installing) this.pollInstall()
		},
		setMode(mode, reason) {
			this.mode = mode
			this.reason = reason
			if (this.pending) {
				const p = this.pending
				this.pending = null
				this.$nextTick(() => this.$refs.impl && this.$refs.impl.openUrl(p.url, p.newTab))
			}
		},
		onFallback(reason) {
			if (reason === 'user') writePref(true)
			this.setMode('lite', reason === 'busy' || reason === 'error' ? 'unreachable' : reason)
		},
		useFull() {
			writePref(false)
			this.setMode('full', '')
		},
		async install() {
			this.installBusy = true
			try {
				await downloadSidecar.rbInstall()
				this.installing = true
				this.installFailed = false
				this.pollInstall()
			} catch (e) {
				this.$buefy.toast.open({ message: escapeHtml(this.$t('Could not start the install: {error}', { error: e.message })), type: 'is-danger' })
			} finally {
				this.installBusy = false
			}
		},
		pollInstall() {
			clearTimeout(this.pollTimer)
			this.pollTimer = setTimeout(async () => {
				let st
				try {
					st = await downloadSidecar.rbStatus()
				} catch (e) {
					st = null
				}
				if (st) {
					this.status = st
					if (st.available) {
						this.installing = false
						writePref(false)
						this.$buefy.toast.open({ message: escapeHtml(this.$t('The full browser is ready')), type: 'is-success' })
						this.setMode('full', '')
						return
					}
					if (st.install !== 'installing') {
						this.installing = false
						this.installFailed = st.install === 'failed' || st.reason === 'no_chrome'
						this.reason = st.reason || this.reason
						return
					}
				}
				this.pollInstall()
			}, INSTALL_POLL_MS)
		},
		// Public: used by the app shell to open a URL ("Open Browser").
		openUrl(url, inNewTab) {
			if (this.mode === 'checking' || !this.$refs.impl) {
				this.pending = { url, newTab: inNewTab }
				return
			}
			this.$refs.impl.openUrl(url, inNewTab)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './ds-common.scss';

.ds-browser-host {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 0;

	> :last-child {
		flex: 1 1 auto;
		min-height: 0;
	}
}

.browser-checking {
	display: flex;
	align-items: center;
	justify-content: center;
	height: 100%;
	color: var(--theme-text-muted, #94a3b8);
}

.lite-banner {
	flex: 0 0 auto !important;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	background: var(--color-warning-soft, #fef3c7);
	color: var(--color-warning-fg, #92400e);
	font-size: var(--font-xs);
	line-height: 1.4;
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));

	.lite-text {
		flex: 1 1 auto;
		min-width: 0;
	}

	.ds-primary-btn,
	.ds-secondary-btn {
		height: 1.75rem;
		font-size: var(--font-xs);
	}
}
</style>
