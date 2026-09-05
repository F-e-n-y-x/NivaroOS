<template>
	<div class="cloud-accounts-panel">
		<div class="provider-groups">
			<div v-for="group in providerGroups" :key="group.key" class="provider-group">
				<div class="provider-group-label">{{ group.label }}</div>
				<div class="provider-grid">
					<button
						v-for="p in group.items"
						:key="p.type"
						class="provider-tile"
						:class="{ active: addingType === p.type }"
						:style="{ '--provider-accent': providerAccent(p.type) }"
						@click="startAdd(p)"
					>
						<i class="mdi" :class="`mdi-${p.icon}`"></i>
						<span>{{ p.label }}</span>
					</button>
				</div>
			</div>
		</div>

		<!-- form-kind: S3, B2, WebDAV, SFTP, SMB - one step, fields generated from rclone's own metadata -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'form'" class="account-inline-form is-block">
			<b-field :label="$t('Label')">
				<b-input v-model="form.label" size="is-small" :placeholder="activeProvider.label"></b-input>
			</b-field>
			<b-field v-for="opt in visibleFormOptions" :key="opt.Name" :label="opt.Help || opt.Name">
				<b-select v-if="opt.Examples && opt.Examples.length" v-model="form.values[opt.Name]" size="is-small" expanded>
					<option value="">{{ $t('Choose...') }}</option>
					<option v-for="ex in opt.Examples" :key="ex.Value" :value="ex.Value">{{ ex.Help }}</option>
				</b-select>
				<b-input
					v-else
					v-model="form.values[opt.Name]"
					size="is-small"
					:type="opt.IsPassword ? 'password' : 'text'"
					:password-reveal="opt.IsPassword"
				></b-input>
			</b-field>
			<a v-if="hiddenFormOptionCount" class="advanced-toggle" @click="showAdvanced = !showAdvanced">
				{{ showAdvanced ? $t('Hide advanced options') : $t('Show {n} advanced options', { n: hiddenFormOptionCount }) }}
			</a>
			<div class="form-actions">
				<b-button rounded size="is-small" type="is-dark" :loading="submitting" @click="submitForm">{{ $t('Connect') }}</b-button>
				<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
			</div>
			<p v-if="error" class="error-note">{{ error }}</p>
		</div>

		<!-- token-kind: Drive, Dropbox, OneDrive - a real 2-step sequence -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'token'" class="account-inline-form is-block">
			<b-field :label="$t('Label')">
				<b-input v-model="form.label" size="is-small" :placeholder="activeProvider.label"></b-input>
			</b-field>

			<div class="step">
				<span class="step-number">1</span>
				<div class="step-body">
					<p class="step-title">{{ $t('Sign in to {provider}', { provider: activeProvider.label }) }}</p>
					<p class="step-help">{{ $t('Run this on the server, open the link it prints, and sign in.') }}</p>
					<code class="authorize-cmd">rclone authorize "{{ activeProvider.type }}"</code>
					<a class="advanced-toggle" @click="openTerminal">{{ $t('Run it in Terminal') }}</a>
				</div>
			</div>

			<div class="step">
				<span class="step-number">2</span>
				<div class="step-body">
					<p class="step-title">{{ $t('Paste what it prints back') }}</p>
					<b-input v-model="form.token" type="textarea" size="is-small" rows="3" :placeholder="$t('Paste it here')"></b-input>
				</div>
			</div>

			<div class="form-actions">
				<b-button rounded size="is-small" type="is-dark" :loading="submitting" :disabled="!form.token.trim()" @click="submitToken">{{ $t('Connect') }}</b-button>
				<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
			</div>
			<p v-if="error" class="error-note">{{ error }}</p>
		</div>

		<!-- interactive-kind: iCloud - Apple ID + password, then a 2FA code -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'interactive'" class="account-inline-form is-block">
			<template v-if="!icloud.sessionId">
				<b-field :label="$t('Label')">
					<b-input v-model="form.label" size="is-small" :placeholder="activeProvider.label"></b-input>
				</b-field>
				<div class="step">
					<span class="step-number">1</span>
					<div class="step-body">
						<p class="step-title">{{ $t('Sign in with your Apple ID') }}</p>
						<b-field :label="$t('Apple ID')">
							<b-input v-model="icloud.appleId" size="is-small" type="email"></b-input>
						</b-field>
						<b-field :label="$t('Password')">
							<b-input v-model="icloud.password" size="is-small" type="password" password-reveal></b-input>
						</b-field>
					</div>
				</div>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="submitting" :disabled="!icloud.appleId || !icloud.password" @click="startIcloud">{{ $t('Continue') }}</b-button>
					<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
				</div>
			</template>
			<template v-else>
				<div class="step">
					<span class="step-number">2</span>
					<div class="step-body">
						<p class="step-title">{{ icloud.question ? icloud.question.Help : $t('Enter the code') }}</p>
						<b-input
							v-model="icloud.answer"
							size="is-small"
							:type="icloud.question && icloud.question.IsPassword ? 'password' : 'text'"
						></b-input>
					</div>
				</div>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="submitting" :disabled="!icloud.answer" @click="verifyIcloud">{{ $t('Verify') }}</b-button>
					<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
				</div>
			</template>
			<p v-if="error" class="error-note">{{ error }}</p>
		</div>
	</div>
</template>

<script>
// Genuine grouping, not decoration: these two categories need different
// things from the user (sign in vs. enter server details), which is why
// they get different add-flows below (token/interactive vs form).
const PERSONAL_CLOUD_TYPES = ['drive', 'dropbox', 'onedrive', 'iclouddrive']

// A light per-provider accent for the tile icon only (not the whole tile) -
// brand recognition without turning the grid into a color chart.
const PROVIDER_ACCENTS = {
	drive: '#1FA463',
	dropbox: '#0061FF',
	onedrive: '#0364B8',
	iclouddrive: '#8e8e93',
	s3: '#FF9900',
	b2: '#E21E29',
	webdav: '#6b7280',
	sftp: '#6b7280',
	smb: '#6b7280'
}

export default {
	name: 'cloud-accounts-panel',
	data() {
		return {
			providers: [],
			addingType: null,
			formOptions: [],
			showAdvanced: false,
			submitting: false,
			error: '',
			form: { label: '', values: {}, token: '' },
			icloud: { appleId: '', password: '', sessionId: '', question: null, answer: '' }
		}
	},
	computed: {
		activeProvider() {
			return this.providers.find(p => p.type === this.addingType) || null
		},
		visibleFormOptions() {
			return this.showAdvanced ? this.formOptions : this.formOptions.filter(o => !o.Advanced)
		},
		hiddenFormOptionCount() {
			return this.formOptions.filter(o => o.Advanced).length
		},
		providerGroups() {
			const personal = this.providers.filter(p => PERSONAL_CLOUD_TYPES.includes(p.type))
			const other = this.providers.filter(p => !PERSONAL_CLOUD_TYPES.includes(p.type))
			const groups = []
			if (personal.length) groups.push({ key: 'personal', label: this.$t('Personal cloud'), items: personal })
			if (other.length) groups.push({ key: 'other', label: this.$t('Self-hosted & business storage'), items: other })
			return groups
		}
	},
	created() {
		this.$api.cloud.providers().then(res => {
			if (res.data.success === 200) this.providers = res.data.data || []
		})
	},
	methods: {
		providerAccent(type) {
			return PROVIDER_ACCENTS[type] || '#6b7280'
		},
		startAdd(provider) {
			if (this.addingType === provider.type) {
				this.cancelAdd()
				return
			}
			this.addingType = provider.type
			this.error = ''
			this.form = { label: '', values: {}, token: '' }
			this.icloud = { appleId: '', password: '', sessionId: '', question: null, answer: '' }
			this.formOptions = []
			this.showAdvanced = false
			if (provider.auth_kind === 'form') {
				this.$api.cloud.providerOptions(provider.type).then(res => {
					if (res.data.success === 200) {
						this.formOptions = (res.data.data || []).filter(o => o.Name !== 'description')
						const values = {}
						this.formOptions.forEach(o => {
							values[o.Name] = o.DefaultStr && o.DefaultStr !== '<nil>' ? o.DefaultStr : ''
						})
						this.form.values = values
					}
				})
			}
		},
		cancelAdd() {
			this.addingType = null
			this.error = ''
		},
		submitForm() {
			this.error = ''
			this.submitting = true
			const params = {}
			Object.keys(this.form.values).forEach(k => {
				if (this.form.values[k] !== '' && this.form.values[k] !== null && this.form.values[k] !== undefined) {
					params[k] = String(this.form.values[k])
				}
			})
			this.$api.cloud
				.createAccount({ type: this.addingType, label: this.form.label || this.activeProvider.label, params })
				.then(res => {
					if (res.data.success === 200) {
						this.cancelAdd()
						this.$emit('added')
						this.$buefy.toast.open({ message: this.$t('Connected'), type: 'is-success' })
					} else {
						this.error = res.data.message
					}
				})
				.catch(e => {
					this.error = (e.response && e.response.data && e.response.data.data) || this.$t('Failed to add account')
				})
				.finally(() => {
					this.submitting = false
				})
		},
		submitToken() {
			this.error = ''
			this.submitting = true
			this.$api.cloud
				.createAccount({ type: this.addingType, label: this.form.label || this.activeProvider.label, params: { token: this.form.token.trim() } })
				.then(res => {
					if (res.data.success === 200) {
						this.cancelAdd()
						this.$emit('added')
						this.$buefy.toast.open({ message: this.$t('Connected'), type: 'is-success' })
					} else {
						this.error = res.data.message
					}
				})
				.catch(e => {
					this.error = (e.response && e.response.data && e.response.data.data) || this.$t('Failed to add account')
				})
				.finally(() => {
					this.submitting = false
				})
		},
		startIcloud() {
			this.error = ''
			this.submitting = true
			this.$api.cloud
				.icloudStart({ label: this.form.label || 'iCloud Drive', apple_id: this.icloud.appleId, password: this.icloud.password })
				.then(res => this.handleIcloudStep(res))
				.catch(e => {
					this.error = (e.response && e.response.data && e.response.data.data) || this.$t('Failed to start iCloud sign-in')
				})
				.finally(() => {
					this.submitting = false
				})
		},
		verifyIcloud() {
			this.error = ''
			this.submitting = true
			this.$api.cloud
				.icloudVerify({ session_id: this.icloud.sessionId, code: this.icloud.answer })
				.then(res => this.handleIcloudStep(res))
				.catch(e => {
					this.error = (e.response && e.response.data && e.response.data.data) || this.$t('Verification failed')
				})
				.finally(() => {
					this.submitting = false
				})
		},
		handleIcloudStep(res) {
			if (res.data.success !== 200) {
				this.error = res.data.message
				return
			}
			const step = res.data.data
			if (step.error) {
				this.error = step.error
				return
			}
			if (step.done) {
				this.cancelAdd()
				this.$emit('added')
				this.$buefy.toast.open({ message: this.$t('Connected'), type: 'is-success' })
				return
			}
			this.icloud.sessionId = step.session_id
			this.icloud.question = step.question
			this.icloud.answer = ''
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
			const type = this.addingType
			const initCommand = type
				? `rclone authorize "${type}" 2>&1 | sed -u "s/127\\.0\\.0\\.1:53682/${host}:53682/g"`
				: ''
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { initCommand }
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.cloud-accounts-panel {
	display: flex;
	flex-direction: column;
}

.provider-groups {
	display: flex;
	flex-direction: column;
	gap: 1rem;
	padding: 1rem 1.25rem 0.5rem;
}

.provider-group-label {
	font-size: 0.7rem;
	font-weight: 600;
	letter-spacing: 0.02em;
	color: rgba(0, 0, 0, 0.4);
	margin-bottom: 0.5rem;
}

.provider-grid {
	display: flex;
	flex-wrap: wrap;
	gap: 0.5rem;
}

.provider-tile {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 8px;
	padding: 0.45rem 0.75rem;
	font-size: 0.8rem;
	cursor: pointer;
	transition: background 0.12s ease, border-color 0.12s ease;

	i {
		font-size: 1.05rem;
		color: var(--provider-accent, #6b7280);
	}

	&:hover {
		background: rgba(0, 0, 0, 0.03);
	}

	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);
		color: #3273dc;

		i {
			color: #3273dc;
		}
	}
}

.account-inline-form.is-block {
	display: block;
	padding: 0.75rem 1.25rem 1rem;
}

.step {
	display: flex;
	gap: 0.65rem;
	margin: 0.25rem 0 0.75rem;

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
	background: rgba(50, 115, 220, 0.1);
	color: #3273dc;
	font-size: 0.75rem;
	font-weight: 600;
}

.step-body {
	flex: 1 1 auto;
	min-width: 0;
}

.step-title {
	font-size: 0.8rem;
	font-weight: 600;
	margin-bottom: 0.3rem;
}

.step-help {
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.55);
	margin-bottom: 0.4rem;
}

.authorize-cmd {
	display: block;
	background: rgba(0, 0, 0, 0.05);
	border-radius: 6px;
	padding: 0.4rem 0.6rem;
	font-size: 0.75rem;
	margin-bottom: 0.4rem;
}

.advanced-toggle {
	display: inline-block;
	font-size: 0.75rem;
	color: #3273dc;
	cursor: pointer;
	margin-bottom: 0.5rem;
}

.form-actions {
	display: flex;
	gap: 0.5rem;
	margin-top: 0.5rem;
}

.error-note {
	color: #ef4444;
	font-size: 0.75rem;
	margin-top: 0.5rem;
}
</style>
