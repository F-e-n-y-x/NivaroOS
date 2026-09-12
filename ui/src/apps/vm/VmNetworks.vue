<template>
	<div class="vm-networks">
		<div class="vm-section-toolbar">
			<h2 class="vm-section-title">{{ $t('Networks') }}</h2>
			<button class="create-btn" @click="showCreate = true">
				<b-icon icon="plus" custom-size="mdi-18px"></b-icon>
				<span>{{ $t('Create bridged network') }}</span>
			</button>
		</div>

		<div v-if="loading" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<div v-else class="network-list">
			<div v-for="net in networks" :key="net.name" class="network-row">
				<div class="network-icon" :class="{ 'is-active': net.active }">
					<b-icon :icon="net.mode === 'bridge' ? 'lan-connect' : 'lan'" :custom-size="net.mode === 'bridge' ? 'mdi-18px' : 'mdi-22px'"></b-icon>
				</div>
				<div class="network-info">
					<span class="network-name">{{ net.name }}</span>
					<span class="network-meta">{{ net.mode === 'bridge' ? $t('Bridge') : $t('NAT') }}<template v-if="net.host_nic"> &middot; {{ net.host_nic }}</template></span>
				</div>
				<span class="network-status" :class="{ 'is-active': net.active }">
					<span class="status-dot"></span>{{ net.active ? $t('Active') : $t('Inactive') }}
				</span>
				<button v-if="net.mode === 'bridge'" class="network-remove" :title="$t('Remove')" @click="deletingNetName = net.name">
					<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
				</button>
			</div>
			<div v-if="!networks.length" class="vm-empty">
				<b-icon icon="lan" custom-size="mdi-48px"></b-icon>
				<p class="vm-empty-title">{{ $t('No networks found') }}</p>
			</div>
		</div>

		<vm-overlay-panel :active="showCreate" :title="$t('Create bridged network')" width="26rem" @close="showCreate = false">
			<b-message type="is-warning" :closable="false">
				{{ $t('Bridging the wrong network interface can disconnect this machine from your LAN. ' +
					'The interface currently carrying the default route is refused automatically, but double-check your choice.') }}
			</b-message>
			<b-field :label="$t('Bridge name')">
				<b-input v-model="form.name" size="is-small" placeholder="br-vm0"></b-input>
			</b-field>
			<b-field :label="$t('Host network interface')">
				<vm-dropdown
					v-model="form.host_nic"
					:options="nicOptions"
					:placeholder="$t('Select host interface...')"
					icon="lan"
					size="small"
				></vm-dropdown>
			</b-field>
			<b-message v-if="!interfaces.length && !loadingInterfaces" type="is-info" :closable="false">
				{{ $t('No spare physical network interfaces were found on this machine.') }}
			</b-message>
			<b-field>
				<label class="static-ip-toggle">
					<input type="checkbox" v-model="form.useStaticIP" />
					{{ $t('Give the bridge a static IP') }}
				</label>
			</b-field>
			<template v-if="form.useStaticIP">
				<b-field :label="$t('Static IP')">
					<b-input v-model="form.static_ip" size="is-small" placeholder="192.168.1.50"></b-input>
				</b-field>
				<b-field :label="$t('Netmask')">
					<b-input v-model="form.netmask" size="is-small" placeholder="255.255.255.0"></b-input>
				</b-field>
				<b-field :label="$t('Gateway')">
					<b-input v-model="form.gateway" size="is-small" placeholder="192.168.1.1"></b-input>
				</b-field>
				<p class="static-ip-hint">
					{{ $t('Recommended when bridging the interface carrying this host\'s own connection - a DHCP-assigned bridge can get a different address than the host had before.') }}
				</p>
			</template>
			<b-message v-if="error" type="is-danger" :closable="false">{{ error }}</b-message>

			<template #footer>
				<b-button @click="showCreate = false">{{ $t('Cancel') }}</b-button>
				<b-button type="is-primary" :loading="creating" :disabled="!canCreate" @click="create">
					{{ $t('Create') }}
				</b-button>
			</template>
		</vm-overlay-panel>

		<vm-overlay-panel :active="!!deletingNetName" :title="$t('Remove Network')" width="24rem" @close="deletingNetName = null">
			<p>{{ $t('Remove bridged network') }} "{{ deletingNetName }}"? {{ $t('Any VM attached to it will lose network connectivity until reconfigured.') }}</p>
			<template #footer>
				<b-button @click="deletingNetName = null">{{ $t('Cancel') }}</b-button>
				<b-button type="is-danger" :loading="deletingNet" @click="performDeleteNetwork">{{ $t('Remove') }}</b-button>
			</template>
		</vm-overlay-panel>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmOverlayPanel from './VmOverlayPanel.vue'
import VmDropdown from './VmDropdown.vue'

export default {
	name: 'vm-networks',
	components: { VmOverlayPanel, VmDropdown },
	data() {
		return {
			networks: [],
			interfaces: [],
			loading: true,
			loadingInterfaces: true,
			showCreate: false,
			creating: false,
			deletingNetName: null,
			deletingNet: false,
			error: null,
			form: { name: '', host_nic: '', useStaticIP: false, static_ip: '', netmask: '', gateway: '' }
		}
	},
	computed: {
		canCreate() {
			if (!this.form.name || !this.form.host_nic) return false
			if (this.form.useStaticIP && (!this.form.static_ip || !this.form.netmask || !this.form.gateway)) return false
			return true
		},
		nicOptions() {
			return (this.interfaces || []).map((nic) => ({
				value: nic,
				label: nic,
				icon: 'lan',
			}))
		},
	},
	watch: {
		showCreate(isOpen) {
			if (isOpen) this.loadInterfaces()
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		async refresh() {
			this.loading = true
			try {
				this.networks = await vmSidecar.listNetworks()
			} finally {
				this.loading = false
			}
		},
		async loadInterfaces() {
			this.loadingInterfaces = true
			try {
				this.interfaces = await vmSidecar.listHostInterfaces()
			} finally {
				this.loadingInterfaces = false
			}
		},
		async create() {
			this.error = null
			this.creating = true
			try {
				const payload = { name: this.form.name, host_nic: this.form.host_nic }
				if (this.form.useStaticIP) {
					payload.static_ip = this.form.static_ip
					payload.netmask = this.form.netmask
					payload.gateway = this.form.gateway
				}
				await vmSidecar.createBridge(payload)
				this.showCreate = false
				await this.refresh()
			} catch (e) {
				this.error = e.message
			} finally {
				this.creating = false
			}
		},
		async performDeleteNetwork() {
			const name = this.deletingNetName
			this.deletingNet = true
			try {
				await vmSidecar.deleteBridge(name)
				this.deletingNetName = null
				await this.refresh()
			} catch (e) {
				this.error = e.message
				this.deletingNetName = null
			} finally {
				this.deletingNet = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-networks {
	padding: var(--space-6);
}
.vm-section-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	flex-wrap: wrap;
	row-gap: var(--space-2);
	margin-bottom: var(--space-5);
}
.vm-section-title {
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	margin: 0;
	letter-spacing: -0.01em;
}
.create-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 500;
	height: 2rem;
	padding: 0 var(--space-3);
	border-radius: var(--radius-sm);
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: #1d4ed8;
	}
}
.vm-loading {
	display: flex;
	justify-content: center;
	padding: var(--space-8) 0;
	color: var(--theme-text-muted, #94a3b8);

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
	}
}
.network-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}
.network-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.07));
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);
	box-shadow: var(--shadow-sm);
	transition: border-color 0.15s ease, box-shadow 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.25);
		box-shadow: var(--shadow-lg);
	}
}
.network-icon {
	flex-shrink: 0;
	width: 2.25rem;
	height: 2.25rem;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	&.is-active {
		background: rgba(59, 130, 246, 0.1);
		color: #2563eb;
	}
}
.network-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.network-name {
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	font-size: var(--font-base);
}
.network-meta {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}
.network-status {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-2xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	&.is-active {
		background: rgba(16, 185, 129, 0.1);
		color: #059669;

		.status-dot {
			background: #10b981;
		}
	}
}
.status-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: var(--theme-text-muted, #94a3b8);
}
.network-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.85rem;
	height: 1.85rem;
	border-radius: var(--radius-sm);
	margin-left: var(--space-1);
	transition: background 0.12s ease, color 0.12s ease;

	&:hover {
		color: #dc2626;
		background: var(--color-danger-soft, #fee2e2);
	}
}
.static-ip-toggle {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-size: var(--font-base);
	color: var(--theme-text-secondary, #334155);
	cursor: pointer;
}
.static-ip-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	margin-top: calc(var(--space-1) * -1);
}
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-muted, #94a3b8);

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
		color: var(--theme-text-muted, #cbd5e1);
	}

	.vm-empty-title {
		font-size: var(--font-md);
		font-weight: 600;
		color: var(--theme-text-secondary, #475569);
		margin: var(--space-1) 0 0;
	}
}
</style>
