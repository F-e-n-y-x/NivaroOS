<template>
	<div class="account-list">
		<div class="account-inline-form panel-header">
			<p class="hint">{{ $t('System (Linux) users, distinct from NivaroOS accounts.') }}</p>
			<button class="add-button" type="button" :title="$t('Add system user')" @click="showAddForm = !showAddForm">
				<b-icon :icon="showAddForm ? 'close-outline' : 'add-outline'" pack="casa" size="is-20"></b-icon>
			</button>
		</div>

		<form v-if="showAddForm" class="account-inline-form" @submit.prevent="createUser">
			<b-input v-model="newUser.username" :placeholder="$t('Username')" size="is-small" class="add-input"></b-input>
			<b-input v-model="newUser.password" :placeholder="$t('Password')" type="password" size="is-small" class="add-input"></b-input>
			<label class="account-toggle-chip" :class="{ on: newUser.sudo }">
				<input type="checkbox" v-model="newUser.sudo" />
				{{ $t('Sudo') }}
			</label>
			<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="creating">
				{{ $t('Add') }}
			</b-button>
		</form>
		<p v-if="error" class="error-note">{{ error }}</p>

		<div v-for="u in users" :key="u.username" class="account-row">
			<div class="account-avatar">{{ u.username.charAt(0).toUpperCase() }}</div>
			<div class="account-main">
				<div class="account-name">
					{{ u.username }}
					<span v-if="u.protected" class="setting-chip">{{ $t('Protected') }}</span>
				</div>
				<div class="account-sub">{{ u.full_name || u.home }}</div>
			</div>
			<div class="account-meta">
				<label class="account-toggle-chip" :class="{ on: u.sudo }">
					<input type="checkbox" :checked="u.sudo" :disabled="u.protected" @change="setGroup(u, 'sudo', $event.target.checked)" />
					{{ $t('Sudo') }}
				</label>
				<label class="account-toggle-chip" :class="{ on: u.docker }">
					<input type="checkbox" :checked="u.docker" :disabled="u.protected" @change="setGroup(u, 'docker', $event.target.checked)" />
					{{ $t('Docker') }}
				</label>
			</div>
			<div class="account-actions">
				<b-button rounded size="is-small" @click="togglePassword(u)">{{ $t('Password') }}</b-button>
				<b-button v-if="!u.protected" rounded size="is-small" type="is-danger" outlined @click="confirmDelete(u)">
					{{ $t('Delete') }}
				</b-button>
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

		<div v-if="!users.length" class="account-empty">{{ $t('No additional system users yet.') }}</div>
	</div>
</template>

<script>
export default {
	name: 'system-users-panel',
	data() {
		return {
			users: [],
			newUser: { username: '', password: '', sudo: false },
			creating: false,
			error: '',
			showAddForm: false,
			passwordTarget: null,
			newPassword: '',
			savingPassword: false
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.sys.getSystemUsers().then(res => {
				if (res.data.success === 200) this.users = res.data.data || []
			})
		},
		async createUser() {
			this.error = ''
			if (!this.newUser.username || !this.newUser.password) return
			this.creating = true
			try {
				await this.$api.sys.createSystemUser(this.newUser)
				this.newUser = { username: '', password: '', sudo: false }
				this.showAddForm = false
				this.refresh()
			} catch (e) {
				this.error = e.response && e.response.data ? e.response.data.data : this.$t('Failed to create user')
			} finally {
				this.creating = false
			}
		},
		setGroup(user, group, value) {
			this.$api.sys.setSystemUserGroups(user.username, { [group]: value }).then(() => this.refresh())
		},
		confirmDelete(user) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Delete system user'),
				message: this.$t('This permanently removes {user} and its home directory. Continue?', { user: user.username }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.sys.deleteSystemUser(user.username).then(() => this.refresh())
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
				await this.$api.sys.setSystemUserPassword(user.username, this.newPassword)
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
