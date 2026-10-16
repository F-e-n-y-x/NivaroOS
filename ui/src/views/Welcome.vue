<template>
	<div id="login-page" class="is-flex is-justify-content-center is-align-items-center">
		<div v-if="!isLoading" v-animate-css="initAni" :class="'step' + step" class="login-panel is-shadow">

			<div v-if="step == 1" class="has-text-centered">
				<img v-animate-css="s2Ani" :src="$assetUrl('img/logo/icon.svg')" alt="" class="welcome-mark"/>
				<h2 v-animate-css="s2Ani" class="title is-2 mb-5 has-text-centered __attached_title">{{
						$t('Welcome to NivaroOS')
					}}</h2>
				<h2 v-animate-css="s3Ani" class="subtitle  has-text-centered __attached_sub_title">{{
						$t(`Let's create your initial account`)
					}}</h2>
				<b-button v-animate-css="s4Ani" class="mt-2" rounded type="is-primary" @click="goToStep(2)">{{
						$t(`Go →`)
					}}
				</b-button>
				<p v-animate-css="s4Ani" class="mt-4">
					<a href="#" @click.prevent="goToStep(4)">{{ $t('settings_restore.link') }}</a>
				</p>
			</div>

			<!-- Restore a "NivaroOS settings" backup instead of starting fresh
			     (services/backup POST /config-restore/open, then /apply). -->
			<div v-if="step === 4">
				<h2 class="title is-3 has-text-centered">{{ $t('settings_restore.title') }}</h2>
				<template v-if="!restore.plan">
					<p class="mb-4">{{ $t('settings_restore.intro') }}</p>
					<b-field :label="$t('settings_restore.file')">
						<input class="input" type="file" accept=".001,.7z" @change="restore.file = $event.target.files[0] || null" />
					</b-field>
					<b-field :label="$t('settings_restore.key')" :message="$t('settings_restore.key_hint')">
						<b-input v-model="restore.key" password-reveal type="password" autocomplete="off" @keyup.enter="openRestore"></b-input>
					</b-field>
					<b-field :label="$t('settings_restore.key_file')" :message="$t('settings_restore.key_file_hint')">
						<input class="input" type="file" accept=".json" @change="restore.keyFile = $event.target.files[0] || null" />
					</b-field>
					<p v-if="restore.error" class="has-text-danger mt-2" role="alert">{{ restore.error }}</p>
					<b-button class="mt-4" expanded rounded type="is-primary" :loading="restore.busy" :disabled="!restore.file || !restore.key" @click="openRestore">
						{{ $t('settings_restore.check') }}
					</b-button>
					<b-button class="mt-2" expanded rounded type="is-text" @click="goToStep(1)">{{ $t('settings_restore.back') }}</b-button>
				</template>
				<template v-else-if="!restore.started">
					<p>{{ $t('settings_restore.from', { host: restore.plan.from_hostname, version: restore.plan.from_version || '?', date: new Date(restore.plan.created).toLocaleString() }) }}</p>
					<p class="mt-2">{{ $t('settings_restore.counts', restoreCounts) }}</p>
					<p v-if="restore.plan.smb_users.length" class="mt-2">{{ $t('settings_restore.smb_users', { users: restore.plan.smb_users.join(', ') }) }}</p>
					<p v-if="newPort" class="mt-2">{{ $t('settings_restore.port', { port: restore.plan.gateway_port }) }}</p>
					<p v-for="w in restore.plan.warnings" :key="w" class="mt-2 __op60">{{ w }}</p>
					<p class="mt-2 __op60">{{ $t('settings_restore.data_kept') }}</p>
					<p v-if="restore.plan.problem" class="has-text-danger mt-2" role="alert">{{ restore.plan.problem }}</p>
					<p v-if="restore.error" class="has-text-danger mt-2" role="alert">{{ restore.error }}</p>
					<b-button class="mt-4" expanded rounded type="is-primary" :loading="restore.busy" :disabled="!!restore.plan.problem" @click="applyRestore">
						{{ $t('settings_restore.restore') }}
					</b-button>
					<b-button class="mt-2" expanded rounded type="is-text" @click="restore.plan = null">{{ $t('settings_restore.back') }}</b-button>
				</template>
				<template v-else>
					<p role="status">{{ $t('settings_restore.restarting') }}</p>
					<p v-if="newPort" class="mt-2"><a :href="newUrl">{{ newUrl }}</a></p>
				</template>
			</div>

			<div v-if="step == 2">
				<h2 class="title is-3  has-text-centered">{{ $t('Create Account') }}</h2>
				<div class="is-flex is-justify-content-center ">
					<div class="has-text-centered">
						<b-image :src="$assetUrl('img/account/default-avatar.svg')" class="is-128x128"
								 rounded></b-image>
					</div>
				</div>
				<b-field :label="$t('Username')" :message="fieldMessage('username')" :type="fieldType('username')">
					<b-input v-model="username" type="text" @keyup.enter="submit"></b-input>
				</b-field>
				<b-field :label="$t('Password')" :message="fieldMessage('password')" :type="fieldType('password')" class="mt-4">
					<b-input v-model="password" password-reveal type="password" @keyup.enter="submit"></b-input>
				</b-field>
				<b-field :label="$t('Confirm Password')" :message="fieldMessage('confirmation')" :type="fieldType('confirmation')" class="mt-4">
					<b-input v-model="confirmation" password-reveal type="password" @keyup.enter="submit"></b-input>
				</b-field>
				<b-button class="mt-5" expanded rounded type="is-primary" @click="submit">
					{{ $t('Create') }}
				</b-button>
			</div>

			<div v-if="step == 3" class="has-text-centered ">
				<h2 class="title is-3  has-text-centered">{{ $t('All things done!') }}</h2>
				<div class="is-flex is-align-items-center is-justify-content-center">
					<div ref="doneAnimation" class="animation"></div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import lottie from 'lottie-web/build/player/lottie_light'
import smoothReflow from '@/mixins/smoothReflow'
import formChecks, { firstError, minLength, required, sameAs } from '@/mixins/formChecks'
import doneAnimation from '@/assets/ani/done.json'
import backup from '@/service/backup'

export default {

	name: "welcome-page",
	mixins: [smoothReflow, formChecks],
	data() {
		return {
			step: 1,
			username: '',
			password: '',
			confirmation: "",
			isLoading: true,
			isLogin: false,
			message: "",
			notificationShow: false,
			restore: { file: null, keyFile: null, key: '', busy: false, error: '', plan: null, token: '', started: false },
			initAni: {
				classes: 'zoomIn',
				delay: 1000,
				duration: 700
			},
			s2Ani: {
				classes: 'fadeInUp',
				delay: 1700,
				duration: 700
			},
			s3Ani: {
				classes: 'fadeInUp',
				delay: 1900,
				duration: 700
			},
			s4Ani: {
				classes: 'fadeIn',
				delay: 2500,
				duration: 700
			}
		}
	},
	computed: {
		restoreCounts() {
			const n = { add: 0, replace: 0, same: 0, import: 0 }
			for (const it of this.restore.plan.items) n[it.action] = (n[it.action] || 0) + 1
			return { changed: n.add + n.replace, same: n.same }
		},
		// The dashboard's port after the restore, when it differs from now.
		newPort() {
			const p = this.restore.plan && this.restore.plan.gateway_port
			return p && p !== (window.location.port || '80') ? p : ''
		},
		newUrl() {
			return `${window.location.protocol}//${window.location.hostname}:${this.newPort}/`
		},
		fieldErrors() {
			return {
				username: firstError(this.username, [required]),
				password: firstError(this.password, [required, minLength(5)]),
				confirmation: firstError(this.confirmation, [required, sameAs(this.password)]),
			}
		},
	},
	watch: {
		username() {
			this.markChecked('username')
		},
		password() {
			this.markChecked('password')
		},
		confirmation() {
			this.markChecked('confirmation')
		},
		step(step) {
			if (step !== 3) return
			this.$nextTick(() => {
				this.animation = lottie.loadAnimation({ container: this.$refs.doneAnimation, renderer: 'svg', loop: false, autoplay: true, animationData: doneAnimation })
				this.animation.addEventListener('complete', this.complete)
			})
		},
	},
	beforeUnmount() {
		if (this.animation) this.animation.destroy()
		clearTimeout(this.restoreTimer)
	},

	mounted() {
		this.$smoothReflow({
			el: '.login-panel',
			property: ['height', 'width'],
		})
		this.isLoading = false;

	},

	methods: {
		submit() {
			if (this.validateAll()) this.register()
		},
		/**
		 * @description: register
		 * @return {*}
		 */
		register() {
			const initKey = this.$store.state.initKey;
			this.$api.users.register(this.username, this.password, initKey).then(res => {
				if (res.data.success == 200) {
					this.login().then(() => {
						// First login set default app order
						this.$api.users.setCustomStorage("app_order", {data: ["App Store", "Files"]})
					});
					this.goToStep(3);
				}
			}).catch(err => {
				this.$buefy.toast.open({
					message: err.response.data.message,
					type: 'is-danger',
					position: 'is-top',
					duration: 5000,
					queue: false
				})
			})
		},

		/**
		 * @description: login
		 * @return {*}
		 */
		async login() {
			const userRes = await this.$api.users.login(this.username, this.password)
			if (userRes.data.success == 200) {
				localStorage.setItem("access_token", userRes.data.data.token.access_token);
				localStorage.setItem("refresh_token", userRes.data.data.token.refresh_token);
				localStorage.setItem("expires_at", userRes.data.data.token.expires_at);
				localStorage.setItem("user", JSON.stringify(userRes.data.data.user));

				this.$store.commit("SET_NEED_INITIALIZATION", false);
				this.$store.commit("SET_INIT_KEY", "");
				this.$store.commit("SET_USER", userRes.data.data.user);
				this.$store.commit("SET_ACCESS_TOKEN", userRes.data.data.token.access_token);
				this.$store.commit("SET_REFRESH_TOKEN", userRes.data.data.token.refresh_token);
				sessionStorage.setItem("fromWelcome", true);
				this.isLogin = true

			} else {
				this.isLogin = false
				this.message = this.$t("Username or Password error!")
				this.notificationShow = true
			}
		},
		async openRestore() {
			const r = this.restore
			if (!r.file || !r.key || r.busy) return
			r.busy = true
			r.error = ''
			try {
				const form = new FormData()
				form.append('bundle', r.file)
				form.append('key', r.key)
				if (r.keyFile) form.append('key_file', r.keyFile)
				const res = await backup.openSettingsRestore(form)
				r.token = res.token
				r.plan = res.plan
			} catch (e) {
				r.error = this.restoreError(e)
			} finally {
				r.busy = false
			}
		},
		async applyRestore() {
			const r = this.restore
			r.busy = true
			r.error = ''
			try {
				await backup.applySettingsRestore(r.token)
				r.started = true
				if (!this.newPort) this.restoreTimer = setTimeout(this.waitRestored, 5000)
			} catch (e) {
				r.error = this.restoreError(e)
			} finally {
				r.busy = false
			}
		},
		// The services restart; the restored accounts are there once
		// user-service answers "initialized" again.
		async waitRestored() {
			try {
				const res = await this.$api.users.getUserStatus()
				if (res.data.success === 200 && res.data.data.initialized) {
					this.$store.commit('SET_NEED_INITIALIZATION', false)
					this.$router.push('/login')
					return
				}
			} catch {
				// restarting
			}
			this.restoreTimer = setTimeout(this.waitRestored, 3000)
		},
		restoreError(e) {
			if (e.code === 'wrong_password') return this.$t('settings_restore.wrong_key')
			if (e.code === 'service_unavailable') return this.$t('settings_restore.unavailable')
			return e.detail || e.message || String(e)
		},
		goToStep(step) {
			this.step = step
		},
		complete() {
			if (this.isLogin) {
				this.$router.push("/");
			} else {
				this.$router.push("/login");
			}
		}
	}
}
</script>

<style lang="scss">
.welcome-mark {
	display: block;
	width: 4.5rem;
	height: 4.5rem;
	margin: 0 auto var(--space-6);
}

.animation {
	width: 120px;
	height: 120px;
}


#login-page {
	height: calc(100% - 5.5rem);
	position: relative;
	z-index: 500;

	.login-panel {
		text-align: left;
		background: var(--theme-card-bg, rgba(255, 255, 255, 0.46));
		backdrop-filter: blur(1rem);
		border-radius: var(--radius-control);
		padding: var(--space-8) var(--space-16);

		.label {
			color: var(--theme-text-primary, #1e293b);
		}

		.input {
			background: var(--theme-input-bg, rgba(255, 255, 255, 0.32));
			border-color: transparent;
		}

		&.step1 {
			padding: var(--space-16) 6rem;
		}

		&.step2 {
			padding: var(--space-8) var(--space-16);
			width: 32rem;
		}

		&.step3 {
			padding: var(--space-16) 8rem;
		}

		&.step4 {
			width: 28rem;
		}
	}
}

@media screen and (max-width: 480px) {
	.login-panel {
		text-align: left;
		background: var(--theme-card-bg, rgba(255, 255, 255, 0.46));
		backdrop-filter: blur(1rem);
		border-radius: var(--radius-control);
		margin: 0 var(--space-8);
		padding: var(--space-8) !important;

		.label {
			color: var(--theme-text-primary, #1e293b);
		}

		.input {
			background: var(--theme-input-bg, rgba(255, 255, 255, 0.32));
			border-color: transparent;
		}

		.is-128x128 {
			height: 96px;
			width: 96px;
		}

		.is-3 {
			font-size: var(--font-2xl);
		}

		&.step1 {
			.is-2 {
				font-size: var(--font-2xl);
			}

			.subtitle {
				font-size: var(--font-md);
			}
		}

		&.step3 {
			padding: var(--space-16) !important;
		}
	}
}


// Temporary
.__attached_title {
	// former color.Not in existing architecture.
	color: hsl(211, 72%, 20%, 100%);;
}

.__attached_sub_title {
	color: hsl(211, 72%, 20%, 60%);
}

.__op60 {
	opacity: 0.6;
}
</style>
