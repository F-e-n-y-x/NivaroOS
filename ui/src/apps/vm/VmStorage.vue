<template>
	<div class="vm-storage">
		<div class="vm-section-toolbar">
			<h2 class="vm-section-title">{{ $t('Virtual Disks') }}</h2>
		</div>

		<div v-if="loading" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>
		<div v-else class="iso-list">
			<div v-for="disk in vmDisks" :key="disk.vmName + disk.path" class="iso-row">
				<div class="iso-icon" :class="{ 'is-ssd': disk.ssd }">
					<b-icon :icon="disk.ssd ? 'harddisk' : 'database'" :custom-size="disk.ssd ? 'mdi-18px' : 'mdi-22px'"></b-icon>
				</div>
				<div class="iso-info">
					<span class="iso-name">{{ disk.vmName }} <span class="iso-name-target">&middot; {{ disk.target }}</span></span>
					<span class="iso-meta">{{ disk.gib }} GB<template v-if="disk.bus"> &middot; {{ disk.bus.toUpperCase() }}</template>{{ disk.ssd ? ' · SSD' : '' }}</span>
					<span v-if="disk.path" class="iso-meta iso-path" :title="disk.path">{{ disk.path }}</span>
				</div>
			</div>
			<div v-if="!vmDisks.length" class="vm-empty">
				<b-icon icon="harddisk" custom-size="mdi-48px"></b-icon>
				<p class="vm-empty-title">{{ $t('No virtual disks yet') }}</p>
			</div>
		</div>

		<div class="vm-section-toolbar vm-section-toolbar-secondary">
			<h2 class="vm-section-title">{{ $t('ISOs') }}</h2>
			<b-upload v-model="fileToUpload" accept=".iso,application/x-iso9660-image" :disabled="!!uploading" @input="upload">
				<a class="create-btn">
					<b-icon icon="upload-outline" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Upload ISO') }}</span>
				</a>
			</b-upload>
		</div>

		<div v-if="uploading" class="upload-progress" role="status">
			<div class="upload-progress-head">
				<span class="upload-progress-name" :title="uploading.name">{{ $t('Uploading {name}', { name: uploading.name }) }}</span>
				<span class="upload-progress-pct">{{ uploadPct }}%<template v-if="uploadEta"> · {{ uploadEta }}</template></span>
				<b-button size="is-small" @click="cancelUpload">{{ $t('Cancel') }}</b-button>
			</div>
			<progress class="progress is-small is-primary" :value="uploadPct" max="100" :aria-label="$t('Upload progress')">{{ uploadPct }}%</progress>
		</div>
		<div class="vm-notice is-danger" v-if="error" role="alert">{{ error }}</div>

		<div v-if="loading" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<div v-else class="iso-list">
			<div v-for="iso in isos" :key="iso.name" class="iso-row">
				<div class="iso-icon">
					<b-icon icon="disc" custom-size="mdi-22px"></b-icon>
				</div>
				<div class="iso-info">
					<span class="iso-name" :title="iso.name">{{ iso.name }}</span>
					<span class="iso-meta">{{ formatMib(iso.size_mib) }}<template v-if="isoUsers(iso).length"> &middot; {{ $t('In use by {vms}', { vms: isoUsers(iso).join(', ') }) }}</template></span>
				</div>
				<button class="iso-remove" :title="$t('Remove')" :aria-label="$t('Remove {name}', { name: iso.name })" @click="askDelete(iso)">
					<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
				</button>
			</div>
			<div v-if="!isos.length" class="vm-empty">
				<b-icon icon="disc-alert" custom-size="mdi-48px"></b-icon>
				<p class="vm-empty-title">{{ $t('No ISOs uploaded yet') }}</p>
			</div>
		</div>

		<vm-overlay-panel :active="!!deletingIsoName" :title="$t('Remove ISO')" width="24rem" @close="deletingIsoName = null">
			<p>{{ $t('Remove the ISO "{name}"?', { name: deletingIsoName }) }}</p>
			<p v-if="deletingIsoUsers.length" class="field-error mt-2">{{ $t('It is inserted in: {vms}. Eject it there first.', { vms: deletingIsoUsers.join(', ') }) }}</p>
			<div v-if="deleteError" class="vm-notice is-danger mt-3" role="alert">{{ deleteError }}</div>
			<template #footer>
				<b-button @click="deletingIsoName = null">{{ $t('Cancel') }}</b-button>
				<b-button type="is-danger" :loading="deleting" @click="performDelete">{{ $t('Remove') }}</b-button>
			</template>
		</vm-overlay-panel>
	</div>
</template>

<script>
import { vmSidecar } from '@/api/vmSidecar'
import VmOverlayPanel from './VmOverlayPanel.vue'

export default {
	name: 'vm-storage',
	components: { VmOverlayPanel },
	data() {
		return {
			isos: [],
			loading: true,
			uploading: null,
			deleteError: '',
			deletingIsoUsers: [],
			deleting: false,
			deletingIsoName: null,
			error: null,
			fileToUpload: null,
			vms: []
		}
	},
	computed: {
		uploadPct() {
			const u = this.uploading
			return u && u.total ? Math.floor((u.loaded / u.total) * 100) : 0
		},
		uploadEta() {
			const u = this.uploading
			if (!u || !u.loaded || u.loaded >= u.total) return ''
			const secs = ((Date.now() - u.startedAt) / 1000 / u.loaded) * (u.total - u.loaded)
			if (!isFinite(secs) || secs < 1) return ''
			return secs < 90 ? this.$t('{n} s left', { n: Math.round(secs) }) : this.$t('{n} min left', { n: Math.round(secs / 60) })
		},
		vmDisks() {
			const rows = []
			for (const vm of this.vms) {
				for (const disk of vm.disks || []) {
					rows.push({ vmName: vm.name, ...disk })
				}
			}
			return rows
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		async refresh() {
			this.loading = true
			try {
				const [isos, vms] = await Promise.all([vmSidecar.listISOs(), vmSidecar.listVMs().catch(() => [])])
				this.isos = isos || []
				this.vms = vms || []
				this.error = null
			} catch (e) {
				this.error = this.$t('Could not load ISOs: {reason}', { reason: e.message })
			} finally {
				this.loading = false
			}
		},
		isoUsers(iso) {
			return this.vms.filter((vm) => vm.iso_path && vm.iso_path.split('/').pop() === iso.name).map((vm) => vm.name)
		},
		askDelete(iso) {
			this.deleteError = ''
			this.deletingIsoUsers = this.isoUsers(iso)
			this.deletingIsoName = iso.name
		},
		cancelUpload() {
			if (this.uploadHandle) this.uploadHandle.abort()
		},
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		async upload(file) {
			if (!file || this.uploading) return
			if (!/\.iso$/i.test(file.name)) {
				this.error = this.$t('{name} is not an .iso file.', { name: file.name })
				this.fileToUpload = null
				return
			}
			this.error = null
			this.uploading = { name: file.name, loaded: 0, total: file.size, startedAt: Date.now() }
			try {
				this.uploadHandle = vmSidecar.uploadISOWithProgress(file, (loaded, total) => {
					if (this.uploading) Object.assign(this.uploading, { loaded, total })
				})
				await this.uploadHandle.promise
				this.$buefy.toast.open({ message: this.$t('{name} uploaded.', { name: file.name }), type: 'is-success' })
				await this.refresh()
			} catch (e) {
				if (!e.cancelled) this.error = e.message
			} finally {
				this.uploading = null
				this.uploadHandle = null
				this.fileToUpload = null
			}
		},
		async performDelete() {
			const name = this.deletingIsoName
			this.deleting = true
			try {
				await vmSidecar.deleteISO(name)
				this.deletingIsoName = null
				await this.refresh()
			} catch (e) {
				this.deleteError = e.message
			} finally {
				this.deleting = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.vm-storage {
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
.vm-section-toolbar-secondary {
	margin-top: var(--space-8);
}
.vm-section-title {
	font-size: var(--font-lg);
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	margin: 0;
	letter-spacing: -0.01em;
}
.create-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	border: none;
	background: var(--color-primary);
	color: #fff;
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 500;
	height: 2rem;
	padding: 0 var(--space-3);
	border-radius: var(--radius-sm);
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: var(--color-primary-hover);
		color: #fff;
	}
}
.vm-loading {
	display: flex;
	justify-content: center;
	padding: var(--space-8) 0;
	color: var(--theme-text-muted, #94a3b8);

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
	}
}
.iso-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}
.iso-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.07));
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);
	transition: border-color 0.15s ease, box-shadow 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.25);
	}
}
.iso-icon {
	flex-shrink: 0;
	width: 2.25rem;
	height: 2.25rem;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	background: var(--theme-card-subtle, #f1f5f9);
	color: var(--theme-text-muted, #64748b);

	&.is-ssd {
		background: rgba(59, 130, 246, 0.1);
		color: var(--color-primary-fg);
	}
}
.iso-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}
.iso-name-target {
	font-weight: 400;
	color: var(--theme-text-muted, #94a3b8);
	font-size: var(--font-xs);
}
.iso-name {
	font-weight: 600;
	color: var(--theme-text-primary, #0f172a);
	font-size: var(--font-base);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.iso-meta {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}
.iso-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, #94a3b8);
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.85rem;
	height: 1.85rem;
	border-radius: var(--radius-sm);
	transition: background 0.12s ease, color 0.12s ease;

	&:hover {
		color: var(--color-danger-fg);
		background: rgba(239, 68, 68, 0.1);
	}
}
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	padding: var(--space-8) var(--space-4);
	text-align: center;
	color: var(--theme-text-muted, #94a3b8);

	::v-deep .icon {
		width: 2.5rem;
		height: 2.5rem;
		color: var(--theme-text-muted, #cbd5e1);
	}

	.vm-empty-title {
		font-size: var(--font-md);
		font-weight: 600;
		color: var(--theme-text-secondary, #475569);
		margin: var(--space-1) 0 0;
	}
}
.iso-path {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	max-width: 100%;
}
.field-error {
	font-size: var(--font-sm);
	color: var(--color-danger-fg);
}
.upload-progress {
	margin-bottom: var(--space-4);
	padding: var(--space-3) var(--space-4);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg, #fff);

	.progress {
		margin: var(--space-2) 0 0;
	}
}
.upload-progress-head {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	font-size: var(--font-sm);
	color: var(--theme-text-primary, #1e293b);
}
.upload-progress-name {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.upload-progress-pct {
	color: var(--theme-text-secondary, #475569);
	font-variant-numeric: tabular-nums;
}
.iso-remove:focus-visible,
.create-btn:focus-visible {
	outline: 2px solid var(--color-primary-fg);
	outline-offset: 2px;
}
.create-btn {
	white-space: nowrap;
	flex-shrink: 0;
}
// b-upload renders the button as an <a>, and the global link colour
// (light blue in dark mode) beat .create-btn's white text.
a.create-btn,
a.create-btn:hover,
a.create-btn span {
	color: #fff !important;
}
</style>
