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

			<!-- Each real mount point (root + every /DATA/* volume the user
			has, formatted through Recasa or just mounted by hand - both
			show up here) gets its own row with its own used/total, instead
			of one number combining everything into a single blob. -->
			<div class="columns is-mobile is-multiline pt-2">
				<p v-if="!visibleDisks.length" class="disk-info no-disks column is-full">{{ $t('No drives to show.') }}</p>
				<div v-for="d in visibleDisks" :key="d.mount_point" class="column is-full pb-0">
					<div class="is-flex is-align-items-center">
						<div class="header-icon">
							<b-image :src="require('@/assets/img/storage/storage.svg')" class="is-64x64"></b-image>
						</div>
						<div class="ml-2 is-flex-grow-1 ">
							<h4 class="title is-size-14px mb-1 mt-0 has-text-left has-text-white one-line">
								{{ mountLabel(d.mount_point) }}
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

			<!-- Usb Disk List Start - part of the same card, not a second
			stacked widget, so this widget always renders exactly one box
			like every other widget (needed for the auto-arrange grid to
			treat every widget as the same nominal size). -->
			<div v-if="usbDisks.length > 0" class="usb-disks pt-1">
				<div class="columns is-mobile is-multiline">
					<div v-for="(item) in usbDisks" :key="'usb_' + item.name" class="column is-full pb-0">
						<div class="is-flex">
							<div class="header-icon is-flex-shrink-0">
								<b-image :src="require('@/assets/img/storage/USB.svg')" class="is-64x64"></b-image>
							</div>
							<div class="ml-2 is-flex-grow-1 ">
								<h4 class="title is-size-14px mb-1 mt-0 has-text-left has-text-white one-line ">
									{{ item.model }}
								</h4>
								<p class="has-text-left is-size-14px disk-info">
									{{ $t('Used') }}: {{ renderSize(item.size - item.avail) }}<br>
									{{ $t('Total') }}: {{ renderSize(item.size) }}
								</p>
							</div>
						</div>
						<b-progress :type="(Math.floor((item.size - item.avail) * 100 / item.size)) | getProgressType"
							:value="Math.floor((item.size - item.avail) * 100 / item.size)" class="mt-2"
							size="is-small"></b-progress>
					</div>
				</div>
			</div>
			<!-- Usb Disk List End -->
		</div>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin';
import events from '@/events/events'

const storageWidgetConfigKey = "storage_widget_config"
const REFRESH_MS = 30000 // disk usage isn't part of the 5s hardware-utilization
// socket push, so this widget polls it on its own, and re-reads the
// hidden-mounts config on the same cadence as a fallback - the actual
// instant update when a mount is hidden/shown from Settings comes over
// the EventBus instead (see events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS
// below), matching how SideBar.vue's own widget hide/show already works.

export default {
	// eslint-disable-next-line vue/multi-word-component-names
	name: 'disks',
	icon: "storage-outline",
	title: "Storage Status",
	gridCols: 3, // "normal" size - 3 icon-columns wide (see SideBar.vue)
	gridRows: 2, // "normal" size - 2 icon-rows tall
	initShow: true,
	mixins: [mixin],

	data() {
		return {
			disksUsage: [],
			hiddenMounts: [],
			usbDisks: [],
			timer: 0
		}
	},

	computed: {
		visibleDisks() {
			return this.disksUsage.filter(d => !this.hiddenMounts.includes(d.mount_point))
		}
	},

	mounted() {
		this.refresh()
		this.timer = setInterval(this.refresh, REFRESH_MS)
		this.usbDisks = this.$store.state.hardwareInfo.sys_usb
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
				}
			})
		},

		setHiddenMounts(hiddenMounts) {
			this.hiddenMounts = hiddenMounts
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
			// The old StorageManagerPanel modal floated outside any window
			// (not movable/resizable, inconsistent with the rest of the
			// app) - jump straight to Appearance > Widgets, where this
			// widget's own drive show/hide config actually lives.
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540,
				props: { section: 'appearance' }
			})
		},
	},
	sockets: {
		"nivaroos:system:utilization"(res) {
			this.usbDisks = JSON.parse(res.Properties.sys_usb)
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

	.usb-disks {
		margin-top: 0.5rem;
		border-top: 1px solid rgba(255, 255, 255, 0.1);
	}
}
</style>
