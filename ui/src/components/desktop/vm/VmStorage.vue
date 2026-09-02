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
					<span class="iso-meta">{{ disk.gib }} GiB &middot; {{ disk.bus.toUpperCase() }}{{ disk.ssd ? ' · SSD' : '' }}</span>
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
	padding: 1.5rem;
}
.vm-section-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}
.vm-section-toolbar-secondary {
	margin-top: 2rem;
}
.vm-section-title {
	font-size: 1.15rem;
	font-weight: 600;
	color: #0f172a;
	margin: 0;
	letter-spacing: -0.01em;
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
.iso-list {
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
}
.iso-row {
	display: flex;
	align-items: center;
	gap: 0.85rem;
	padding: 0.85rem 1.15rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 12px;
	background: #fff;
	box-shadow: 0 1px 3px rgba(0, 0, 0, 0.02);
	transition: border-color 0.15s ease, box-shadow 0.15s ease;

	&:hover {
		border-color: rgba(37, 99, 235, 0.25);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
	}
}
.iso-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: #f1f5f9;
	color: #64748b;

	&.is-ssd {
		background: #eff6ff;
		color: #2563eb;
	}
}
.iso-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}
.iso-name-target {
	font-weight: 400;
	color: #94a3b8;
	font-size: 0.78rem;
}
.iso-name {
	font-weight: 600;
	color: #0f172a;
	font-size: 0.92rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.iso-meta {
	font-size: 0.75rem;
	color: #64748b;
}
.iso-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: #94a3b8;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.95rem;
	height: 1.95rem;
	border-radius: 7px;
	transition: background 0.12s ease, color 0.12s ease;

	&:hover {
		color: #dc2626;
		background: #fee2e2;
	}
}
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.5rem;
	padding: 3.5rem 1rem;
	color: #94a3b8;

	::v-deep .icon {
		width: 3.5rem;
		height: 3.5rem;
		color: #cbd5e1;
	}

	.vm-empty-title {
		font-size: 0.95rem;
		font-weight: 600;
		color: #475569;
		margin: 0.25rem 0 0;
	}
}
</style>
