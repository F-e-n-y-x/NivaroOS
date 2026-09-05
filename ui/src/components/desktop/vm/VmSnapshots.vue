<!-- src/components/desktop/vm/VmSnapshots.vue -->
<template>
	<div class="vm-snapshots">
		<div class="vm-snapshots-toolbar is-flex is-align-items-center is-justify-content-between">
			<div class="is-flex is-align-items-center">
				<h2 class="vm-snapshots-title mr-3">{{ $t('VM Snapshots') }}</h2>
				<!-- VM Selector -->
				<div v-if="vms.length" class="vm-selector-wrap">
					<vm-dropdown
						v-model="selectedVmName"
						:options="vmDropdownOptions"
						:placeholder="$t('Select Virtual Machine')"
						size="small"
						@input="loadSnapshots"
					></vm-dropdown>
				</div>
			</div>

			<div v-if="selectedVm" class="toolbar-actions is-flex is-align-items-center">
				<button
					class="refresh-btn mr-2"
					:title="$t('Refresh snapshots')"
					:disabled="loadingSnapshots"
					@click="loadSnapshots"
				>
					<b-icon icon="refresh" :custom-class="loadingSnapshots ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
				</button>

				<button
					class="create-snap-btn"
					:disabled="takingSnapshot"
					@click="openTakeSnapshotModal"
				>
					<b-icon icon="camera-plus-outline" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Take Snapshot') }}</span>
				</button>
			</div>
		</div>

		<!-- Loading State -->
		<div v-if="loadingVms" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<!-- No VMs on Host -->
		<div v-else-if="!vms.length" class="vm-empty">
			<b-icon icon="monitor-off" custom-size="mdi-48px"></b-icon>
			<p class="vm-empty-title">{{ $t('No VMs found') }}</p>
			<p class="vm-empty-hint">{{ $t('Create a virtual machine first to take and manage snapshots.') }}</p>
		</div>

		<!-- Main Snapshots View for Selected VM -->
		<div v-else class="snapshots-body">
			<!-- VM Info Header Summary Card -->
			<div v-if="selectedVm" class="vm-summary-card mb-4">
				<div class="is-flex is-align-items-center is-justify-content-between">
					<div class="is-flex is-align-items-center">
						<div class="vm-avatar mr-3">
							<b-icon :icon="osIcon(selectedVm)" custom-size="mdi-24px"></b-icon>
						</div>
						<div>
							<div class="is-flex is-align-items-center">
								<span class="vm-name-title font-weight-bold mr-2">{{ selectedVm.name }}</span>
								<span class="vm-state-pill" :class="'is-' + selectedVm.state">
									<span class="state-dot"></span>
									{{ stateLabel(selectedVm.state) }}
								</span>
							</div>
							<div class="vm-specs-text text-muted is-size-7 mt-1">
								<span>{{ selectedVm.vcpus }} {{ $t('vCPU') }}</span>
								<span class="mx-2">&middot;</span>
								<span>{{ formatMib(selectedVm.memory_mib) }}</span>
								<span class="mx-2">&middot;</span>
								<span>{{ snapshots.length }} {{ snapshots.length === 1 ? $t('snapshot') : $t('snapshots') }}</span>
							</div>
						</div>
					</div>

					<div class="safety-net-badge is-flex is-align-items-center">
						<b-icon
							:icon="snapshots.length ? 'shield-check' : 'shield-alert-outline'"
							:class="snapshots.length ? 'has-text-success' : 'has-text-warning'"
							custom-size="mdi-20px"
							class="mr-2"
						></b-icon>
						<span class="is-size-7 font-weight-bold" :class="snapshots.length ? 'has-text-success' : 'has-text-grey-dark'">
							{{ snapshots.length ? $t('Safety Net Active') : $t('No Safety Net') }}
						</span>
					</div>
				</div>
			</div>

			<!-- Snapshots List Loading -->
			<div v-if="loadingSnapshots" class="p-6 has-text-centered text-muted">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-32px"></b-icon>
				<div class="mt-2 is-size-7">{{ $t('Loading snapshots...') }}</div>
			</div>

			<!-- No Snapshots for Current VM -->
			<div v-else-if="!snapshots.length" class="empty-snapshots-card has-text-centered p-6">
				<div class="empty-icon-wrap mb-3">
					<b-icon icon="camera-outline" custom-size="mdi-40px"></b-icon>
				</div>
				<h3 class="is-size-6 font-weight-bold mb-1">{{ $t('No snapshots yet for {name}', { name: selectedVm.name }) }}</h3>
				<p class="text-muted is-size-7 mb-4 max-w-md mx-auto">
					{{ $t('Snapshots capture the exact virtual disk and machine state at a point in time, allowing you to instantly revert any unwanted changes.') }}
				</p>
				<button class="create-snap-btn" @click="openTakeSnapshotModal">
					<b-icon icon="camera-plus-outline" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Take Safety Snapshot') }}</span>
				</button>
			</div>

			<!-- Snapshots Timeline List -->
			<div v-else class="snapshots-timeline">
				<div
					v-for="snap in snapshots"
					:key="snap.name"
					class="snapshot-item-card mb-3"
					:class="{ 'is-current': snap.current }"
				>
					<div class="snapshot-header is-flex is-align-items-center is-justify-content-between">
						<div class="is-flex is-align-items-center">
							<div class="snap-icon-wrap mr-3" :class="snap.current ? 'is-active' : ''">
								<b-icon icon="camera-outline" custom-size="mdi-20px"></b-icon>
							</div>
							<div>
								<div class="is-flex is-align-items-center">
									<span class="snap-name font-weight-bold mr-2">{{ snap.name }}</span>
									<span v-if="snap.current" class="current-badge mr-2">
										{{ $t('Current Point') }}
									</span>
									<span class="snap-state-pill" :class="'is-' + snap.state">
										{{ snap.state }}
									</span>
								</div>
								<div v-if="snap.description" class="snap-desc is-size-7 text-muted mt-1">
									{{ snap.description }}
								</div>
								<div class="snap-meta text-muted is-size-7 mt-1 is-flex is-align-items-center">
									<b-icon icon="clock-outline" custom-size="mdi-14px" class="mr-1"></b-icon>
									<span>{{ formatDate(snap.creation_time) }}</span>
									<span class="mx-1">&middot;</span>
									<span>{{ formatTimeAgo(snap.creation_time) }}</span>
								</div>
							</div>
						</div>

						<div class="snap-actions is-flex is-align-items-center">
							<!-- Revert Button -->
							<b-button
								rounded
								size="is-small"
								type="is-primary"
								outlined
								class="mr-2"
								:loading="revertingName === snap.name"
								:disabled="revertingName === snap.name || deletingName === snap.name"
								@click="confirmRevert(snap)"
							>
								<b-icon icon="history" custom-size="mdi-16px" class="mr-1"></b-icon>
								{{ $t('Revert') }}
							</b-button>

							<!-- Delete Button -->
							<b-button
								rounded
								size="is-small"
								class="has-text-danger"
								:loading="deletingName === snap.name"
								:disabled="revertingName === snap.name || deletingName === snap.name"
								@click="confirmDelete(snap)"
								:title="$t('Delete snapshot')"
							>
								<b-icon icon="trash-can-outline" custom-size="mdi-16px"></b-icon>
							</b-button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- ==================== TAKE SNAPSHOT MODAL ==================== -->
		<b-modal
			v-model="showTakeModal"
			has-modal-card
			trap-focus
			aria-modal
		>
			<div class="modal-card snap-modal-card">
				<header class="modal-card-head">
					<p class="modal-card-title is-size-6 font-weight-bold">
						<b-icon icon="camera-plus-outline" custom-size="mdi-18px" class="mr-2"></b-icon>
						{{ $t('Take VM Snapshot') }} - {{ selectedVm ? selectedVm.name : '' }}
					</p>
					<button type="button" class="delete" @click="showTakeModal = false" />
				</header>
				<section class="modal-card-body">
					<div class="notification is-info is-light is-size-7 mb-4">
						<b-icon icon="information-outline" size="is-small" class="mr-1"></b-icon>
						{{ $t('Captures the exact virtual disk and RAM state of the virtual machine. You can revert back to this point anytime.') }}
					</div>

					<b-field :label="$t('Snapshot Name')">
						<b-input
							v-model="newSnapName"
							:placeholder="$t('e.g. pre-update, baseline, clean-install')"
							maxlength="64"
							required
						></b-input>
					</b-field>

					<b-field :label="$t('Description (Optional)')">
						<b-input
							v-model="newSnapDesc"
							type="textarea"
							:placeholder="$t('Notes about this state (e.g. Before upgrading graphics drivers or installing packages)')"
							rows="2"
							maxlength="256"
						></b-input>
					</b-field>
				</section>
				<footer class="modal-card-foot is-justify-content-flex-end">
					<b-button rounded size="is-small" @click="showTakeModal = false">{{ $t('Cancel') }}</b-button>
					<b-button
						rounded
						size="is-small"
						type="is-primary"
						:loading="takingSnapshot"
						@click="takeSnapshot"
					>
						{{ $t('Create Snapshot') }}
					</b-button>
				</footer>
			</div>
		</b-modal>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmDropdown from './VmDropdown.vue'
import activityService from '@/service/activity'

export default {
	name: 'vm-snapshots',
	components: {
		VmDropdown
	},
	data() {
		return {
			vms: [],
			loadingVms: false,
			selectedVmName: '',
			snapshots: [],
			loadingSnapshots: false,
			showTakeModal: false,
			newSnapName: '',
			newSnapDesc: '',
			takingSnapshot: false,
			revertingName: null,
			deletingName: null
		}
	},
	computed: {
		selectedVm() {
			return this.vms.find(v => v.name === this.selectedVmName) || null
		},
		vmDropdownOptions() {
			return this.vms.map(v => ({
				value: v.name,
				label: v.name,
				meta: this.stateLabel(v.state),
				icon: this.osIcon(v)
			}))
		}
	},
	async created() {
		await this.loadVMs()
	},
	methods: {
		async loadVMs() {
			this.loadingVms = true
			try {
				const list = await vmSidecar.listVMs()
				this.vms = Array.isArray(list) ? list : []
				if (this.vms.length && !this.selectedVmName) {
					this.selectedVmName = this.vms[0].name
				}
				if (this.selectedVmName) {
					await this.loadSnapshots()
				}
			} catch (err) {
				console.error('Failed to load VMs:', err)
			} finally {
				this.loadingVms = false
			}
		},
		async loadSnapshots() {
			if (!this.selectedVmName) return
			this.loadingSnapshots = true
			try {
				const res = await vmSidecar.listSnapshots(this.selectedVmName)
				this.snapshots = Array.isArray(res) ? res : []
			} catch (err) {
				console.error('Failed to load snapshots:', err)
				this.snapshots = []
			} finally {
				this.loadingSnapshots = false
			}
		},
		openTakeSnapshotModal() {
			const now = new Date()
			const pad = n => String(n).padStart(2, '0')
			this.newSnapName = `snap-${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}`
			this.newSnapDesc = ''
			this.showTakeModal = true
		},
		async takeSnapshot() {
			if (!this.newSnapName.trim()) {
				this.$buefy.toast.open({
					message: this.$t('Please enter a snapshot name'),
					type: 'is-warning',
					position: 'is-top',
					duration: 2000
				})
				return
			}
			this.takingSnapshot = true
			try {
				await vmSidecar.createSnapshot(this.selectedVmName, {
					name: this.newSnapName.trim(),
					description: this.newSnapDesc.trim()
				})
				this.$buefy.toast.open({
					message: this.$t('Snapshot "{name}" created successfully', { name: this.newSnapName }),
					type: 'is-success',
					position: 'is-top',
					duration: 2500
				})
				activityService.add({
					title: this.$t('VM Snapshot Created'),
					message: `${this.selectedVmName}: ${this.newSnapName}`,
					type: 'vm',
					status: 'success'
				})
				this.showTakeModal = false
				await this.loadSnapshots()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to create snapshot'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3500
				})
			} finally {
				this.takingSnapshot = false
			}
		},
		confirmRevert(snap) {
			this.$buefy.dialog.confirm({
				title: this.$t('Revert to Snapshot'),
				message: this.$t('Are you sure you want to revert "{vm}" to snapshot "{snap}"? Any disk changes made after this snapshot was taken will be permanently lost.', {
					vm: this.selectedVmName,
					snap: snap.name
				}),
				confirmText: this.$t('Revert to Snapshot'),
				cancelText: this.$t('Cancel'),
				type: 'is-warning',
				hasIcon: true,
				icon: 'history',
				iconPack: 'mdi',
				onConfirm: async () => {
					this.revertingName = snap.name
					try {
						await vmSidecar.revertSnapshot(this.selectedVmName, snap.name)
						this.$buefy.toast.open({
							message: this.$t('Successfully reverted "{vm}" to "{snap}"', {
								vm: this.selectedVmName,
								snap: snap.name
							}),
							type: 'is-success',
							position: 'is-top',
							duration: 3000
						})
						activityService.add({
							title: this.$t('VM Reverted to Snapshot'),
							message: `${this.selectedVmName} reverted to "${snap.name}"`,
							type: 'vm',
							status: 'info'
						})
						await this.loadVMs()
					} catch (err) {
						this.$buefy.toast.open({
							message: err.message || this.$t('Failed to revert to snapshot'),
							type: 'is-danger',
							position: 'is-top',
							duration: 4000
						})
					} finally {
						this.revertingName = null
					}
				}
			})
		},
		confirmDelete(snap) {
			this.$buefy.dialog.confirm({
				title: this.$t('Delete Snapshot'),
				message: this.$t('Are you sure you want to delete snapshot "{snap}" from "{vm}"?', {
					vm: this.selectedVmName,
					snap: snap.name
				}),
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				hasIcon: true,
				icon: 'trash-can-outline',
				iconPack: 'mdi',
				onConfirm: async () => {
					this.deletingName = snap.name
					try {
						await vmSidecar.deleteSnapshot(this.selectedVmName, snap.name, true)
						this.$buefy.toast.open({
							message: this.$t('Snapshot deleted'),
							type: 'is-success',
							position: 'is-top',
							duration: 2000
						})
						activityService.add({
							title: this.$t('VM Snapshot Deleted'),
							message: `Snapshot "${snap.name}" deleted from ${this.selectedVmName}`,
							type: 'vm',
							status: 'info'
						})
						await this.loadSnapshots()
					} catch (err) {
						this.$buefy.toast.open({
							message: err.message || this.$t('Failed to delete snapshot'),
							type: 'is-danger',
							position: 'is-top',
							duration: 3500
						})
					} finally {
						this.deletingName = null
					}
				}
			})
		},
		formatMib(mib) {
			if (!mib) return '0 MB'
			if (mib >= 1024) return (mib / 1024).toFixed(mib % 1024 === 0 ? 0 : 1) + ' GB'
			return mib + ' MB'
		},
		stateLabel(state) {
			switch (state) {
				case 'running': return this.$t('Running')
				case 'paused': return this.$t('Paused')
				case 'shutoff':
				case 'stopped': return this.$t('Shut Off')
				default: return state || this.$t('Unknown')
			}
		},
		osIcon(vm) {
			const name = (vm.name || '').toLowerCase()
			if (name.includes('win')) return 'microsoft-windows'
			if (name.includes('ubuntu') || name.includes('debian') || name.includes('linux') || name.includes('mint')) return 'linux'
			return 'monitor'
		},
		formatDate(timestamp) {
			if (!timestamp) return ''
			const d = new Date(timestamp * 1000)
			if (isNaN(d.getTime())) return ''
			return d.toLocaleString([], { month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit' })
		},
		formatTimeAgo(timestamp) {
			if (!timestamp) return ''
			const d = new Date(timestamp * 1000)
			if (isNaN(d.getTime())) return ''
			const diffSec = Math.floor((new Date() - d) / 1000)
			if (diffSec < 60) return this.$t('Just now')
			if (diffSec < 3600) return `${Math.floor(diffSec / 60)} ${this.$t('min ago')}`
			if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} ${this.$t('hours ago')}`
			return `${Math.floor(diffSec / 86400)} ${this.$t('days ago')}`
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-snapshots {
	padding: 1.25rem 1.5rem;
	height: 100%;
	overflow-y: auto;
}

.vm-snapshots-toolbar {
	margin-bottom: 1.25rem;
}

.vm-snapshots-title {
	font-size: 1.15rem;
	font-weight: 700;
	color: #0f172a;
	margin: 0;
}

.vm-selector-wrap {
	min-width: 220px;
}

.refresh-btn {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	width: 32px;
	height: 32px;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	color: #64748b;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		background: #f8fafc;
		color: #1e293b;
		border-color: #cbd5e1;
	}
}

.create-snap-btn {
	background: #2563eb;
	color: #ffffff;
	border: none;
	border-radius: 8px;
	height: 32px;
	padding: 0 12px;
	display: inline-flex;
	align-items: center;
	gap: 6px;
	font-size: 0.8rem;
	font-weight: 600;
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: #1d4ed8;
	}

	&:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
}

.vm-summary-card {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 14px 18px;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
}

.vm-avatar {
	width: 40px;
	height: 40px;
	border-radius: 10px;
	background: rgba(37, 99, 235, 0.1);
	color: #2563eb;
	display: flex;
	align-items: center;
	justify-content: center;
}

.vm-name-title {
	font-size: 1rem;
	color: #0f172a;
}

.vm-state-pill {
	display: inline-flex;
	align-items: center;
	gap: 5px;
	font-size: 11px;
	font-weight: 600;
	padding: 2px 8px;
	border-radius: 9999px;

	.state-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
	}

	&.is-running {
		background: rgba(16, 185, 129, 0.12);
		color: #059669;
		.state-dot { background: #10b981; }
	}

	&.is-shutoff,
	&.is-stopped {
		background: rgba(100, 116, 139, 0.12);
		color: #64748b;
		.state-dot { background: #94a3b8; }
	}

	&.is-paused {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
		.state-dot { background: #f59e0b; }
	}
}

.safety-net-badge {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	padding: 6px 12px;
	border-radius: 8px;
}

.empty-snapshots-card {
	background: #ffffff;
	border: 1px dashed #cbd5e1;
	border-radius: 14px;

	.empty-icon-wrap {
		width: 64px;
		height: 64px;
		border-radius: 50%;
		background: #f1f5f9;
		color: #94a3b8;
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}
}

.max-w-md {
	max-width: 440px;
}

.snapshot-item-card {
	background: #ffffff;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 14px 18px;
	transition: all 0.15s ease;

	&:hover {
		border-color: #cbd5e1;
		box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
	}

	&.is-current {
		border-color: rgba(37, 99, 235, 0.35);
		box-shadow: 0 0 0 1px rgba(37, 99, 235, 0.2);
	}
}

.snap-icon-wrap {
	width: 38px;
	height: 38px;
	border-radius: 9px;
	background: #f1f5f9;
	color: #64748b;
	display: flex;
	align-items: center;
	justify-content: center;

	&.is-active {
		background: rgba(37, 99, 235, 0.12);
		color: #2563eb;
	}
}

.snap-name {
	font-size: 0.95rem;
	color: #0f172a;
}

.current-badge {
	font-size: 10px;
	font-weight: 700;
	color: #2563eb;
	background: rgba(37, 99, 235, 0.1);
	padding: 1px 7px;
	border-radius: 4px;
	text-transform: uppercase;
	letter-spacing: 0.4px;
}

.snap-state-pill {
	font-size: 10px;
	font-weight: 600;
	color: #64748b;
	background: #f1f5f9;
	padding: 1px 6px;
	border-radius: 4px;
	text-transform: capitalize;
}

.snap-modal-card {
	width: 480px;
	max-width: 90vw;
}

.vm-loading,
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	height: 60%;
	color: #94a3b8;
	text-align: center;
}

.vm-empty-title {
	font-size: 1.1rem;
	font-weight: 600;
	color: #334155;
	margin-top: 0.75rem;
}

.vm-empty-hint {
	font-size: 0.85rem;
	color: #64748b;
	margin-bottom: 1.25rem;
}
</style>
