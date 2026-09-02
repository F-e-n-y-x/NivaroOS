<template>
	<div class="widget-visibility-panel">
		<div v-for="w in widgets" :key="w.name">
			<div class="setting-row hover-effect _is-radius">
				<b-icon class="row-icon" :icon="w.icon" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t(w.title) }}</div>
				<div class="row-control">
					<button v-if="w.name === 'disks'" class="icon-button" type="button" :title="$t('Configure drives')"
						@click="toggleDiskConfig">
						<b-icon :icon="showDiskConfig ? 'expand-up2' : 'expand-down2'" pack="casa" size="is-16"></b-icon>
					</button>
					<b-switch :value="!w.hidden" class="is-flex-direction-row-reverse mr-0" type="is-dark"
						@input="toggle(w)"></b-switch>
				</div>
			</div>

			<div v-if="w.name === 'disks' && showDiskConfig" class="disk-config">
				<p class="hint">{{ $t('Choose which drives show up in the Storage widget.') }}</p>
				<div v-for="d in disksUsage" :key="d.mount_point" class="disk-config-row">
					<span class="disk-config-label">{{ d.mount_point }}</span>
					<button class="icon-button" type="button"
						:title="isMountHidden(d) ? $t('Show in widget') : $t('Hide from widget')"
						@click="toggleMountHidden(d)">
						<b-icon :icon="isMountHidden(d) ? 'eye-off-outline' : 'eye-outline'" pack="casa" size="is-16"></b-icon>
					</button>
				</div>
				<p v-if="!disksUsage.length" class="hint">{{ $t('No drives detected.') }}</p>
			</div>
		</div>
	</div>
</template>

<script>
import events from '@/events/events'

const widgetsConfig = "widgets_config"
const storageWidgetConfigKey = "storage_widget_config"

// Independent of SideBar.vue's own require.context call - this panel only
// needs name/title/icon metadata to render the list and doesn't touch
// SideBar's runtime state directly. SideBar remains the sole writer of
// widgets_config (see setWidgetHidden there); this panel only reads it
// once for initial state and sends toggle commands over the EventBus, so
// there's no risk of two components racing to save the same record.
const widgetFiles = require.context('@/widgets', false, /\.vue$/)

export default {
	name: 'widget-visibility-panel',
	data() {
		return {
			widgets: [],
			showDiskConfig: false,
			disksUsage: [],
			hiddenMounts: [],
			hasCustomHiddenMounts: false
		}
	},
	created() {
		this.widgets = widgetFiles.keys().map(fileName => {
			const app = require(`@/widgets/${fileName.replace("./", "")}`).default
			return { name: app.name, title: app.title, icon: app.icon, hidden: false }
		})
		this.$api.users.getCustomStorage(widgetsConfig).then(res => {
			const saved = res.status === 200 && res.data.data && res.data.data.length ? res.data.data : []
			this.widgets.forEach(w => {
				const pos = saved.find(p => p.name === w.name)
				w.hidden = !!(pos && pos.hidden)
			})
		})
	},
	methods: {
		toggle(w) {
			w.hidden = !w.hidden
			this.$EventBus.$emit(events.SET_WIDGET_HIDDEN, w.name, w.hidden)
		},
		toggleDiskConfig() {
			this.showDiskConfig = !this.showDiskConfig
			if (this.showDiskConfig) this.loadDiskConfig()
		},
		loadDiskConfig() {
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
		// Mirrors Disks.vue's own visibleDisks logic: until an explicit
		// config has been saved, EFI partitions read as hidden by default
		// so this panel's eye icons match what the widget is actually
		// showing, rather than looking "on" for something already hidden.
		isMountHidden(disk) {
			if (this.hiddenMounts.includes(disk.mount_point)) return true
			return !this.hasCustomHiddenMounts && this.isEfiPartition(disk)
		},
		toggleMountHidden(disk) {
			const mountPoint = disk.mount_point
			this.hiddenMounts = this.isMountHidden(disk)
				? this.hiddenMounts.filter(m => m !== mountPoint)
				: this.hiddenMounts.concat([mountPoint])
			this.hasCustomHiddenMounts = true
			this.$api.users.setCustomStorage(storageWidgetConfigKey, { hiddenMounts: this.hiddenMounts })
			// Settings and the desktop widget are separate component trees -
			// broadcast the change so an already-open widget updates
			// immediately instead of waiting on its own poll interval.
			this.$EventBus.$emit(events.SET_STORAGE_WIDGET_HIDDEN_MOUNTS, this.hiddenMounts)
		},
	},
}
</script>

<style lang="scss" scoped>
.icon-button {
	flex-shrink: 0;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.7rem;
	height: 1.7rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);
	margin-right: 0.5rem;

	&:hover {
		background: rgba(0, 0, 0, 0.1);
	}
}

.disk-config {
	padding: 0 1.25rem 1rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.5rem;
}

.disk-config-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.4rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);

	&:last-of-type {
		border-bottom: none;
	}
}

.disk-config-label {
	font-size: 0.8rem;
}
</style>
