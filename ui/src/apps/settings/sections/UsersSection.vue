<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Users & Access') }}</h2>

		<div class="setting-card">
			<div class="profile-row">
				<button type="button" class="profile-avatar" :title="$t('Change profile picture')" :aria-label="$t('Change profile picture')" @click="editingAvatar = !editingAvatar">
					<user-avatar :username="username" :version="me.avatar_version || ''" size="4rem"></user-avatar>
					<span class="profile-avatar-overlay">
						<b-icon icon="edit-outline" pack="casa" size="is-16"></b-icon>
					</span>
				</button>

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
						<b-input v-model="nameInput" size="is-small" @keyup.enter="saveName"></b-input>
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
					@keyup.enter="savePassword"></b-input>
				<div class="password-form-actions">
					<b-button rounded size="is-small" @click="cancelPassword">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-primary" :loading="savingPassword" @click="savePassword">{{ $t('Save') }}</b-button>
				</div>
				<p v-if="passwordError" class="error-note">{{ passwordError }}</p>
			</div>

			<div class="sessions-row">
				<div class="sessions-text">
					<div class="sessions-title">{{ $t('Other sessions') }}</div>
					<div class="profile-sub">{{ $t('Signs out every other browser, phone and app signed in to this account. This one stays signed in.') }}</div>
				</div>
				<b-button rounded size="is-small" :loading="signingOutOthers" @click="confirmSignOutOthers">{{ $t('Sign out other devices') }}</b-button>
			</div>

			<avatar-editor v-if="editingAvatar" :has-avatar="!!me.avatar_version" @close="editingAvatar = false" @saved="onAvatarSaved"></avatar-editor>
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
import AvatarEditor from '@/apps/settings/account/AvatarEditor.vue'
import UserAvatar from '@/shared/basicComponents/UserAvatar.vue'
import { currentUser, forgetAvatar, rememberAvatar, saveCurrentUser } from '@/utils/avatar'
import NivaroosUsersPanel from '@/apps/settings/NivaroOSUsersPanel.vue'
import SystemUsersPanel from '@/apps/settings/SystemUsersPanel.vue'
import SmbUsersPanel from '@/apps/settings/SmbUsersPanel.vue'

// Search index: labels are the titles this section renders (the search
// jumps to them); keywords are other words people type for them.
export const ROWS = [
	{ label: 'Account', keywords: 'my account password avatar profile picture photo name' },
	{ label: 'Other Admin Accounts', keywords: 'admin users' },
	{ label: 'System Users', keywords: 'linux users sudo docker' },
	{ label: 'SMB Users', keywords: 'samba smb password' }
]

export default {
	name: 'users-section',
	components: { AvatarEditor, UserAvatar, NivaroosUsersPanel, SystemUsersPanel, SmbUsersPanel },
	data() {
		return {
			editingAvatar: false,
			editingName: false,
			nameInput: '',
			editingPassword: false,
			oriPassword: '',
			newPassword1: '',
			newPassword2: '',
			savingPassword: false,
			signingOutOthers: false,
			passwordError: '',
		}
	},
	computed: {
		me() {
			return currentUser(this.$store)
		},
		username() {
			return this.me.username
		}
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
				saveCurrentUser(this.$store, res.data.data)
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
			this.$api.users.changePassword({ old_password: this.oriPassword, password: this.newPassword1 }).then(res => {
				// Other sessions end on a password change; this one continues
				// with the fresh tokens the server returns.
				this.adoptTokens(res.data && res.data.data && res.data.data.token)
				this.cancelPassword()
				this.$buefy.toast.open({ message: this.$t('Password changed - other devices have to sign in again'), type: 'is-success', duration: 4000 })
			}).catch(e => {
				this.passwordError = e.response && e.response.data ? e.response.data.message : this.$t('Failed to change password')
			}).finally(() => {
				this.savingPassword = false
			})
		},
		// The server ended every other session and handed this one fresh
		// tokens (the old ones no longer work anywhere).
		adoptTokens(t) {
			if (!t || !t.access_token) return
			localStorage.setItem('access_token', t.access_token)
			localStorage.setItem('refresh_token', t.refresh_token)
			localStorage.setItem('expires_at', t.expires_at)
			this.$store.commit('SET_ACCESS_TOKEN', t.access_token)
			this.$store.commit('SET_REFRESH_TOKEN', t.refresh_token)
		},
		confirmSignOutOthers() {
			this.$buefy.dialog.confirm({
				title: this.$t('Sign out other devices?'),
				message: this.$t('Every other browser, the phone app and anything else signed in to this account will have to sign in again.'),
				confirmText: this.$t('Sign out other devices'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				onConfirm: () => this.signOutOthers()
			})
		},
		signOutOthers() {
			this.signingOutOthers = true
			this.$api.users.signOutOtherSessions().then(res => {
				this.adoptTokens(res.data && res.data.data && res.data.data.token)
				this.$buefy.toast.open({ message: this.$t('Other devices were signed out'), type: 'is-success', duration: 4000 })
			}).catch(e => {
				const msg = e.response && e.response.data && e.response.data.message
				this.$buefy.toast.open({ message: msg || this.$t('Could not sign out other devices'), type: 'is-danger', duration: 5000 })
			}).finally(() => {
				this.signingOutOthers = false
			})
		},
		onAvatarSaved(user, canvas) {
			if (!user) return
			saveCurrentUser(this.$store, { ...this.me, ...user })
			if (canvas) rememberAvatar(user.username, canvas)
			else forgetAvatar()
			this.editingAvatar = false
			this.$buefy.toast.open({ message: this.$t(canvas ? 'Profile picture updated' : 'Profile picture removed'), type: 'is-success' })
		},
		logout() {
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
	gap: var(--space-4);
	padding: var(--space-5);
	flex-wrap: wrap;
}

.profile-avatar {
	flex-shrink: 0;
	position: relative;
	display: flex;
	padding: 0;
	border: 0;
	border-radius: 50%;
	overflow: hidden;
	background: none;
	cursor: pointer;
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

.profile-avatar:hover .profile-avatar-overlay,
.profile-avatar:focus-visible .profile-avatar-overlay {
	opacity: 1;
}

.profile-main {
	flex: 1 1 12rem;
	min-width: 0;
}

.profile-name {
	font-weight: 500;
	font-size: var(--font-md);
	display: flex;
	align-items: center;
	gap: var(--space-1);
}

.profile-name-edit {
	display: flex;
	align-items: center;
	max-width: 16rem;
}

.profile-sub {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	margin-top: var(--space-1);
}

.profile-actions {
	display: flex;
	gap: var(--space-2);
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
	color: var(--theme-text-muted);

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}

	&.is-confirm {
		background: hsla(140, 60%, 45%, 0.15);
		color: hsla(140, 60%, 32%, 1);
	}
}

.sessions-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-3);
	flex-wrap: wrap;
	padding: 0 var(--space-5) var(--space-5);
}

.sessions-text {
	min-width: 0;
	flex: 1 1 14rem;
}

.sessions-title {
	font-weight: 500;
}

.password-form {
	padding: 0 var(--space-5) var(--space-5);
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	max-width: 22rem;
}

.password-form-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
}

.error-note {
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
}
</style>
