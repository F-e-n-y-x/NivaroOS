<template>
	<div class="connections-panel">
		<div v-for="c in connections" :key="c.id" class="setting-row">
			<b-icon class="row-icon" icon="server-network" pack="mdi" size="is-20"></b-icon>
			<div class="row-label">
				<div class="setting-title">{{ c.host }}</div>
				<div class="setting-desc">
					{{ c.username }}<span class="share-meta-sep">&middot;</span>{{ c.mount_point }}
					<template v-if="folderCount(c)">
						<span class="share-meta-sep">&middot;</span>{{ folderCount(c) }} {{ $t('folders') }}
					</template>
				</div>
			</div>
			<div class="row-control">
				<b-button rounded size="is-small" type="is-danger" outlined :loading="disconnecting === c.id" @click="confirmDisconnect(c)">
					{{ $t('Disconnect') }}
				</b-button>
			</div>
		</div>

		<div v-if="!connections.length" class="account-empty">
			{{ $t("Nothing connected yet - connect to another device's SMB share and it shows up as a location in Files.") }}
		</div>

		<div class="add-row">
			<b-button rounded size="is-small" type="is-dark" @click="openAdd">
				<i class="mdi mdi-plus mr-1"></i>{{ $t('Connect to a Network Share') }}
			</b-button>
		</div>

		<settings-overlay :active="showModal" :title="$t('Connect to a Network Share')" width="28rem" @close="closeModal">
			<p class="modal-hint">{{ $t("Enter the other device's address and SMB login - every folder it shares will be connected and show up in Files.") }}</p>
			<b-field :label="$t('Device address')" :message="$t('IP address or hostname, e.g. 192.168.1.20')">
				<b-input v-model="form.host" size="is-small" placeholder="192.168.1.20"></b-input>
			</b-field>
			<b-field :label="$t('Username')">
				<b-input v-model="form.username" size="is-small"></b-input>
			</b-field>
			<b-field :label="$t('Password')">
				<b-input v-model="form.password" type="password" password-reveal size="is-small"></b-input>
			</b-field>

			<a class="advanced-toggle" @click="showAdvanced = !showAdvanced">
				<i class="mdi" :class="showAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
				{{ $t('Advanced') }}
			</a>
			<b-field v-if="showAdvanced" :label="$t('Port')">
				<b-input v-model="form.port" size="is-small" placeholder="445"></b-input>
			</b-field>

			<p v-if="error" class="error-note">{{ error }}</p>

			<template #footer>
				<b-button rounded size="is-small" @click="closeModal">{{ $t('Cancel') }}</b-button>
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					:loading="connecting"
					:disabled="!form.host.trim() || !form.username.trim() || !form.password"
					@click="submit"
				>
					{{ $t('Connect') }}
				</b-button>
			</template>
		</settings-overlay>
	</div>
</template>

<script>
import SettingsOverlay from '@/components/settings/SettingsOverlay.vue'
import events from '@/events/events'

const emptyForm = () => ({ host: '', username: '', password: '', port: '' })

export default {
	name: 'network-connections-panel',
	components: { SettingsOverlay },
	data() {
		return {
			connections: [],
			showModal: false,
			showAdvanced: false,
			form: emptyForm(),
			connecting: false,
			disconnecting: null,
			error: ''
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		folderCount(c) {
			return (c.directories || '').split(',').filter(Boolean).length
		},
		refresh() {
			this.$api.samba.getConnections().then(res => {
				if (res.data.success === 200) this.connections = res.data.data || []
			})
			// Files' MountList has no other way to learn a connection was
			// added/removed here - see the same pattern used for cloud
			// storage and fstab drives.
			this.$EventBus.$emit(events.RELOAD_MOUNT_LIST)
		},
		openAdd() {
			this.form = emptyForm()
			this.showAdvanced = false
			this.error = ''
			this.showModal = true
		},
		closeModal() {
			this.showModal = false
		},
		submit() {
			this.error = ''
			this.connecting = true
			const payload = {
				host: this.form.host.trim(),
				username: this.form.username.trim(),
				password: this.form.password
			}
			if (this.form.port.trim()) payload.port = this.form.port.trim()
			this.$api.samba.createConnection(payload).then(res => {
				if (res.data.success === 200) {
					this.showModal = false
					this.refresh()
					this.$buefy.toast.open({ message: this.$t('Connected'), type: 'is-success' })
				} else {
					this.error = res.data.message
				}
			}).catch(e => {
				this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to connect')
			}).finally(() => {
				this.connecting = false
			})
		},
		confirmDisconnect(c) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Disconnect'),
				message: this.$t('Disconnect from {host}? This unmounts it from Files - the files stay on that device, untouched.', { host: c.host }),
				type: 'is-danger',
				confirmText: this.$t('Disconnect'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.disconnecting = c.id
					this.$api.samba.deleteConnection(c.id).then(() => this.refresh()).finally(() => {
						this.disconnecting = null
					})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.connections-panel {
	display: flex;
	flex-direction: column;
}

.share-meta-sep {
	margin: 0 0.3rem;
}

.add-row {
	padding: 0.9rem 1.25rem;
}

.modal-hint {
	font-size: 0.8125rem;
	color: var(--color-text-muted);
	margin-bottom: 0.85rem;
}

.advanced-toggle {
	display: inline-block;
	font-size: 0.75rem;
	color: var(--color-primary);
	cursor: pointer;
	margin: 0.25rem 0 0.5rem;
}

.error-note {
	padding: 0 0.6rem;
	color: var(--color-danger);
	font-size: 0.775rem;
	margin-top: 0.25rem;
	margin-bottom: 0.5rem;
}
</style>
