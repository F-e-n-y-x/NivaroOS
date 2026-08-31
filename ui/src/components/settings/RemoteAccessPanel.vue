<template>
	<div class="remote-access-panel">
		<div v-if="loading" class="hint">{{ $t('Checking Tailscale status...') }}</div>

		<template v-else>
			<div class="setting-card">
				<div class="setting-row">
					<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
					<div class="row-label">{{ $t('Tailscale') }}</div>
					<div class="row-control">
						<b-switch :value="isRunning" class="is-flex-direction-row-reverse mr-0" type="is-dark" :loading="toggling" @input="toggle"></b-switch>
					</div>
				</div>

				<div v-if="isRunning" class="setting-row">
					<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
					<div class="row-label">{{ $t('This device') }}</div>
					<div class="row-control chip-row">
						<span class="setting-chip">{{ selfIp }}</span>
						<span v-for="cap in selfCapabilities" :key="cap" class="setting-chip is-feature">{{ $t(cap) }}</span>
					</div>
				</div>
			</div>

			<template v-if="isRunning">
				<h3 class="setting-card-title">{{ $t('Tailnet devices') }}</h3>
				<div class="setting-card">
					<div v-for="p in peers" :key="p.hostName" class="setting-row">
						<b-icon class="row-icon" icon="laptop" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ p.hostName }}</div>
							<div class="setting-desc">
								<span>{{ p.ip }}</span>
								<span class="mx-2">&middot;</span>
								<span>{{ p.os }}</span>
							</div>
						</div>
						<div class="row-control">
							<span class="setting-chip mr-2" :class="{ 'is-good': p.online }">
								{{ p.online ? $t('Online') : $t('Offline') }}
							</span>
						</div>
					</div>
					<div v-if="!peers.length" class="account-empty">{{ $t('No other devices in this tailnet.') }}</div>
				</div>

				<h3 class="setting-card-title is-flex is-align-items-center is-justify-content-between">
					<span>{{ $t('Advanced Tailscale Settings') }}</span>
					<button class="icon-button" type="button" @click="toggleAdvanced">
						<b-icon :icon="showAdvanced ? 'chevron-up' : 'chevron-down'" pack="mdi" size="is-16"></b-icon>
					</button>
				</h3>

				<div v-if="showAdvanced" class="setting-card">
					<div class="setting-row">
						<b-icon class="row-icon" icon="transit-connection-variant" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Accept routes from other devices') }}</div>
							<div class="setting-desc">{{ $t('Allow routing traffic through other tailnet nodes') }}</div>
						</div>
						<div class="row-control">
							<b-switch :value="prefs.accept_routes" class="is-flex-direction-row-reverse mr-0" type="is-dark"
								:disabled="savingPref === 'accept_routes'" @input="setPref('accept_routes', $event)"></b-switch>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="dns-outline" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Accept DNS configuration') }}</div>
							<div class="setting-desc">{{ $t('Use MagicDNS and custom tailnet nameservers') }}</div>
						</div>
						<div class="row-control">
							<b-switch :value="prefs.accept_dns" class="is-flex-direction-row-reverse mr-0" type="is-dark"
								:disabled="savingPref === 'accept_dns'" @input="setPref('accept_dns', $event)"></b-switch>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="console-line" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Run Tailscale SSH server') }}</div>
							<div class="setting-desc">{{ $t('Secure SSH access authenticated via Tailscale') }}</div>
						</div>
						<div class="row-control">
							<b-switch :value="prefs.run_ssh" class="is-flex-direction-row-reverse mr-0" type="is-dark"
								:disabled="savingPref === 'run_ssh'" @input="setPref('run_ssh', $event)"></b-switch>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="shield-outline" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Block incoming connections (shields up)') }}</div>
							<div class="setting-desc">{{ $t('Prevent other tailnet devices from initiating connections') }}</div>
						</div>
						<div class="row-control">
							<b-switch :value="prefs.shields_up" class="is-flex-direction-row-reverse mr-0" type="is-dark"
								:disabled="savingPref === 'shields_up'" @input="setPref('shields_up', $event)"></b-switch>
						</div>
					</div>
					<div class="setting-row">
						<b-icon class="row-icon" icon="lan-connect" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Allow LAN access when using an exit node') }}</div>
							<div class="setting-desc">{{ $t('Maintain local network access while tunneling traffic') }}</div>
						</div>
						<div class="row-control">
							<b-switch :value="prefs.exit_node_allow_lan_access" class="is-flex-direction-row-reverse mr-0" type="is-dark"
								:disabled="savingPref === 'exit_node_allow_lan_access'" @input="setPref('exit_node_allow_lan_access', $event)"></b-switch>
						</div>
					</div>
					<div v-if="prefs.advertise_routes && prefs.advertise_routes.length" class="setting-row">
						<b-icon class="row-icon" icon="transit-connection-variant" pack="mdi" size="is-20"></b-icon>
						<div class="row-label">
							<div class="setting-title">{{ $t('Advertised routes') }}</div>
							<div class="setting-desc">{{ (prefs.advertise_routes || []).join(', ') }}</div>
						</div>
					</div>
					<p v-if="prefError" class="error-note">{{ prefError }}</p>
				</div>
			</template>
			<p v-if="error" class="error-note">{{ error }}</p>
		</template>
	</div>
</template>

<script>
// Builds the small set of human-readable capability chips ("Exit Node",
// "Using as Exit Node", "Subnet Router") from a Tailscale status node
// (Self or one Peer entry) - same shape for both, so this is shared.
function capabilitiesOf(node) {
	const caps = []
	if (node.ExitNode) caps.push('Using as Exit Node')
	else if (node.ExitNodeOption) caps.push('Exit Node')
	if (Array.isArray(node.PrimaryRoutes) && node.PrimaryRoutes.length) caps.push('Subnet Router')
	return caps
}

export default {
	name: 'remote-access-panel',
	data() {
		return {
			loading: true,
			toggling: false,
			backendState: '',
			selfIp: '',
			selfCapabilities: [],
			peers: [],
			error: '',
			showAdvanced: false,
			prefs: {},
			prefsLoaded: false,
			savingPref: '',
			prefError: ''
		}
	},
	computed: {
		isRunning() {
			return this.backendState === 'Running'
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.loading = true
			this.$api.tailscale.getStatus().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.backendState = data.BackendState
					this.selfIp = (data.TailscaleIPs && data.TailscaleIPs[0]) || ''
					this.selfCapabilities = data.Self ? capabilitiesOf(data.Self) : []
					const peerMap = data.Peer || {}
					this.peers = Object.values(peerMap).map(p => ({
						hostName: p.HostName,
						os: p.OS,
						ip: (p.TailscaleIPs && p.TailscaleIPs[0]) || '',
						online: !!p.Online,
						capabilities: capabilitiesOf(p)
					}))
				}
			}).catch(() => {
				this.error = this.$t('Failed to reach Tailscale')
			}).finally(() => {
				this.loading = false
			})
		},
		toggle(value) {
			this.toggling = true
			this.error = ''
			this.$api.tailscale.setState(value ? 'up' : 'down').then(() => {
				this.refresh()
			}).catch(e => {
				this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to change Tailscale state')
			}).finally(() => {
				this.toggling = false
			})
		},
		toggleAdvanced() {
			this.showAdvanced = !this.showAdvanced
			if (this.showAdvanced && !this.prefsLoaded) this.loadPrefs()
		},
		loadPrefs() {
			this.$api.tailscale.getPrefs().then(res => {
				if (res.data.success === 200) {
					this.prefs = res.data.data || {}
					this.prefsLoaded = true
				}
			}).catch(() => {
				this.prefError = this.$t('Failed to load advanced options')
			})
		},
		setPref(key, value) {
			this.prefError = ''
			this.savingPref = key
			this.$api.tailscale.setPrefs({ [key]: value }).then(res => {
				if (res.data.success === 200) {
					this.$set(this.prefs, key, value)
				} else {
					this.prefError = res.data.message
				}
			}).catch(e => {
				this.prefError = e.response && e.response.data ? e.response.data.message : this.$t('Failed to update Tailscale settings')
			}).finally(() => {
				this.savingPref = ''
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.remote-access-panel {
	display: flex;
	flex-direction: column;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
}

.no-peers {
	padding: 1rem 1.25rem;
}

.peer-row {
	align-items: flex-start;
}

.user-main {
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
	flex: 1;
}

.user-name {
	font-weight: 500;
	font-size: 0.85rem;
}

.chip-row {
	display: flex;
	flex-wrap: wrap;
	gap: 0.4rem;
}

.setting-chip.is-feature {
	border-color: rgba(31, 111, 235, 0.25);
	background: rgba(31, 111, 235, 0.06);
	color: #1f6feb;
}

.setting-chip.is-online {
	border-color: rgba(35, 168, 90, 0.3);
	background: rgba(35, 168, 90, 0.08);
	color: #1f8a4c;
}

.advanced-toggle {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	align-self: flex-start;
	margin: 0.75rem 0 0.5rem;
	padding: 0;
	border: none;
	background: transparent;
	color: rgba(44, 62, 80, 0.6);
	font-size: 0.8rem;
	font-weight: 500;
	cursor: pointer;

	&:hover {
		color: rgba(44, 62, 80, 0.9);
	}
}

.advertise-hint {
	padding: 0 1.25rem 1rem;
}
</style>
