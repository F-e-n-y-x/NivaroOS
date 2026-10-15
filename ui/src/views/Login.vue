<template>
	<div id="login-page" class="is-flex is-justify-content-center is-align-items-center ">
		<div v-if="!isLoading" class="login-panel step4 is-shadow">
			<div class="login-brand">
				<img :src="$assetUrl('img/logo/logo.svg')" alt="NivaroOS" class="login-brand-img is-on-light"/>
				<img :src="$assetUrl('img/logo/logo-white.svg')" alt="NivaroOS" class="login-brand-img is-on-dark"/>
			</div>
			<div class="is-flex is-justify-content-center pb-3 ">
				<!-- The typed account's picture fades in over the placeholder; same box, nothing moves. -->
				<div class="login-avatar" aria-hidden="true">
					<img :src="$assetUrl('img/account/default-avatar.svg')" alt="" draggable="false" />
					<img v-if="photoSrc" :class="{ 'is-shown': photo }" :src="photoSrc" alt="" class="login-avatar-photo" draggable="false" />
				</div>

			</div>
			<b-notification v-model="notificationShow" aria-close-label="Close notification" role="alert"
							type="is-danger">
				{{ message }}
			</b-notification>
			<b-field :label="$t('Username')" :message="fieldMessage('username')" :type="fieldType('username')" class="mt-3">
				<b-input v-model="username" :autofocus="!username" type="text" @keyup.enter="submit"></b-input>
			</b-field>
			<b-field :label="$t('Password')" :message="fieldMessage('password')" :type="fieldType('password')" class="mt-2">
				<b-input v-model="password" :autofocus="!!username" password-reveal type="password" @keyup.enter="submit"></b-input>
			</b-field>
			<b-button class="mt-5" expanded rounded type="is-primary" :loading="busy" @click="submit">{{ $t('Login') }}
			</b-button>
		</div>
	</div>
</template>

<script>
import formChecks, { firstError, required } from '@/mixins/formChecks'
import { createAvatarLookup, rememberedAvatar, syncRememberedAvatar } from '@/utils/avatar'

const wallpaperConfig = "wallpaper"

export default {

	name: "login-page",
	mixins: [formChecks],
	data() {
		return {
			username: '',
			password: '',
			isLoading: false,
			busy: false,
			message: "",
			notificationShow: false,
			photo: '', // shown picture; '' = the placeholder
			photoSrc: '', // the last picture, kept while it fades out
		}
	},
	created() {
		this.lookupAvatar = createAvatarLookup(src => this.showPhoto(src || this.rememberedPicture))
	},
	computed: {
		fieldErrors() {
			return {
				username: firstError(this.username, [required]),
				// Sign-in only needs a password; length rules belong to sign-up.
				password: firstError(this.password, [required]),
			}
		},
		// The account last signed in on this browser: its picture at once.
		rememberedPicture() {
			return rememberedAvatar(this.username)
		}
	},
	beforeMount(){
		let userString = localStorage.getItem('user')
		if (userString) {
			let name = JSON.parse(userString).username || '';
			this.username = name;
		}
	},
	mounted() {
		document.querySelector('.modal.is-active ')?.remove();
		this.refreshWallpaper();
	},

	watch: {
		username() {
			this.markChecked('username')
			if (this.rememberedPicture) this.showPhoto(this.rememberedPicture)
			this.lookupAvatar(this.username)
		},
		password() {
			this.markChecked('password')
		},
	},
	methods: {
		showPhoto(src) {
			if (src) this.photoSrc = src
			this.photo = src
		},
		// A slow server used to take every extra click/Enter as another login.
		async submit() {
			if (this.busy || !this.validateAll()) return
			this.busy = true
			this.notificationShow = false
			try {
				await this.login()
			} finally {
				this.busy = false
			}
		},
		// Errors only: a green username after a failed sign-in read as
		// "username right, password wrong".
		fieldType(f) {
			return this.checkedFields[f] && this.fieldErrors[f] ? 'is-danger' : ''
		},
		// The background here otherwise only ever reflects whatever was
		// last cached to localStorage the last time this browser fully
		// loaded the desktop (see state.js) - if the wallpaper was changed
		// since then (or this is a different browser/session), login
		// shows a stale picture instead of the real one. This is the same
		// fetch Home.vue does after login, just attempted early using
		// whatever token (possibly expired, possibly still valid) is
		// already sitting in localStorage - fails silently and leaves the
		// existing cached background untouched if there's no usable
		// session yet, which is the normal/expected case for a fresh
		// login.
		refreshWallpaper() {
			this.$api.users.getUserStatus().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					if (data.wallpaper && data.wallpaper.path) {
						this.$store.commit('SET_WALLPAPER', data.wallpaper)
					}
					if (data.appearance) {
						if (data.appearance.alpha !== undefined && data.appearance.alpha !== null) {
							document.documentElement.style.setProperty('--ui-backdrop-alpha', data.appearance.alpha)
							localStorage.setItem('uiBackdropAlpha', data.appearance.alpha)
						}
						if (data.appearance.blur !== undefined && data.appearance.blur !== null) {
							document.documentElement.style.setProperty('--ui-backdrop-blur', `${data.appearance.blur}px`)
							localStorage.setItem('uiBackdropBlur', data.appearance.blur)
						}
					}
				}
			}).catch(() => {})

			if (localStorage.getItem('access_token')) {
				this.$api.users.getCustomStorage(wallpaperConfig).then(res => {
					if (res.data.success === 200 && res.data.data != "") {
						this.$store.commit('SET_WALLPAPER', {
							path: res.data.data.path,
							from: res.data.data.from
						})
					}
				}).catch(() => {})
			}
		},

		async login() {
			try {
				const userRes = await this.$api.users.login(this.username, this.password)
				localStorage.setItem("access_token", userRes.data.data.token.access_token);
				localStorage.setItem("refresh_token", userRes.data.data.token.refresh_token);
				localStorage.setItem("expires_at", userRes.data.data.token.expires_at);
				localStorage.setItem("user", JSON.stringify(userRes.data.data.user));

				this.$store.commit("SET_USER", userRes.data.data.user);
				syncRememberedAvatar(userRes.data.data.user)
				this.$store.commit("SET_ACCESS_TOKEN", userRes.data.data.token.access_token);
				this.$store.commit("SET_REFRESH_TOKEN", userRes.data.data.token.refresh_token);

				const versionRes = await this.$api.sys.getVersion();
				if (versionRes.data.success == 200) {
					localStorage.setItem("version", versionRes.data.data.current_version);
				}
				await this.$router.push("/");
				// The desktop is loaded on demand. A tab opened before an update
				// asks for files the server no longer has, and the router drops
				// that error - the login screen just stayed up until a manual
				// refresh. A full load gets the current build, already signed in.
				if (this.$route.path === "/login") {
					window.location.hash = "#/";
					window.location.reload();
				}
			} catch (err) {
				// No response at all (server restarting, network down) has no
				// message to show; reading one threw and the click did nothing.
				const msg = err && err.response && err.response.data && err.response.data.message
				this.message = msg ? this.$t(msg) : this.$t("Couldn't reach NivaroOS. Check the connection and try again.")
				this.notificationShow = true
			}
		}
	}
}
</script>

<style lang="scss">
html[data-theme="dark"] #login-page .login-brand-img {
	&.is-on-light {
		display: none;
	}

	&.is-on-dark {
		display: block;
	}
}

#login-page {
	height: calc(100% - 5.5rem);
	position: relative;
	z-index: 500;

	.login-brand {
		display: flex;
		justify-content: center;
		margin-bottom: var(--space-6);
	}

	.login-avatar {
		position: relative;
		width: 6rem;
		height: 6rem;
		border-radius: 50%;
		overflow: hidden;

		img {
			position: absolute;
			inset: 0;
			width: 100%;
			height: 100%;
			object-fit: cover;
		}

		.login-avatar-photo {
			opacity: 0;
			transition: opacity 250ms cubic-bezier(0.2, 0, 0, 1);

			&.is-shown {
				opacity: 1;
			}

			@media (prefers-reduced-motion: reduce) {
				transition: none;
			}
		}
	}

	.login-brand-img {
		height: 2rem;
		width: auto;

		&.is-on-dark {
			display: none;
		}
	}

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
</style>