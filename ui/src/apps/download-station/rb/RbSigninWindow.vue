<!-- src/apps/download-station/rb/RbSigninWindow.vue -->
<!-- "Sign in with the browser" for cloud accounts that need a site's login
     cookie (TeraBox). A real browser on the server opens the provider's
     login page in a throwaway cookie jar; the user signs in any way the
     site offers (password, Google, QR code from the phone app). Once the
     provider confirms the sign-in, "Connect" hands only that provider's
     cookie to Online Accounts - nobody has to copy it out of DevTools. -->
<template>
	<div class="rb-signin">
		<div class="signin-bar">
			<button class="ds-icon-btn is-flat" type="button" :title="$t('Back')" :aria-label="$t('Back')" :disabled="!tab || !tab.canBack" @click="send({ t: 'back', tab: tab.id })">
				<b-icon icon="arrow-left" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" type="button" :title="$t('Reload')" :aria-label="$t('Reload')" :disabled="!tab" @click="send({ t: 'reload', tab: tab.id })">
				<b-icon :icon="tab && tab.loading ? 'loading' : 'refresh'" :custom-class="tab && tab.loading ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
			</button>
			<div class="signin-address" :title="tab ? tab.url : ''">
				<b-icon :icon="secure ? 'lock-outline' : 'web'" custom-size="mdi-16px" :class="{ secure }"></b-icon>
				<span class="one-line">{{ host }}</span>
			</div>
			<span class="private-pill" :title="$t('This window has its own cookies - it is not signed in to anything else, and everything is thrown away when it closes.')">
				<b-icon icon="incognito" custom-size="mdi-14px"></b-icon>{{ $t('Private') }}
			</span>
		</div>

		<div class="signin-status" :class="'is-' + phase" role="status" aria-live="polite">
			<template v-if="phase === 'signed_in'">
				<b-icon icon="check-circle" custom-size="mdi-20px"></b-icon>
				<span class="status-text">{{ account ? $t('Signed in to {provider} as {account}.', { provider: providerName, account }) : $t('Signed in to {provider}.', { provider: providerName }) }}</span>
				<button class="ds-primary-btn" type="button" :disabled="connecting" @click="connect">
					<b-icon v-if="connecting" icon="loading" custom-class="mdi-spin" custom-size="mdi-16px"></b-icon>
					{{ purpose === 'reconnect' ? $t('Use this sign-in') : $t('Connect this account') }}
				</button>
			</template>
			<template v-else-if="phase === 'verifying'">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-20px"></b-icon>
				<span class="status-text">{{ $t('Checking your sign-in with {provider}...', { provider: providerName }) }}</span>
			</template>
			<template v-else-if="phase === 'stuck'">
				<b-icon icon="alert-outline" custom-size="mdi-20px"></b-icon>
				<span class="status-text">
					<strong>{{ $t('Sign-in didn\'t stick?') }}</strong>
					{{ stuckText }}
					<button class="link-btn" type="button" :aria-expanded="String(showDiag)" @click="showDiag = !showDiag">{{ showDiag ? $t('Hide details') : $t('Details') }}</button>
				</span>
				<button class="ds-secondary-btn" type="button" @click="copyDiag">{{ diagCopied ? $t('Copied') : $t('Copy details') }}</button>
				<button class="ds-primary-btn" type="button" @click="restart">{{ $t('Try again') }}</button>
			</template>
			<template v-else-if="phase === 'error'">
				<b-icon icon="alert-circle-outline" custom-size="mdi-20px"></b-icon>
				<span class="status-text">{{ error }}</span>
				<button class="ds-secondary-btn" type="button" @click="restart">{{ $t('Try again') }}</button>
			</template>
			<template v-else>
				<b-icon icon="account-key-outline" custom-size="mdi-20px"></b-icon>
				<span class="status-text">{{ $t('Sign in to {provider} below - with your email, Google, or the QR code (scan it with the {provider} app). This window notices when you are signed in.', { provider: providerName }) }}</span>
			</template>
		</div>

		<pre v-if="phase === 'stuck' && showDiag" class="signin-diag">{{ diagText }}</pre>

		<div class="signin-frame">
			<ds-browser-viewport ref="viewport" :class="{ 'is-offstage': state !== 'open' && state !== 'reconnecting' }" :send="send" :tab="tab ? tab.id : 0"
				:inert="!!(menu || dialog)" :label="providerName" @resize="s => send({ t: 'resize', ...s })" @contextmenu="onContextMenu" @shortcut="onShortcut"></ds-browser-viewport>
			<div v-if="state === 'connecting' || (state === 'reconnecting' && !everOpen)" class="frame-note" role="status">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-24px"></b-icon>
				<p>{{ $t('Opening {provider}...', { provider: providerName }) }}</p>
			</div>
			<div v-else-if="state === 'fatal'" class="frame-note" role="alert">
				<b-icon icon="alert-circle-outline" custom-size="mdi-36px"></b-icon>
				<p>{{ fatal && fatal.text }}</p>
				<button class="ds-primary-btn" type="button" @click="restart">{{ $t('Try again') }}</button>
			</div>
			<div v-if="dialog" class="frame-modal" role="dialog" aria-modal="true">
				<form class="frame-dialog" @submit.prevent="answer(true)">
					<p class="dialog-message">{{ dialog.message }}</p>
					<input v-if="dialog.kind === 'prompt'" v-model="dialogText" class="ds-input" :aria-label="dialog.message" />
					<div class="dialog-actions">
						<button v-if="dialog.kind !== 'alert'" class="ds-secondary-btn" type="button" @click="answer(false)">{{ $t('Cancel') }}</button>
						<button class="ds-primary-btn" type="submit">{{ $t('OK') }}</button>
					</div>
				</form>
			</div>
		</div>

		<ds-browser-menu v-if="menu" :sections="menu.sections" :x="menu.x" :y="menu.y" @select="menu.onSelect" @close="menu = null"></ds-browser-menu>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import DsBrowserViewport from './DsBrowserViewport.vue'
import DsBrowserMenu from './DsBrowserMenu.vue'
import { RbSession, wsUrl, hostOf, copyText } from './rbClient'
import { isStuck, stuckReason, diagReport } from './signinDiag'

const PROVIDER_NAMES = { terabox: 'TeraBox' }

export default {
	emits: ['close'],
	name: 'RbSigninWindow',
	components: { DsBrowserViewport, DsBrowserMenu },
	props: {
		winId: { type: String, default: '' },
		provider: { type: String, default: 'terabox' },
		// 'add' (a new account) or 'reconnect' (an expired one).
		purpose: { type: String, default: 'add' },
		// onConnected(cookie, accountName) - saves the account; may return a
		// promise. The cookie is not kept anywhere in this window.
		onConnected: { type: Function, required: true }
	},
	data() {
		return {
			state: 'idle',
			everOpen: false,
			fatal: null,
			tabs: [],
			activeId: 0,
			signin: { state: 'waiting', account: '', context: '', diag: null },
			showDiag: false,
			diagCopied: false,
			connecting: false,
			error: '',
			menu: null,
			dialog: null,
			dialogText: ''
		}
	},
	computed: {
		providerName() {
			return PROVIDER_NAMES[this.provider] || this.provider
		},
		tab() {
			return this.tabs.find(t => t.id === this.activeId) || null
		},
		host() {
			return this.tab ? hostOf(this.tab.url) || this.tab.url : ''
		},
		secure() {
			return !!(this.tab && /^https:/.test(this.tab.url))
		},
		account() {
			return this.signin.account
		},
		phase() {
			if (this.error) return 'error'
			if (this.signin.state === 'waiting' && isStuck(this.signin.diag)) return 'stuck'
			return this.signin.state
		},
		stuckText() {
			return stuckReason(this.signin.diag, this.providerName, (s, p) => this.$t(s, p))
		},
		diagText() {
			return diagReport(this.signin.diag, this.providerName)
		}
	},
	mounted() {
		this.start()
	},
	beforeUnmount() {
		// Closing the window disposes the throwaway cookie jar (the server
		// does it when this connection ends).
		if (this.session) this.session.close()
	},
	methods: {
		start() {
			if (this.session) this.session.close()
			this.error = ''
			this.fatal = null
			this.signin = { state: 'waiting', account: '', context: '', diag: null }
			this.showDiag = false
			const touch = window.matchMedia && window.matchMedia('(pointer: coarse)').matches
			this.session = new RbSession({
				url: () => wsUrl(window.location, downloadSidecar.authToken),
				hello: () => ({ ...(this.$refs.viewport ? this.$refs.viewport.size() : { w: 900, h: 600, dpr: 1 }), touch, mode: 'signin', provider: this.provider }),
				onMessage: m => this.onMessage(m),
				onFrame: f => {
					const ack = () => this.send({ t: 'ack' })
					if (this.$refs.viewport) this.$refs.viewport.drawFrame(f, ack)
					else ack()
				},
				onState: (s, detail) => {
					this.state = s
					if (s === 'open') this.everOpen = true
					if (s === 'fatal') this.fatal = detail
					// A sign-in window can't pick up where it left off: its
					// cookie jar went away with the connection.
					if (s === 'reconnecting' && this.everOpen) {
						this.session.close()
						this.state = 'fatal'
						this.fatal = { text: this.$t('The connection to the browser was lost. Try again to sign in.') }
					}
				}
			})
			this.session.connect()
		},
		copyDiag() {
			copyText(this.diagText)
			this.diagCopied = true
			setTimeout(() => (this.diagCopied = false), 1500)
		},
		restart() {
			this.everOpen = false
			this.start()
		},
		send(msg) {
			return !!(this.session && this.session.send(msg))
		},
		onMessage(m) {
			switch (m.t) {
				case 'tabs':
					this.tabs = m.tabs || []
					if (m.active) this.activeId = m.active
					this.$nextTick(() => this.$refs.viewport && this.$refs.viewport.focus())
					break
				case 'cursor':
					if (this.$refs.viewport) this.$refs.viewport.setCursor(m.css)
					break
				case 'signin':
					this.signin = { state: m.state, account: m.account || '', context: m.context || this.signin.context, diag: m.diag || this.signin.diag }
					break
				case 'dialog':
					this.dialog = m
					this.dialogText = m.default || ''
					break
				case 'clip':
					if (this.clipResolve) this.clipResolve(m)
					break
				case 'fatal':
					this.fatal = m
					break
			}
		},
		async connect() {
			this.connecting = true
			this.error = ''
			try {
				const res = await downloadSidecar.rbExportCookies(this.provider, this.signin.context)
				await this.onConnected(res.cookie, res.account_hint || this.signin.account || '')
				this.close()
			} catch (e) {
				this.error = (e && e.message) || this.$t('Could not connect the account')
			} finally {
				this.connecting = false
			}
		},
		answer(accept) {
			if (!this.dialog) return
			this.send({ t: 'dialog', id: this.dialog.id, accept, text: this.dialogText })
			this.dialog = null
		},
		onShortcut(name) {
			if (!this.tab) return
			if (name === 'reload' || name === 'hardReload') this.send({ t: 'reload', tab: this.tab.id })
			else if (name === 'back') this.send({ t: 'back', tab: this.tab.id })
			else if (name === 'forward') this.send({ t: 'fwd', tab: this.tab.id })
			else if (name === 'copy' || name === 'cut') this.copySelection(name === 'cut')
		},
		copySelection(cut) {
			const seq = Date.now()
			this.clipResolve = m => {
				this.clipResolve = null
				if (m.text) copyText(m.text)
			}
			this.send({ t: 'act', action: cut ? 'cut' : 'copy', seq })
		},
		// A small menu: editing actions for the sign-in form, Back/Reload.
		onContextMenu(p) {
			if (!this.tab) return
			this.menu = {
				x: p.clientX,
				y: p.clientY,
				sections: [
					[
						{ id: 'cut', label: 'Cut', icon: 'content-cut', hint: 'Ctrl+X' },
						{ id: 'copy', label: 'Copy', icon: 'content-copy', hint: 'Ctrl+C' },
						{ id: 'paste', label: 'Paste', icon: 'content-paste', hint: 'Ctrl+V' },
						{ id: 'selectAll', label: 'Select all', icon: 'select-all', hint: 'Ctrl+A' }
					],
					[
						{ id: 'back', label: 'Back', icon: 'arrow-left', disabled: !this.tab.canBack },
						{ id: 'reload', label: 'Reload', icon: 'refresh' }
					]
				],
				onSelect: async item => {
					if (item.id === 'cut' || item.id === 'copy') this.copySelection(item.id === 'cut')
					else if (item.id === 'selectAll') this.send({ t: 'act', action: 'selectAll' })
					else if (item.id === 'paste') {
						let text = ''
						try {
							if (navigator.clipboard && navigator.clipboard.readText) text = await navigator.clipboard.readText()
						} catch (e) {}
						if (text) this.send({ t: 'paste', text })
						else this.$buefy.toast.open({ message: this.$t('Press Ctrl+V to paste'), type: 'is-info' })
					} else if (item.id === 'back') this.send({ t: 'back', tab: this.tab.id })
					else if (item.id === 'reload') this.send({ t: 'reload', tab: this.tab.id })
					this.$nextTick(() => this.$refs.viewport && this.$refs.viewport.focus())
				}
			}
		},
		close() {
			const id = this.winId || (this.$parent && this.$parent.win && this.$parent.win.id)
			if (id) this.$store.commit('CLOSE_WINDOW', id)
			this.$emit('close')
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';

.rb-signin {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 0;
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-primary, #1e293b);
}

.signin-bar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 2px;
	padding: var(--space-2);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
}

.signin-address {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	height: 2rem;
	margin: 0 var(--space-2);
	padding: 0 var(--space-3);
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	font-size: var(--font-sm);
	color: var(--theme-text-secondary, #475569);

	:deep(.icon) {
		flex-shrink: 0;
		color: var(--theme-text-muted, #94a3b8);
	}

	.secure {
		color: var(--color-success-fg, #047857);
	}
}

.private-pill {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	gap: 0.25rem;
	padding: 0.15rem 0.55rem;
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-secondary, #475569);
	font-size: var(--font-2xs);
}

.signin-status {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-height: 3rem;
	padding: var(--space-2) var(--space-3);
	font-size: var(--font-sm);
	line-height: 1.4;
	background: var(--color-primary-soft, rgba(37, 99, 235, 0.08));
	color: var(--color-primary-fg, #1d4ed8);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));

	.status-text {
		flex: 1 1 auto;
		min-width: 0;
	}

	&.is-signed_in {
		background: var(--color-success-soft, rgba(16, 185, 129, 0.12));
		color: var(--color-success-fg, #047857);
	}

	&.is-error {
		background: var(--color-danger-soft, #fee2e2);
		color: var(--color-danger-fg, #b91c1c);
	}

	&.is-stuck {
		flex-wrap: wrap;
		background: var(--color-warning-soft, #fef3c7);
		color: var(--color-warning-fg, #92400e);
	}

	.link-btn {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
}

.signin-diag {
	flex-shrink: 0;
	max-height: 10rem;
	margin: 0;
	padding: var(--space-2) var(--space-3);
	overflow: auto;
	font-size: var(--font-2xs);
	white-space: pre-wrap;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-secondary, #475569);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
}

.signin-frame {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
}

.is-offstage {
	visibility: hidden;
}

.frame-note {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-3);
	padding: var(--space-5);
	text-align: center;
	color: var(--theme-text-secondary, #64748b);
	font-size: var(--font-sm);

	p {
		max-width: 26rem;
		margin: 0;
	}
}

.frame-modal {
	position: absolute;
	inset: 0;
	display: flex;
	align-items: flex-start;
	justify-content: center;
	padding: 10vh var(--space-4) var(--space-4);
	background: rgba(15, 23, 42, 0.35);
}

.frame-dialog {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	width: min(26rem, 100%);
	padding: var(--space-5);
	border-radius: var(--radius-md, 12px);
	background: var(--theme-card-bg, #fff);
	box-shadow: 0 20px 48px rgba(15, 23, 42, 0.28);
}

.dialog-message {
	margin: 0;
	font-size: var(--font-sm);
	white-space: pre-wrap;
	word-break: break-word;
}

.dialog-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
}

.one-line {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
</style>
