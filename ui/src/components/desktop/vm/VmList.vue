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
						<span class="vm-spec"><b-icon icon="chip" custom-size="mdi-14px"></b-icon>{{ vm.vcpus }} {{ $t('vCPU') }}</span>
						<span class="vm-spec"><b-icon icon="memory" custom-size="mdi-14px"></b-icon>{{ formatMib(vm.memory_mib) }}</span>
						<span class="vm-spec"><b-icon icon="lan" custom-size="mdi-14px"></b-icon>{{ networkLabel(vm) }}</span>
					</div>
				</div>

				<div class="vm-card-actions">
					<div class="vm-action-group">
						<button v-if="vm.state !== 'running'" class="vm-action-btn" :title="$t('Start')" @click="start(vm.name)">
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
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		networkLabel(vm) {
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
	padding: 1.25rem;
	height: 100%;
	overflow: auto;
}
.vm-list-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}
.vm-list-title {
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
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.5rem;
	padding: 4rem 1rem;
	color: rgba(0, 0, 0, 0.4);

	// Scoped to the direct icon child only - a blanket ".icon" selector
	// here also matched the Create VM button's own (much smaller) plus
	// icon just below it, since the button is nested inside .vm-empty
	// too, inflating that icon to match and making the whole button look
	// oversized.
	> ::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	.vm-empty-title {
		font-size: 1rem;
		font-weight: 600;
		color: rgba(0, 0, 0, 0.6);
		margin: 0.25rem 0 0;
	}
	.vm-empty-hint {
		margin: 0 0 1rem;
		font-size: 0.85rem;
	}
}
.create-btn-large {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	border: none;
	background: #3273dc;
	color: #fff;
	font-family: inherit;
	font-size: 0.9rem;
	font-weight: 600;
	padding: 0.7rem 1.35rem;
	border-radius: 10px;
	cursor: pointer;
	box-shadow: 0 4px 14px rgba(50, 115, 220, 0.25);
	transition: background 0.15s ease, transform 0.15s ease, box-shadow 0.15s ease;

	::v-deep .icon {
		width: 1.15rem;
		height: 1.15rem;
	}

	&:hover {
		background: #2366d1;
		transform: translateY(-1px);
		box-shadow: 0 6px 18px rgba(50, 115, 220, 0.32);
	}
	&:active {
		transform: translateY(0);
	}
}
.vm-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
	gap: 1rem;
}
.vm-card {
	display: flex;
	flex-direction: column;
	border-radius: 12px;
	border: 1px solid rgb(228 233 237);
	overflow: hidden;
	background: #fff;
	transition: box-shadow 0.15s ease, border-color 0.15s ease;

	&:hover {
		box-shadow: 0 6px 18px rgba(0, 0, 0, 0.08);
		border-color: rgb(210 217 224);
	}
}
.vm-preview {
	position: relative;
	aspect-ratio: 16 / 9;
	background: #111;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	overflow: hidden;

	&.is-off {
		background: rgba(0, 0, 0, 0.04);
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
	color: rgba(0, 0, 0, 0.2);

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
	background: rgba(0, 0, 0, 0.35);
	color: #fff;
	opacity: 0;
	transition: opacity 0.15s ease;
}
.vm-card-body {
	padding: 0.75rem 0.85rem 0.5rem;
}
.vm-card-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	margin-bottom: 0.4rem;
}
.vm-name {
	font-weight: 600;
	font-size: 0.9rem;
	color: #2c3e50;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.vm-state-badge {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.3rem;
	font-size: 0.7rem;
	font-weight: 600;
	color: rgba(0, 0, 0, 0.5);
}
.vm-state-dot {
	width: 7px;
	height: 7px;
	border-radius: 50%;
	background: #b5b5b5;
}
.vm-state-badge.is-running .vm-state-dot {
	background: #23d160;
}
.vm-state-badge.is-crashed .vm-state-dot {
	background: #ff3860;
}
.vm-state-badge.is-paused .vm-state-dot {
	background: #ffdd57;
}
.vm-specs {
	display: flex;
	flex-wrap: wrap;
	gap: 0.6rem;
}
.vm-spec {
	display: flex;
	align-items: center;
	gap: 0.25rem;
	font-size: 0.72rem;
	color: rgba(0, 0, 0, 0.5);
}
.vm-card-actions {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.4rem 0.6rem;
	border-top: 1px solid rgb(228 233 237);
	background: rgba(0, 0, 0, 0.012);
}
.vm-action-group {
	display: flex;
	align-items: center;
	gap: 0.15rem;
}
.vm-action-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.9rem;
	height: 1.9rem;
	border: none;
	background: transparent;
	color: rgba(0, 0, 0, 0.55);
	border-radius: 6px;
	cursor: pointer;

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.06);
		color: #2c3e50;
	}
	&:disabled {
		opacity: 0.35;
		cursor: default;
	}
	&.is-danger:hover:not(:disabled) {
		background: rgba(242, 83, 74, 0.1);
		color: #f2534a;
	}
}
</style>
