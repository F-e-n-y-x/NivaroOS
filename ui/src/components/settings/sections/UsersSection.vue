<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Users & Access') }}</h2>

		<div class="setting-card">
			<div class="profile-row">
				<div class="profile-avatar" @click="triggerAvatarPick">
					<img :src="avatarUrl" @error="avatarBroken = true" v-show="!avatarBroken" />
					<span v-show="avatarBroken">{{ (username || '?').charAt(0).toUpperCase() }}</span>
					<div class="profile-avatar-overlay">
						<b-icon icon="edit-outline" pack="casa" size="is-16"></b-icon>
					</div>
					<input ref="avatarInput" type="file" accept="image/*" class="avatar-input" @change="onAvatarPicked" />
				</div>

				<div class="profile-main">
					<template v-if="!editingName">
						<div class="profile-name">
							{{ username }}
							<button class="icon-button" type="button" :title="$t('Edit')" @click="startEditName">
								<b-icon icon="edit-outline" pack="casa" size="is-16"></b-icon>
							</button>
						</div>
					</template>
					<div v-else class="profile-name-edit">
						<b-input v-model="nameInput" size="is-small" @keyup.enter.native="saveName"></b-input>
						<button class="icon-button is-confirm" type="button" :title="$t('Apply')" @click="saveName">
							<b-icon icon="check-outline" pack="casa" size="is-16"></b-icon>
						</button>
						<button class="icon-button" type="button" :title="$t('Cancel')" @click="editingName = false">
							<b-icon icon="close-outline" pack="casa" size="is-16"></b-icon>
						</button>
					</div>
					<div class="profile-sub">{{ $t('Administrator') }}</div>
				</div>

				<div class="profile-actions">
					<b-button rounded size="is-small" @click="editingPassword = !editingPassword">{{ $t('Change password') }}</b-button>
					<b-button rounded size="is-small" type="is-danger" outlined @click="logout">{{ $t('Sign out') }}</b-button>
				</div>
			</div>

			<div v-if="editingPassword" class="password-form">
				<b-input v-model="oriPassword" :placeholder="$t('Current password')" type="password" password-reveal size="is-small"></b-input>
				<b-input v-model="newPassword1" :placeholder="$t('New password')" type="password" password-reveal size="is-small"></b-input>
				<b-input v-model="newPassword2" :placeholder="$t('Confirm new password')" type="password" password-reveal size="is-small"
					@keyup.enter.native="savePassword"></b-input>
				<div class="password-form-actions">
					<b-button rounded size="is-small" @click="cancelPassword">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-dark" :loading="savingPassword" @click="savePassword">{{ $t('Save') }}</b-button>
				</div>
				<p v-if="passwordError" class="error-note">{{ passwordError }}</p>
			</div>

			<div v-if="cropping" class="avatar-crop">
				<div class="cropper-wrap">
					<cropper :src="cropImage" :stencil-props="{ aspectRatio: 1 }" :canvas="{ width: 200, height: 200 }" @change="onCropChange"></cropper>
				</div>
				<div class="avatar-crop-actions">
					<b-button rounded size="is-small" @click="cancelCrop">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-dark" :loading="savingAvatar" @click="saveAvatar">{{ $t('Save') }}</b-button>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Other Admin Accounts') }}</h3>
		<div class="setting-card">
			<nivaroos-users-panel></nivaroos-users-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('System Users') }}</h3>
		<div class="setting-card">
			<system-users-panel></system-users-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('SMB Users') }}</h3>
		<div class="setting-card">
			<smb-users-panel></smb-users-panel>
		</div>
	</section>
</template>

<script>
import { Cropper } from 'vue-advanced-cropper'
import 'vue-advanced-cropper/dist/style.css'
import NivaroOSUsersPanel from '@/components/settings/NivaroOSUsersPanel.vue'
import SystemUsersPanel from '@/components/settings/SystemUsersPanel.vue'
import SmbUsersPanel from '@/components/settings/SmbUsersPanel.vue'

export const ROWS = [
	{ label: 'My Account' },
	{ label: 'Other Admin Accounts' },
	{ label: 'System Users' },
	{ label: 'SMB Users' }
]

export default {
	name: 'users-section',
	components: { Cropper, NivaroOSUsersPanel, SystemUsersPanel, SmbUsersPanel },
	data() {
		return {
			avatarBroken: false,
			editingName: false,
			nameInput: '',
			editingPassword: false,
			oriPassword: '',
			newPassword1: '',
			newPassword2: '',
			savingPassword: false,
			passwordError: '',
			cropping: false,
			cropImage: null,
			cropResult: null,
			savingAvatar: false
		}
	},
	computed: {
		username() {
			return this.$store.state.user.username
		},
		avatarUrl() {
			const token = this.$store.state.access_token || localStorage.getItem('access_token')
			return `v1/users/avatar?token=${token}&t=${this.avatarCacheBust || 0}`
		}
	},
	created() {
		this.avatarCacheBust = Date.now()
	},
	methods: {
		startEditName() {
			this.nameInput = this.username
			this.editingName = true
		},
		saveName() {
			if (!this.nameInput || this.nameInput === this.username) {
				this.editingName = false
				return
			}
			this.$api.users.setUserInfo({ ...this.$store.state.user, username: this.nameInput }).then(res => {
				this.$store.commit('SET_USER', res.data.data)
				this.editingName = false
			})
		},
		cancelPassword() {
			this.editingPassword = false
			this.oriPassword = ''
			this.newPassword1 = ''
			this.newPassword2 = ''
			this.passwordError = ''
		},
		savePassword() {
			this.passwordError = ''
			if (!this.oriPassword || !this.newPassword1) {
				this.passwordError = this.$t('Enter your current and new password')
				return
			}
			if (this.newPassword1 !== this.newPassword2) {
				this.passwordError = this.$t('New passwords do not match')
				return
			}
			this.savingPassword = true
			this.$api.users.changePassword({ old_password: this.oriPassword, password: this.newPassword1 }).then(() => {
				this.cancelPassword()
			}).catch(e => {
				this.passwordError = e.response && e.response.data ? e.response.data.message : this.$t('Failed to change password')
			}).finally(() => {
				this.savingPassword = false
			})
		},
		triggerAvatarPick() {
			this.$refs.avatarInput.click()
		},
		onAvatarPicked(event) {
			const file = event.target.files && event.target.files[0]
			if (!file) return
			if (this.cropImage) URL.revokeObjectURL(this.cropImage)
			this.cropImage = URL.createObjectURL(file)
			this.cropping = true
			event.target.value = ''
		},
		onCropChange({ canvas }) {
			this.cropResult = canvas
		},
		cancelCrop() {
			this.cropping = false
			if (this.cropImage) URL.revokeObjectURL(this.cropImage)
			this.cropImage = null
			this.cropResult = null
		},
		saveAvatar() {
			if (!this.cropResult) return
			this.savingAvatar = true
			const imageData = this.cropResult.toDataURL()
			this.$api.users.saveAvatar({ file: imageData }).then(() => {
				this.avatarBroken = false
				this.avatarCacheBust = Date.now()
				this.$buefy.toast.open({ message: this.$t('Update successful'), type: 'is-success' })
				this.cancelCrop()
			}).catch(() => {
				this.$buefy.toast.open({ message: this.$t('Update failure'), type: 'is-danger' })
			}).finally(() => {
				this.savingAvatar = false
			})
		},
		logout() {
			this.$messageBus('account_setting_logout')
			this.$store.commit('SET_DEFAULT_WALLPAPER')
			this.$router.push('/logout')
		}
	}
}
</script>

<style lang="scss" scoped>
.profile-row {
	display: flex;
	align-items: center;
	gap: 1rem;
	padding: 1.25rem;
	flex-wrap: wrap;
}

.profile-avatar {
	position: relative;
	flex-shrink: 0;
	width: 3.5rem;
	height: 3.5rem;
	border-radius: 50%;
	overflow: hidden;
	background: hsla(208, 100%, 50%, 0.1);
	color: hsla(208, 100%, 45%, 1);
	display: flex;
	align-items: center;
	justify-content: center;
	font-weight: 700;
	font-size: 1.3rem;
	cursor: pointer;

	img {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.avatar-input {
		display: none;
	}
}

.profile-avatar-overlay {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.45);
	color: #fff;
	display: flex;
	align-items: center;
	justify-content: center;
	opacity: 0;
	transition: opacity 0.15s ease;
}

.profile-avatar:hover .profile-avatar-overlay {
	opacity: 1;
}

.profile-main {
	flex: 1 1 12rem;
	min-width: 0;
}

.profile-name {
	font-weight: 700;
	font-size: 1rem;
	display: flex;
	align-items: center;
	gap: 0.3rem;
}

.profile-name-edit {
	display: flex;
	align-items: center;
	max-width: 16rem;
}

.profile-sub {
	font-size: 0.75rem;
	color: rgba(44, 62, 80, 0.55);
	margin-top: 0.15rem;
}

.profile-actions {
	display: flex;
	gap: 0.5rem;
	flex-shrink: 0;
}

.icon-button {
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.5rem;
	height: 1.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}

	&.is-confirm {
		background: hsla(140, 60%, 45%, 0.15);
		color: hsla(140, 60%, 32%, 1);
	}
}

.password-form,
.avatar-crop {
	padding: 0 1.25rem 1.25rem;
	display: flex;
	flex-direction: column;
	gap: 0.6rem;
	max-width: 22rem;
}

.password-form-actions,
.avatar-crop-actions {
	display: flex;
	justify-content: flex-end;
	gap: 0.5rem;
}

.cropper-wrap {
	height: 16rem;
	background: rgba(0, 0, 0, 0.03);
	border-radius: 8px;
	overflow: hidden;
}

.error-note {
	color: #d64545;
	font-size: 0.75rem;
}
</style>
