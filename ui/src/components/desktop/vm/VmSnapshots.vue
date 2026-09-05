<!-- src/components/desktop/vm/VmSnapshots.vue -->
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
					<span>{{ snapshots.length ? $t('Safety Net Active') : $t('No Safety Net') }}</span>
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
	padding: 1.5rem;
}

.vm-section-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}

.toolbar-left,
.toolbar-right {
	display: flex;
	align-items: center;
}

.vm-section-title {
	font-size: 1.15rem;
	font-weight: 600;
	color: #0f172a;
	margin: 0;
	letter-spacing: -0.01em;
}

.vm-selector-dropdown {
	min-width: 12rem;
}

.refresh-icon-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	background: transparent;
	border: 1px solid #e2e8f0;
	color: #64748b;
	width: 2rem;
	height: 2rem;
	border-radius: 8px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		background: #f1f5f9;
		color: #0f172a;
		border-color: #cbd5e1;
	}
}

.safety-net-badge {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.8rem;
	font-weight: 600;
	padding: 0.3rem 0.65rem;
	border-radius: 9999px;
	margin-right: 0.85rem;

	&.is-active {
		background: #ecfdf5;
		color: #059669;
		border: 1px solid #a7f3d0;
	}

	&.is-empty {
		background: #fffbeb;
		color: #d97706;
		border: 1px solid #fde68a;
	}
}

.create-btn {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: 0.85rem;
	font-weight: 500;
	padding: 0.5rem 0.95rem;
	border-radius: 8px;
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: #1d4ed8;
		color: #fff;
	}
}

.create-btn-large {
	display: inline-flex;
	align-items: center;
	gap: 0.45rem;
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: 0.9rem;
	font-weight: 600;
	padding: 0.65rem 1.25rem;
	border-radius: 8px;
	cursor: pointer;
	margin-top: 1rem;
	transition: background 0.15s ease;

	&:hover {
		background: #1d4ed8;
		color: #fff;
	}
}

.vm-loading {
	display: flex;
	justify-content: center;
	padding: 3rem 0;
	color: #94a3b8;

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
	padding: 4rem 1.5rem;
	text-align: center;
	background: #ffffff;
	border: 1px dashed #cbd5e1;
	border-radius: 12px;
	color: #64748b;
}

.vm-empty-title {
	font-size: 1rem;
	font-weight: 600;
	color: #1e293b;
	margin: 0.75rem 0 0.25rem;
}

.vm-empty-hint {
	font-size: 0.8rem;
	color: #64748b;
	max-width: 26rem;
	line-height: 1.4;
	margin: 0;
}

.snapshot-list {
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
}

.snapshot-row {
	display: flex;
	align-items: center;
	gap: 1rem;
	padding: 0.95rem 1.25rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 12px;
	background: #ffffff;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.02);
	transition: all 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.3);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
	}

	&.is-current {
		border-color: rgba(37, 99, 235, 0.4);
		background: #f8fafc;
	}
}

.snapshot-icon {
	flex-shrink: 0;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 10px;
	display: flex;
	align-items: center;
	justify-content: center;
	background: #f1f5f9;
	color: #64748b;

	&.is-current {
		background: #eff6ff;
		color: #2563eb;
	}
}

.snapshot-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
}

.snapshot-head {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-wrap: wrap;
}

.snapshot-name {
	font-size: 0.95rem;
	font-weight: 600;
	color: #0f172a;
}

.current-badge {
	display: inline-flex;
	align-items: center;
	gap: 0.25rem;
	font-size: 0.7rem;
	font-weight: 700;
	color: #2563eb;
	background: #eff6ff;
	border: 1px solid #bfdbfe;
	padding: 0.1rem 0.5rem;
	border-radius: 9999px;
	text-transform: uppercase;
	letter-spacing: 0.02em;
}

.state-pill {
	font-size: 0.7rem;
	font-weight: 600;
	padding: 0.1rem 0.45rem;
	border-radius: 6px;
	background: #f1f5f9;
	color: #64748b;
	text-transform: capitalize;

	&.is-running {
		background: #ecfdf5;
		color: #059669;
	}

	&.is-shutoff {
		background: #f1f5f9;
		color: #64748b;
	}
}

.snapshot-desc {
	font-size: 0.8rem;
	color: #475569;
	margin: 0;
	line-height: 1.35;
}

.snapshot-meta {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.75rem;
	color: #94a3b8;
}

.meta-sep {
	opacity: 0.6;
}

.snapshot-actions {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin-left: auto;
}

.revert-btn {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	background: #ffffff;
	border: 1px solid #cbd5e1;
	color: #334155;
	font-size: 0.8rem;
	font-weight: 600;
	padding: 0.4rem 0.8rem;
	border-radius: 8px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		background: #2563eb;
		color: #ffffff;
		border-color: #2563eb;
	}

	&:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
}

.delete-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	background: transparent;
	border: 1px solid transparent;
	color: #94a3b8;
	padding: 0.4rem;
	border-radius: 8px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		color: #ef4444;
		background: #fee2e2;
		border-color: #fecaca;
	}

	&:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
}

.dialog-body {
	display: flex;
	flex-direction: column;
	gap: 0.85rem;
}

.dialog-hint {
	font-size: 0.85rem;
	color: #475569;
	margin: 0;
}

.warning-banner {
	display: flex;
	align-items: flex-start;
	background: #fffbeb;
	border: 1px solid #fde68a;
	color: #b45309;
	padding: 0.75rem;
	border-radius: 8px;
	font-size: 0.8rem;
	line-height: 1.4;
}
</style>
