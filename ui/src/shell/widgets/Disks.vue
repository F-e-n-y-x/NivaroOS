<template>
	<div class="widget disk is-relative">
		<div class="blur-background"></div>
		<div class="widget-content">
			<!-- Header Start -->
			<div class="widget-header">
				<div class="widget-header-left">
					<div class="widget-badge is-disks">
						<i class="mdi mdi-database-outline"></i>
					</div>
					<div class="widget-header-text">
						<span class="widget-title">{{ $t('Storage') }}</span>
						<span class="widget-header-meta">
							{{ visibleDisks.length }} {{ visibleDisks.length === 1 ? $t('Drive Mounted') : $t('Drives Mounted') }}
						</span>
					</div>
				</div>
				<div class="widget-header-right">
					<button
						type="button"
						class="widget-icon-btn"
						:title="$t('Storage Settings')"
						:aria-label="$t('Storage Settings')"
						@click="showDiskManagement"
					>
						<i class="mdi mdi-cog-outline" aria-hidden="true"></i>
					</button>
				</div>
			</div>
			<!-- Header End -->

			<!-- Unified Bento Disks & USB Drives List -->
			<div class="disks-bento-list pt-1">
				<div v-if="loadError && !disksUsage.length" class="has-text-centered is-size-7 py-3 text-muted" role="status">
					{{ $t('Could not read storage usage') }}
				</div>
				<div v-else-if="!visibleDisks.length" class="has-text-centered is-size-7 py-3 text-muted">
					{{ $t('No storage drives found') }}
				</div>
				<div v-for="d in visibleDisks" :key="d.mount_point" class="disk-bento-card">
					<div class="disk-card-top">
						<div class="disk-card-left">
							<span class="disk-type-pill" :class="diskKind(d).cls">
								<i class="mdi" :class="diskKind(d).icon" aria-hidden="true"></i>
								{{ diskKind(d).label }}
							</span>
							<span class="disk-name" :title="getDiskTitle(d)">
								{{ getDiskTitle(d) }}
							</span>
						</div>
						<span class="disk-pct-badge">{{ diskPercent(d) }}%</span>
					</div>

					<div
						class="disk-track"
						role="progressbar"
						:aria-label="$t('{name} usage', { name: getDiskTitle(d) })"
						:aria-valuenow="diskPercent(d)"
						aria-valuemin="0"
						aria-valuemax="100"
					>
						<div
							class="disk-fill"
							:class="getDiskBarClass(diskPercent(d))"
							:style="{ width: diskPercent(d) + '%' }"
						></div>
					</div>

					<div class="disk-meta-row">
						<span>{{ diskUsed(d) }} / {{ diskTotal(d) }}</span>
						<span class="free-pill">{{ getFreeDisplay(d) }}</span>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin';
import events from '@/events/events'

const storageWidgetConfigKey = "storage_widget_config"
const REFRESH_MS = 20000

// Media kind (from the backend's sysfs lookup) -> pill label/icon/colour.
const DISK_KINDS = {
	nvme: { label: 'NVMe', icon: 'mdi-harddisk', cls: 'is-internal' },
	ssd: { label: 'SSD', icon: 'mdi-harddisk', cls: 'is-internal' },
	hdd: { label: 'HDD', icon: 'mdi-harddisk', cls: 'is-internal' },
	usb: { label: 'USB', icon: 'mdi-usb', cls: 'is-usb' },
	mmc: { label: 'SD', icon: 'mdi-sd', cls: 'is-usb' },
	raid: { label: 'RAID', icon: 'mdi-database', cls: 'is-pool' },
	pool: { label: 'Pool', icon: 'mdi-database', cls: 'is-pool' },
	network: { label: 'Network', icon: 'mdi-server-network', cls: 'is-network' },
	virtual: { label: 'Virtual', icon: 'mdi-harddisk', cls: 'is-internal' }
}

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: 'disks',
	icon: "storage-outline",
	title: "Storage Status",
	gridCols: 3,
	gridRows: 2,
	initShow: true,
	mixins: [mixin],

	data() {
		return {
			disksUsage: [],
			hiddenMounts: [],
			hasCustomHiddenMounts: false,
			loadError: false,
			loading: false,
			timer: 0
		}
	},

	computed: {
		visibleDisks() {
			if (!this.hasCustomHiddenMounts) {
				return this.disksUsage.filter(d => !this.isEfiPartition(d))
			}
			return this.disksUsage.filter(d => !this.hiddenMounts.includes(d.mount_point))
		}
	},

	mounted() {
		this.refresh()
		// The hidden-mounts config only changes through Settings, which
		// broadcasts SET_STORAGE_WIDGET_HIDDEN_MOUNTS - fetch it once here
		// rather than on every usage refresh.
		this.loadWidgetConfig()
		this.timer = setInterval(this.refresh, REFRESH_MS)
		this.$EventBus.$on(events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS, this.setHiddenMounts)
		document.addEventListener('visibilitychange', this.onVisibility)
	},

	beforeDestroy() {
		this.$EventBus.$off(events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS, this.setHiddenMounts)
		document.removeEventListener('visibilitychange', this.onVisibility)
		clearInterval(this.timer)
	},

	methods: {
		refresh() {
			if (document.hidden || this.loading) return
			this.loading = true
			this.$api.sys.getDisksUsage().then(res => {
				if (res.data.success === 200) {
					this.disksUsage = res.data.data || []
					this.loadError = false
				}
			}).catch(() => {
				this.loadError = true
			}).finally(() => {
				this.loading = false
			})
		},

		loadWidgetConfig() {
			this.$api.users.getCustomStorage(storageWidgetConfigKey).then(res => {
				if (res.data.success === 200 && res.data.data) {
					this.hiddenMounts = res.data.data.hiddenMounts || []
					this.hasCustomHiddenMounts = true
				}
			}).catch(() => { /* keep defaults */ })
		},

		onVisibility() {
			if (!document.hidden) this.refresh()
		},

		diskKind(d) {
			const k = DISK_KINDS[d.kind]
			if (k) return { ...k, label: this.$t(k.label) }
			if (d.is_usb) return { ...DISK_KINDS.usb, label: this.$t('USB') }
			return { label: this.$t('Disk'), icon: 'mdi-harddisk', cls: d.mount_point === '/' ? 'is-system' : 'is-internal' }
		},

		// Sizes come as bytes and are formatted with the same renderSize()
		// (1024-based, KB/MB/GB/TB) as the rest of the UI; the pre-formatted
		// strings are only a fallback for an older backend.
		diskTotal(d) {
			return d.size_bytes ? this.renderSize(d.size_bytes) : (d.total || '')
		},

		diskUsed(d) {
			return d.size_bytes ? this.renderSize(d.used_bytes || 0) : (d.used || '')
		},

		diskPercent(d) {
			if (d.size_bytes) {
				return Math.min(100, Math.max(0, Math.round(((d.used_bytes || 0) / d.size_bytes) * 100)))
			}
			return this.percentValue(d.percent)
		},

		isEfiPartition(disk) {
			return disk.fstype === 'vfat' && /efi/i.test(disk.mount_point || '')
		},

		setHiddenMounts(hiddenMounts) {
			this.hiddenMounts = hiddenMounts
			this.hasCustomHiddenMounts = true
		},

		getDiskTitle(d) {
			if (d.mount_point === '/') return this.$t('System Root')
			if (d.is_usb) {
				if (d.model && d.label && d.label !== 'New Volume') {
					return `${d.model} (${d.label})`
				}
				if (d.model) return d.model
				if (d.label) return d.label
			}
			if (d.label) return d.label
			return this.mountLabel(d.mount_point)
		},

		mountLabel(mountPoint) {
			if (mountPoint === '/') return this.$t('System')
			return mountPoint.split('/').filter(Boolean).pop() || mountPoint
		},

		percentValue(percent) {
			return parseInt(percent, 10) || 0
		},

		getDiskBarClass(percent) {
			const p = parseInt(percent, 10) || 0
			if (p >= 90) return 'is-danger'
			if (p >= 75) return 'is-warning'
			return 'is-normal'
		},

		getFreeDisplay(d) {
			if (d.size_bytes && typeof d.avail_bytes === 'number') {
				return this.$t('{size} free', { size: this.renderSize(d.avail_bytes) })
			}
			if (d.free) return this.$t('{size} free', { size: d.free })
			return this.$t('{size} free', { size: `${100 - this.diskPercent(d)}%` })
		},

		showDiskManagement() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'storage' }
			})
		},
	},
	sockets: {
		"local-storage:disk:added"() {
			this.refresh()
		},
		"local-storage:disk:removed"() {
			this.refresh()
		}
	}
}
</script>
