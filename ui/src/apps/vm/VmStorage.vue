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
					<span class="iso-meta">{{ disk.gib }} GB &middot; {{ disk.bus.toUpperCase() }}{{ disk.ssd ? ' · SSD' : '' }}</span>
				</div>
			</div>
			<div v-if="!vmDisks.length" class="vm-empty">
				<b-icon icon="harddisk" custom-size="mdi-48px"></b-icon>
				<p class="vm-empty-title">{{ $t('No virtual disks yet') }}</p>
			</div>
		</div>

		<div class="vm-section-toolbar vm-section-toolbar-secondary">
			<h2 class="vm-section-title">{{ $t('ISOs') }}</h2>
			<b-upload v-model="fileToUpload" @input="upload">
				<a class="create-btn">
					<b-icon icon="upload-outline" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Upload ISO') }}</span>
				</a>
			</b-upload>
		</div>

		<b-message v-if="uploading" type="is-info" :closable="false">{{ $t('Uploading...') }}</b-message>
		<b-message v-if="error" type="is-danger" :closable="false">{{ error }}</b-message>

		<div v-if="loading" class="vm-loading">
			<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
		</div>

		<div v-else class="iso-list">
			<div v-for="iso in isos" :key="iso.name" class="iso-row">
				<div class="iso-icon">
					<b-icon icon="disc" custom-size="mdi-22px"></b-icon>
				</div>
				<div class="iso-info">
					<span class="iso-name">{{ iso.name }}</span>
					<span class="iso-meta">{{ formatMib(iso.size_mib) }}</span>
				</div>
				<button class="iso-remove" :title="$t('Remove')" @click="deletingIsoName = iso.name">
					<b-icon icon="trash-can-outline" custom-size="mdi-18px"></b-icon>
				</button>
			</div>
			<div v-if="!isos.length" class="vm-empty">
				<b-icon icon="disc-alert" custom-size="mdi-48px"></b-icon>
				<p class="vm-empty-title">{{ $t('No ISOs uploaded yet') }}</p>
			</div>
		</div>

		<vm-overlay-panel :active="!!deletingIsoName" :title="$t('Remove ISO')" width="24rem" @close="deletingIsoName = null">
			<p>{{ $t('Remove') }} "{{ deletingIsoName }}"? {{ $t('Any VM still using it as boot media will need a different ISO.') }}</p>
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
			uploading: false,
			deleting: false,
			deletingIsoName: null,
			error: null,
			fileToUpload: null,
			vms: []
		}
	},
	computed: {
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
				this.isos = isos
				this.vms = vms
			} finally {
				this.loading = false
			}
		},
		formatMib(mib) {
			if (!mib || isNaN(mib)) return '0 MB'
			return mib >= 1024 ? `${(mib / 1024).toFixed(mib % 1024 ? 1 : 0)} GB` : `${mib} MB`
		},
		async upload(file) {
			if (!file) return
			this.error = null
			this.uploading = true
			try {
				const formData = new FormData()
				formData.append('iso', file)
				await vmSidecar.uploadISO(formData)
				await this.refresh()
			} catch (e) {
				this.error = e.message
			} finally {
				this.uploading = false
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
				this.error = e.message
				this.deletingIsoName = null
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

	&:hover {
		background: #1d4ed8;
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
	box-shadow: var(--shadow-sm);
	transition: border-color 0.15s ease, box-shadow 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.25);
		box-shadow: var(--shadow-lg);
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
		color: #2563eb;
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
		color: #dc2626;
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
</style>
