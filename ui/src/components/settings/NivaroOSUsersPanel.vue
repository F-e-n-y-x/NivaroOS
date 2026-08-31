<template>
	<div class="account-list">
		<div class="account-inline-form panel-header">
			<p class="hint">{{ $t('Other people with full admin access to this NivaroOS system - everyone has the same permission level, there is no separate role yet.') }}</p>
			<button class="add-button" type="button" :title="$t('Add admin account')" @click="showAddForm = !showAddForm">
				<b-icon :icon="showAddForm ? 'close-outline' : 'add-outline'" pack="casa" size="is-20"></b-icon>
			</button>
		</div>

		<form v-if="showAddForm" class="account-inline-form" @submit.prevent="createUser">
			<b-input v-model="newUser.username" :placeholder="$t('Username')" size="is-small" class="add-input"></b-input>
			<b-input v-model="newUser.password" :placeholder="$t('Password')" type="password" size="is-small" class="add-input"></b-input>
			<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="creating">
				{{ $t('Add') }}
			</b-button>
		</form>
		<p v-if="error" class="error-note">{{ error }}</p>

		<div v-for="u in otherUsers" :key="u" class="account-row">
			<div class="account-avatar">{{ u.charAt(0).toUpperCase() }}</div>
			<div class="account-main">
				<div class="account-name">{{ u }}</div>
			</div>
			<div class="account-actions">
				<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(u)">
					{{ $t('Delete') }}
				</b-button>
			</div>
		</div>

		<div v-if="!otherUsers.length" class="account-empty">{{ $t('No other NivaroOS users yet - your own account is managed above.') }}</div>
	</div>
</template>

<script>
export default {
	name: 'nivaroos-users-panel',
	data() {
		return {
			users: [],
			newUser: { username: '', password: '' },
			creating: false,
			error: '',
			showAddForm: false
		}
	},
	computed: {
		currentUsername() {
			return this.$store.state.user.username
		},
		otherUsers() {
			return this.users.filter(u => u !== this.currentUsername)
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.users.getAllUserName().then(res => {
				if (res.data.success === 200) this.users = res.data.data || []
			})
		},
		async createUser() {
			this.error = ''
			if (!this.newUser.username || !this.newUser.password) return
			if (this.newUser.password.length < 6) {
				this.error = this.$t('Password must be at least 6 characters')
				return
			}
			this.creating = true
			try {
				const keyRes = await this.$api.users.generateRegisterKey()
				const key = keyRes.data.data
				await this.$api.users.register(this.newUser.username, this.newUser.password, key)
				this.newUser = { username: '', password: '' }
				this.showAddForm = false
				this.refresh()
			} catch (e) {
				this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to create user')
			} finally {
				this.creating = false
			}
		},
		async confirmDelete(username) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Delete NivaroOS user'),
				message: this.$t('Delete the NivaroOS account {user}?', { user: username }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: async () => {
					const infoRes = await this.$api.users.getUserInfoByName(username)
					const id = infoRes.data.data && infoRes.data.data.id
					if (id) {
						await this.$api.users.deleteUser(id)
						this.refresh()
					}
				}
			})
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

.error-note {
	color: #d64545;
	font-size: 0.75rem;
	padding: 0 1.25rem 0.75rem;
}
</style>
