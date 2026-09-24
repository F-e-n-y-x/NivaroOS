<template>
	<div class="disks-panel">
		<!-- Available Disks -->
		<h3 class="setting-card-title">{{ $t('Available Disks') }}</h3>
		<div class="setting-card">
			<div v-for="d in avail" :key="d.path">
				<div class="setting-row">
					<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ diskName(d) }}</div>
						<div class="setting-desc">{{ diskSummary(d) }}</div>
						<div class="setting-desc">{{ filesystemText(d) }}</div>
					</div>
					<div class="row-control">
						<button class="icon-button mr-2" type="button" :title="$t('Drive info')" :aria-label="$t('Drive info')"
							:aria-expanded="String(expandedPath === d.path)" @click="toggleDetails(d)">
							<b-icon icon="information-outline" pack="casa" size="is-16"></b-icon>
						</button>
						<b-button v-if="formatPath !== d.path" rounded size="is-small" type="is-primary"
							:disabled="!!job" @click="openFormat(d)">
							{{ $t('Use for storage') }}
						</b-button>
					</div>
				</div>

				<!-- Format confirmation lives inline, in the disk's own row:
				     every identifying detail is right there, and the button
				     only unlocks after the disk's model or device name is
				     typed - one stray click can no longer wipe a drive. -->
				<div v-if="formatPath === d.path" class="format-confirm" role="group" :aria-label="$t('Format {name}', { name: diskName(d) })"
					@keydown.esc.stop="cancelFormat">
					<template v-if="!job">
						<p class="format-warning">
							<b-icon icon="alert" pack="mdi" size="is-small"></b-icon>
							<span>{{ $t('Everything on this disk will be erased. It will be repartitioned, formatted and mounted as NivaroOS storage.') }}</span>
						</p>
						<dl class="format-facts">
							<dt>{{ $t('Model') }}</dt><dd>{{ d.model || $t('Unknown') }}</dd>
							<dt>{{ $t('Serial number') }}</dt><dd>{{ d.serial || $t('Unknown') }}</dd>
							<dt>{{ $t('Device') }}</dt><dd>{{ d.path }}</dd>
							<dt>{{ $t('Capacity') }}</dt><dd>{{ formatSize(d.size) }} &middot; {{ d.disk_type }}</dd>
							<dt>{{ $t('Current filesystem') }}</dt><dd>{{ filesystemText(d) }}</dd>
						</dl>
						<label class="format-label" :for="'format-input-' + d.name">
							{{ $t('To confirm, type {target}', { target: confirmTarget(d) }) }}
						</label>
						<div class="format-actions">
							<input :id="'format-input-' + d.name" ref="formatInput" v-model="formatTyped" class="input is-small format-input"
								type="text" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="confirmTarget(d)"
								@keydown.enter.prevent="startFormat(d)">
							<b-button rounded size="is-small" @click="cancelFormat">{{ $t('Cancel') }}</b-button>
							<b-button rounded size="is-small" type="is-danger" :disabled="!typedMatches(d)" @click="startFormat(d)">
								{{ $t('Erase & use') }}
							</b-button>
						</div>
					</template>
					<template v-else>
						<div class="format-progress" role="status" aria-live="polite">
							<div class="format-progress-text">
								<span>{{ stepText(job.step) || $t('Preparing {name}...', { name: diskName(d) }) }}</span>
								<span v-if="job.progress !== null">{{ job.progress }}%</span>
							</div>
							<progress class="progress is-small is-primary" :value="job.progress !== null ? job.progress : undefined" max="100"
								:aria-label="$t('Format progress')"></progress>
							<p v-if="job.reconnecting" class="hint">{{ $t('Reconnecting to the server...') }}</p>
						</div>
					</template>
				</div>
				<p v-if="formatError && formatError.path === d.path" class="error-note" role="alert">{{ formatError.message }}</p>
				<drive-details-panel v-if="expandedPath === d.path" :disk="d" @close="expandedPath = ''"></drive-details-panel>
			</div>
			<p v-if="formatError && !avail.some(a => a.path === formatError.path)" class="error-note" role="alert">{{ formatError.message }}</p>
			<div v-if="!avail.length" class="account-empty">{{ $t('No unformatted disks detected.') }}</div>
		</div>

		<!-- Mounted Disks -->
		<h3 class="setting-card-title">{{ $t('Mounted Disks') }}</h3>
		<div class="setting-card">
			<div v-for="d in mountedDisks" :key="d.path">
				<div class="setting-row">
					<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ diskName(d) }}</div>
						<div class="setting-desc">{{ diskSummary(d) }} &middot; <span :class="healthClass(d)">{{ healthText(d) }}</span></div>
						<div v-if="mountSummary(d)" class="setting-desc">{{ mountSummary(d) }}</div>
						<div v-if="poolOf(d)" class="setting-desc pool-note">{{ $t('Member of the {pool} storage pool', { pool: poolOf(d).mount_point }) }}</div>
					</div>
					<div class="row-control">
						<button class="icon-button mr-2" type="button" :title="$t('Drive info')" :aria-label="$t('Drive info')"
							:aria-expanded="String(expandedPath === d.path)" @click="toggleDetails(d)">
							<b-icon icon="information-outline" pack="casa" size="is-16"></b-icon>
						</button>
						<!-- The disk the system runs from is never offered for removal. -->
						<span v-if="d.model === 'System'" class="setting-chip">{{ $t('System disk') }}</span>
						<b-button v-else rounded size="is-small" type="is-danger" outlined :loading="busyPath === d.path" @click="confirmRemove(d)">
							{{ $t('Remove') }}
						</b-button>
					</div>
				</div>
				<drive-details-panel v-if="expandedPath === d.path" :disk="d" @close="expandedPath = ''"></drive-details-panel>
			</div>
			<div v-if="!mountedDisks.length" class="account-empty">{{ $t('No mounted disks.') }}</div>
		</div>

		<!-- USB Drives -->
		<h3 class="setting-card-title">{{ $t('USB Drives & Automount') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="usb-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Automount USB Drive') }}</div>
					<div class="setting-desc">{{ $t('Automatically mount external USB drives when connected') }}</div>
				</div>
				<div class="row-control">
					<b-switch v-model="autoUsbMount" class="is-flex-direction-row-reverse mr-0" type="is-primary" :aria-label="$t('Automount USB Drive')" @input="toggleAutoMount"></b-switch>
				</div>
			</div>
			<div v-for="u in usb" :key="u.name" class="setting-row">
				<b-icon class="row-icon" icon="usb-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ u.model || u.name }}</div>
					<div class="setting-desc">{{ u.name }} &middot; {{ formatSize(u.size) }}</div>
				</div>
				<div class="row-control">
					<b-button v-for="c in u.children" :key="c.mount_point" rounded size="is-small" type="is-danger" outlined class="ml-2" @click="confirmEject(c)">
						{{ $t('Eject {mount}', { mount: c.mount_point }) }}
					</b-button>
				</div>
			</div>
			<div v-if="!usb.length" class="account-empty">{{ $t('No USB drives connected.') }}</div>
		</div>
		<p v-if="error" class="error-note" role="alert">{{ error }}</p>
		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import { escapeHtml } from '@/utils/escapeHtml'
import DriveDetailsPanel from '@/apps/settings/DriveDetailsPanel.vue'
import { formatSize } from '@/utils/formatSize'
import { confirmWindowMixin } from '@/mixins/confirmWindow'
import events from '@/events/events'
import activityService from '@/service/activity'
import { createStorage, STORAGE_JOB_EVENTS, jobIdFromEvent, kickJob } from '@/apps/storage/storageJobs'

// Nudge the job poller as soon as the backend publishes a job event,
// instead of waiting for the next poll tick.
const jobSockets = {}
STORAGE_JOB_EVENTS.forEach(name => {
	jobSockets[name] = function (res) {
		kickJob(jobIdFromEvent(res) || (this.job && this.job.id))
	}
})

export default {
	name: 'disks-panel',
	components: { DriveDetailsPanel },
	mixins: [confirmWindowMixin],
	data() {
		return {
			disks: [],
			avail: [],
			usb: [],
			storages: [],
			merges: [],
			autoUsbMount: false,
			busyPath: '',
			error: '',
			expandedPath: '',
			formatPath: '',
			formatTyped: '',
			formatError: null,
			// { id, path, progress, step, reconnecting } while a format runs
			job: null
		}
	},
	computed: {
		mountedDisks() {
			return this.disks.filter(d => !this.avail.some(a => a.path === d.path))
		}
	},
	created() {
		this.refreshAll()
		this.$EventBus.$on(events.STORAGE_CHANGED, this.refreshAll)
	},
	beforeDestroy() {
		this.isClosed = true
		clearTimeout(this.hotplugTimer)
		this.$EventBus.$off(events.STORAGE_CHANGED, this.refreshAll)
	},
	methods: {
		formatSize,
		refreshAll() {
			this.refresh()
			this.refreshUsb()
			this.refreshStorages()
			this.refreshMerges()
			this.getAutoMountStatus()
		},
		diskName(d) {
			return d.model && d.model !== 'System' ? d.model : (d.model === 'System' ? this.$t('System disk') : d.path)
		},
		diskSummary(d) {
			const parts = [d.path, formatSize(d.size), d.disk_type]
			if (d.serial) parts.push(this.$t('S/N {serial}', { serial: d.serial }))
			return parts.filter(Boolean).join(' · ')
		},
		// The "available" list only holds disks with no filesystem, but a
		// newer backend can report one (fstype/label) - show whatever it says.
		filesystemText(d) {
			const fs = d.fstype || d.fs_type
			if (fs) return d.label ? `${fs} · "${d.label}"` : fs
			if (d.children_number > 0) return this.$t('{count} partition(s), no filesystem', { count: d.children_number })
			return this.$t('No filesystem (blank disk)')
		},
		// health: 'true' | 'false' | 'unknown' (never read while awake);
		// `sleeping` means the drive is spun down right now.
		healthText(d) {
			if (d.sleeping) return this.$t('Sleeping')
			if (d.health === 'true') return this.$t('Healthy')
			if (d.health === 'false') return this.$t('Check disk')
			return this.$t('Health unknown')
		},
		healthClass(d) {
			return !d.sleeping && d.health === 'false' ? 'health-bad' : ''
		},
		stepText(step) {
			if (!step) return ''
			const s = String(step)
			const known = {
				queued: 'Waiting to start...',
				unmounting: 'Unmounting...',
				'deleting partitions': 'Removing old partitions...',
				'creating partition and filesystem': 'Creating partition and filesystem...',
				formatting: 'Formatting...',
				'waiting for the new filesystem': 'Waiting for the new filesystem...',
				mounting: 'Mounting...',
				done: 'Done'
			}
			if (known[s]) return this.$t(known[s])
			if (s.startsWith('mounting ')) return this.$t('Mounting {device}...', { device: s.slice(9) })
			return s
		},
		deviceName(d) {
			return (d.path || '').split('/').pop()
		},
		confirmTarget(d) {
			return (d.model && d.model !== 'System' ? d.model : this.deviceName(d)).trim()
		},
		typedMatches(d) {
			const typed = this.formatTyped.trim().toLowerCase()
			if (!typed) return false
			return [d.model, this.deviceName(d), d.path].filter(Boolean).some(v => v.trim().toLowerCase() === typed)
		},
		storageOf(d) {
			return this.storages.find(s => s.path === d.path)
		},
		mountSummary(d) {
			const s = this.storageOf(d)
			if (!s || !Array.isArray(s.children)) return ''
			return s.children
				.filter(c => c.mount_point)
				.map(c => (c.label && c.label !== c.mount_point.split('/').pop() ? `${c.mount_point} ("${c.label}", ${c.type || '?'})` : `${c.mount_point} (${c.type || '?'})`))
				.join(', ')
		},
		// The merge (pool) this disk contributes a volume to, if any.
		poolOf(d) {
			const s = this.storageOf(d)
			if (!s || !Array.isArray(s.children)) return null
			const uuids = s.children.map(c => c.uuid).filter(Boolean)
			const mounts = s.children.map(c => c.mount_point).filter(Boolean)
			return this.merges.find(m => {
				const srcs = Array.isArray(m.source_volume_uuids) ? m.source_volume_uuids : []
				return srcs.some(u => uuids.includes(u)) || mounts.includes(m.mount_point)
			}) || null
		},
		toggleDetails(disk) {
			this.expandedPath = this.expandedPath === disk.path ? '' : disk.path
		},
		apiMessage(e, fallback) {
			return (e && e.response && e.response.data && e.response.data.message) || (e && e.message) || fallback
		},
		refresh() {
			return this.$api.disks.getDiskList().then(res => {
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
		// Mount points / labels / volume UUIDs per disk (for display and
		// the storage-pool check). Optional - the list still works without.
		refreshStorages() {
			this.$api.storage.list({ system: 'show' }).then(res => {
				if (res.data.success === 200) this.storages = res.data.data || []
			}).catch(() => {})
		},
		refreshMerges() {
			this.$api.local_storage.getMergerfsInfo().then(res => {
				this.merges = (res.data && res.data.data) || []
			}).catch(() => {
				this.merges = []
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
				this.autoUsbMount = !previousState
				this.error = this.$t('Failed to toggle automount')
			})
		},
		openFormat(disk) {
			if (this.job) return
			this.formatPath = disk.path
			this.formatTyped = ''
			this.error = ''
			this.formatError = null
			this.$nextTick(() => {
				const el = this.$refs.formatInput
				const input = Array.isArray(el) ? el[0] : el
				if (input) input.focus()
			})
		},
		cancelFormat() {
			if (this.job) return
			this.formatPath = ''
			this.formatTyped = ''
		},
		async startFormat(disk) {
			if (this.job || !this.typedMatches(disk)) return
			const name = this.diskName(disk)
			this.error = ''
			this.formatError = null
			this.job = { id: '', path: disk.path, progress: null, step: '', reconnecting: false }
			try {
				const result = await createStorage({ path: disk.path, name: '', format: true }, {
					onProgress: p => {
						if (!this.job) return
						if (p.jobId) this.job.id = p.jobId
						this.job.reconnecting = !!p.reconnecting
						if (p.reconnecting) return
						if (p.progress !== null && p.progress !== undefined) this.job.progress = Math.max(0, Math.min(100, Math.round(p.progress)))
						if (p.step) this.job.step = String(p.step)
					}
				})
				const mounted = String((result && result.mountPoint) || '').split(',').map(m => m.trim()).filter(Boolean).join(', ')
				activityService.add({
					title: this.$t('Disk ready for storage'),
					message: mounted ? `${name} (${disk.path}) - ${mounted}` : `${name} (${disk.path})`,
					type: 'storage',
					status: 'success'
				})
				if (!this.isClosed) {
					this.$buefy.toast.open({
						message: mounted
							? this.$t('{name} is formatted and mounted at {path}', { name: escapeHtml(name), path: escapeHtml(mounted) })
							: this.$t('{name} is formatted and ready to use', { name: escapeHtml(name) }),
						type: 'is-success'
					})
				}
			} catch (e) {
				const msg = this.apiMessage(e, this.$t('Failed to add storage'))
				activityService.add({
					title: this.$t('Formatting failed'),
					message: `${name} (${disk.path}): ${msg}`,
					type: 'storage',
					status: 'error'
				})
				if (!this.isClosed) this.formatError = { path: disk.path, message: msg }
			} finally {
				this.job = null
				this.formatPath = ''
				this.formatTyped = ''
				// Every storage panel (Persistent Mounts, pools, this one) and
				// the Files sidebar re-read their lists.
				this.$EventBus.$emit(events.STORAGE_CHANGED)
			}
		},
		fail(e, fallback) {
			this.$buefy.toast.open({ message: escapeHtml(this.apiMessage(e, fallback)), type: 'is-danger', duration: 6000 })
		},
		confirmRemove(disk) {
			const pool = this.poolOf(disk)
			const esc = escapeHtml
			if (pool && pool.mount_point === '/DATA') {
				this.confirmWindow({
					title: this.$t('Disk is part of /DATA'),
					message: this.$t('<b>{name}</b> ({path}) is a member of the <b>/DATA</b> storage pool. Removing it would make the files and apps stored on it disappear from /DATA. Take it out of the pool first.',
						{ name: esc(this.diskName(disk)), path: esc(disk.path) }),
					type: 'is-warning',
					confirmText: this.$t('OK'),
					cancelText: this.$t('Close')
				})
				return
			}
			const mounts = this.mountSummary(disk)
			let message = this.$t('Unmount and stop using <b>{name}</b> ({path}) for storage?', { name: esc(this.diskName(disk)), path: esc(disk.path) })
			if (mounts) message += '<br><br>' + this.$t('Mounted at: {mounts}', { mounts: esc(mounts) })
			if (pool) message += '<br><br><b>' + this.$t('This disk is part of the {pool} storage pool - files on it will disappear from the pool.', { pool: esc(pool.mount_point) }) + '</b>'
			this.confirmWindow({
				title: this.$t('Remove disk'),
				message,
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				height: pool || mounts ? 280 : 210,
				onConfirm: () => {
					this.busyPath = disk.path
					this.$api.disks.umount({ path: disk.path })
						.then(res => {
							if (res.data && res.data.success !== undefined && res.data.success !== 200) this.fail({ message: res.data.message }, this.$t('Could not remove the disk'))
						})
						.catch(e => this.fail(e, this.$t('Could not remove the disk')))
						.finally(() => {
							this.busyPath = ''
							this.$EventBus.$emit(events.STORAGE_CHANGED)
						})
				}
			})
		},
		confirmEject(child) {
			this.confirmWindow({
				title: this.$t('Eject USB drive'),
				message: this.$t('Safely eject {mount}?', { mount: escapeHtml(child.mount_point) }),
				type: 'is-danger',
				confirmText: this.$t('Eject'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.disks.umountUsb({ mount_point: child.mount_point })
						.then(() => this.$buefy.toast.open({ message: this.$t('Ejected - you can unplug the drive'), type: 'is-success' }))
						.catch(e => this.fail(e, this.$t('Could not eject the drive')))
						.finally(() => this.refreshUsb())
				}
			})
		},
		// Plug/unplug: refresh once the burst of udev events settles
		// (skipped while our own format is repartitioning a disk).
		onHotplug() {
			if (this.job) return
			clearTimeout(this.hotplugTimer)
			this.hotplugTimer = setTimeout(() => {
				if (!this.isClosed) this.refreshAll()
			}, 1500)
		}
	},
	sockets: {
		...jobSockets,
		'local-storage:disk:added'() {
			this.onHotplug()
		},
		'local-storage:disk:removed'() {
			this.onHotplug()
		}
	}
}
</script>

<style lang="scss" scoped>
.disks-panel {
	position: relative;
}

.error-note {
	padding: 0 var(--space-5) var(--space-3);
	color: var(--color-danger-fg);
	font-size: var(--font-xs);
}

.health-bad {
	color: var(--color-danger-fg);
}

.pool-note {
	color: var(--color-warning-fg);
}

.icon-button {
	flex-shrink: 0;
	border: none;
	background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	width: 1.7rem;
	height: 1.7rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: var(--theme-text-muted);

	&:hover {
		color: var(--theme-text-primary);
	}

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: 2px;
	}
}

.format-confirm {
	margin: var(--space-2) var(--space-4) var(--space-3);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--color-danger);
	border-radius: var(--radius-card);
	background: var(--color-danger-soft, var(--theme-danger-soft));
	color: var(--theme-text-primary);
	font-size: var(--font-sm);
}

.format-warning {
	display: flex;
	gap: var(--space-2);
	align-items: flex-start;
	font-weight: 600;
	color: var(--color-danger-fg);
	margin-bottom: var(--space-2);
}

.format-facts {
	display: grid;
	grid-template-columns: max-content 1fr;
	gap: var(--space-1) var(--space-3);
	margin-bottom: var(--space-3);

	dt {
		color: var(--theme-text-secondary);
	}

	dd {
		margin: 0;
		word-break: break-all;
		font-weight: 500;
	}
}

.format-label {
	display: block;
	margin-bottom: var(--space-1);
	color: var(--theme-text-secondary);
}

.format-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.format-input {
	flex: 1 1 12rem;
	min-width: 0;
	background: var(--theme-input-bg);
	border-color: var(--theme-input-border);
	color: var(--theme-input-text);

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: 1px;
	}
}

.format-progress-text {
	display: flex;
	justify-content: space-between;
	gap: var(--space-2);
	margin-bottom: var(--space-1);
}

.format-progress .progress {
	margin-bottom: var(--space-1);
}

.hint {
	color: var(--theme-text-secondary);
	font-size: var(--font-xs);
}
</style>
