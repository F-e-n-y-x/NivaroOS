<template>
	<div class="oauth-steps">
		<div class="step">
			<span class="step-number">1</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Choose how to sign in') }}</p>
				<div class="segmented-control" role="radiogroup">
					<button type="button" class="segmented-option" role="radio" :aria-checked="!own" :class="{ active: !own }" @click="own = false">
						{{ $t("rclone's app") }}
					</button>
					<button type="button" class="segmented-option" role="radio" :aria-checked="own" :class="{ active: own }" @click="own = true">
						{{ $t('Your own app') }}
					</button>
				</div>
				<p class="step-help">
					<template v-if="!own">{{ $t("Quickest to set up. rclone's app is shared by every rclone user, so {provider} slows it down - fine for small folders.", { provider }) }}</template>
					<template v-else>{{ $t('Your own {provider} app gets its own speed limits - much faster for big backups like photo libraries. It takes about 10 minutes to create, once.', { provider }) }}</template>
					<a :href="guideUrl" target="_blank" rel="noopener">{{ $t('How to create one') }}</a>
				</p>
				<p v-if="own && type === 'drive'" class="publish-warning">
					<i class="mdi mdi-alert-outline"></i>
					<span><b>{{ $t('Publish the app or it expires in 7 days.') }}</b> {{ $t('A Google app left in Testing mode is signed out by Google every 7 days. Publish it first: Google Cloud › OAuth consent screen › Publish app. Already expired? Publish it, then Reconnect.') }}</span>
				</p>
				<p v-if="usingSaved" class="saved-app">
					{{ $t('Using your saved app ({id})', { id: shortSavedId }) }}
					<a class="advanced-toggle" @click="useSaved = false">{{ $t('Use different keys') }}</a>
				</p>
				<template v-else-if="own">
					<b-field :label="$t('Client ID')" :type="idError ? 'is-danger' : ''" :message="idError">
						<b-input v-model.trim="clientId" size="is-small" autocomplete="off" spellcheck="false"></b-input>
					</b-field>
					<b-field :label="$t('Client secret')" :type="secretError ? 'is-danger' : ''" :message="secretError">
						<b-input v-model.trim="clientSecret" type="password" password-reveal size="is-small" autocomplete="off"></b-input>
					</b-field>
				</template>
			</div>
		</div>

		<div class="step">
			<span class="step-number">2</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Sign in to {provider}', { provider }) }}</p>
				<template v-if="usingSaved">
					<b-button rounded size="is-small" icon-left="login" :loading="auth.busy && !auth.path" @click="startSaved">{{ $t('Sign in with your saved app') }}</b-button>
					<p v-if="auth.path" class="step-help">
						<a class="saved-link" :href="relayBase + auth.path" target="_blank" rel="noopener">{{ $t('Open the {provider} sign-in page', { provider }) }}</a>
						<template v-if="auth.busy"> &middot; {{ $t('Waiting for you to sign in...') }}</template>
					</p>
					<p v-if="auth.error" class="step-help error-text">{{ auth.error }}</p>
				</template>
				<p v-else-if="!credsOk" class="step-help error-text creds-needed">{{ $t("Enter your app's client ID and client secret in step 1 first, or choose rclone's app.") }}</p>
				<template v-else>
					<p class="step-help">{{ $t('Run this on the server, open the link it prints, and sign in.') }}</p>
					<code class="authorize-cmd">{{ command }}</code>
					<a class="advanced-toggle" @click="openTerminal()">{{ $t('Run it in Terminal') }}</a>
				</template>
				<p class="step-help">{{ $t('If the page after signing in does not open, replace {from} at the start of its address with {to}.', { from: 'http://127.0.0.1:53682 (or http://localhost:53682)', to: relayBase }) }}</p>
			</div>
		</div>

		<div class="step">
			<span class="step-number">3</span>
			<div class="step-body">
				<p class="step-title">{{ $t('Paste what it prints back') }}</p>
				<b-field :type="tokenError ? 'is-danger' : ''" :message="tokenError">
					<b-input v-model="token" type="textarea" size="is-small" rows="3" :placeholder="$t('Paste it here')"></b-input>
				</b-field>
			</div>
		</div>
	</div>
</template>

<script>
// Sign-in steps for OAuth providers (Drive, Dropbox, OneDrive): rclone's
// shared app, or the user's own client ID/secret. Emits `change` with the
// rclone params to save ({ token, client_id, client_secret }) or null
// while incomplete. Empty client_id/secret mean rclone's app, so a
// reconnect can switch back. Reconnecting an account that has its own
// app reuses it: the server runs the sign-in with the saved secret (which
// never reaches the browser) and `change` carries only the token, so the
// saved client_id/secret stay as they are.

const GUIDE = 'https://github.com/F-e-n-y-x/NivaroOS/blob/master/docs/guides/cloud-own-app.md'
// The command runs in a shell: allow only what real client IDs/secrets use.
const SAFE = /^[A-Za-z0-9._~-]+$/

export default {
	name: 'OAuthSignInSteps',
	props: {
		type: { type: String, required: true }, // rclone backend: drive, dropbox, onedrive
		provider: { type: String, required: true }, // display name
		ownClient: { type: Boolean, default: false }, // the account already uses its own app
		name: { type: String, default: '' }, // the account's remote, when reconnecting
		savedClientId: { type: String, default: '' }, // its saved app's (public) client ID
	},
	emits: ['change'],
	data() {
		return {
			own: this.ownClient,
			useSaved: this.ownClient && !!this.name && !!this.savedClientId,
			clientId: '',
			clientSecret: '',
			token: '',
			auth: { busy: false, path: '', error: '' },
			pollTimer: null,
		}
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
		tokenOk() {
			try {
				return !!JSON.parse(this.token.trim()).access_token
			} catch (e) {
				return false
			}
		},
		tokenError() {
			return this.token.trim() && !this.tokenOk ? this.$t('That isn\'t the whole token. Copy the full line that starts with {start} and ends with {end}', { start: '{"access_token"', end: '}' }) : ''
		},
		usingSaved() {
			return this.own && this.useSaved
		},
		shortSavedId() {
			return this.savedClientId.slice(0, 6) + '…'
		},
		credsOk() {
			return !this.own || this.usingSaved || (SAFE.test(this.clientId) && SAFE.test(this.clientSecret))
		},
		payload() {
			if (!this.tokenOk || !this.credsOk) return null
			if (this.usingSaved) return { token: this.token.trim() }
			return {
				token: this.token.trim(),
				client_id: this.own ? this.clientId : '',
				client_secret: this.own ? this.clientSecret : '',
			}
		},
		relayBase() {
			return `${window.location.origin}/v1/rclone-auth`
		},
		command() {
			if (!this.own) return `rclone authorize "${this.type}"`
			return `rclone authorize "${this.type}" "${this.clientId || 'CLIENT_ID'}" "${this.clientSecret || 'CLIENT_SECRET'}"`
		},
	},
	// Emitted from a watcher, not the inputs' input events: those fire
	// before v-model updates, so the value sent was always one edit behind.
	watch: {
		payload: {
			handler(v) {
				this.$emit('change', v)
			},
			immediate: true,
		},
	},
	beforeUnmount() {
		this.stopped = true
		clearTimeout(this.pollTimer)
	},
	methods: {
		async startSaved() {
			clearTimeout(this.pollTimer)
			this.auth = { busy: true, path: '', error: '' }
			try {
				this.onAuth((await this.$api.cloud.authorize(this.name)).data)
			} catch (e) {
				this.auth = { busy: false, path: '', error: e?.response?.data?.message || this.$t('Could not start the sign-in') }
			}
		},
		onAuth(res) {
			if (this.stopped) return
			const v = res && res.data
			if (!v) return (this.auth = { busy: false, path: '', error: (res && res.message) || this.$t('Could not start the sign-in') })
			this.auth = { busy: v.running, path: v.path || this.auth.path, error: v.error || '' }
			if (v.token) this.token = v.token
			if (v.running) this.pollTimer = setTimeout(() => this.$api.cloud.authorizeStatus(this.name).then(r => this.onAuth(r.data), () => this.onAuth({ data: v })), 2000)
		},
		openTerminal() {
			// `rclone authorize` serves its link and the provider's redirect
			// back on 127.0.0.1:53682 only (hardcoded upstream) - unreachable
			// unless the browser is on this machine. The gateway relays
			// /v1/rclone-auth to it on the dashboard's own port
			// (services/gateway/route/rclone_auth.go), so the printed link is
			// rewritten to that; the redirect back is fixed in the address bar
			// (see relayHint).
			const args = this.own ? ` "${this.clientId}" "${this.clientSecret}"` : ''
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { initCommand: `rclone authorize "${this.type}"${args} 2>&1 | sed -u -e "s#http://127\\.0\\.0\\.1:53682#${this.relayBase}#g" -e "s#http://localhost:53682#${this.relayBase}#g"` },
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
.publish-warning {
	display: flex;
	gap: var(--space-2);
	font-size: var(--font-xs);
	color: var(--color-warning-fg, #92400e);
	background: var(--color-warning-soft, rgba(245, 158, 11, 0.1));
	border-radius: var(--radius-sm);
	padding: var(--space-2);
	margin: var(--space-2) 0;
}
.error-text {
	color: var(--color-danger-fg, #b91c1c);
}
.saved-app {
	font-size: var(--font-sm);

	.advanced-toggle {
		margin-left: var(--space-2);
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
