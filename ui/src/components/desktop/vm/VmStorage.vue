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
					<b-icon :icon="disk.ssd ? 'harddisk' : 'database'" custom-size="mdi-22px"></b-icon>
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
	padding: 1.25rem;
	height: 100%;
	overflow: auto;
}
.vm-section-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1.25rem;
}
.vm-section-toolbar-secondary {
	margin-top: 2rem;
	padding-top: 1.25rem;
	border-top: 1px solid rgb(228 233 237);
}
.vm-section-title {
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
		color: #fff;
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
.iso-list {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}
.iso-row {
	display: flex;
	align-items: center;
	gap: 0.85rem;
	padding: 0.75rem 1rem;
	border: 1px solid rgb(228 233 237);
	border-radius: 10px;
	background: #fff;
}
.iso-icon {
	flex-shrink: 0;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(0, 0, 0, 0.04);
	color: rgba(0, 0, 0, 0.4);

	&.is-ssd {
		background: rgba(50, 115, 220, 0.1);
		color: #3273dc;
	}
}
.iso-info {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
}
.iso-name-target {
	font-weight: 400;
	color: rgba(0, 0, 0, 0.4);
	font-size: 0.78rem;
}
.iso-name {
	font-weight: 600;
	color: #2c3e50;
	font-size: 0.9rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.iso-meta {
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.45);
}
.iso-remove {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: rgba(0, 0, 0, 0.35);
	cursor: pointer;
	display: flex;
	align-items: center;
	padding: 0.35rem;
	border-radius: 6px;

	&:hover {
		color: #f2534a;
		background: rgba(242, 83, 74, 0.08);
	}
}
.vm-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.5rem;
	padding: 3rem 1rem;
	color: rgba(0, 0, 0, 0.3);

	::v-deep .icon {
		width: 3rem;
		height: 3rem;
	}

	.vm-empty-title {
		font-size: 0.9rem;
		font-weight: 600;
		color: rgba(0, 0, 0, 0.5);
		margin: 0;
	}
}
</style>
