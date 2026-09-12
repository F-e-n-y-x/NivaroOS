<!-- src/apps/vm/VmSnapshots.vue -->
<template>
	<div class="vm-snapshots">
		<!-- Section Toolbar -->
		<div class="vm-section-toolbar">
			<div class="toolbar-left">
				<h2 class="vm-section-title">{{ $t('Snapshots') }}</h2>
				<vm-dropdown
					v-if="vms.length"
					v-model="selectedVmName"
					:options="vmDropdownOptions"
					:placeholder="$t('Select VM...')"
					size="small"
					class="vm-selector-dropdown ml-3"
					@input="onVmChange"
				></vm-dropdown>
				<button
					class="refresh-icon-btn ml-2"
					:title="$t('Refresh')"
					:disabled="loadingSnapshots"
					@click="loadSnapshots"
				>
					<b-icon icon="refresh" :custom-class="loadingSnapshots ? 'mdi-spin' : ''" custom-size="mdi-18px"></b-icon>
				</button>
			</div>

			<div class="toolbar-right">
				<div v-if="selectedVm" class="safety-net-badge" :class="snapshots.length ? 'is-active' : 'is-empty'">
					<b-icon :icon="snapshots.length ? 'shield-check' : 'shield-alert-outline'" custom-size="mdi-16px"></b-icon>
					<span>{{ snapshots.length ? `${snapshots.length} ${snapshots.length === 1 ? $t('Snapshot') : $t('Snapshots')}` : $t('No Safety Net') }}</span>
				</div>

				<button
					v-if="selectedVm"
					class="create-btn"
					:disabled="loadingSnapshots"
					@click="openTakeSnapshotModal"
				>
					<b-icon icon="camera-plus-outline" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Take Snapshot') }}</span>
				</button>
			</div>
		</div>

		<!-- Selected VM Spec Banner -->
		<div v-if="selectedVm && !loadingVms" class="vm-spec-banner">
			<div class="spec-banner-left">
				<div class="spec-banner-icon" :class="'is-' + selectedVm.state">
					<b-icon :icon="osIcon(selectedVm)" custom-size="mdi-20px"></b-icon>
				</div>
				<div class="spec-banner-info">
					<div class="spec-banner-title-row">
						<span class="spec-banner-name">{{ selectedVm.name }}</span>
						<span class="spec-banner-state" :class="'is-' + selectedVm.state">
							<span class="state-dot"></span>{{ stateLabel(selectedVm.state) }}
						</span>
					</div>
					<div class="spec-banner-specs">
						<span class="spec-item">
							<b-icon icon="memory" custom-size="mdi-13px"></b-icon>
							<span>{{ selectedVm.vcpus }} {{ $t('vCPU') }}</span>
						</span>
						<span class="spec-sep">&middot;</span>
						<span class="spec-item">
							<b-icon icon="chip" custom-size="mdi-13px"></b-icon>
							<span>{{ formatMib(selectedVm.memory_mib) }}</span>
						</span>
						<template v-if="getDiskTotal(selectedVm)">
							<span class="spec-sep">&middot;</span>
							<span class="spec-item">
								<b-icon icon="harddisk" custom-size="mdi-13px"></b-icon>
								<span>{{ getDiskTotal(selectedVm) }}</span>
							</span>
						</template>
						<span class="spec-sep">&middot;</span>
						<span class="spec-item">
							<b-icon icon="lan" custom-size="mdi-13px"></b-icon>
							<span>{{ networkLabel(selectedVm) }}</span>
						</span>
					</div>
				</div>
			</div>
			<div class="spec-banner-right">
				<div class="spec-banner-count">
					<span class="count-num">{{ snapshots.length }}</span>
					<span class="count-text">{{ snapshots.length === 1 ? $t('Restore Point') : $t('Restore Points') }}</span>
				</div>
			</div>
		</div>

		<!-- Loading State -->
		<div v-if="loadingSnapshots || loadingVms" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<!-- No VMs at all -->
		<div v-else-if="!vms.length" class="vm-empty">
			<b-icon icon="monitor-off" custom-size="mdi-48px"></b-icon>
			<p class="vm-empty-title">{{ $t('No Virtual Machines') }}</p>
			<p class="vm-empty-hint">{{ $t('Create a virtual machine first to manage its snapshots.') }}</p>
		</div>

		<!-- No Snapshots for Current Selected VM -->
		<div v-else-if="!snapshots.length" class="vm-empty">
			<b-icon icon="camera-outline" custom-size="mdi-48px"></b-icon>
			<p class="vm-empty-title">{{ $t('No snapshots yet for') }} {{ selectedVmName }}</p>
			<p class="vm-empty-hint">
				{{ $t('Snapshots capture the exact virtual disk and machine state at a point in time, allowing you to instantly revert any unwanted changes.') }}
			</p>
			<button class="create-btn-large" @click="openTakeSnapshotModal">
				<b-icon icon="camera-plus-outline" custom-size="mdi-18px"></b-icon>
				<span>{{ $t('Take Safety Snapshot') }}</span>
			</button>
		</div>

		<!-- Snapshots List -->
		<div v-else class="snapshot-list">
			<div
				v-for="snap in snapshots"
				:key="snap.name"
				class="snapshot-row"
				:class="{ 'is-current': snap.current }"
			>
				<div class="snapshot-icon" :class="{ 'is-current': snap.current }">
					<b-icon icon="camera-outline" :custom-size="snap.current ? 'mdi-22px' : 'mdi-20px'"></b-icon>
				</div>

				<div class="snapshot-info">
					<div class="snapshot-head">
						<span class="snapshot-name" :title="snap.name">{{ snap.name }}</span>
						<span v-if="snap.current" class="current-badge">
							<b-icon icon="check-circle" custom-size="mdi-13px"></b-icon>
							<span>{{ $t('Current Point') }}</span>
						</span>
						<span class="state-pill" :class="'is-' + (snap.state || 'running')">
							{{ snap.state }}
						</span>
					</div>
					<p v-if="snap.description" class="snapshot-desc">{{ snap.description }}</p>
					<div class="snapshot-meta">
						<b-icon icon="clock-outline" custom-size="mdi-13px"></b-icon>
						<span>{{ formatCreationTime(snap.creation_time) }}</span>
						<span v-if="formatTimeAgo(snap.creation_time)" class="meta-sep">&middot;</span>
						<span v-if="formatTimeAgo(snap.creation_time)">{{ formatTimeAgo(snap.creation_time) }}</span>
					</div>
				</div>

				<div class="snapshot-actions">
					<button
						class="revert-btn"
						:title="$t('Revert to this snapshot')"
						:disabled="revertingName === snap.name"
						@click="targetRevertSnap = snap"
					>
						<b-icon
							:icon="revertingName === snap.name ? 'loading' : 'history'"
							:custom-class="revertingName === snap.name ? 'mdi-spin' : ''"
							custom-size="mdi-16px"
						></b-icon>
						<span>{{ revertingName === snap.name ? $t('Reverting...') : $t('Revert') }}</span>
					</button>

					<button
						class="delete-btn"
						:title="$t('Delete snapshot')"
						:disabled="deletingName === snap.name"
						@click="targetDeleteSnap = snap"
					>
						<b-icon
							:icon="deletingName === snap.name ? 'loading' : 'trash-can-outline'"
							:custom-class="deletingName === snap.name ? 'mdi-spin' : ''"
							custom-size="mdi-18px"
						></b-icon>
					</button>
				</div>
			</div>
		</div>

		<!-- Take Snapshot Dialog -->
		<vm-overlay-panel
			:active="showTakeModal"
			:title="$t('Take Safety Snapshot')"
			width="28rem"
			@close="showTakeModal = false"
		>
			<div class="dialog-body">
				<p class="dialog-hint">
					{{ $t('Create an instant restore point for') }} <strong>{{ selectedVmName }}</strong>.
				</p>

				<b-field :label="$t('Snapshot Name')">
					<b-input
						v-model="newSnapName"
						size="is-small"
						:placeholder="$t('e.g. before-upgrade, clean-state')"
						required
					></b-input>
				</b-field>

				<b-field :label="$t('Notes / Description (Optional)')">
					<b-input
						v-model="newSnapDesc"
						type="textarea"
						size="is-small"
						rows="2"
						:placeholder="$t('Add any notes or context about this snapshot...')"
					></b-input>
				</b-field>
			</div>

			<template #footer>
				<b-button @click="showTakeModal = false">{{ $t('Cancel') }}</b-button>
				<b-button
					type="is-primary"
					:loading="takingSnapshot"
					:disabled="!newSnapName.trim()"
					@click="takeSnapshot"
				>
					<b-icon icon="camera-plus-outline" size="is-small" class="mr-1"></b-icon>
					<span>{{ $t('Create Snapshot') }}</span>
				</b-button>
			</template>
		</vm-overlay-panel>

		<!-- Revert Confirm Dialog -->
		<vm-overlay-panel
			:active="!!targetRevertSnap"
			:title="$t('Revert to Snapshot')"
			width="26rem"
			@close="targetRevertSnap = null"
		>
			<div v-if="targetRevertSnap" class="dialog-body">
				<div class="warning-banner mb-3">
					<b-icon icon="alert-outline" custom-size="mdi-20px" class="mr-2"></b-icon>
					<span>{{ $t('Warning: Any unsaved disk modifications made after this snapshot was taken will be permanently overwritten.') }}</span>
				</div>
				<p>
					{{ $t('Revert') }} <strong>{{ selectedVmName }}</strong> {{ $t('to snapshot') }} <strong>"{{ targetRevertSnap.name }}"</strong>?
				</p>
			</div>
			<template #footer>
				<b-button @click="targetRevertSnap = null">{{ $t('Cancel') }}</b-button>
				<b-button
					type="is-warning"
					:loading="!!revertingName"
					@click="executeRevert"
				>
					<b-icon icon="history" size="is-small" class="mr-1"></b-icon>
					<span>{{ $t('Revert to Snapshot') }}</span>
				</b-button>
			</template>
		</vm-overlay-panel>

		<!-- Delete Confirm Dialog -->
		<vm-overlay-panel
			:active="!!targetDeleteSnap"
			:title="$t('Delete Snapshot')"
			width="24rem"
			@close="targetDeleteSnap = null"
		>
			<div v-if="targetDeleteSnap" class="dialog-body">
				<p>
					{{ $t('Delete snapshot') }} <strong>"{{ targetDeleteSnap.name }}"</strong> {{ $t('from') }} <strong>{{ selectedVmName }}</strong>?
				</p>
			</div>
			<template #footer>
				<b-button @click="targetDeleteSnap = null">{{ $t('Cancel') }}</b-button>
				<b-button
					type="is-danger"
					:loading="!!deletingName"
					@click="executeDelete"
				>
					<b-icon icon="trash-can-outline" size="is-small" class="mr-1"></b-icon>
					<span>{{ $t('Delete') }}</span>
				</b-button>
			</template>
		</vm-overlay-panel>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmDropdown from './VmDropdown.vue'
import VmOverlayPanel from './VmOverlayPanel.vue'
import activityService from '@/service/activity'

export default {
	name: 'vm-snapshots',
	components: {
		VmDropdown,
		VmOverlayPanel
	},
	props: {
		initialVm: {
			type: String,
			default: ''
		}
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
			deletingName: null,
			targetRevertSnap: null,
			targetDeleteSnap: null
		}
	},
	computed: {
		selectedVm() {
			return this.vms.find(v => v.name === this.selectedVmName) || null
		},
		vmDropdownOptions() {
			return this.vms.map(v => {
				const memoryStr = v.memory_mib >= 1024
					? `${(v.memory_mib / 1024).toFixed(v.memory_mib % 1024 ? 1 : 0)} GB`
					: `${v.memory_mib} MB`
				const vcpuStr = `${v.vcpus} vCPU`
				const diskTotalGib = (v.disks || []).reduce((acc, d) => acc + (Number(d.gib) || 0), 0)
				const diskStr = diskTotalGib > 0 ? `${diskTotalGib} GB Disk` : ''
				
				const specsParts = [vcpuStr, memoryStr, diskStr].filter(Boolean)
				const specsStr = specsParts.join(' · ')

				return {
					value: v.name,
					label: v.name,
					icon: this.osIcon(v),
					state: v.state,
					stateLabel: this.stateLabel(v.state),
					specs: specsStr,
					meta: this.stateLabel(v.state),
					raw: v
				}
			})
		}
	},
	watch: {
		initialVm(newVal) {
			if (newVal && newVal !== this.selectedVmName) {
				this.selectedVmName = newVal
				this.loadSnapshots()
			}
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
				if (this.initialVm && this.vms.some(v => v.name === this.initialVm)) {
					this.selectedVmName = this.initialVm
				} else if (this.vms.length && !this.selectedVmName) {
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
		onVmChange() {
			this.loadSnapshots()
		},
		openTakeSnapshotModal() {
			const now = new Date()
			const pad = n => String(n).padStart(2, '0')
			this.newSnapName = `snap-${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}`
			this.newSnapDesc = ''
			this.showTakeModal = true
		},
		async takeSnapshot() {
			if (!this.newSnapName.trim()) return
			this.takingSnapshot = true
			try {
				await vmSidecar.createSnapshot(this.selectedVmName, {
					name: this.newSnapName.trim(),
					description: this.newSnapDesc.trim()
				})
				this.$buefy.toast.open({
					message: this.$t('Snapshot "{name}" created successfully', { name: this.newSnapName }),
					type: 'is-success',
					position: 'is-top'
				})
				this.showTakeModal = false
				await this.loadSnapshots()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to create snapshot'),
					type: 'is-danger'
				})
			} finally {
				this.takingSnapshot = false
			}
		},
		async executeRevert() {
			if (!this.targetRevertSnap) return
			const snap = this.targetRevertSnap
			this.revertingName = snap.name
			try {
				await vmSidecar.revertSnapshot(this.selectedVmName, snap.name)
				this.$buefy.toast.open({
					message: this.$t('Successfully reverted "{vm}" to "{snap}"', {
						vm: this.selectedVmName,
						snap: snap.name
					}),
					type: 'is-success'
				})
				this.targetRevertSnap = null
				await this.loadVMs()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to revert to snapshot'),
					type: 'is-danger'
				})
			} finally {
				this.revertingName = null
			}
		},
		async executeDelete() {
			if (!this.targetDeleteSnap) return
			const snap = this.targetDeleteSnap
			this.deletingName = snap.name
			try {
				await vmSidecar.deleteSnapshot(this.selectedVmName, snap.name, true)
				this.$buefy.toast.open({
					message: this.$t('Snapshot deleted'),
					type: 'is-success'
				})
				this.targetDeleteSnap = null
				await this.loadSnapshots()
			} catch (err) {
				this.$buefy.toast.open({
					message: err.message || this.$t('Failed to delete snapshot'),
					type: 'is-danger'
				})
			} finally {
				this.deletingName = null
			}
		},
		stateLabel(state) {
			const map = {
				running: this.$t('Running'),
				paused: this.$t('Paused'),
				shutoff: this.$t('Shut Off'),
				crashed: this.$t('Crashed'),
				pmsuspended: this.$t('Suspended')
			}
			return map[state] || state
		},
		osIcon(vm) {
			const name = (vm.name || '').toLowerCase()
			const os = (vm.os_type || '').toLowerCase()
			if (name.includes('win') || os.includes('win')) return 'microsoft-windows'
			if (name.includes('ubuntu') || os.includes('ubuntu')) return 'ubuntu'
			if (name.includes('debian') || os.includes('debian')) return 'debian'
			if (name.includes('fedora') || os.includes('fedora')) return 'fedora'
			if (name.includes('centos') || name.includes('rhel')) return 'redhat'
			if (name.includes('arch')) return 'arch'
			if (name.includes('mint')) return 'linux-mint'
			return 'linux'
		},
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		getDiskTotal(vm) {
			if (!vm || !vm.disks || !vm.disks.length) return ''
			const total = vm.disks.reduce((acc, d) => acc + (Number(d.gib) || 0), 0)
			return total ? `${total} GB` : ''
		},
		networkLabel(vm) {
			if (!vm) return this.$t('None')
			if (vm.networks && vm.networks.length > 0) {
				const n = vm.networks[0]
				return n.mode === 'bridge' ? (n.bridge_name || 'Bridge') : this.$t('NAT')
			}
			if (!vm.network_mode) return this.$t('None')
			return vm.network_mode.startsWith('bridge:') ? vm.network_mode.replace('bridge:', '') : this.$t('NAT')
		},
		formatCreationTime(isoStr) {
			if (!isoStr) return ''
			const d = new Date(isoStr)
			if (isNaN(d.getTime())) return isoStr
			return d.toLocaleString([], {
				month: 'short',
				day: 'numeric',
				year: 'numeric',
				hour: '2-digit',
				minute: '2-digit'
			})
		},
		formatTimeAgo(isoStr) {
			if (!isoStr) return ''
			const d = new Date(isoStr)
			if (isNaN(d.getTime())) return ''
			const now = new Date()
			const diffSec = Math.floor((now - d) / 1000)
			if (diffSec < 30) return this.$t('Just now')
			if (diffSec < 60) return `${diffSec}s ago`
			if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`
			if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`
			const diffDays = Math.floor(diffSec / 86400)
			return `${diffDays}d ago`
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-snapshots {
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

.toolbar-left,
.toolbar-right {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	row-gap: var(--space-2);
	gap: var(--space-2);
}

.vm-section-title {
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	margin: 0;
	letter-spacing: -0.01em;
}

.vm-selector-dropdown {
	min-width: min(14rem, 100%);
}

.refresh-icon-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	background: transparent;
	border: 1px solid var(--theme-card-border, #e2e8f0);
	color: var(--theme-text-muted, #64748b);
	width: 2rem;
	height: 2rem;
	border-radius: var(--radius-sm);
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #0f172a);
		border-color: var(--theme-text-muted, #cbd5e1);
	}
}

.safety-net-badge {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-xs);
	font-weight: 600;
	padding: var(--space-1) var(--space-3);
	border-radius: var(--radius-pill);

	&.is-active {
		background: rgba(16, 185, 129, 0.1);
		color: #059669;
		border: 1px solid #a7f3d0;
	}

	&.is-empty {
		background: var(--theme-warning-soft, #fffbeb);
		color: #d97706;
		border: 1px solid #fde68a;
	}
}

.vm-spec-banner {
	display: flex;
	align-items: center;
	justify-content: space-between;
	flex-wrap: wrap;
	row-gap: var(--space-2);
	gap: var(--space-4);
	padding: var(--space-3) var(--space-4);
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.07));
	border-radius: var(--radius-card);
	margin-bottom: var(--space-5);
	box-shadow: var(--shadow-sm);
}

.spec-banner-left {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	min-width: 0;
}

.spec-banner-icon {
	width: 2.25rem;
	height: 2.25rem;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);
	flex-shrink: 0;

	&.is-running {
		background: rgba(16, 185, 129, 0.1);
		color: #059669;
	}
	&.is-paused {
		background: var(--theme-warning-soft, #fffbeb);
		color: #d97706;
	}
	&.is-crashed {
		background: var(--color-danger-soft, #fef2f2);
		color: #dc2626;
	}
}

.spec-banner-info {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.spec-banner-title-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.spec-banner-name {
	font-size: var(--font-base);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
}

.spec-banner-state {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-2xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	.state-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		background: var(--theme-text-muted, #94a3b8);
	}

	&.is-running {
		background: rgba(16, 185, 129, 0.1);
		color: #059669;
		.state-dot {
			background: #10b981;
			box-shadow: 0 0 4px rgba(16, 185, 129, 0.4);
		}
	}
	&.is-paused {
		background: var(--theme-warning-soft, #fffbeb);
		color: #d97706;
		.state-dot { background: #f59e0b; }
	}
	&.is-crashed {
		background: var(--color-danger-soft, #fef2f2);
		color: #dc2626;
		.state-dot { background: #ef4444; }
	}
}

.spec-banner-specs {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
	flex-wrap: wrap;
}

.spec-item {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	color: var(--theme-text-muted, #64748b);
}

.spec-sep {
	opacity: 0.5;
}

.spec-banner-right {
	flex-shrink: 0;
}

.spec-banner-count {
	display: flex;
	flex-direction: column;
	align-items: flex-end;
	text-align: right;
}

.count-num {
	font-size: var(--font-md);
	font-weight: 700;
	color: var(--theme-text-primary, #0f172a);
	line-height: 1;
}

.count-text {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	font-weight: 500;
	margin-top: var(--space-1);
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

	&:hover:not(:disabled) {
		background: #1d4ed8;
		color: #fff;
	}

	&:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
}

.create-btn-large {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 600;
	height: 2.15rem;
	padding: 0 var(--space-4);
	border-radius: var(--radius-sm);
	cursor: pointer;
	margin-top: var(--space-4);
	transition: background 0.15s ease;

	&:hover {
		background: #1d4ed8;
		color: #fff;
	}
}

.vm-loading {
	display: flex;
	justify-content: center;
	padding: var(--space-12) 0;
	color: var(--theme-text-muted, #94a3b8);

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
	}
}

.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--space-14) var(--space-6);
	text-align: center;
	background: var(--theme-card-bg, #ffffff);
	border: 1px dashed var(--theme-card-border, #cbd5e1);
	border-radius: var(--radius-card);
	color: var(--theme-text-muted, #64748b);
}

.vm-empty-title {
	font-size: var(--font-md);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
	margin: var(--space-3) 0 var(--space-1);
}

.vm-empty-hint {
	font-size: var(--font-sm);
	color: var(--theme-text-muted, #64748b);
	max-width: 26rem;
	line-height: 1.45;
	margin: 0;
}

.snapshot-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.snapshot-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.07));
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #ffffff);
	box-shadow: var(--shadow-sm);
	transition: all 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.25);
		box-shadow: var(--shadow-lg);
	}

	&.is-current {
		border-color: rgba(37, 99, 235, 0.35);
		background: var(--theme-card-subtle, #f8fafc);
	}
}

.snapshot-icon {
	flex-shrink: 0;
	width: 2.25rem;
	height: 2.25rem;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	&.is-current {
		background: rgba(59, 130, 246, 0.1);
		color: #2563eb;
	}
}

.snapshot-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.snapshot-head {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	flex-wrap: wrap;
}

.snapshot-name {
	font-size: var(--font-base);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
}

.current-badge {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-2xs);
	font-weight: 700;
	color: #2563eb;
	background: rgba(59, 130, 246, 0.1);
	border: 1px solid #bfdbfe;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	text-transform: uppercase;
	letter-spacing: 0.02em;
}

.state-pill {
	font-size: var(--font-2xs);
	font-weight: 600;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-xs);
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);
	text-transform: capitalize;

	&.is-running {
		background: rgba(16, 185, 129, 0.1);
		color: #059669;
	}

	&.is-shutoff {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-muted, #64748b);
	}
}

.snapshot-desc {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	margin: 0;
	line-height: 1.35;
}

.snapshot-meta {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #94a3b8);
}

.meta-sep {
	opacity: 0.6;
}

.snapshot-actions {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin-left: auto;
}

.revert-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, #cbd5e1);
	color: var(--theme-text-secondary, #334155);
	font-size: var(--font-xs);
	font-weight: 600;
	height: 1.85rem;
	padding: 0 var(--space-3);
	border-radius: var(--radius-sm);
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(59, 130, 246, 0.1);
		color: #2563eb;
		border-color: #93c5fd;
	}

	&:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
}

.delete-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	background: transparent;
	border: 1px solid transparent;
	color: var(--theme-text-muted, #94a3b8);
	width: 1.85rem;
	height: 1.85rem;
	border-radius: var(--radius-sm);
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		color: #ef4444;
		background: var(--color-danger-soft, #fee2e2);
	}

	&:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
}

.dialog-body {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.dialog-hint {
	font-size: var(--font-base);
	color: var(--theme-text-secondary, #475569);
	margin: 0;
}

.warning-banner {
	display: flex;
	align-items: flex-start;
	background: var(--theme-warning-soft, #fffbeb);
	border: 1px solid #fde68a;
	color: var(--color-warning);
	padding: var(--space-3);
	border-radius: var(--radius-control);
	font-size: var(--font-sm);
	line-height: 1.4;
}
</style>
