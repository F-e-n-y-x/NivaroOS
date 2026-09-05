<template>
	<div class="cloud-accounts-panel">
		<div v-for="a in accounts" :key="a.mount_point" class="setting-row">
			<i class="row-icon mdi" :class="`mdi-${a.icon || 'cloud-outline'}`"></i>
			<div class="row-label">
				<div class="setting-title">{{ a.name || a.fs }}</div>
				<div class="setting-desc">{{ a.mount_point }}</div>
			</div>
			<div class="row-control">
				<b-button rounded size="is-small" type="is-danger" outlined :loading="removingKey === a.mount_point" @click="confirmRemove(a)">
					{{ $t('Remove') }}
				</b-button>
			</div>
		</div>

		<div v-if="!accounts.length" class="account-empty">
			{{ $t('No online accounts connected yet.') }}
		</div>

		<div class="provider-grid mt-3">
			<button
				v-for="p in providers"
				:key="p.type"
				class="provider-tile"
				:class="{ active: addingType === p.type }"
				@click="startAdd(p)"
			>
				<i class="mdi" :class="`mdi-${p.icon}`"></i>
				<span>{{ p.label }}</span>
			</button>
		</div>

		<!-- form-kind: S3, B2, WebDAV, SFTP, SMB - fields generated from rclone's own metadata -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'form'" class="account-inline-form is-block mt-3">
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

		<!-- token-kind: Drive, Dropbox, OneDrive - paste a token from `rclone authorize` -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'token'" class="account-inline-form is-block mt-3">
			<b-field :label="$t('Label')">
				<b-input v-model="form.label" size="is-small" :placeholder="activeProvider.label"></b-input>
			</b-field>
			<p class="field-help">
				{{ $t('Run this command on any computer with a web browser (or use the built-in Terminal), sign in, then paste the token it prints below.') }}
			</p>
			<code class="authorize-cmd">rclone authorize "{{ activeProvider.type }}"</code>
			<a class="advanced-toggle" @click="openTerminal">{{ $t('Open Terminal') }}</a>
			<b-field :label="$t('Token')">
				<b-input v-model="form.token" type="textarea" size="is-small" rows="3" :placeholder="$t('Paste the {...} token here')"></b-input>
			</b-field>
			<div class="form-actions">
				<b-button rounded size="is-small" type="is-dark" :loading="submitting" :disabled="!form.token.trim()" @click="submitToken">{{ $t('Connect') }}</b-button>
				<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
			</div>
			<p v-if="error" class="error-note">{{ error }}</p>
		</div>

		<!-- interactive-kind: iCloud - Apple ID + password, then a 2FA code -->
		<div v-if="activeProvider && activeProvider.auth_kind === 'interactive'" class="account-inline-form is-block mt-3">
			<template v-if="!icloud.sessionId">
				<b-field :label="$t('Label')">
					<b-input v-model="form.label" size="is-small" :placeholder="activeProvider.label"></b-input>
				</b-field>
				<b-field :label="$t('Apple ID')">
					<b-input v-model="icloud.appleId" size="is-small" type="email"></b-input>
				</b-field>
				<b-field :label="$t('Password')">
					<b-input v-model="icloud.password" size="is-small" type="password" password-reveal></b-input>
				</b-field>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="submitting" @click="startIcloud">{{ $t('Continue') }}</b-button>
					<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
				</div>
			</template>
			<template v-else>
				<b-field :label="icloud.question ? icloud.question.Help : ''">
					<b-input
						v-model="icloud.answer"
						size="is-small"
						:type="icloud.question && icloud.question.IsPassword ? 'password' : 'text'"
					></b-input>
				</b-field>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="submitting" @click="verifyIcloud">{{ $t('Verify') }}</b-button>
					<b-button rounded size="is-small" @click="cancelAdd">{{ $t('Cancel') }}</b-button>
				</div>
			</template>
			<p v-if="error" class="error-note">{{ error }}</p>
		</div>
	</div>
</template>

<script>
export default {
	name: 'cloud-accounts-panel',
	data() {
		return {
			accounts: [],
			providers: [],
			removingKey: null,
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
		}
	},
	created() {
		this.refresh()
		this.$api.cloud.providers().then(res => {
			if (res.data.success === 200) this.providers = res.data.data || []
		})
	},
	methods: {
		refresh() {
			this.$api.cloud.list().then(res => {
				if (res.data.success === 200) this.accounts = res.data.data || []
			})
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
						this.refresh()
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
						this.refresh()
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
				this.refresh()
				return
			}
			this.icloud.sessionId = step.session_id
			this.icloud.question = step.question
			this.icloud.answer = ''
		},
		confirmRemove(account) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove account'),
				message: this.$t('Disconnect {name}? This unmounts it from Files - it will no longer be accessible from here.', { name: account.name || account.fs }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.removingKey = account.mount_point
					this.$api.cloud
						.umount({ mount_point: account.mount_point })
						.then(() => this.refresh())
						.finally(() => {
							this.removingKey = null
						})
				}
			})
		},
		openTerminal() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480
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

.provider-grid {
	display: flex;
	flex-wrap: wrap;
	gap: 0.5rem;
	padding: 0 1.25rem 0.5rem;
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
	}

	&:hover {
		background: rgba(0, 0, 0, 0.03);
	}

	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);
		color: #3273dc;
	}
}

.account-inline-form.is-block {
	display: block;
	padding: 0.75rem 1.25rem 1rem;
}

.field-help {
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.55);
	margin-bottom: 0.35rem;
}

.authorize-cmd {
	display: block;
	background: rgba(0, 0, 0, 0.05);
	border-radius: 6px;
	padding: 0.4rem 0.6rem;
	font-size: 0.75rem;
	margin-bottom: 0.5rem;
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
