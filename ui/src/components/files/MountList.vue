<!-- src/components/files/MountList.vue -->
<template>
	<div class="mount-list">
		<!-- Merge fs storage item -->
		<div v-if="hasMergerFunction" class="mount-group">
			<div
				class="tree-node"
				:class="{ active: isMergeRowActive }"
				@click="filesController.navigate('/DATA')"
				@contextmenu.prevent="onContextMenu({ name: $t('NivaroOS HD'), path: '/DATA', is_dir: true, mountType: 'merger-root' }, $event)"
			>
				<span class="tree-node-icon is-relative">
					<i
						class="casa casa-22px"
						:class="{
							'casa-storage-merger': (!dorpdown && !hover) || mergeStorageList.length === 0,
							'casa-expand': hover && !dorpdown && mergeStorageList.length !== 0,
							'casa-expand-down': dorpdown && mergeStorageList.length !== 0,
						}"
						@click.stop="warning"
						@mouseleave="hover = false"
						@mouseover="hover = true"
					></i>
					<span v-show="!dorpdown && !hover && mergeStorageList.length !== 0" class="hint">
						{{ mergeStorageList.length }}
					</span>
				</span>
				<span class="tree-node-label one-line">{{ $t('NivaroOS HD') }}</span>
				<b-icon
					v-if="testMergeMiss > 0"
					class="warn"
					custom-size="casa-16px"
					icon="danger-outline"
					pack="casa"
				></b-icon>
			</div>
			<div v-show="dorpdown && mergeStorageList.length > 0" class="mount-sublist">
				<div
					v-for="item in mergeStorageList"
					:key="item.path || item.uuid"
					class="tree-node"
					:class="{ active: isItemActive(item) }"
					@click="open(item)"
					@contextmenu.prevent="onContextMenu({ ...item, is_dir: true, mountType: 'merger' }, $event)"
				>
					<span class="tree-node-icon">
						<b-icon
							v-if="item.icon !== 'danger'"
							:icon="item.icon"
							:pack="item.pack"
							class="casa-color-blue"
							custom-size="casa-22px"
						></b-icon>
						<b-icon v-else class="warn" custom-size="casa-16px" icon="danger" pack="casa"></b-icon>
					</span>
					<span class="tree-node-label one-line">{{ item.name }}</span>
				</div>
			</div>
		</div>

		<!-- Local Storage List Start -->
		<div
			v-for="item in localStorageList"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), 'drop-target': dragHoverPath === item.path }"
			@click="open(item)"
			@contextmenu.prevent="onContextMenu({ ...item, is_dir: true, mountType: 'local' }, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon">
				<b-icon :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="casa-22px"></b-icon>
			</span>
			<span class="tree-node-label one-line">{{ item.name }}</span>
		</div>
		<!-- Local Storage List End -->

		<!-- Network Storage List Start -->
		<div
			v-for="item in networkStorageList"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), 'drop-target': dragHoverPath === item.path }"
			@click="open(item)"
			@contextmenu.prevent="onContextMenu({ ...item, is_dir: true, mountType: 'network' }, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon">
				<b-icon :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="casa-22px"></b-icon>
			</span>
			<span class="tree-node-label one-line">{{ item.name }}</span>
			<span class="tree-node-right-icon" @click.stop="umountNetwork(item)">
				<b-icon icon="eject" :pack="item.pack" class="casa-color-gray" custom-size="casa-16px"></b-icon>
			</span>
		</div>
		<!-- Network Storage List End -->

		<!-- USB List Start -->
		<div
			v-for="item in usbStorageList"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), 'drop-target': dragHoverPath === item.path }"
			@click="open(item)"
			@contextmenu.prevent="onContextMenu({ ...item, is_dir: true, mountType: 'usb' }, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon">
				<b-icon :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="casa-22px"></b-icon>
			</span>
			<span class="tree-node-label one-line">{{ item.name }}</span>
			<span class="tree-node-right-icon" @click.stop="umountUsb(item)">
				<b-icon icon="eject" :pack="item.pack" class="casa-color-gray" custom-size="casa-16px"></b-icon>
			</span>
		</div>
		<!-- USB List End -->

		<!-- Cloud List Start -->
		<div
			v-for="item in cloudStorageList"
			:key="item.path"
			class="tree-node"
			:class="{ active: isItemActive(item), 'drop-target': dragHoverPath === item.path }"
			@click="open(item)"
			@contextmenu.prevent="onContextMenu({ ...item, is_dir: true, mountType: 'cloud' }, $event)"
			@dragover="onDragOver(item, $event)"
			@dragleave="onDragLeave(item)"
			@drop="onDrop(item, $event)"
		>
			<span class="tree-node-icon">
				<b-image v-if="item.icon_type === 'svg'" :src="item.icon" style="width: 19px; height: 19px;"></b-image>
				<b-icon v-else :icon="item.icon" :pack="item.pack" class="casa-color-blue" custom-size="mdi-19px"></b-icon>
			</span>
			<span class="tree-node-label one-line">{{ item.name }}</span>
			<span class="tree-node-right-icon" @click.stop="umountCloud(item)">
				<b-icon icon="eject" pack="casa" class="casa-color-gray" custom-size="casa-16px"></b-icon>
			</span>
		</div>
		<!-- Cloud List End -->

		<sidebar-context-menu ref="contextMenu" @eject="handleEject"></sidebar-context-menu>
		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { isFilesDragEvent, getFilesDragData } from '@/utils/files/dragDrop'
import SidebarContextMenu from './SidebarContextMenu.vue'

const HOVER_OPEN_DELAY = 700

export default {
	name: 'mount-list',
	mixins: [mixin],
	inject: ['filesController'],
	components: {
		SidebarContextMenu,
	},
	data() {
		return {
			isLoading: false,
			hasMergerFunction: false,
			usbStorageList: [],
			localStorageList: [],
			networkStorageList: [],
			cloudStorageList: [],
			dorpdown: false,
			mergeStorageList: [],
			testMergeMiss: 0,
			hover: false,
			dragHoverPath: null,
			hoverTimer: null,
		}
	},
	computed: {
		isMergeRowActive() {
			return '/DATA' === this.filesController.currentPath
		},
	},
	created() {
		this.getStorageList()
		this.checkMergerFunction()
	},

	mounted() {
		this.$EventBus.$on(events.RELOAD_MOUNT_LIST, this.getStorageList)
	},

	beforeDestroy() {
		// Unlike the legacy singleton sidebar, this component can be destroyed/recreated
		// whenever the Files sidebar toggles, so the listener must be removed.
		this.$EventBus.$off(events.RELOAD_MOUNT_LIST, this.getStorageList)
		clearTimeout(this.hoverTimer)
	},

	methods: {
		// Whether the "merge fs" (NivaroOS HD) feature is available on this box.
		async checkMergerFunction() {
			try {
				const hasMergeState = await this.$api.local_storage.getMergerfsInfo().then((res) => res.status)
				this.hasMergerFunction = hasMergeState == 200
			} catch (e) {
				console.error(e)
			}
		},

		getStorageList() {
			this.getLocalStorage()
			// this.getUsbStorage()
			this.getNetworkStorage()
			this.getCloudStorage()
		},
		// Local Storage (include Mergerfs)
		async getLocalStorage() {
			let mergeRes
			try {
				mergeRes = await this.$api.local_storage
					.getMergerfsInfo()
					.then((res) => res.data.data[0].source_volume_uuids)
			} catch (error) {
				mergeRes = []
				console.log(error)
			}

			// Local Storage
			try {
				const storageRes = await this.$api.storage.list()
				const storageArray = []
				const usbStorageArray = []
				storageRes.data.data.forEach((item) => {
					item.children.forEach((part) => {
						if (!mergeRes.find((mp) => mp === part.uuid))
							if (item.type === 'usb') {
								usbStorageArray.push(part)
							} else {
								storageArray.push(part)
							}
					})
				})
				this.localStorageList = storageArray.map((storage) => {
					return {
						name: storage.label,
						icon: 'storage-other',
						pack: 'casa',
						path: storage.mount_point,
						visible: true,
						selected: true,
						extensions: null,
					}
				})
				this.usbStorageList = usbStorageArray.map((storage) => {
					return {
						name: storage.label,
						icon: 'storage-USB',
						pack: 'casa',
						path: storage.mount_point,
						visible: true,
						selected: true,
						extensions: null,
					}
				})
			} catch (error) {
				this.isLoading = false
				console.log(error.response?.data?.message || error.message || error)
			}

			// Merger Storage
			try {
				this.mergeStorageList = []
				const storageRes = await this.$api.storage.list()
				let storageList = []
				storageRes.data.data.forEach((item) => {
					item.children.forEach((part) => {
						part.disk = item.path
						part.diskName = item.disk_name
						storageList.push(part)
					})
				})
				mergeRes.forEach((item) => {
					let storage = storageList.find((storage) => {
						return storage.uuid === item
					})
					if (storage) {
						this.mergeStorageList.push({
							uuid: storage.uuid,
							name: storage.label,
							icon: '',
							pack: 'casa',
							path: storage.mount_point,
							visible: true,
							selected: true,
							extensions: null,
						})
					} else {
						this.testMergeMiss += 1
						this.mergeStorageList.push({
							uuid: item,
							name: 'undefined',
							icon: 'danger',
							pack: 'casa',
							path: '',
							visible: true,
							selected: true,
							extensions: null,
						})
					}
				})
			} catch (error) {
				this.isLoading = false
				console.log(error.response?.data?.message || error.message || error)
			}
		},
		// Network Storage
		async getNetworkStorage() {
			try {
				const networkRes = await this.$api.samba.getConnections()
				this.networkStorageList = networkRes.data.data.map((storage) => {
					return {
						id: storage.id,
						name: storage.host,
						icon: 'storage-network',
						pack: 'casa',
						path: storage.mount_point,
						visible: true,
						selected: true,
						extensions: null,
					}
				})
			} catch (error) {
				this.isLoading = false
				console.log(error.response?.data?.message || error.message || error)
			}
		},
		// USB Storage
		async getUsbStorage() {
			try {
				const usbListRes = await this.$api.disks.getUsbs()
				const usbStorageArray = []
				usbListRes.data.data.forEach((item) => {
					item.children.forEach((part) => {
						usbStorageArray.push(part)
					})
				})
				this.usbStorageList = usbStorageArray.map((storage) => {
					return {
						name: storage.name,
						icon: 'storage-USB',
						pack: 'casa',
						path: storage.mount_point,
						visible: true,
						selected: true,
						extensions: null,
					}
				})
			} catch (error) {
				this.isLoading = false
				console.log(error.response?.data?.message || error.message || error)
			}
		},
		// Cloud Storage
		async getCloudStorage() {
			try {
				const cloudRes = await this.$api.cloud.list()
				this.cloudStorageList = cloudRes.data.data.map((storage) => {
					// Backend now returns an mdi icon name (e.g. "google-drive") rather
					// than a served SVG URL - render it as a normal icon, not an image.
					return {
						id: storage.fs,
						name: storage.name,
						icon: storage.icon || 'cloud-outline',
						icon_type: 'mdi',
						pack: 'mdi',
						path: storage.mount_point,
						visible: true,
						selected: true,
						extensions: null,
					}
				})
			} catch (error) {
				console.log(error.response?.data?.message || error.message || error)
			}
		},

		// umount cloud storage
		umountCloud(item) {
			this.$api.cloud
				.umount({ mount_point: item.path })
				.then(() => {
					this.getStorageList()
					this.goToDataFolder(item)
					this.$buefy.toast.open({
						message: this.$t('Eject Success'),
						type: 'is-success',
					})
				})
				.catch(() => {
					this.$buefy.toast.open({
						message: this.$t('Eject Failed'),
						type: 'is-danger',
					})
				})
		},

		// umount usb storage
		umountUsb(item) {
			this.$api.disks
				.umountUsb({ mount_point: item.path })
				.then(() => {
					this.getStorageList()
					this.goToDataFolder(item)
					this.$buefy.toast.open({
						message: this.$t('Eject Success'),
						type: 'is-success',
					})
				})
				.catch(() => {
					this.$buefy.toast.open({
						message: this.$t('Eject Failed'),
						type: 'is-danger',
					})
				})
		},

		// umount network storage
		umountNetwork(item) {
			this.$api.samba
				.deleteConnection(item.id)
				.then(() => {
					this.getStorageList()
					this.goToDataFolder(item)
					this.$buefy.toast.open({
						message: this.$t('Eject Success'),
						type: 'is-success',
					})
				})
				.catch(() => {
					this.$buefy.toast.open({
						message: this.$t('Eject Failed'),
						type: 'is-danger',
					})
				})
		},

		// go to DATA folder (adapted: filesController.navigate replaces filePanel.getFileList)
		goToDataFolder(item) {
			if (this.filesController.currentPath.startsWith(item.path)) {
				this.filesController.navigate('/DATA')
			}
		},

		async warning() {
			if (this.dorpdown) {
				this.dorpdown = false
				return
			}
			let notFirst = await this.$api.users
				.getCustomStorage('notFirstOpenMergerStorage')
				.then((res) => res.data.data)
			if (notFirst) {
				this.dorpdown = !this.dorpdown
				return
			}
			this.$buefy.dialog.confirm({
				title: this.$t('Data Protected'),
				message: this.$t('Changing internal files may break the structure of the NivaroOS HD'),
				confirmText: this.$t('Continue'),
				cancelText: this.$t('Cancel'),
				iconPack: 'casa',
				icon: 'danger',
				type: 'is-danger',
				hasIcon: true,
				onConfirm: () => {
					this.dorpdown = !this.dorpdown
					this.$api.users.setCustomStorage('notFirstOpenMergerStorage', true)
				},
			})
		},

		// Mirrors legacy TreeListItem's `isActived` computed, reading the injected
		// filesController.currentPath instead of the page-level isActive prop / $store.state.currentPath.
		isItemActive(item) {
			const currentPath = this.filesController.currentPath
			if (item.path === currentPath) {
				return true
			}
			if (item.path !== '/' && item.path !== '/DATA') {
				return currentPath.indexOf(`${item.path}/`) !== -1
			}
			return false
		},

		open(item) {
			this.filesController.navigate(item.path)
		},

		onDragOver(item, event) {
			if (!isFilesDragEvent(event)) return
			event.preventDefault()
			if (this.dragHoverPath === item.path) return
			this.dragHoverPath = item.path
			clearTimeout(this.hoverTimer)
			this.hoverTimer = setTimeout(() => {
				this.filesController.navigate(item.path)
			}, HOVER_OPEN_DELAY)
		},
		onDragLeave(item) {
			if (this.dragHoverPath !== item.path) return
			this.dragHoverPath = null
			clearTimeout(this.hoverTimer)
		},
		onDrop(item, event) {
			this.dragHoverPath = null
			clearTimeout(this.hoverTimer)
			if (!isFilesDragEvent(event)) return
			event.preventDefault()
			event.stopPropagation()
			const payload = getFilesDragData(event)
			if (!payload) return
			if (payload.from === item.path || payload.items.includes(item.path)) return
			this.$store.commit('SHOW_DRAG_DROP_MENU', { x: event.clientX, y: event.clientY, payload, targetPath: item.path })
		},
		onContextMenu(item, event) {
			if (!item || !item.path) return
			if (this.$refs.contextMenu) {
				this.$refs.contextMenu.open(event, { ...item, is_dir: true }, item.mountType)
			}
		},
		handleEject(item, mountType) {
			if (mountType === 'usb') {
				this.umountUsb(item)
			} else if (mountType === 'network') {
				this.umountNetwork(item)
			} else if (mountType === 'cloud') {
				this.umountCloud(item)
			}
		},
	},
	sockets: {
		'local-storage:disk:added'() {
			setTimeout(() => {
				// this.getUsbStorage()
				this.getLocalStorage()
			}, 500)
		},
		'local-storage:disk:removed'() {
			setTimeout(() => {
				// this.getUsbStorage()
				this.getLocalStorage()
			}, 500)
		},
		storage_status() {
			setTimeout(() => {
				this.$api.storage
					.list()
					.then((res) => {
						const storageArray = []
						res.data.data.forEach((item) => {
							item.children.forEach((part) => {
								storageArray.push(part)
							})
						})
						this.localStorageList = storageArray.map((storage) => {
							return {
								name: storage.label,
								icon: 'storage-other',
								pack: 'casa',
								path: storage.mount_point,
								visible: true,
								selected: true,
								extensions: null,
							}
						})
					})
					.catch((error) => {
						console.log(error.response?.data?.message || error.message || error)
					})
			}, 500)
		},
		'nivaroos:file:recover'(data) {
			data = data.Properties
			let toastType
			const reg = /^["|'](.*)["|']$/g
			const status = data.status.replace(reg, '$1')
			const driver = data.driver.replace(reg, '$1')
			switch (status) {
				case 'warn':
					toastType = 'is-warning'
					this.getCloudStorage()
					break
				case 'fail':
					toastType = 'is-danger'
					break
				default:
					toastType = 'is-success'
					if (driver === 'Dropbox') {
						this.$messageBus('files_addlocation_dropbox')
					} else if (driver === 'Google Drive') {
						this.$messageBus('files_addlocation_googledrive')
					} else if (driver === 'OneDrive') {
						this.$messageBus('files_addlocation_onedrive')
					}
					this.getCloudStorage()
					break
			}
			this.$buefy.toast.open({
				message: this.$t(data.message.replace(reg, '$1')),
				duration: 5000,
				type: toastType,
			})
		},
	},
}
</script>

<style lang="scss" scoped>
.mount-list {
	padding: 0.25rem 0.5rem;
}
.tree-node {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.35rem 0.5rem;
	border-radius: 6px;
	cursor: pointer;

	&:hover {
		background: rgba(0, 0, 0, 0.05);
	}
	&.active {
		background: rgba(50, 115, 220, 0.14);
		color: #3273dc;
		font-weight: 600;
	}
	&.drop-target {
		background: rgba(50, 115, 220, 0.25);
		outline: 2px solid #3273dc;
		outline-offset: -2px;
	}
}
.tree-node-icon {
	flex-shrink: 0;
	display: flex;
	align-items: center;
}
.tree-node-label {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-size: 0.85rem;
}
.tree-node-right-icon {
	flex-shrink: 0;
	display: flex;
	align-items: center;
}
.mount-sublist {
	padding-left: 1.25rem;
}
.warn {
	color: hsla(348, 86%, 61%, 1);
}
.hint {
	position: absolute;
	color: white;
	font-size: 10px;
	background-color: black;
	width: 15px;
	height: 15px;
	line-height: 13px;
	text-align: center;
	border-radius: 24px;
	border: 1px solid #ffffff;
	top: -0.5rem;
	left: 0.9rem;
}
</style>
