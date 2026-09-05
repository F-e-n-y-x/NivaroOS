<!-- src/components/desktop/vm/VmList.vue -->
<template>
	<div class="vm-list">
		<div class="vm-list-toolbar">
			<h2 class="vm-list-title">{{ $t('Virtual Machines') }}</h2>
			<button v-if="loading || vms.length" class="create-btn" @click="openCreate">
				<b-icon icon="plus" custom-size="mdi-18px"></b-icon>
				<span>{{ $t('Create VM') }}</span>
			</button>
		</div>

		<div v-if="loading" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<div v-else-if="!vms.length" class="vm-empty">
			<b-icon icon="monitor-off" custom-size="mdi-48px"></b-icon>
			<p class="vm-empty-title">{{ $t('No VMs yet') }}</p>
			<p class="vm-empty-hint">{{ $t('Create your first virtual machine to get started.') }}</p>
			<button class="create-btn-large" @click="openCreate">
				<b-icon icon="plus" custom-size="mdi-18px"></b-icon>
				<span>{{ $t('Create VM') }}</span>
			</button>
		</div>

		<div v-else class="vm-grid">
			<div v-for="vm in vms" :key="vm.name" class="vm-card">
				<div class="vm-preview" :class="{ 'is-off': vm.state !== 'running' }" @dblclick="openConsole(vm.name)">
					<img v-if="vm.state === 'running'" :src="previewUrl(vm)" class="vm-preview-img" alt="" @error="onPreviewError(vm)" />
					<div v-else class="vm-preview-placeholder">
						<b-icon :icon="osIcon(vm)" custom-size="mdi-40px"></b-icon>
					</div>
					<div v-if="vm.state === 'running'" class="vm-preview-overlay">
						<b-icon icon="fullscreen" custom-size="mdi-24px"></b-icon>
					</div>
				</div>

				<div class="vm-card-body">
					<div class="vm-card-head">
						<span class="vm-name" :title="vm.name">{{ vm.name }}</span>
						<span class="vm-state-badge" :class="'is-' + vm.state">
							<span class="vm-state-dot"></span>{{ stateLabel(vm.state) }}
						</span>
					</div>
					<div class="vm-specs">
						<span class="vm-spec"><b-icon icon="memory" custom-size="mdi-14px"></b-icon>{{ vm.vcpus }} {{ $t('vCPU') }}</span>
						<span class="vm-spec"><b-icon icon="chip" custom-size="mdi-14px"></b-icon>{{ formatMib(vm.memory_mib) }}</span>
						<span class="vm-spec"><b-icon icon="lan" custom-size="mdi-14px"></b-icon>{{ networkLabel(vm) }}</span>
					</div>
				</div>

				<div class="vm-card-actions">
					<div class="vm-action-group">
						<button v-if="vm.state !== 'running'" class="vm-action-btn vm-play-btn" :title="$t('Start')" @click="start(vm.name)">
							<b-icon icon="play" custom-size="mdi-18px"></b-icon>
						</button>
						<template v-else>
							<button class="vm-action-btn" :title="$t('Console')" @click="openConsole(vm.name)">
								<b-icon icon="monitor" custom-size="mdi-18px"></b-icon>
							</button>
							<button class="vm-action-btn" :title="$t('Open in New Tab')" @click="openConsoleTab(vm.name)">
								<b-icon icon="open-in-new" custom-size="mdi-18px"></b-icon>
							</button>
							<button class="vm-action-btn" :title="$t('Reset')" @click="reset(vm.name)">
								<b-icon icon="restart" custom-size="mdi-18px"></b-icon>
							</button>
							<button class="vm-action-btn" :title="$t('Shutdown')" @click="shutdown(vm.name)">
								<b-icon icon="power" custom-size="mdi-18px"></b-icon>
							</button>
							<button class="vm-action-btn" :title="$t('Force off')" @click="forceOff(vm.name)">
								<b-icon icon="power-plug-off-outline" custom-size="mdi-18px"></b-icon>
							</button>
						</template>
					</div>
					<div class="vm-action-group">
						<button
							class="vm-action-btn"
							:title="$t('Snapshots')"
							@click="openSnapshots(vm.name)"
						>
							<b-icon icon="camera-outline" custom-size="mdi-18px"></b-icon>
						</button>
						<button
						class="vm-action-btn"
						:title="vm.state === 'running' ? $t('Stop the VM to edit it') : $t('Edit')"
						:disabled="vm.state === 'running'"
						@click="openEdit(vm)"
					>
							<b-icon icon="pencil-outline" custom-size="mdi-18px"></b-icon>
						</button>
						<button class="vm-action-btn is-danger" :title="$t('Delete')" @click="confirmDelete(vm.name)">
							<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
						</button>
					</div>
				</div>
			</div>
		</div>

		<vm-overlay-panel :active="!!deletingVmName" :title="$t('Delete VM')" width="24rem" @close="deletingVmName = null">
			<p>{{ $t('Delete') }} "{{ deletingVmName }}" {{ $t('and its disk? This cannot be undone.') }}</p>
			<template #footer>
				<b-button @click="deletingVmName = null">{{ $t('Cancel') }}</b-button>
				<b-button type="is-danger" @click="performDelete">{{ $t('Delete') }}</b-button>
			</template>
		</vm-overlay-panel>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmOverlayPanel from './VmOverlayPanel.vue'

const POLL_INTERVAL_MS = 2000
// Screenshots only need to feel "live", not video-smooth - grabbing one
// every tick would just load the sidecar/libvirt for no visible benefit.
const PREVIEW_INTERVAL_MS = 3000

export default {
	name: 'vm-list',
	components: { VmOverlayPanel },
	data() {
		return {
			vms: [],
			loading: true,
			deletingVmName: null,
			timer: null,
			previewTimer: null,
			previewTick: 0,
			previewErrors: {},
		}
	},
	computed: {
		isMinimized() {
			const win = this.$store.state.windows.find((w) => w.id === 'vms')
			return !!(win && win.minimized)
		},
	},
	created() {
		this.poll()
		this.timer = setInterval(this.poll, POLL_INTERVAL_MS)
		this.previewTimer = setInterval(() => {
			if (!this.isMinimized) this.previewTick++
		}, PREVIEW_INTERVAL_MS)
	},
	beforeDestroy() {
		clearInterval(this.timer)
		clearInterval(this.previewTimer)
	},
	methods: {
		async poll() {
			if (this.isMinimized) return
			try {
				this.vms = await vmSidecar.listVMs()
			} catch (e) {
				// Leave the last known list showing rather than clearing it on
				// a transient sidecar error - the next poll tick retries.
			} finally {
				this.loading = false
			}
		},
		previewUrl(vm) {
			// previewTick busts the <img> cache on each interval tick - a
			// screenshot endpoint has no reason to be cached, and the URL
			// must actually change for the browser to re-fetch it at all.
			return `${vmSidecar.screenshotUrl(vm.name)}?t=${this.previewErrors[vm.name] ? 'err' : this.previewTick}`
		},
		onPreviewError(vm) {
			// A freshly-started VM has no framebuffer yet - fall back to the
			// placeholder icon instead of a broken-image glyph until the
			// next tick's request succeeds.
			this.$set(this.previewErrors, vm.name, true)
			setTimeout(() => this.$set(this.previewErrors, vm.name, false), PREVIEW_INTERVAL_MS)
		},
		osIcon(vm) {
			return vm.state === 'running' ? 'monitor' : 'monitor-off'
		},
		stateLabel(state) {
			return { running: this.$t('Running'), shutoff: this.$t('Stopped'), paused: this.$t('Paused'), crashed: this.$t('Crashed') }[state] || state
		},
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
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
		async runAction(name, actionFn) {
			try {
				await actionFn(name)
				await this.poll()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
			}
		},
		start(name) {
			return this.runAction(name, vmSidecar.startVM)
		},
		shutdown(name) {
			// A graceful ACPI power-off request - it only does anything if the
			// guest OS is actually running and has something listening for it
			// (systemd/acpid, Windows' own power button handler). A VM with no
			// OS installed, or one that's still booting, has nothing to
			// receive it and will just keep running - Force off is the only
			// way to stop those.
			this.$buefy.toast.open({ message: this.$t('Sent shutdown signal - the guest OS decides when to actually power off.'), type: 'is-info' })
			return this.runAction(name, vmSidecar.shutdownVM)
		},
		reset(name) {
			return this.runAction(name, vmSidecar.resetVM)
		},
		forceOff(name) {
			return this.runAction(name, vmSidecar.forceOffVM)
		},
		openCreate() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'create-vm',
				title: this.$t('Create VM'),
				component: 'CreateVmModal',
				width: 640,
				height: 580,
			})
		},
		openEdit(vm) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'edit-vm-' + vm.name,
				title: this.$t('Edit') + ' ' + vm.name,
				component: 'EditVmModal',
				// Tall enough that vCPU/Memory/Firmware/Display/Boot ISO plus
				// Disks/Network Adapters/Hardware Passthrough mostly fit
				// without scrolling - at 620 only the first couple of
				// sections were visible without it being obvious the rest
				// needed a scroll.
				width: 680,
				height: 700,
				props: { vm },
			})
		},
		openConsole(name) {
			this.$store.commit('OPEN_WINDOW', {
				id: 'vm-console-' + name,
				title: name,
				component: 'VmConsolePanel',
				width: 960,
				height: 640,
				props: { vmName: name },
			})
		},
		openSnapshots(name) {
			this.$emit('open-snapshots', name)
			if (this.$parent && this.$parent.activeSection !== undefined) {
				this.$parent.activeSection = 'snapshots'
			}
		},
		openConsoleTab(name) {
			const url = this.$router.resolve({ name: 'VmConsoleStandalone', params: { name } }).href
			window.open(url, '_blank')
		},
		confirmDelete(name) {
			// Buefy's $buefy.dialog.confirm() renders a viewport-wide overlay
			// over the whole desktop, not confined to this window - the same
			// "confined to the window" problem every dialog in Files already
			// solved via its own overlay instead of that global API.
			this.deletingVmName = name
		},
		async performDelete() {
			const name = this.deletingVmName
			this.deletingVmName = null
			await this.runAction(name, (n) => vmSidecar.deleteVM(n, true))
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-list {
	padding: 1.5rem;
}
.vm-list-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}
.vm-list-title {
	font-size: 1.15rem;
	font-weight: 600;
	color: #0f172a;
	margin: 0;
	letter-spacing: -0.01em;
}
.create-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.4rem;
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 500;
	height: 2rem;
	padding: 0 0.85rem;
	border-radius: 6px;
	cursor: pointer;
	transition: background 0.15s ease, transform 0.1s ease;

	&:hover {
		background: #1d4ed8;
	}
	&:active {
		transform: scale(0.98);
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
	padding: 3.5rem 1rem;
	text-align: center;
	color: #94a3b8;

	> ::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
		color: #cbd5e1;
	}

	.vm-empty-title {
		font-size: 0.95rem;
		font-weight: 600;
		color: #1e293b;
		margin: 0.5rem 0 0.25rem;
	}
	.vm-empty-hint {
		margin: 0 0 1rem;
		font-size: 0.8rem;
		color: #64748b;
	}
}
.create-btn-large {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.45rem;
	border: none;
	background: #2563eb;
	color: #fff;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 600;
	height: 2.15rem;
	padding: 0 1rem;
	border-radius: 6px;
	cursor: pointer;
	transition: background 0.15s ease, transform 0.15s ease;

	::v-deep .icon {
		width: 1rem;
		height: 1rem;
	}

	&:hover {
		background: #1d4ed8;
		transform: translateY(-1px);
	}
	&:active {
		transform: translateY(0);
	}
}
.vm-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr));
	gap: 1.15rem;
}
.vm-card {
	display: flex;
	flex-direction: column;
	border-radius: 12px;
	border: 1px solid rgba(0, 0, 0, 0.08);
	overflow: hidden;
	background: #fff;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.03);
	transition: box-shadow 0.18s ease, border-color 0.18s ease, transform 0.18s ease;

	&:hover {
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.07);
		border-color: rgba(37, 99, 235, 0.25);
		transform: translateY(-2px);
	}
}
.vm-preview {
	position: relative;
	aspect-ratio: 16 / 9;
	background: #0f172a;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	overflow: hidden;

	&.is-off {
		background: #f1f5f9;
	}

	&:hover .vm-preview-overlay {
		opacity: 1;
	}
}
.vm-preview-img {
	width: 100%;
	height: 100%;
	object-fit: contain;
	background: #000;
}
.vm-preview-placeholder {
	color: #94a3b8;

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
	}
}
.vm-preview-overlay {
	position: absolute;
	inset: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(15, 23, 42, 0.45);
	backdrop-filter: blur(2px);
	color: #fff;
	opacity: 0;
	transition: opacity 0.15s ease;
}
.vm-card-body {
	padding: 0.85rem 1rem 0.65rem;
}
.vm-card-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	margin-bottom: 0.5rem;
}
.vm-name {
	font-weight: 600;
	font-size: 0.92rem;
	color: #0f172a;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.vm-state-badge {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.72rem;
	font-weight: 500;
	padding: 0.2rem 0.55rem;
	border-radius: 9999px;
	background: #f1f5f9;
	color: #475569;
}
.vm-state-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: #94a3b8;
}
.vm-state-badge.is-running {
	background: #ecfdf5;
	color: #059669;

	.vm-state-dot {
		background: #10b981;
		box-shadow: 0 0 6px rgba(16, 185, 129, 0.4);
	}
}
.vm-state-badge.is-crashed {
	background: #fef2f2;
	color: #dc2626;

	.vm-state-dot {
		background: #ef4444;
	}
}
.vm-state-badge.is-paused {
	background: #fffbeb;
	color: #d97706;

	.vm-state-dot {
		background: #f59e0b;
	}
}
.vm-specs {
	display: flex;
	flex-wrap: wrap;
	gap: 0.75rem;
}
.vm-spec {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	font-size: 0.74rem;
	color: #64748b;
}
.vm-card-actions {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.25rem 0.85rem 0.85rem;
	border-top: none;
	background: #ffffff;
}
.vm-action-group {
	display: flex;
	align-items: center;
	gap: 0.25rem;
}
.vm-action-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.85rem;
	height: 1.85rem;

	// A right-pointing play triangle's visual weight sits left of its own
	// glyph bounding box, so centering the box (as every other icon here
	// correctly does) still reads as off-center to the eye - nudge it right
	// to compensate. Verified against the other action icons (measured via
	// their real getBoundingClientRect, not a guessed crop) which don't
	// need this.
	&.vm-play-btn .icon {
		transform: translateX(1px);
	}
	border: none;
	background: #f8fafc;
	color: #64748b;
	border-radius: 6px;
	cursor: pointer;
	transition: background 0.12s ease, color 0.12s ease, transform 0.1s ease;

	&:hover:not(:disabled) {
		background: #e2e8f0;
		color: #0f172a;
	}
	&:active:not(:disabled) {
		transform: scale(0.95);
	}
	&:disabled {
		opacity: 0.35;
		cursor: default;
	}
	&.is-danger:hover:not(:disabled) {
		background: #fee2e2;
		color: #dc2626;
	}
}
</style>
