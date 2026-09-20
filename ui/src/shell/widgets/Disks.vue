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
					<div class="widget-icon-btn" :title="$t('Storage Settings')" @click="showDiskManagement">
						<i class="mdi mdi-cog-outline"></i>
					</div>
				</div>
			</div>
			<!-- Header End -->

			<!-- Unified Bento Disks & USB Drives List -->
			<div class="disks-bento-list pt-1">
				<div v-if="!visibleDisks.length" class="has-text-centered is-size-7 py-3 text-muted">
					{{ $t('No storage drives found') }}
				</div>
				<div v-for="d in visibleDisks" :key="d.mount_point" class="disk-bento-card">
					<div class="disk-card-top">
						<div class="disk-card-left">
							<span class="disk-type-pill" :class="d.is_usb ? 'is-usb' : 'is-internal'">
								<i class="mdi" :class="d.is_usb ? 'mdi-usb' : 'mdi-harddisk'"></i>
								{{ d.is_usb ? 'USB' : (d.mount_point === '/' ? 'System' : 'NVMe') }}
							</span>
							<span class="disk-name" :title="getDiskTitle(d)">
								{{ getDiskTitle(d) }}
							</span>
						</div>
						<span class="disk-pct-badge">{{ percentValue(d.percent) }}%</span>
					</div>

					<div class="disk-track">
						<div
							class="disk-fill"
							:class="getDiskBarClass(d.percent)"
							:style="{ width: percentValue(d.percent) + '%' }"
						></div>
					</div>

					<div class="disk-meta-row">
						<span>{{ d.used }} / {{ d.total }}</span>
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
		this.timer = setInterval(this.refresh, REFRESH_MS)
		this.$EventBus.$on(events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS, this.setHiddenMounts)
	},

	beforeDestroy() {
		this.$EventBus.$off(events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS, this.setHiddenMounts)
		clearInterval(this.timer)
	},

	methods: {
		refresh() {
			this.$api.sys.getDisksUsage().then(res => {
				if (res.data.success === 200) this.disksUsage = res.data.data || []
			})
			this.$api.users.getCustomStorage(storageWidgetConfigKey).then(res => {
				if (res.data.success === 200 && res.data.data) {
					this.hiddenMounts = res.data.data.hiddenMounts || []
					this.hasCustomHiddenMounts = true
				}
			})
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
			if (d.free) return `${d.free} ${this.$t('Free')}`
			const freePct = 100 - this.percentValue(d.percent)
			return `${freePct}% ${this.$t('Free')}`
		},

		showDiskManagement() {
			this.$messageBus('widget_storagemanager');
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
		"nivaroos:system:utilization"() {
			// Kept in sync with system updates
		},
		"local-storage:disk:added"() {
			this.refresh()
		},
		"local-storage:disk:removed"() {
			this.refresh()
		}
	}
}
</script>
