<template>
	<div class="cloud-accounts-list">
		<div v-for="a in accounts" :key="a.mount_point">
			<div class="setting-row">
				<i class="row-icon mdi" :class="`mdi-${a.icon || 'cloud-outline'}`"></i>
				<div class="row-label">
					<template v-if="editingKey === a.mount_point">
						<b-input v-model="editLabel" size="is-small" @keyup.enter.native="submitRename(a)"></b-input>
					</template>
					<template v-else>
						<div class="setting-title">{{ a.name || a.fs }}</div>
						<div class="setting-desc">
							{{ a.mount_point }}
							<span v-if="speedResults[a.mount_point]" class="speed-result">
								&middot; &uarr; {{ speedResults[a.mount_point].upload_mbps.toFixed(1) }} {{ $t('MB/s') }}
								&middot; &darr; {{ speedResults[a.mount_point].download_mbps.toFixed(1) }} {{ $t('MB/s') }}
							</span>
							<span v-if="speedErrors[a.mount_point]" class="speed-result is-error">&middot; {{ speedErrors[a.mount_point] }}</span>
						</div>
					</template>
				</div>
				<div class="row-control">
					<template v-if="editingKey === a.mount_point">
						<b-button rounded size="is-small" type="is-dark" :loading="renaming" @click="submitRename(a)">{{ $t('Save') }}</b-button>
						<b-button rounded size="is-small" @click="editingKey = null">{{ $t('Cancel') }}</b-button>
					</template>
					<template v-else>
						<button class="icon-button" type="button" :title="$t('Test speed')" :disabled="speedTestingKey === a.mount_point" @click="runSpeedTest(a)">
							<i class="mdi mdi-speedometer" :class="{ 'speedtest-pulse': speedTestingKey === a.mount_point }"></i>
						</button>
						<button v-if="reconnectableKinds[a.type]" class="icon-button" type="button" :title="$t('Reconnect')" @click="toggleReconnect(a)">
							<i class="mdi mdi-refresh"></i>
						</button>
						<button class="icon-button" type="button" :title="$t('Rename')" @click="startRename(a)">
							<i class="mdi mdi-pencil-outline"></i>
						</button>
						<b-button rounded size="is-small" type="is-danger" outlined :loading="removingKey === a.mount_point" @click="confirmRemove(a)">
							{{ $t('Remove') }}
						</b-button>
					</template>
				</div>
			</div>

			<!-- Reconnect: token-kind (Drive/Dropbox/OneDrive) - paste a fresh token -->
			<div v-if="reconnectingKey === a.mount_point && reconnectableKinds[a.type] === 'token'" class="reconnect-form">
				<p class="field-help">{{ $t('Run this, sign in again, then paste the fresh token below.') }}</p>
				<code class="authorize-cmd">rclone authorize "{{ a.type }}"</code>
				<a class="advanced-toggle" @click="openTerminal(a)">{{ $t('Run it in Terminal') }}</a>
				<b-input v-model="reconnectToken" type="textarea" size="is-small" rows="3" :placeholder="$t('Paste it here')"></b-input>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="reconnecting" :disabled="!reconnectToken.trim()" @click="submitReconnectToken(a)">{{ $t('Reconnect') }}</b-button>
					<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
				</div>
				<p v-if="reconnectError" class="error-note">{{ reconnectError }}</p>
			</div>

			<!-- Reconnect: iCloud - Apple ID + password, then a 2FA code -->
			<div v-if="reconnectingKey === a.mount_point && reconnectableKinds[a.type] === 'interactive'" class="reconnect-form">
				<template v-if="!icloud.sessionId">
					<b-field :label="$t('Apple ID')">
						<b-input v-model="icloud.appleId" size="is-small" type="email"></b-input>
					</b-field>
					<b-field :label="$t('Password')">
						<b-input v-model="icloud.password" size="is-small" type="password" password-reveal></b-input>
					</b-field>
					<div class="form-actions">
						<b-button rounded size="is-small" type="is-dark" :loading="reconnecting" :disabled="!icloud.appleId || !icloud.password" @click="submitReconnectIcloud(a)">{{ $t('Continue') }}</b-button>
						<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
					</div>
				</template>
				<template v-else>
					<b-field :label="icloud.question ? icloud.question.Help : $t('Enter the code')">
						<b-input v-model="icloud.answer" size="is-small" :type="icloud.question && icloud.question.IsPassword ? 'password' : 'text'"></b-input>
					</b-field>
					<div class="form-actions">
						<b-button rounded size="is-small" type="is-dark" :loading="reconnecting" :disabled="!icloud.answer" @click="verifyReconnectIcloud(a)">{{ $t('Verify') }}</b-button>
						<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
					</div>
				</template>
				<p v-if="reconnectError" class="error-note">{{ reconnectError }}</p>
			</div>
		</div>

		<div v-if="!accounts.length" class="account-empty">
			{{ $t('Nothing connected yet - add one below and it shows up as a location in Files.') }}
		</div>
	</div>
</template>

<script>
import events from '@/events/events'

export default {
	name: 'cloud-accounts-list',
	data() {
		return {
			accounts: [],
			removingKey: null,
			editingKey: null,
			editLabel: '',
			renaming: false,
			reconnectableKinds: {},
			reconnectingKey: null,
			reconnectToken: '',
			reconnecting: false,
			reconnectError: '',
			icloud: { appleId: '', password: '', sessionId: '', question: null, answer: '' },
			speedTestingKey: null,
			speedResults: {},
			speedErrors: {}
		}
	},
	created() {
		this.refresh()
		this.$api.cloud.providers().then(res => {
			if (res.data.success === 200) {
				const kinds = {}
				;(res.data.data || []).forEach(p => {
					// Reconnect only makes sense for kinds whose credentials can
					// expire/revoke independent of the stored fields themselves
					// (an OAuth token, an Apple session) - form-kind server
					// details are edited directly instead, not "reconnected".
					if (p.auth_kind === 'token' || p.auth_kind === 'interactive') kinds[p.type] = p.auth_kind
				})
				this.reconnectableKinds = kinds
			}
		})
	},
	methods: {
		refresh() {
			this.$api.cloud.list().then(res => {
				if (res.data.success === 200) this.accounts = res.data.data || []
			})
			// Files' MountList lives in a separate component tree with no other
			// way to learn a cloud account was added/renamed/reconnected/removed.
			this.$EventBus.$emit(events.RELOAD_MOUNT_LIST)
		},
		startRename(a) {
			this.editingKey = a.mount_point
			this.editLabel = a.name || ''
		},
		submitRename(a) {
			if (!this.editLabel.trim()) return
			this.renaming = true
			this.$api.cloud.rename(a.fs, this.editLabel.trim()).then(res => {
				if (res.data.success === 200) {
					this.editingKey = null
					this.refresh()
				}
			}).finally(() => {
				this.renaming = false
			})
		},
		toggleReconnect(a) {
			this.reconnectError = ''
			if (this.reconnectingKey === a.mount_point) {
				this.reconnectingKey = null
				return
			}
			this.reconnectingKey = a.mount_point
			this.reconnectToken = ''
			this.icloud = { appleId: '', password: '', sessionId: '', question: null, answer: '' }
		},
		submitReconnectToken(a) {
			this.reconnectError = ''
			this.reconnecting = true
			this.$api.cloud.reconnect(a.fs, { token: this.reconnectToken.trim() }).then(res => {
				if (res.data.success === 200) {
					this.reconnectingKey = null
					this.refresh()
					this.$buefy.toast.open({ message: this.$t('Reconnected'), type: 'is-success' })
				} else {
					this.reconnectError = res.data.message
				}
			}).catch(e => {
				this.reconnectError = (e.response && e.response.data && e.response.data.data) || this.$t('Failed to reconnect')
			}).finally(() => {
				this.reconnecting = false
			})
		},
		submitReconnectIcloud(a) {
			this.reconnectError = ''
			this.reconnecting = true
			this.$api.cloud.icloudStart({ label: a.name, apple_id: this.icloud.appleId, password: this.icloud.password, name: a.fs }).then(res => {
				this.handleIcloudStep(a, res)
			}).catch(e => {
				this.reconnectError = (e.response && e.response.data && e.response.data.data) || this.$t('Failed to start iCloud sign-in')
			}).finally(() => {
				this.reconnecting = false
			})
		},
		verifyReconnectIcloud(a) {
			this.reconnectError = ''
			this.reconnecting = true
			this.$api.cloud.icloudVerify({ session_id: this.icloud.sessionId, code: this.icloud.answer }).then(res => {
				this.handleIcloudStep(a, res)
			}).catch(e => {
				this.reconnectError = (e.response && e.response.data && e.response.data.data) || this.$t('Verification failed')
			}).finally(() => {
				this.reconnecting = false
			})
		},
		handleIcloudStep(a, res) {
			if (res.data.success !== 200) {
				this.reconnectError = res.data.message
				return
			}
			const step = res.data.data
			if (step.error) {
				this.reconnectError = step.error
				return
			}
			if (step.done) {
				this.reconnectingKey = null
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Reconnected'), type: 'is-success' })
				return
			}
			this.icloud.sessionId = step.session_id
			this.icloud.question = step.question
			this.icloud.answer = ''
		},
		runSpeedTest(a) {
			this.$set(this.speedErrors, a.mount_point, null)
			this.speedTestingKey = a.mount_point
			this.$api.cloud.speedTest(a.fs).then(res => {
				if (res.data.success === 200) {
					this.$set(this.speedResults, a.mount_point, res.data.data)
				} else {
					this.$set(this.speedErrors, a.mount_point, res.data.message)
				}
			}).catch(e => {
				this.$set(this.speedErrors, a.mount_point, (e.response && e.response.data && e.response.data.data) || this.$t('Speed test failed'))
			}).finally(() => {
				this.speedTestingKey = null
			})
		},
		openTerminal(a) {
			const host = window.location.hostname
			const initCommand = `rclone authorize "${a.type}" 2>&1 | sed -u "s/127\\.0\\.0\\.1:53682/${host}:53682/g"`
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { initCommand }
			})
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
		}
	}
}
</script>

<style lang="scss" scoped>
.icon-button {
	flex-shrink: 0;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.7rem;
	height: 1.7rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);
	margin-right: 0.4rem;

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.09);
		color: #1e293b;
	}

	&:disabled {
		opacity: 0.5;
		cursor: default;
	}
}

.mdi-spin {
	animation: cloud-list-spin 1s linear infinite;
}

@keyframes cloud-list-spin {
	from { transform: rotate(0deg); }
	to { transform: rotate(360deg); }
}

// A speedometer needle doesn't read as "working" when spun in a full circle
// like a refresh icon - a small back-and-forth tilt plus a soft pulse reads
// as "measuring" instead, without implying rotation the icon doesn't have.
.speedtest-pulse {
	display: inline-block;
	animation: speedtest-pulse 0.9s ease-in-out infinite;
	color: #3273dc;
}

@keyframes speedtest-pulse {
	0%, 100% { transform: rotate(-12deg); opacity: 0.55; }
	50% { transform: rotate(12deg); opacity: 1; }
}

.speed-result {
	color: #3273dc;

	&.is-error {
		color: #ef4444;
	}
}

.reconnect-form {
	padding: 0.75rem 1.25rem 1rem 3.25rem;
	background: rgba(0, 0, 0, 0.02);
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
