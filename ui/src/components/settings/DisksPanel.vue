<template>
	<div class="disks-panel">
		<div class="row-label-heading">{{ $t('Available disks') }}</div>
		<p v-if="!avail.length" class="hint">{{ $t('No unformatted disks detected.') }}</p>
		<div v-for="d in avail" :key="d.path">
			<div class="user-row">
				<div class="user-main">
					<div class="user-name">{{ d.path }}</div>
					<span class="badge">{{ formatSize(d.size) }} &middot; {{ d.disk_type }}</span>
				</div>
				<div class="row-actions">
					<button class="icon-button" type="button" :title="$t('Drive info')" @click="toggleDetails(d)">
						<b-icon icon="information-outline" pack="casa" size="is-16"></b-icon>
					</button>
					<b-button rounded size="is-small" type="is-dark" :loading="busyPath === d.path" @click="confirmAdd(d)">
						{{ $t('Use for storage') }}
					</b-button>
				</div>
			</div>
			<drive-details-panel v-if="expandedPath === d.path" :disk="d" @close="expandedPath = ''"></drive-details-panel>
		</div>

		<div class="row-label-heading mt-4">{{ $t('Mounted disks') }}</div>
		<div v-for="d in mountedDisks" :key="d.path">
			<div class="user-row">
				<div class="user-main">
					<div class="user-name">{{ d.path }}</div>
					<span class="badge">{{ formatSize(d.size) }} &middot; {{ d.disk_type }} &middot; {{ d.health === 'true' ? $t('Healthy') : $t('Check disk') }}</span>
				</div>
				<div class="row-actions">
					<button class="icon-button" type="button" :title="$t('Drive info')" @click="toggleDetails(d)">
						<b-icon icon="information-outline" pack="casa" size="is-16"></b-icon>
					</button>
					<b-button rounded size="is-small" type="is-danger" outlined :loading="busyPath === d.path" @click="confirmRemove(d)">
						{{ $t('Remove') }}
					</b-button>
				</div>
			</div>
			<drive-details-panel v-if="expandedPath === d.path" :disk="d" @close="expandedPath = ''"></drive-details-panel>
		</div>

		<div class="row-label-heading mt-4">{{ $t('USB drives') }}</div>
		<div class="toggle-row">
			<b-icon class="row-icon" icon="usb-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Automount USB Drive') }}</div>
			<b-switch v-model="autoUsbMount" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="toggleAutoMount"></b-switch>
		</div>
		<p v-if="!usb.length" class="hint">{{ $t('No USB drives connected.') }}</p>
		<div v-for="u in usb" :key="u.name" class="user-row">
			<div class="user-main">
				<div class="user-name">{{ u.name }}</div>
				<span class="badge">{{ formatSize(u.size) }}</span>
			</div>
			<b-button v-for="c in u.children" :key="c.mount_point" rounded size="is-small" type="is-danger" outlined @click="confirmEject(c)">
				{{ $t('Eject {mount}', { mount: c.mount_point }) }}
			</b-button>
		</div>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
import DriveDetailsPanel from '@/components/settings/DriveDetailsPanel.vue'
import { formatSize } from '@/utils/formatSize'

export default {
	name: 'disks-panel',
	components: { DriveDetailsPanel },
	data() {
		return {
			disks: [],
			avail: [],
			usb: [],
			autoUsbMount: false,
			busyPath: '',
			error: '',
			expandedPath: ''
		}
	},
	computed: {
		mountedDisks() {
			return this.disks.filter(d => !this.avail.some(a => a.path === d.path))
		}
	},
	created() {
		this.refresh()
		this.refreshUsb()
		this.getAutoMountStatus()
	},
	methods: {
		formatSize,
		toggleDetails(disk) {
			this.expandedPath = this.expandedPath === disk.path ? '' : disk.path
		},
		refresh() {
			this.$api.disks.getDiskList().then(res => {
				if (res.data.success === 200) {
					this.disks = res.data.data.disks || []
					this.avail = res.data.data.avail || []
				}
			}).catch(() => {
				this.error = this.$t('Failed to load disks')
			})
		},
		refreshUsb() {
			this.$api.disks.getUsbs().then(res => {
				if (res.data.success === 200) this.usb = res.data.data || []
			}).catch(() => {
				this.error = this.$t('Failed to load USB drives')
			})
		},
		getAutoMountStatus() {
			this.$api.sys.getUsbStatus().then(res => {
				if (res.data.success === 200) this.autoUsbMount = res.data.data === 'True'
			}).catch(() => {
				this.error = this.$t('Failed to load automount status')
			})
		},
		toggleAutoMount() {
			const previousState = this.autoUsbMount
			this.$api.sys.toggleUsbAutoMount({ state: this.autoUsbMount ? 'on' : 'off' }).catch(() => {
				this.autoUsbMount = previousState
				this.error = this.$t('Failed to toggle automount')
			})
		},
		confirmAdd(disk) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Use disk for storage'),
				message: this.$t('Format and mount {path} for use as storage? Any existing data on it will be erased.', { path: disk.path }),
				type: 'is-danger',
				confirmText: this.$t('Format & use'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.busyPath = disk.path
					this.error = ''
					this.$api.storage.create({ path: disk.path, name: '', format: true }).then(res => {
						if (res.data.success !== 200) this.error = res.data.message
						this.refresh()
					}).catch(e => {
						this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to add storage')
					}).finally(() => {
						this.busyPath = ''
					})
				}
			})
		},
		confirmRemove(disk) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove disk'),
				message: this.$t('Unmount and stop using {path} for storage?', { path: disk.path }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.busyPath = disk.path
					this.$api.disks.umount({ path: disk.path }).then(() => this.refresh()).finally(() => {
						this.busyPath = ''
					})
				}
			})
		},
		confirmEject(child) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Eject USB drive'),
				message: this.$t('Safely eject {mount}?', { mount: child.mount_point }),
				type: 'is-danger',
				confirmText: this.$t('Eject'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.disks.umountUsb({ mount_point: child.mount_point }).then(() => this.refreshUsb())
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.disks-panel {
	padding: 1.25rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.mt-4 {
	margin-top: 1.5rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.toggle-row {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	padding: 0.6rem 0;
	margin-bottom: 0.6rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);

	.row-icon {
		color: hsla(208, 16%, 42%, 1);
	}

	.row-label {
		flex: 1;
		font-size: 0.85rem;
		font-weight: 500;
	}
}

.user-main {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.user-name {
	font-weight: 500;
	font-size: 0.85rem;
}

.badge {
	font-size: 0.7rem;
	opacity: 0.6;
}

.row-actions {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-shrink: 0;
}

.icon-button {
	flex-shrink: 0;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.7rem;
	height: 1.7rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}
</style>
