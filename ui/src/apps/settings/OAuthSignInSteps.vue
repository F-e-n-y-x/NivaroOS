<template>
	<div class="oauth-steps">
		<div class="step">
			<span class="step-number">1</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Choose how to sign in') }}</p>
				<div class="segmented-control" role="radiogroup">
					<button type="button" class="segmented-option" role="radio" :aria-checked="!own" :class="{ active: !own }" @click="setOwn(false)">
						{{ $t("rclone's app") }}
					</button>
					<button type="button" class="segmented-option" role="radio" :aria-checked="own" :class="{ active: own }" @click="setOwn(true)">
						{{ $t('Your own app') }}
					</button>
				</div>
				<p class="step-help">
					<template v-if="!own">{{ $t("Quickest to set up. rclone's app is shared by every rclone user, so {provider} slows it down - fine for small folders.", { provider }) }}</template>
					<template v-else>{{ $t('Your own {provider} app gets its own speed limits - much faster for big backups like photo libraries. It takes about 10 minutes to create, once.', { provider }) }}</template>
					<a :href="guideUrl" target="_blank" rel="noopener">{{ $t('How to create one') }}</a>
				</p>
				<template v-if="own">
					<b-field :label="$t('Client ID')" :type="idError ? 'is-danger' : ''" :message="idError">
						<b-input v-model.trim="clientId" size="is-small" autocomplete="off" spellcheck="false" @input="emit"></b-input>
					</b-field>
					<b-field :label="$t('Client secret')" :type="secretError ? 'is-danger' : ''" :message="secretError">
						<b-input v-model.trim="clientSecret" type="password" password-reveal size="is-small" autocomplete="off" @input="emit"></b-input>
					</b-field>
				</template>
			</div>
		</div>

		<div class="step">
			<span class="step-number">2</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Sign in to {provider}', { provider }) }}</p>
				<p class="step-help">{{ $t('Run this on the server, open the link it prints, and sign in.') }}</p>
				<code class="authorize-cmd">{{ command }}</code>
				<a class="advanced-toggle" :class="{ 'is-disabled': !credsOk }" @click="credsOk && openTerminal()">{{ $t('Run it in Terminal') }}</a>
			</div>
		</div>

		<div class="step">
			<span class="step-number">3</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Paste what it prints back') }}</p>
				<b-input v-model="token" type="textarea" size="is-small" rows="3" :placeholder="$t('Paste it here')" @input="emit"></b-input>
			</div>
		</div>
	</div>
</template>

<script>
// Sign-in steps for OAuth providers (Drive, Dropbox, OneDrive): rclone's
// shared app, or the user's own client ID/secret. Emits `change` with the
// rclone params to save ({ token, client_id, client_secret }) or null
// while incomplete. Empty client_id/secret mean rclone's app, so a
// reconnect can switch back.

const GUIDE = 'https://github.com/F-e-n-y-x/NivaroOS/blob/master/docs/guides/cloud-own-app.md'
// The command runs in a shell: allow only what real client IDs/secrets use.
const SAFE = /^[A-Za-z0-9._~-]+$/

export default {
	name: 'OAuthSignInSteps',
	props: {
		type: { type: String, required: true }, // rclone backend: drive, dropbox, onedrive
		provider: { type: String, required: true }, // display name
		ownClient: { type: Boolean, default: false }, // the account already uses its own app
	},
	emits: ['change'],
	data() {
		return { own: this.ownClient, clientId: '', clientSecret: '', token: '' }
	},
	computed: {
		guideUrl() {
			return `${GUIDE}#${this.type === 'drive' ? 'google-drive' : this.type}`
		},
		idError() {
			return this.clientId && !SAFE.test(this.clientId) ? this.$t('That does not look like a client ID') : ''
		},
		secretError() {
			return this.clientSecret && !SAFE.test(this.clientSecret) ? this.$t('That does not look like a client secret') : ''
		},
		credsOk() {
			return !this.own || (SAFE.test(this.clientId) && SAFE.test(this.clientSecret))
		},
		command() {
			if (!this.own) return `rclone authorize "${this.type}"`
			return `rclone authorize "${this.type}" "${this.clientId || 'CLIENT_ID'}" "${this.clientSecret || 'CLIENT_SECRET'}"`
		},
	},
	methods: {
		setOwn(v) {
			this.own = v
			this.emit()
		},
		emit() {
			const token = this.token.trim()
			if (!token || !this.credsOk) return this.$emit('change', null)
			this.$emit('change', {
				token,
				client_id: this.own ? this.clientId : '',
				client_secret: this.own ? this.clientSecret : '',
			})
		},
		openTerminal() {
			// `rclone authorize` always prints a 127.0.0.1:53682 link (hardcoded
			// upstream), and the provider's redirect back after sign-in is
			// hardcoded to that same literal address too - unreachable unless
			// the browser is on this exact machine. A small always-on proxy
			// (services/local-storage/pkg/oauthproxy) listens on port 53682
			// on this box's real LAN address(es), same port rclone uses, just
			// not on loopback - so swapping only the host (not the port) in
			// whatever 127.0.0.1:53682 link/redirect shows up, here or by
			// hand in the URL bar, lands somewhere real.
			const host = window.location.hostname
			const args = this.own ? ` "${this.clientId}" "${this.clientSecret}"` : ''
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { initCommand: `rclone authorize "${this.type}"${args} 2>&1 | sed -u "s/127\\.0\\.0\\.1:53682/${host}:53682/g"` },
			})
		},
	},
}
</script>

<style lang="scss" scoped>
.step {
	display: flex;
	gap: var(--space-3);
	margin: var(--space-1) 0 var(--space-3);

	&:last-child {
		margin-bottom: 0;
	}
}
.step-number {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.35rem;
	height: 1.35rem;
	border-radius: 50%;
	background: var(--color-primary-soft, rgba(50, 115, 220, 0.1));
	color: var(--color-primary-fg);
	font-size: var(--font-xs);
	font-weight: 600;
}
.step-body {
	flex: 1 1 auto;
	min-width: 0;
}
.step-title {
	font-size: var(--font-sm);
	font-weight: 600;
	margin-bottom: var(--space-1);
}
.step-help {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	margin: var(--space-2) 0;

	a {
		color: var(--color-primary-fg);
		white-space: nowrap;
	}
}
.authorize-cmd {
	display: block;
	background: var(--theme-card-subtle, #f8fafc);
	border-radius: var(--radius-sm);
	padding: var(--space-2);
	font-size: var(--font-xs);
	margin-bottom: var(--space-2);
	overflow-wrap: anywhere;
}
.advanced-toggle {
	display: inline-block;
	font-size: var(--font-xs);
	color: var(--color-primary-fg);
	cursor: pointer;

	&.is-disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
}
</style>
