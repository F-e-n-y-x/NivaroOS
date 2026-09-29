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
							<span v-if="speedLive[a.mount_point]" class="speed-result is-live">
								&middot; {{ speedPhaseLabel(speedLive[a.mount_point]) }}<template v-if="speedLive[a.mount_point].live_mbps > 0"> {{ fmtMbps(speedLive[a.mount_point].live_mbps) }} Mbps</template>
								<a class="speed-stop" @click="stopSpeedTest(a)">{{ $t('Stop') }}</a>
							</span>
							<span v-else-if="speedResults[a.mount_point]" class="speed-result" :title="speedDetail(speedResults[a.mount_point])">
								&middot; &darr; {{ fmtMbps(speedResults[a.mount_point].download.mbps) }} Mbps ({{ speedResults[a.mount_point].download.mb_per_s.toFixed(1) }} {{ $t('MB/s') }})
								&middot; &uarr; {{ fmtMbps(speedResults[a.mount_point].upload.mbps) }} Mbps ({{ speedResults[a.mount_point].upload.mb_per_s.toFixed(1) }} {{ $t('MB/s') }})
								&middot; {{ $t('{time} setup', { time: fmtMs(speedResults[a.mount_point].download.latency_ms) }) }}
							</span>
							<span v-if="speedErrors[a.mount_point]" class="speed-result is-error">&middot; {{ speedErrors[a.mount_point] }}</span>
							<span v-for="(note, i) in (!speedLive[a.mount_point] && speedResults[a.mount_point] && speedResults[a.mount_point].notes) || []" :key="i" class="speed-note">
								<i class="mdi mdi-information-outline"></i> {{ note }}
							</span>
						</div>
					</template>
				</div>
				<div class="row-control">
					<template v-if="editingKey === a.mount_point">
						<b-button rounded size="is-small" type="is-primary" :loading="renaming" @click="submitRename(a)">{{ $t('Save') }}</b-button>
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
					<b-button rounded size="is-small" type="is-primary" :loading="reconnecting" :disabled="!reconnectToken.trim()" @click="submitReconnectToken(a)">{{ $t('Reconnect') }}</b-button>
					<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
				</div>
				<p v-if="reconnectError" class="error-note">{{ reconnectError }}</p>
			</div>

			<!-- Reconnect: cookie-login (TeraBox) - sign in again in the browser,
			     or paste a fresh cookie -->
			<div v-if="reconnectingKey === a.mount_point && reconnectableKinds[a.type] === 'browser'" class="reconnect-form">
				<p class="field-help">{{ $t('Sign in to {provider} again - the new sign-in replaces the old one, and the account keeps its name and place in Files.', { provider: a.name || a.type }) }}</p>
				<div class="form-actions">
					<b-button rounded size="is-small" type="is-primary" icon-left="login" :loading="browserSignin.checking" :disabled="!browserSignin.available" @click="reconnectWithBrowser(a)">{{ $t('Sign in again') }}</b-button>
					<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
				</div>
				<p v-if="browserSignin.hint" class="field-help">{{ browserSignin.hint }}</p>
				<a class="advanced-toggle" @click="pasteCookie = !pasteCookie">{{ pasteCookie ? $t('Hide') : $t('Paste a cookie instead (advanced)') }}</a>
				<template v-if="pasteCookie">
					<b-input v-model="reconnectToken" type="textarea" size="is-small" rows="3" :placeholder="$t('ndus=...')"></b-input>
					<div class="form-actions">
						<b-button rounded size="is-small" type="is-primary" :loading="reconnecting" :disabled="!reconnectToken.trim()" @click="submitReconnectCookie(a, reconnectToken.trim())">{{ $t('Reconnect') }}</b-button>
					</div>
				</template>
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
						<b-button rounded size="is-small" type="is-primary" :loading="reconnecting" :disabled="!icloud.appleId || !icloud.password" @click="submitReconnectIcloud(a)">{{ $t('Continue') }}</b-button>
						<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
					</div>
				</template>
				<template v-else>
					<b-field :label="icloud.question ? icloud.question.Help : $t('Enter the code')">
						<b-input v-model="icloud.answer" size="is-small" :type="icloud.question && icloud.question.IsPassword ? 'password' : 'text'"></b-input>
					</b-field>
					<div class="form-actions">
						<b-button rounded size="is-small" type="is-primary" :loading="reconnecting" :disabled="!icloud.answer" @click="verifyReconnectIcloud(a)">{{ $t('Verify') }}</b-button>
						<b-button rounded size="is-small" @click="reconnectingKey = null">{{ $t('Cancel') }}</b-button>
					</div>
				</template>
				<p v-if="reconnectError" class="error-note">{{ reconnectError }}</p>
			</div>
		</div>

		<div v-if="!accounts.length" class="account-empty">
			{{ $t('Nothing connected yet - add one below and it shows up as a location in Files.') }}
		</div>
		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import { escapeHtml } from '@/utils/escapeHtml'
import { apiError } from '@/utils/apiError'
import events from '@/events/events'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import { browserSigninAvailability, openBrowserSignin } from '@/apps/download-station/rb/browserSignin'

// Cookie-login providers: reconnecting means signing in again in Download
// Station's browser (rb/RbSigninWindow.vue).
const BROWSER_SIGNIN_TYPES = ['terabox']

export default {
	name: 'cloud-accounts-list',
	mixins: [confirmWindowMixin],
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
			pasteCookie: false,
			browserSignin: { available: false, checking: false, hint: '' },
			icloud: { appleId: '', password: '', sessionId: '', question: null, answer: '' },
			speedTestingKey: null,
			speedResults: {},
			speedErrors: {},
			speedLive: {},
			speedTimers: {}
		}
	},
	beforeDestroy() {
		Object.values(this.speedTimers).forEach(t => clearTimeout(t))
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
					// A cookie sign-in expires too; it is renewed by signing in again.
					if (BROWSER_SIGNIN_TYPES.includes(p.type)) kinds[p.type] = 'browser'
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
			}).catch(e => this.$buefy.toast.open({ message: escapeHtml(apiError(e, this.$t('Could not rename the account'))), type: 'is-danger', duration: 6000 })).finally(() => {
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
			this.pasteCookie = false
			this.icloud = { appleId: '', password: '', sessionId: '', question: null, answer: '' }
			if (this.reconnectableKinds[a.type] === 'browser') {
				this.browserSignin = { available: false, checking: true, hint: '' }
				browserSigninAvailability(k => this.$t(k)).then(av => {
					this.browserSignin = { checking: false, ...av }
					if (!av.available) this.pasteCookie = true
				})
			}
		},
		reconnectWithBrowser(a) {
			openBrowserSignin(this.$store, {
				provider: a.type,
				purpose: 'reconnect',
				title: this.$t('Sign in to {provider} again', { provider: a.name || a.type }),
				onConnected: cookie => this.submitReconnectCookie(a, cookie, true)
			})
		},
		// fromWindow: errors go back to the sign-in window (which shows them)
		// instead of this form.
		async submitReconnectCookie(a, cookie, fromWindow) {
			this.reconnectError = ''
			this.reconnecting = true
			try {
				let res
				try {
					res = await this.$api.cloud.reconnect(a.fs, { cookie })
				} catch (e) {
					throw new Error((e.response && e.response.data && e.response.data.data) || this.$t('Failed to reconnect'))
				}
				if (res.data.success !== 200) throw new Error(res.data.message || this.$t('Failed to reconnect'))
				this.reconnectingKey = null
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Reconnected'), type: 'is-success' })
			} catch (e) {
				if (fromWindow) throw e
				this.reconnectError = e.message
			} finally {
				this.reconnecting = false
			}
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
		// The test runs in the background on the server (upload that grows
		// until it lasts a few seconds, then time-boxed single- and
		// multi-stream downloads); POST starts it, GET polls it.
		runSpeedTest(a) {
			const key = a.mount_point
			this.$set(this.speedErrors, key, null)
			this.speedTestingKey = key
			this.$api.cloud.speedTest(a.fs).then(res => {
				if (res.data.success === 200) {
					this.onSpeedState(a, res.data.data)
				} else {
					this.speedFailed(key, res.data.message)
				}
			}).catch(e => {
				this.speedFailed(key, (e.response && e.response.data && e.response.data.data) || this.$t('Speed test failed'))
			})
		},
		pollSpeedTest(a) {
			const key = a.mount_point
			this.$set(this.speedTimers, key, setTimeout(() => {
				this.$api.cloud.speedTestStatus(a.fs).then(res => {
					if (res.data.success === 200 && res.data.data) {
						this.onSpeedState(a, res.data.data)
					} else {
						this.speedFailed(key, this.$t('Speed test failed'))
					}
				}).catch(() => {
					// A dropped poll isn't a failed test - try again.
					this.pollSpeedTest(a)
				})
			}, 800))
		},
		onSpeedState(a, st) {
			const key = a.mount_point
			if (st.running) {
				this.$set(this.speedLive, key, st)
				this.pollSpeedTest(a)
				return
			}
			this.$delete(this.speedLive, key)
			if (this.speedTestingKey === key) this.speedTestingKey = null
			if (st.result) {
				this.$set(this.speedResults, key, st.result)
			} else if (st.cancelled) {
				// Stopped on request - keep whatever was shown before.
			} else {
				this.speedFailed(key, st.error || this.$t('Speed test failed'))
			}
		},
		speedFailed(key, msg) {
			this.$delete(this.speedLive, key)
			if (this.speedTestingKey === key) this.speedTestingKey = null
			this.$set(this.speedErrors, key, msg)
		},
		stopSpeedTest(a) {
			this.$api.cloud.speedTestCancel(a.fs).catch(() => {})
		},
		speedPhaseLabel(st) {
			const labels = {
				prepare: this.$t('Preparing'),
				upload: this.$t('Uploading'),
				download: this.$t('Downloading'),
				download_parallel: this.$t('Downloading on several connections'),
				cleanup: this.$t('Cleaning up')
			}
			return labels[st.phase] || this.$t('Testing')
		},
		fmtMbps(v) {
			return v < 10 ? v.toFixed(1) : Math.round(v).toString()
		},
		fmtMs(v) {
			return v >= 1000 ? `${(v / 1000).toFixed(1)} s` : `${Math.round(v)} ms`
		},
		speedDetail(r) {
			const d = r.download
			const u = r.upload
			const lines = [
				this.$t('Download: {single} Mbps on one connection', { single: this.fmtMbps(d.single_mbps) }) +
					(d.parallel_mbps ? ', ' + this.$t('{mbps} Mbps on {n} connections', { mbps: this.fmtMbps(d.parallel_mbps), n: d.parallel_streams }) : ''),
				this.$t('Upload: {mbps} Mbps', { mbps: this.fmtMbps(u.mbps) }),
				this.$t('Setup before the first byte: {down} download, {up} upload', { down: this.fmtMs(d.latency_ms), up: this.fmtMs(u.latency_ms) }),
				this.$t('Test file: {size} MB', { size: Math.round(r.file_bytes / 1e6) })
			]
			return lines.join('\n')
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
			this.confirmWindow({
				title: this.$t('Remove account'),
				message: this.$t('Disconnect {name}? This unmounts it from Files - it will no longer be accessible from here.', { name: account.name || account.fs }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.removingKey = account.mount_point
					this.$api.cloud
						.umount({ mount_point: account.mount_point })
						.then(() => this.$buefy.toast.open({ message: this.$t('Account removed'), type: 'is-success' }))
						.catch(e => this.$buefy.toast.open({ message: escapeHtml(apiError(e, this.$t('Could not remove the account'))), type: 'is-danger', duration: 6000 }))
						.finally(() => {
							this.removingKey = null
							this.refresh()
						})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.cloud-accounts-list {
	position: relative;
}

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
	color: var(--theme-text-muted);
	margin-right: var(--space-2);

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.09);
		color: var(--theme-text-primary, #1e293b);
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
	color: var(--color-primary-fg);
}

@keyframes speedtest-pulse {
	0%, 100% { transform: rotate(-12deg); opacity: 0.55; }
	50% { transform: rotate(12deg); opacity: 1; }
}

.speed-result {
	color: var(--color-primary-fg);

	&.is-error {
		color: var(--color-danger-fg);
	}

	&.is-live {
		color: var(--theme-text-secondary, #475569);
		font-variant-numeric: tabular-nums;
	}
}

.speed-stop {
	margin-left: var(--space-2);
}

.speed-note {
	display: block;
	margin-top: var(--space-1);
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
}

.reconnect-form {
	padding: var(--space-3) var(--space-5) var(--space-4) var(--space-8);
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.02));
}

.field-help {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	margin-bottom: var(--space-1);
}

.authorize-cmd {
	display: block;
	background: var(--theme-card-subtle, #f8fafc);
	border-radius: var(--radius-sm);
	padding: var(--space-2) var(--space-2);
	font-size: var(--font-xs);
	margin-bottom: var(--space-2);
}

.advanced-toggle {
	display: inline-block;
	font-size: var(--font-xs);
	color: var(--color-primary-fg);
	cursor: pointer;
	margin-bottom: var(--space-2);
}

.form-actions {
	display: flex;
	gap: var(--space-2);
	margin-top: var(--space-2);
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
	margin-top: var(--space-2);
}
</style>
