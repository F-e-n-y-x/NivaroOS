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
					<b-icon :icon="net.mode === 'bridge' ? 'lan-connect' : 'lan'" custom-size="mdi-22px"></b-icon>
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
				<b-select v-model="form.host_nic" size="is-small" expanded>
					<option v-for="nic in interfaces" :key="nic" :value="nic">{{ nic }}</option>
				</b-select>
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
				<b-button type="is-danger" :loading="creating" :disabled="!canCreate" @click="create">
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

export default {
	name: 'vm-networks',
	components: { VmOverlayPanel },
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
		}
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
	padding: 1.25rem;
	height: 100%;
	overflow: auto;
}
.vm-section-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}
.vm-section-title {
	font-size: 1.1rem;
	font-weight: 700;
	color: #2c3e50;
	margin: 0;
}
.create-btn {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	border: none;
	background: #3273dc;
	color: #fff;
	font-family: inherit;
	font-size: 0.85rem;
	font-weight: 600;
	padding: 0.55rem 1rem;
	border-radius: 8px;
	cursor: pointer;

	&:hover {
		background: #2366d1;
	}
}
.vm-loading {
	display: flex;
	justify-content: center;
	padding: 3rem 0;
	color: rgba(0, 0, 0, 0.35);

	// Buefy's <b-icon> wraps every glyph in a Bulma .icon span fixed at
	// 1.5rem (24px) by default - custom-size only scales the glyph's own
	// font-size, so anything bigger than 24px overflows its own wrapper
	// unless the wrapper itself is resized to match here.
	::v-deep .icon {
		width: 2.25rem;
		height: 2.25rem;
	}
}
.network-list {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}
.network-row {
	display: flex;
	align-items: center;
	gap: 0.85rem;
	padding: 0.75rem 1rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
}
.network-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.04);
	color: rgba(0, 0, 0, 0.4);

	&.is-active {
		background: rgba(50, 115, 220, 0.1);
		color: #3273dc;
	}
}
.network-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
}
.network-name {
	font-weight: 600;
	color: #2c3e50;
	font-size: 0.9rem;
}
.network-meta {
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.45);
}
.network-status {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.75rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.4);
}
.status-dot {
	width: 7px;
	height: 7px;
	border-radius: 50%;
	background: #b5b5b5;
}
.network-status.is-active .status-dot {
	background: #23d160;
}
.network-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: rgba(0, 0, 0, 0.35);
	cursor: pointer;
	display: flex;
	align-items: center;
	padding: 0.35rem;
	border-radius: 6px;
	margin-left: 0.5rem;

	&:hover {
		color: #f2534a;
		background: rgba(242, 83, 74, 0.08);
	}
}
.static-ip-toggle {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	font-size: 0.85rem;
	color: rgba(0, 0, 0, 0.65);
	cursor: pointer;
}
.static-ip-hint {
	font-size: 0.78rem;
	color: rgba(0, 0, 0, 0.45);
	margin-top: -0.25rem;
}
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.5rem;
	padding: 3rem 1rem;
	color: rgba(0, 0, 0, 0.3);

	::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	.vm-empty-title {
		font-size: 0.9rem;
		font-weight: 600;
		color: rgba(0, 0, 0, 0.5);
		margin: 0;
	}
}
</style>
