<template>
	<div class="account-list">
		<div class="account-inline-form panel-header">
			<p class="hint">{{ $t('SMB users must match an existing system user - add one under System Users first if needed.') }}</p>
			<button class="add-button" type="button" :title="$t('Add SMB user')" @click="showAddForm = !showAddForm">
				<b-icon :icon="showAddForm ? 'close-outline' : 'add-outline'" pack="casa" size="is-20"></b-icon>
			</button>
		</div>

		<form v-if="showAddForm" class="account-inline-form" @submit.prevent="createUser">
			<b-select v-model="newUser.username" :placeholder="$t('System user')" size="is-small">
				<option v-for="name in availableSystemUsers" :key="name" :value="name">{{ name }}</option>
			</b-select>
			<b-input v-model="newUser.password" :placeholder="$t('Password')" type="password" size="is-small" class="add-input"></b-input>
			<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="creating">
				{{ $t('Add') }}
			</b-button>
		</form>
		<p v-if="error" class="error-note">{{ error }}</p>

		<div v-for="u in users" :key="u.username" class="account-row">
			<div class="account-avatar">{{ u.username.charAt(0).toUpperCase() }}</div>
			<div class="account-main">
				<div class="account-name">{{ u.username }}</div>
			</div>
			<div class="account-actions">
				<b-button rounded size="is-small" @click="togglePassword(u)">{{ $t('Password') }}</b-button>
				<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(u)">{{ $t('Delete') }}</b-button>
			</div>

			<div v-if="passwordTarget === u.username" class="account-inline-form full-width">
				<b-input v-model="newPassword" :placeholder="$t('New password')" type="password" size="is-small" expanded
					@keyup.enter.native="savePassword(u)"></b-input>
				<b-button rounded size="is-small" @click="passwordTarget = null">{{ $t('Cancel') }}</b-button>
				<b-button rounded size="is-small" type="is-dark" :loading="savingPassword" @click="savePassword(u)">
					{{ $t('Save') }}
				</b-button>
			</div>
		</div>

		<div v-if="!users.length" class="account-empty">{{ $t('No SMB users yet.') }}</div>
	</div>
</template>

<script>
export default {
	name: 'smb-users-panel',
	data() {
		return {
			users: [],
			systemUsernames: [],
			newUser: { username: '', password: '' },
			creating: false,
			error: '',
			showAddForm: false,
			passwordTarget: null,
			newPassword: '',
			savingPassword: false
		}
	},
	computed: {
		availableSystemUsers() {
			const taken = this.users.map(u => u.username)
			return this.systemUsernames.filter(n => !taken.includes(n))
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.sys.getSmbUsers().then(res => {
				if (res.data.success === 200) this.users = res.data.data || []
			})
			this.$api.sys.getSystemUsers().then(res => {
				if (res.data.success === 200) this.systemUsernames = (res.data.data || []).map(u => u.username)
			})
		},
		async createUser() {
			this.error = ''
			if (!this.newUser.username || !this.newUser.password) return
			this.creating = true
			try {
				await this.$api.sys.createSmbUser(this.newUser)
				this.newUser = { username: '', password: '' }
				this.showAddForm = false
				this.refresh()
			} catch (e) {
				this.error = e.response && e.response.data ? e.response.data.data : this.$t('Failed to create SMB user')
			} finally {
				this.creating = false
			}
		},
		confirmDelete(user) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Delete SMB user'),
				message: this.$t('Remove SMB access for {user}?', { user: user.username }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.sys.deleteSmbUser(user.username).then(() => this.refresh())
				}
			})
		},
		togglePassword(user) {
			this.newPassword = ''
			this.passwordTarget = this.passwordTarget === user.username ? null : user.username
		},
		async savePassword(user) {
			if (!this.newPassword) return
			this.savingPassword = true
			try {
				await this.$api.sys.setSmbUserPassword(user.username, this.newPassword)
				this.passwordTarget = null
			} finally {
				this.savingPassword = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.panel-header {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: 1rem;
	padding-top: 1.1rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
}

.add-button {
	flex-shrink: 0;
	width: 1.9rem;
	height: 1.9rem;
	border-radius: 50%;
	border: none;
	background: hsla(208, 100%, 50%, 1);
	color: #fff;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;

	&:hover {
		background: hsla(208, 100%, 44%, 1);
	}
}

.add-input {
	width: 10rem;
}

.full-width {
	flex-basis: 100%;
	padding: 0 0 0.9rem 3.25rem;
}

.error-note {
	color: var(--color-danger);
	font-size: 0.75rem;
	padding: 0 1.25rem 0.75rem;
}
</style>
