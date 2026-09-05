<template>
	<div class="widget has-text-white disk is-relative">
		<div class="blur-background"></div>
		<div class="widget-content">
			<!-- Header Start -->
			<div class="widget-header is-flex">
				<div class="widget-title is-flex-grow-1">
					{{ $t('Storage') }}
				</div>
				<div class="widget-icon-button is-flex-shrink-0" @click="showDiskManagement">
					<b-icon icon="settings-outline" pack="casa" size="is-20"></b-icon>
				</div>
			</div>
			<!-- Header End -->

			<!-- Unified Disks & USB Drives List -->
			<div class="columns is-mobile is-multiline pt-2">
				<p v-if="!visibleDisks.length" class="disk-info no-disks column is-full">{{ $t('No drives to show.') }}</p>
				<div v-for="d in visibleDisks" :key="d.mount_point" class="column is-full pb-0 mb-2">
					<div class="is-flex is-align-items-center">
						<div class="header-icon is-flex-shrink-0">
							<b-image :src="d.is_usb ? require('@/assets/img/storage/USB.svg') : require('@/assets/img/storage/storage.svg')" class="is-64x64"></b-image>
						</div>
						<div class="ml-2 is-flex-grow-1 min-w-0">
							<h4 class="title is-size-14px mb-1 mt-0 has-text-left has-text-white one-line" :title="getDiskTitle(d)">
								{{ getDiskTitle(d) }}
							</h4>
							<p class="has-text-left is-size-14px disk-info">
								{{ $t('Used') }}: {{ d.used }}<br>
								{{ $t('Total') }}: {{ d.total }}
							</p>
						</div>
					</div>
					<b-progress :type="percentValue(d.percent) | getProgressType" :value="percentValue(d.percent)" class="mt-2"
						size="is-small"></b-progress>
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
			if (d.mount_point === '/') return this.$t('System')
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

		showDiskManagement() {
			this.$messageBus('widget_storagemanager');
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540,
				props: { section: 'appearance' }
			})
		},
	},
	sockets: {
		"nivaroos:system:utilization"() {
			// Periodically keep disks in sync when utilization ticks
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

<style lang="scss">
.disk {
	.progress {
		border-radius: 6px;
		height: 12px;

		&::-webkit-progress-bar {
			background: rgba(172, 184, 195, 0.4);
		}

		&::-webkit-progress-value {
			opacity: 1;
			border-radius: 6px;
		}

	}

	.disk-info {
		font-size: 0.875rem;
		line-height: 1.25rem;
		font-weight: 400;
		color: $grey-400;
	}

	.no-disks {
		padding: 0.5rem 0;
	}

	.min-w-0 {
		min-width: 0;
	}
}
</style>
