<template>
	<section class="settings-section">
		<div class="section-header is-flex is-align-items-center is-justify-content-between mb-4">
			<div>
				<h2 class="section-title mb-1">{{ $t('Companion Devices') }}</h2>
				<p class="section-subtitle text-muted is-size-7">
					{{ $t('Manage connected phones, tablets, and companion clients. Seamlessly browse shared files and monitor real-time hardware status.') }}
				</p>
			</div>
			<div class="header-actions is-flex is-align-items-center">
				<b-button
					rounded
					size="is-small"
					class="mr-2"
					:loading="loading"
					@click="fetchDevices"
					:title="$t('Refresh devices')"
				>
					<b-icon icon="refresh" pack="mdi" size="is-small" class="mr-1"></b-icon>
					{{ $t('Refresh') }}
				</b-button>
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					@click="downloadApk"
				>
					<b-icon icon="android" pack="mdi" size="is-small" class="mr-1"></b-icon>
					{{ $t('Get Android App') }}
				</b-button>
			</div>
		</div>

		<!-- Overview Card -->
		<h3 class="setting-card-title">{{ $t('Overview') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="cellphone-link" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Companion Device Gateway') }}</div>
					<div class="setting-desc">
						{{ onlineDevicesCount }} {{ $t('online') }} &middot; {{ devices.length }} {{ $t('paired') }} &middot; {{ $t('Combined Capacity') }}: {{ formatBytes(totalCompanionStorage) }}
					</div>
				</div>
			</div>
		</div>

		<!-- Connected Devices Card -->
		<h3 class="setting-card-title">{{ $t('Connected Devices') }}</h3>
		<div class="setting-card">
			<div v-if="loading && devices.length === 0" class="account-empty has-text-centered p-5">
				<b-icon icon="loading" pack="mdi" size="is-medium" custom-class="mdi-spin"></b-icon>
				<div class="mt-2 is-size-7 text-muted">{{ $t('Discovering companion devices...') }}</div>
			</div>

			<div v-else-if="!devices.length" class="account-empty has-text-centered p-5">
				<div class="mb-2"><b-icon icon="cellphone-link" pack="mdi" size="is-large" class="text-muted"></b-icon></div>
				<div class="setting-title mb-1">{{ $t('No Companion Devices Connected') }}</div>
				<p class="setting-desc mb-3" style="max-width: 480px; margin: 0 auto 1rem;">
					{{ $t('Download and connect the NivaroOS Companion App on your Android or iOS device to sync files and manage your system.') }}
				</p>
				<b-button type="is-primary" size="is-small" rounded @click="downloadApk">
					<b-icon icon="download" pack="mdi" size="is-small" class="mr-1"></b-icon>
					{{ $t('Download NivaroOS.apk') }}
				</b-button>
			</div>

			<div v-else v-for="dev in devices" :key="dev.id" class="setting-row">
				<b-icon :icon="getDeviceIcon(dev.platform, dev.model, dev.name)" pack="mdi" class="row-icon"></b-icon>
				<div class="row-label">
					<div class="is-flex is-align-items-center">
						<span class="setting-title mr-2">{{ dev.name || dev.model || 'Companion' }}</span>
						<span class="companion-status-tag" :class="dev.is_online ? 'is-online' : 'is-offline'">
							<span class="status-dot"></span>
							{{ dev.is_online ? $t('Online') : $t('Offline') }}
						</span>
						<span v-if="dev.battery_level > 0" class="companion-meta-pill ml-2" :title="$t('Battery')">
							<b-icon :icon="getBatteryIconClass(dev.battery_level)" pack="mdi" size="is-small" class="mr-1" :style="{ color: getBatteryColor(dev.battery_level) }"></b-icon>
							{{ dev.battery_level }}%
						</span>
						<span class="companion-meta-pill ml-2" v-if="dev.ip">
							IP: {{ dev.ip }}
						</span>
					</div>
					<div class="setting-desc">
						{{ dev.model }} &middot; {{ dev.platform }}<template v-if="dev.os_version"> &middot; {{ dev.os_version }}</template> &middot; {{ dev.app_version || 'v1.0' }} &middot; {{ dev.is_online ? $t('Active now') : ($t('Last seen ') + formatTime(dev.last_seen)) }}
					</div>
					<div v-if="dev.storage_total > 0" class="companion-storage-strip mt-2">
						<div class="is-flex is-align-items-center is-size-7 text-muted mb-1">
							<span>{{ $t('Device Storage') }}: {{ formatBytes(dev.storage_used) }} / {{ formatBytes(dev.storage_total) }} ({{ getStoragePercent(dev) }}%)</span>
							<span v-if="dev.server_storage_used > 0" class="ml-2">&middot; {{ $t('Backed up') }}: {{ formatBytes(dev.server_storage_used) }}</span>
						</div>
						<div class="companion-progress-track">
							<div class="companion-progress-fill" :style="{ width: getStoragePercent(dev) + '%' }"></div>
						</div>
					</div>
				</div>
				<div class="row-control">
					<b-button rounded size="is-small" type="is-primary" outlined @click="openStorageFolder(dev)">
						<b-icon icon="folder-outline" pack="mdi" size="is-small" class="mr-1"></b-icon>
						{{ $t('Shared Files') }}
					</b-button>
					<button class="icon-button ml-2" type="button" :title="$t('Rename Device')" @click="openRenameModal(dev)">
						<b-icon icon="pencil-outline" pack="mdi" size="is-16"></b-icon>
					</button>
					<button class="icon-button ml-1 is-danger" type="button" :title="$t('Remove Device')" @click="confirmDelete(dev)">
						<b-icon icon="trash-can-outline" pack="mdi" size="is-16"></b-icon>
					</button>
				</div>
			</div>
		</div>

		<!-- Rename Modal -->
		<b-modal v-model="renameModalActive" has-modal-card trap-focus :destroy-on-hide="false" aria-role="dialog" aria-modal>
			<div class="modal-card" style="max-width: 440px;">
				<header class="modal-card-head">
					<p class="modal-card-title is-size-6">{{ $t('Rename Companion Device') }}</p>
					<button type="button" class="delete" @click="renameModalActive = false" />
				</header>
				<section class="modal-card-body">
					<b-field :label="$t('Device Name')">
						<b-input v-model="newDeviceName" :placeholder="$t('e.g. My Phone')" required></b-input>
					</b-field>
				</section>
				<footer class="modal-card-foot is-flex is-justify-content-flex-end">
					<b-button @click="renameModalActive = false">{{ $t('Cancel') }}</b-button>
					<b-button type="is-primary" :loading="saving" @click="saveDeviceName">{{ $t('Save') }}</b-button>
				</footer>
			</div>
		</b-modal>
	</section>
</template>

<script>
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)

export const ROWS = [{ label: 'Overview' }, { label: 'Connected Devices' }]

export default {
	name: 'companion-section',
	data() {
		return {
			devices: [],
			loading: false,
			saving: false,
			renameModalActive: false,
			selectedDevice: null,
			newDeviceName: ''
		}
	},
	computed: {
		onlineDevicesCount() {
			return this.devices.filter(d => d.is_online).length
		},
		totalCompanionStorage() {
			return this.devices.reduce((acc, d) => acc + (d.storage_total || 0), 0)
		}
	},
	created() {
		this.fetchDevices()
	},
	methods: {
		async fetchDevices() {
			this.loading = true
			try {
				const res = await this.$api.companion.getDevices()
				if (res.data && res.data.data) {
					this.devices = res.data.data
				}
			} catch (err) {
				console.error('Failed to fetch companion devices', err)
			} finally {
				this.loading = false
			}
		},
		getDeviceIcon(platform, model, name) {
			const p = (platform || '').toLowerCase()
			const m = (model || '').toLowerCase()
			const n = (name || '').toLowerCase()

			if (p.includes('ios') || p.includes('iphone') || m.includes('iphone') || n.includes('iphone')) return 'cellphone-apple'
			if (p.includes('ipad') || m.includes('ipad') || n.includes('ipad')) return 'tablet-ipad'
			if (m.includes('tablet') || m.includes('pad') || m.includes('tab') || m.includes('ruan') || n.includes('tablet') || n.includes('pad') || n.includes('tab')) return 'tablet-android'
			if (p.includes('mac') || m.includes('mac')) return 'laptop-mac'
			if (p.includes('windows') || m.includes('windows')) return 'microsoft-windows'
			return 'cellphone'
		},
		getBatteryIconClass(level) {
			if (level >= 90) return 'battery'
			if (level >= 75) return 'battery-80'
			if (level >= 50) return 'battery-50'
			if (level >= 30) return 'battery-30'
			if (level >= 15) return 'battery-20'
			return 'battery-alert'
		},
		getBatteryColor(level) {
			if (level <= 20) return '#ef4444'
			if (level <= 45) return '#f59e0b'
			return '#10b981'
		},
		getStoragePercent(dev) {
			if (!dev.storage_total || dev.storage_total === 0) return 0
			return Math.round((dev.storage_used / dev.storage_total) * 100)
		},
		openRenameModal(dev) {
			this.selectedDevice = dev
			this.newDeviceName = dev.name || dev.model
			this.renameModalActive = true
		},
		async saveDeviceName() {
			if (!this.selectedDevice || !this.newDeviceName.trim()) return
			this.saving = true
			try {
				await this.$api.companion.updateDevice(this.selectedDevice.id, {
					name: this.newDeviceName.trim()
				})
				this.$buefy.toast.open({
					message: this.$t('Device renamed successfully!'),
					type: 'is-success'
				})
				this.renameModalActive = false
				this.fetchDevices()
			} catch (err) {
				this.$buefy.toast.open({
					message: this.$t('Failed to rename device: ') + (err.response?.data?.message || err.message),
					type: 'is-danger'
				})
			} finally {
				this.saving = false
			}
		},
		confirmDelete(dev) {
			this.$buefy.dialog.confirm({
				title: this.$t('Remove Companion Device'),
				message: this.$t('Are you sure you want to remove "{name}"? It will need to reconnect to pair again.', { name: dev.name || dev.model }),
				confirmText: this.$t('Remove'),
				type: 'is-danger',
				hasIcon: true,
				onConfirm: async () => {
					try {
						await this.$api.companion.deleteDevice(dev.id)
						this.$buefy.toast.open({
							message: this.$t('Device removed'),
							type: 'is-success'
						})
						this.fetchDevices()
					} catch (err) {
						this.$buefy.toast.open({
							message: this.$t('Failed to remove device: ') + err.message,
							type: 'is-danger'
						})
					}
				}
			})
		},
		openStorageFolder(dev) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'files',
				title: this.$t('Files'),
				component: 'FilesApp',
				width: 960,
				height: 620,
				props: { initialPath: dev.storage_path || `/DATA/Companion/${dev.name}` }
			})
		},
		downloadApk() {
			window.open('/DATA/Downloads/NivaroOS.apk', '_blank')
		},
		formatBytes(bytes) {
			if (!bytes || bytes === 0) return '0 B'
			const k = 1024
			const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
			const i = Math.floor(Math.log(bytes) / Math.log(k))
			return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
		},
		formatTime(ts) {
			if (!ts) return 'Never'
			return dayjs(ts).fromNow()
		}
	}
}
</script>

<style lang="scss" scoped>
.companion-status-tag {
	display: inline-flex;
	align-items: center;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-card);
	font-size: var(--font-2xs);
	line-height: 1.2;

	.status-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		margin-right: var(--space-1);
	}

	&.is-online {
		background: rgba(16, 185, 129, 0.12);
		color: #059669;
		.status-dot {
			background: #10b981;
		}
	}

	&.is-offline {
		background: rgba(148, 163, 184, 0.15);
		color: var(--theme-text-muted, #64748b);
		.status-dot {
			background: #94a3b8;
		}
	}
}

.companion-meta-pill {
	display: inline-flex;
	align-items: center;
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	background: var(--theme-pill-bg, rgba(0, 0, 0, 0.04));
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-sm);
}

.companion-storage-strip {
	max-width: 480px;
}

.companion-progress-track {
	height: 4px;
	border-radius: var(--radius-xs);
	background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
	overflow: hidden;
}


.companion-progress-fill {
	height: 100%;
	background: #3b82f6;
	border-radius: var(--radius-xs);
	transition: width 0.3s ease;
}

.icon-button.is-danger:hover {
	background: rgba(239, 68, 68, 0.1);
	color: #ef4444;
}
</style>
