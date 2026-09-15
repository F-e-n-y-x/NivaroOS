<template>
	<div class="common-card is-flex is-align-items-center is-justify-content-center  app-card"
		@contextmenu.prevent.stop="handleCardContextMenu" @mouseleave="hover = true" @mouseover="hover = true">

		<!-- Desktop Context Menu (portal mounted to body on open) -->
		<div
			v-show="menuVisible"
			ref="menu"
			class="desktop-context-menu"
			:style="{ top: menuY + 'px', left: menuX + 'px' }"
			@contextmenu.prevent.stop
		>
			<!-- 1. Open / Launch / Import -->
			<button v-if="isSystemApp" class="ctx-item" @click="openApp(item)">
				<i class="mdi mdi-open-in-new ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open') }}</span>
			</button>
			<button v-else-if="isContainerApp && item.overrideUrl" class="ctx-item" @click="openApp(item)">
				<i class="mdi mdi-open-in-new ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open') }}</span>
			</button>
			<button v-if="isContainerApp" class="ctx-item" @click="closeMenuThen('importApp', item, false)">
				<i class="mdi mdi-download-box-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Import to NivaroOS') }}</span>
			</button>
			<button v-else-if="!isSystemApp && item.status === 'running'" class="ctx-item" @click="openApp(item)">
				<i class="mdi mdi-open-in-new ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open') }}</span>
			</button>
			<button v-else-if="!isSystemApp && !isContainerApp" class="ctx-item" @click="openApp(item)">
				<i class="mdi mdi-play-circle-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('launch-and-open') }}</span>
			</button>

			<!-- 2. Tips & Settings -->
			<button v-if="isV2App" class="ctx-item" @click="openTips(item.name)">
				<i class="mdi mdi-lightbulb-on-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Tips') }}</span>
			</button>
			<button v-if="isV2App || isLinkApp" class="ctx-item" @click="configApp()">
				<i class="mdi mdi-tune-variant ctx-icon"></i>
				<span class="ctx-label">{{ $t('Setting') }}</span>
			</button>

			<!-- 3. Logs & Terminal (for containers / v1 / v2) -->
			<button v-if="isContainerApp || isV2App || isV1App" class="ctx-item" @click="openContainerConsole('logs')">
				<i class="mdi mdi-text-box-search-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Logs') }}</span>
			</button>
			<button v-if="isContainerApp || isV2App || isV1App" class="ctx-item" @click="openContainerConsole('terminal')">
				<i class="mdi mdi-console ctx-icon"></i>
				<span class="ctx-label">{{ $t('Terminal') }}</span>
			</button>

			<!-- 4. Edit (Rename / icon / roundness) - available for all apps -->
			<button class="ctx-item" @click="closeMenuThen('editLegacyApp', item)">
				<i class="mdi mdi-pencil-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Edit') }}</span>
			</button>

			<!-- 5. Advanced Container Management (update check, compose export, rebuild) -->
			<button v-if="isV2App && !item.is_uncontrolled" class="ctx-item" :disabled="isCheckThenUpdate || isUpdating" @click="checkAppVersion(item.name)">
				<i class="mdi mdi-update ctx-icon"></i>
				<span class="ctx-label">{{ $t('Check then update') }}</span>
				<i v-if="isCheckThenUpdate || isUpdating" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
			</button>
			<button v-if="isV1App" class="ctx-item" @click="exportYAML(item)">
				<i class="mdi mdi-file-export-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Export as Compose') }}</span>
			</button>
			<button v-if="isV1App" class="ctx-item" :disabled="isRebuilding" @click="rebuild(item)">
				<i class="mdi mdi-hammer-wrench ctx-icon"></i>
				<span class="ctx-label">{{ $t('Rebuild') }}</span>
				<i v-if="isRebuilding" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
			</button>

			<div class="ctx-divider"></div>

			<!-- 6. Pinning & Folders -->
			<button class="ctx-item" @click="togglePin">
				<i :class="isPinned ? 'mdi mdi-pin-off-outline ctx-icon' : 'mdi mdi-pin-outline ctx-icon'"></i>
				<span class="ctx-label">{{ isPinned ? $t('Unpin from taskbar') : $t('Pin to taskbar') }}</span>
			</button>
			<button v-if="!folderId" class="ctx-item" @click="closeMenuThen('addToFolder', item)">
				<i class="mdi mdi-folder-plus-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Add to folder') }}</span>
			</button>
			<button v-if="folderId" class="ctx-item" @click="closeMenuThen('removeFromFolder', { item, folderId })">
				<i class="mdi mdi-folder-remove-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Remove from folder') }}</span>
			</button>

			<!-- 7. Lifecycle (Restart, Start / Stop) -->
			<template v-if="!isLinkApp && !isSystemApp">
				<div class="ctx-divider"></div>
				<button class="ctx-item" :disabled="item.status !== 'running' || isRestarting" @click="restartApp">
					<i class="mdi mdi-restart ctx-icon"></i>
					<span class="ctx-label">{{ $t('Restart') }}</span>
					<i v-if="isRestarting" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
				</button>
				<button class="ctx-item" :class="{ 'is-danger': item.status === 'running' }" :disabled="isStarting" @click="toggle(item)">
					<i :class="item.status === 'running' ? 'mdi mdi-stop-circle-outline ctx-icon' : 'mdi mdi-play-circle-outline ctx-icon'"></i>
					<span class="ctx-label">{{ item.status === 'running' ? $t('Stop') : $t('Start') }}</span>
					<i v-if="isStarting" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
				</button>
			</template>

			<!-- 8. Deletion & Uninstall (Destructive actions at bottom) -->
			<template v-if="isLinkApp">
				<div class="ctx-divider"></div>
				<button class="ctx-item is-danger" :disabled="isUninstalling" @click="uninstallApp(true)">
					<i class="mdi mdi-delete-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Delete') }}</span>
					<i v-if="isUninstalling" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
				</button>
			</template>
			<template v-else-if="!isContainerApp && !isSystemApp">
				<div class="ctx-divider"></div>
				<button class="ctx-item is-danger" :disabled="isUninstalling" @click="uninstallConfirm">
					<i class="mdi mdi-trash-can-outline ctx-icon"></i>
					<span class="ctx-label">{{ $t('Uninstall') }}</span>
					<i v-if="isUninstalling" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
				</button>
			</template>
		</div>
		<div class="blur-background"></div>
		<div class="cards-content" @click="handleCardClick(item, $event)" @dblclick="handleCardDblClick(item, $event)">
			<!-- Card Content Start -->
			<b-tooltip :always="isActiveTooltip" :animated="true" :label="tooltipLabel" :triggers="tooltipTriger"
				animation="fade1" class="in-card" type="is-white">

				<div class="has-text-centered is-flex is-justify-content-center is-flex-direction-column icon-cell">
					<div class="is-flex is-justify-content-center">
						<div class="is-relative">
							<b-image :class="dotClass(item.status, isLoading)"
								:style="item.iconRadius ? { borderRadius: item.iconRadius + '%', overflow: 'hidden' } : null"
								:src="item.icon" :src-fallback="require('@/assets/img/app-icons/default.svg')" class="is-52x52"
								webp-fallback=".jpg"></b-image>
							<!-- Unstable-->
							<cTooltip v-if="newAppIds.includes(item.name)" class="__position" content="NEW"></cTooltip>
						</div>

						<!-- Loading Bar Start -->
						<b-loading :active="isLoading" :can-cancel="false" :is-full-page="false"
							class="has-background-gray-800 op80 is-52x52"
							style="top: auto;bottom: auto; right: auto; left: auto; border-radius: 10px">
							<img :src="require('@/assets/img/loading/waiting-white.svg')" alt="loading" class="is-20x20" />
						</b-loading>
						<!-- Loading Bar End -->
					</div>

					<p class="app-label one-line">
						<a class="one-line" style="cursor:default">
							{{ i18n(item.title) }}
						</a>
					</p>

				</div>
			</b-tooltip>
			<!-- Card Content End -->
		</div>

		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import events from '@/events/events';
import { escapeHtml } from '@/utils/escapeHtml';
import cTooltip from '@/shared/basicComponents/tooltip/tooltip.vue';
import business_ShowNewAppTag from "@/mixins/app/Business_ShowNewAppTag";
import business_OpenThirdApp from "@/mixins/app/Business_OpenThirdApp";
import business_LinkApp from "@/mixins/app/Business_LinkApp";
import business_DockPins from "@/mixins/app/Business_DockPins";
import isNull from "lodash/isNull";
import tipEditorModal from "@/apps/app-store/TipEditorModal.vue";
import YAML from "yaml";
import commonI18n, { ice_i18n } from "@/mixins/base/common-i18n";
import FileSaver from 'file-saver';
import { confirmWindowMixin } from '@/mixins/confirmWindow';

export default {
	name: "app-card",
	components: {
		cTooltip,
		tipEditorModal,
	},
	mixins: [business_ShowNewAppTag, business_OpenThirdApp, business_LinkApp, business_DockPins, commonI18n, confirmWindowMixin],
	inject: ["homeShowFiles", "openAppStore"],
	data() {
		return {
			hover: false,
			dropState: false,
			menuVisible: false,
			menuX: 0,
			menuY: 0,
			isUninstalling: false,
			isCloning: false,
			isCheckThenUpdate: false,
			isUpdating: false,
			isRestarting: false,
			isStarting: false,
			isRebuilding: false,
			// isStoping: false,
			// Public. Only changes the state of the card, not the state of the button.
			isSaving: false,
			isActiveTooltip: false,
			dropdownPosition: "is-bottom-right",
			isPinned: false,
		}
	},
	props: {
		item: {
			type: Object
		},
		// Set when this card is rendered inside a folder's contents view -
		// swaps "Add to folder" for "Remove from folder" in the menu.
		folderId: {
			type: String,
			default: null
		},
	},

	computed: {
		tooltipLabel() {
			if (this.isContainerApp) {
				// No hover tooltip for the plain "Open" case - it's
				// redundant with the icon/label itself and just clutters
				// the hover state.
				return '';
			} else if (this.item.app_type === "system") {
				return '';
			} else if (this.isUpdating) {
				return this.$t('Updating');
			} else if (this.isUninstalling) {
				return this.$t('Uninstalling');
			} else if (this.isCloning) {
				return this.$t('Cloning');
			} else if (this.isRestarting) {
				return this.$t('Restarting');
			} else if (this.isStarting) {
				return this.$t('updateState');
			} else if (this.isRebuilding) {
				return this.$t('Rebuilding');
			} else if (this.isCheckThenUpdate) {
				return this.$t('CheckThenUpdate');
			} else if (this.item.status === 'running') {
				return '';
			} else {
				return '';
			}
		},
		tooltipTriger() {
			return this.tooltipLabel ? ['hover'] : [];
		},
		isLoading() {
			let active = this.isUninstalling || this.isUpdating || this.isRestarting || this.isStarting || this.isSaving || this.isRebuilding // || this.isStoping || this.isSaving
			return active
		},
		isV1App() {
			return this.item.app_type === "v1app"
		},
		isV2App() {
			return this.item.app_type === "v2app"
		},
		isContainerApp() {
			return this.item.app_type === "container"
		},
		isLinkApp() {
			return this.item.app_type === "LinkApp"
		},
		isSystemApp() {
			return this.item.app_type === "system"
		},
		shutDownClass() {
			return this.item.status !== 'running'? "shutdown-rounded": ""
		},

	},

	created() {
		this.checkPinStatus()
		this.$EventBus.$on(events.RELOAD_APP_LIST, this.checkPinStatus)
	},

	mounted() {
		this.handleCloseOtherMenus = sender => {
			if (sender !== this) {
				this.closeMenu()
			}
		}
		this.$EventBus.$on('CLOSE_ALL_CONTEXT_MENUS', this.handleCloseOtherMenus)
	},

	beforeDestroy() {
		this.closeMenu()
		this.$EventBus.$off(events.RELOAD_APP_LIST, this.checkPinStatus)
		this.$EventBus.$off('CLOSE_ALL_CONTEXT_MENUS', this.handleCloseOtherMenus)
		if (this.$refs.menu && this.$refs.menu.parentNode) {
			this.$refs.menu.parentNode.removeChild(this.$refs.menu)
		}
	},

	watch: {
		isLoading(active) {
			// design :: The first display is three seconds long
			if (this.isCheckThenUpdate && this.activeTimer === undefined) {
				this.activeTimer = setTimeout(() => {
					this.isActiveTooltip = false;
					clearTimeout(this.activeTimer);
					this.activeTimer = undefined;
				}, 3000)
				this.isActiveTooltip = true;
			} else if (active === false && this.isCheckThenUpdate === false && this.activeTimer) {
				clearInterval(this.activeTimer);
				this.activeTimer = undefined;
				this.isActiveTooltip = false;
			}
		},
	},

	methods: {
		checkPinStatus() {
			this.getDockPins().then(pins => {
				const name = this.item.name || this.item.label || this.item.id
				this.isPinned = pins.includes(name) || pins.includes(this.item.name)
			})
		},

		handleDorpdownPosition(event) {
			this.$nextTick(() => {
				const rightOffset = window.innerWidth - event.clientX - 160
				const horizontalPos = rightOffset > 0 ? "right" : "left"
				const bottomOffset = window.innerHeight - event.clientY - 212
				const verticalPos = bottomOffset > 0 ? "bottom" : "top"
				this.dropdownPosition = `is-${verticalPos}-${horizontalPos}`
			})
		},

		handleCardContextMenu(event) {
			if (event) {
				event.preventDefault()
				event.stopPropagation()
			}
			if (this.isUninstalling) return

			this.$EventBus.$emit('CLOSE_ALL_CONTEXT_MENUS', this)
			this.checkPinStatus()

			const targetContainer = document.fullscreenElement || document.body
			if (this.$refs.menu && this.$refs.menu.parentNode !== targetContainer) {
				targetContainer.appendChild(this.$refs.menu)
			}

			const MENU_WIDTH = 224
			const clientX = event ? event.clientX : window.innerWidth / 2
			const clientY = event ? event.clientY : window.innerHeight / 2

			let x = Math.max(12, Math.min(window.innerWidth - MENU_WIDTH - 16, clientX))
			let y = Math.max(12, clientY)

			this.menuX = x
			this.menuY = y
			this.menuVisible = true

			this.addEventListeners()

			this.$nextTick(() => {
				if (!this.$refs.menu) return
				const rect = this.$refs.menu.getBoundingClientRect()
				const maxBottom = window.innerHeight - 80 // Above taskbar dock
				if (rect.bottom > maxBottom) {
					const adjustedY = Math.max(12, clientY - rect.height)
					this.menuY = Math.min(adjustedY, maxBottom - rect.height)
				}
				if (rect.right > window.innerWidth - 12) {
					this.menuX = Math.max(12, window.innerWidth - rect.width - 12)
				}
			})
		},

		closeMenu() {
			if (!this.menuVisible) return
			this.menuVisible = false
			this.removeEventListeners()
		},

		closeMenuThen(eventName, ...args) {
			this.closeMenu()
			this.$emit(eventName, ...args)
		},

		onOutsideClick(event) {
			if (this.menuVisible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.closeMenu()
			}
		},

		onKeyDown(event) {
			if (event.key === 'Escape' && this.menuVisible) {
				this.closeMenu()
			}
		},

		addEventListeners() {
			document.addEventListener('mousedown', this.onOutsideClick)
			document.addEventListener('keydown', this.onKeyDown)
			window.addEventListener('blur', this.closeMenu)
			window.addEventListener('resize', this.closeMenu)
			window.addEventListener('scroll', this.closeMenu, true)
		},

		removeEventListeners() {
			document.removeEventListener('mousedown', this.onOutsideClick)
			document.removeEventListener('keydown', this.onKeyDown)
			window.removeEventListener('blur', this.closeMenu)
			window.removeEventListener('resize', this.closeMenu)
			window.removeEventListener('scroll', this.closeMenu, true)
		},

		togglePin() {
			const next = !this.isPinned
			this.isPinned = next
			const name = this.item.name || this.item.label || this.item.id
			this.setDockPinned(name, next).then(() => {
				this.$EventBus.$emit(events.RELOAD_APP_LIST)
				this.$buefy.toast.open({
					message: next
						? `<i class="mdi mdi-pin-outline mr-1"></i> ${this.$t('Pinned to taskbar')}`
						: `<i class="mdi mdi-pin-off-outline mr-1"></i> ${this.$t('Removed from taskbar')}`,
					type: 'is-dark',
					position: 'is-top',
					duration: 2000,
					queue: false
				})
			})
			this.closeMenu()
		},

		/**
		 * @description: Open app in new windows
		 * @param {String} status App status
		 * @param {String} port App access port
		 * @param {String} index App access index
		 * @return {*} void
		 */
		handleCardClick(item, e) {
			if (this.$store.state.isMobile) {
				this.openApp(item)
			}
		},
		handleCardDblClick(item, e) {
			this.openApp(item)
		},
		openApp(item) {
			this.closeMenu()
			if (this.isContainerApp) {
				// Importing is an explicit menu action now (see the
				// context menu item) rather than triggered by clicking
				// the icon. Clicking opens the custom URL set via Edit, if
				// any, otherwise does nothing.
				if (item.overrideUrl) {
					window.open(item.overrideUrl, '_blank')
				}
				return false
			}
			if (item.app_type === "system") {
				this.openSystemApps(item)
			} else if (this.isLinkApp) {
				window.open(item.hostname, '_blank');
				this.removeIdFromSessionStorage(item.name);
			} else {
				// type is one of 'official' or 'community'.
				if (item.status === 'running') {
					this.openAppToNewWindow(item)
				} else {
					this.toggle(item)
					this.firstOpenThirdApp(item)
				}
			}
		},

		openSystemApps(item) {
			switch (item.name) {
				case "App Store":
					this.$store.commit('OPEN_WINDOW', {
						id: 'appstore', title: this.$t('App Store'), component: 'AppStoreApp', width: 1040, height: 720
					})
					break;
				case "Files":
					this.$store.commit('OPEN_WINDOW', {
						id: 'files', title: this.$t('Files'), component: 'FilesApp', width: 960, height: 620
					})
					break;
				case "Settings":
					this.$store.commit('OPEN_WINDOW', {
						id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540
					})
					break;
				case "Terminal":
					this.$store.commit('OPEN_WINDOW', {
						id: 'terminal', title: this.$t('Terminal'), component: 'TerminalPanel', width: 720, height: 480
					})
					break;
				case "VMs":
					this.$store.commit('OPEN_WINDOW', {
						id: 'vms', title: this.$t('VMs'), component: 'VmManagerApp', width: 880, height: 560
					})
					break;
				case "Host Desktop":
					this.$store.commit('OPEN_WINDOW', {
						id: 'host-desktop', title: this.$t('Host Desktop'), component: 'HostDesktopPanel', width: 1024, height: 680
					})
					break;
				default:
					break;
			}
		},

		/**
		 * @description: Set drop-down menu status
		 * @param {Boolean} e
		 * @return {*} void
		 */
		setDropState(e) {
			this.dropState = e
		},

		openContainerConsole(initialTab = 'terminal') {
			this.closeMenu();
			const containerId = this.item.name || this.item.id;
			let containerName = this.item.title || this.item.name || containerId;
			if (typeof containerName === 'object' && containerName !== null) {
				containerName = containerName.custom || containerName.en_us || containerName['en_US'] || Object.values(containerName)[0] || this.item.name || containerId;
			} else if (typeof containerName === 'string' && containerName.trim().startsWith('{')) {
				try {
					const p = JSON.parse(containerName);
					if (typeof p === 'object' && p !== null) {
						containerName = p.custom || p.en_us || p.en_US || Object.values(p)[0] || this.item.name || containerId;
					}
				} catch (e) {}
			}
			const containerImage = this.item.image || '';
			const status = this.item.status || 'running';
			this.$store.commit('OPEN_WINDOW', {
				id: `container-console-${containerId}`,
				title: `${containerName} - ${initialTab === 'logs' ? this.$t('Logs') : this.$t('Terminal')}`,
				component: 'ContainerConsolePanel',
				width: 860,
				height: 560,
				props: {
					containerId: containerId,
					containerName: containerName,
					containerImage: containerImage,
					initialTab: initialTab,
					status: status
				}
			});
		},

		/**
		 * @description: Restart Application
		 * @return {*} void
		 */
		restartApp() {
			this.$messageBus('apps_restart', this.item.name);
			this.isRestarting = true
			if (this.isV2App) {
				this.restartAppV2();
			} else if (this.isV1App || this.isContainerApp) {
				this.restartAppV1();
			}
			this.closeMenu();
		},

		restartAppV1() {
			this.$api.container.updateState(this.item.name, "restart").then((res) => {
				if (res.data.success === 200) {
					this.updateState()
				}
			}).catch((err) => {
				this.$buefy.toast.open({
					message: err.response.data.data || err.response.data.message,
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
			}).finally(() => {
				this.isRestarting = false;
			})
		},

		restartAppV2() {
			this.$openAPI.appManagement.compose.setComposeAppStatus(this.item.name, "restart").then((res) => {
				this.updateState()
			}).catch((err) => {
				this.$buefy.toast.open({
					message: err.response.data.data || err.response.data.message,
					type: 'is-danger',
					position: 'is-top',
					duration: 5000
				})
			})
		},

		/**
		 * @description: Confirm before uninstall
		 * @return {*} void
		 */
		uninstallConfirm() {
			this.$messageBus('apps_uninstall', this.item.name);
			this.closeMenu();
			this.confirmWindow({
				title: this.$t('Attention'),
				message: this.$t(`Data cannot be recovered after deletion! <br/>Continue on to uninstall this application?<br/>{divS}Delete userdata ( config folder ){divE}`, {
					divS: `<div class="is-flex is-align-items-center mt-4"><input type="checkbox"  id="checkDelConfig">`,
					divE: `</input></div>`
				}),
				type: 'is-dark',
				confirmText: this.$t('Uninstall'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					let checkDelConfig = document.getElementById("checkDelConfig") ? document.getElementById("checkDelConfig").checked : false;
					this.uninstallApp(checkDelConfig)
				}
			})
		},

		/**
		 * @description: Uninstall app
		 * @return {*} void
		 */
		uninstallApp(checkDelConfig) {
			this.closeMenu();
			this.isUninstalling = true
			this.removeIdFromSessionStorage(this.item.name);
			if (this.isLinkApp) {
				this.deleteLinkAppByName(this.item.name).then(res => {
					if (res.data.success === 200) {
						this.$EventBus.$emit(events.RELOAD_APP_LIST);
					}
				})
			} else if (this.isV2App) {
				this.$openAPI.appManagement.compose.uninstallComposeApp(this.item.name, checkDelConfig).then((res) => {
					if (res.status === 200) {
						this.$EventBus.$emit(events.UPDATE_SYNC_STATUS);
					}
				}).catch((err) => {
					this.$buefy.toast.open({
						message: err.response.data.data,
						type: 'is-danger',
						position: 'is-top',
						duration: 5000
					})
				})
			} else {
				// former app uninstall
				this.$api.container.uninstall(this.item.name, { 'delete_config_folder': checkDelConfig }).then((res) => {
					if (res.data.success === 200) {
						this.$EventBus.$emit(events.UPDATE_SYNC_STATUS);
					}
				}).catch((err) => {
					this.$buefy.toast.open({
						message: err.response.data.data,
						type: 'is-danger',
						position: 'is-top',
						duration: 5000
					})
				})
			}

		},

		/**
		 * @description: Emit the event that the app has been updated
		 * @return {*} void
		 */
		updateState() {
			this.closeMenu();
			this.$emit("updateState")
			this.$EventBus.$emit(events.UPDATE_SYNC_STATUS);
		},

		async openTips(name) {
			try {
				const ret = await this.$openAPI.appManagement.compose.myComposeApp(name, {
					headers: {
						'content-type': 'application/yaml',
						'accept': 'application/yaml'
					}
				}).then(res => res.data)
				this.closeMenu();
				this.$buefy.modal.open({
					parent: this,
					component: tipEditorModal,
					hasModalCard: true,
					customClass: 'network-storage-modal',
					trapFocus: true,
					canCancel: [],
					// scroll: "keep",
					animation: "zoom-in",
					props: {
						composeData: YAML.parse(ret),
						name
					}
				})
			} catch (e) {
				console.log('openTips Error:', e)
			}
		},

		/**
		 * @description: Emit the event that the app has been updated with custom_id
		 * @return {*} void
		 */
		configApp() {
			this.$messageBus('apps_setting', this.item.name);
			this.closeMenu();
			this.$emit("configApp", this.item, this.isV2App);
		},

		/**
		 * @description: Start or Stop App
		 * @param {Object} item the app info object
		 * @return {*} void
		 */
		toggle(item) {
			// only have 'apps_stop' event
			this.$messageBus('apps_stop', item.name);
			this.isStarting = true;
			const status = item.status === "running" ? "stop" : "start"
			if (this.isV2App) {
				this.toggleAppV2(item, status);
			} else if (this.isV1App || this.isContainerApp) {
				this.toggleAppV1(item, status);
			}
			this.closeMenu();
		},

		toggleAppV1(item, status) {
			this.$api.container.updateState(item.name, status).then((res) => {
				if (res.data.success === 200) {
					item.status = res.data.data
					this.updateState()
				} else {
					this.confirmWindow({
						title: 'Error',
						message: res.data.data || res.data.message,
						type: 'is-danger',
						cancelText: ''
					})
				}
			}).catch((err) => {
				this.$buefy.toast.open({
					message: err.response.data.data || err.response.data.message,
					type: 'is-danger',
					position: 'is-top',
					duration: 3000
				})
			}).finally(() => {
				this.isStarting = false
			})
		},

		toggleAppV2(item, status) {
			this.$openAPI.appManagement.compose.setComposeAppStatus(item.name, status).then((res) => {
				this.updateState()
				item.status = status
			}).catch((err) => {
				this.confirmWindow({
					title: 'Error',
					message: err.response.data.data || err.response.data.message,
					type: 'is-danger',
					cancelText: ''
				})
			})
		},

		appClone(name) {
			this.isCloning = true;
			this.$api.apps.getAppInfo(name).then(resp => {
				if (resp.data.success == 200) {
					let respData = resp.data.data
					// messageBus :: apps_clone
					this.$messageBus('apps_clone', this.item.name.toString());

					let initData = {}
					initData.protocol = respData.protocol
					initData.host = respData.host
					initData.port_map = respData.port_map
					initData.cpu_shares = 50
					initData.memory = respData.max_memory
					initData.restart = "always"
					initData.label = respData.title
					initData.position = true
					initData.index = respData.index
					initData.icon = respData.icon
					initData.network_model = respData.network_model
					initData.image = respData.image
					initData.description = respData.description
					initData.origin = respData.origin
					initData.ports = isNull(respData.ports) ? [] : respData.ports
					initData.volumes = isNull(respData.volumes) ? [] : respData.volumes
					initData.envs = isNull(respData.envs) ? [] : respData.envs
					initData.devices = isNull(respData.devices) ? [] : respData.devices
					initData.cap_add = isNull(respData.cap_add) ? [] : respData.cap_add
					initData.cmd = isNull(respData.cmd) ? [] : respData.cmd
					initData.privileged = respData.privileged
					initData.host_name = respData.host_name
					initData.appstore_id = name

					this.$api.container.install(initData).catch((err) => {
						this.$buefy.toast.open({
							message: err.response.data.message,
							type: 'is-warning'
						})
					}).then(() => {
						this.isCloning = false;
						this.closeMenu();
					})
				}
			}).catch(() => {
				this.$buefy.toast.open({
					message: this.$t(`There was an error loading the data, please try again!`),
					type: 'is-danger'
				})
			})
		},

		exportYAML(item) {
			this.closeMenu();
			this.$api.container.exportAsCompose(item.name).then(res => {
				const blob = new Blob([res.data], { type: '' });
				FileSaver.saveAs(blob, `${item.image}.yaml`);
			}).catch((err) => {
				this.$buefy.toast.open({
					message: err.response.data.message,
					type: 'is-warning'
				})
			})
		},

		async rebuild(app) {
			this.closeMenu();
			this.isRebuilding = true;
			try {
				// 1. get yaml
				const file = await this.$api.container.exportAsCompose(app.name).then(res => res.data)
				// 2. archive
				await this.$api.container.archive(app.name)
				// 3.install compose
				await this.$openAPI.appManagement.compose.installComposeApp(file, { name: app.name })
			} catch (e) {
				this.isRebuilding = false;
				console.error('rebuild Error:', e)
				this.$buefy.toast.open({
					message: this.$t(`Rebulid error`),
					type: 'is-danger'
				})
			}
			// 4.sockiet :: install-end :: change UI status.
			// this.isRebuilding = false;
			this.closeMenu();
		},

		checkAppVersion(name) {
			this.closeMenu();
			this.isCheckThenUpdate = true;
			this.$openAPI.appManagement.compose.updateComposeApp(name).then(resp => {
				// 200:
				if (resp.status === 200) {
					// messageBus :: apps_checkThenUpdate
					this.$messageBus('apps_checkupdate', this.item.name.toString());
					this.$buefy.toast.open({
						// value is `In the process of asynchronous updating.` or `compose app `app Name` is up to date`
						message: resp.data.message,
						type: 'is-success'
					})

				} else {
					this.$buefy.toast.open({
						message: this.$t(`No updates are currently available for the application.`),
						type: 'is-success'
					})
				}
			}).catch(() => {
				this.$buefy.toast.open({
					message: this.$t(`Unable to update at the moment!`),
					type: 'is-danger'
				})
			}).finally(() => {
				this.closeMenu();
				this.isCheckThenUpdate = false;
			})
		},
		/**
		 * @description: Format Dot Class
		 * @param {String} status
		 * @return {String}
		 */
		dotClass(status, loadState) {
			// For updating
			if (loadState) {
				if (status === "0" || status === "running") {
					return 'disabled start'
				}
				return 'disabled stop'
			}
			if (status === "0") {
				return "start"
			} else {
				return status === 'running' ? 'start' : 'stop'
			}

		},

	},

	sockets: {
		"app:start-error"(res) {
			// toast info.
			this.$buefy.toast.open({
				message: res.Properties["message"],
				duration: 5000,
				type: "is-danger",
			})
		},
		"app:start-end"(res) {
			if (res.Properties["app:name"] === this.item.name) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:stop-error"(res) {
			// toast info.
			this.$buefy.toast.open({
				message: res.Properties["message"],
				duration: 5000,
				type: "is-danger",
			})
		},
		"app:stop-end"(res) {
			if (res.Properties["app:name"] === this.item.name) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:restart-error"(res) {
			// toast info.
			this.$buefy.toast.open({
				message: res.Properties["message"],
				duration: 5000,
				type: "is-danger",
			})
		},
		"app:restart-end"(res) {
			if (res.Properties["app:name"] === this.item.name) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:apply-changes-begin"(res) {
			if (res.Properties["app:name"] === this.item.name) {
				this.isSaving = true
			}
		},
		"app:apply-changes-error"(res) {
			// toast info.
			this.$buefy.toast.open({
				message: res.Properties["message"],
				duration: 5000,
				type: "is-danger",
			})
		},
		"app:apply-changes-end"(res) {
			if (res.Properties["app:name"] === this.item.name) {
				this.isRestarting = false
				this.isStarting = false
				this.isSaving = false
			}
		},
		/**
		 * @description: Update App Status
		 * @param {Object} data
		 * @return {void}
		 */
		'app:update-begin'() {
			
		},

		'docker:image:pull-end'(data) {
			if (data.Properties["app:name"] === this.item.name) {
				if (data.Properties['docker:image:updated'] === 'true') {
					this.isUpdating = true;
				}
				this.isCheckThenUpdate = false;
			}
		},

		'docker:image:pull-error'(data) {
			if (data.Properties["app:name"] === this.item.name) {
				this.isCheckThenUpdate = false;
			}
		},

		/**
		 * @description: Update App Version
		 * @param {Object} data
		 * @return {void}
		 */
		'app:update-end'(data) {
			if (data.Properties["app:name"] !== this.item.name)
				return
			if (data.Properties['docker:image:updated'] === 'true') {
				return
			}
			this.isUpdating = false;
			// item.name comes from the installed app's manifest, not
			// developer-authored text - escape before it hits toast.open's
			// v-html-rendered message.
			this.$buefy.toast.open({
				message: this.$t(`{appName} is the latest version!`, { appName: escapeHtml(this.item.name) }),
				type: 'is-success',
				duration: 5000
			})
		},
		"app:install-end"(res) {
			if (res.Properties["dry_run.name"] === this.item.name) {
				// 4.sockiet :: install-end :: change UI status.
				this.isRebuilding = false;
				// 5.message toast
				this.$buefy.toast.open({
					message: this.$t(`{title} rebulid completed`, { title: escapeHtml(ice_i18n(this.item.title)) }),
					type: 'is-success'
				})
			}
		},
		"app:install-error"(res) {
			if (res.Properties["dry_run.name"] === this.item.name) {
				// 4.sockiet :: install-end :: change UI status.
				this.isRebuilding = false;
				// 5.message toast
				this.$buefy.toast.open({
					message: res.Properties["message"],
					type: 'is-warning'
				})
			}
		},
		"app:uninstall-error"(res) {
			if (res.Properties['id'] === this.item.name) {
				this.isUninstalling = false;
			}
		},
	}

}
</script>

<style lang="scss">
.pb-3px {
	padding-bottom: var(--space-1);
}

.shutdown-rounded {
	border-radius: 50%;
	background-color: #000;
	color: #fff;
}



.in-card.b-tooltip {
	&.is-top .tooltip-content {
		bottom: auto;
		top: -15%;
	}

	.tooltip-content {
		box-shadow: none;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-control);
		font-family: $family-sans-serif;
		font-style: normal;
		line-height: 1.25rem;
		font-feature-settings: 'pnum' on, 'lnum' on;

		color: hsla(208, 20%, 20%, 1);

	}

}

.__position {
	position: absolute !important;
	top: -0.75rem !important;
	left: 3rem !important;
	z-index: 30;
}

// 0.4.4
.dropdown.is-right .dropdown-menu {
	top: 0;
	left: calc(100% + 6px);
}

.dropdown.is-left .dropdown-menu {
	top: 0;
	left: calc(-100% - 14px);
}
</style>
<style lang="scss">
.dialog {
	.modal-card-head {
		padding-left: var(--space-6);
		padding-top: var(--space-6);
		padding-bottom: var(--space-3);
		border: 1px solid var(--theme-card-border);
	}

	.modal-card-body {
		padding: var(--space-4) var(--space-6) var(--space-6);

		#checkDelConfig {
			margin-right: var(--space-2);
			height: 1.25rem;
			width: 1.25rem;
		}

		border: 1px solid var(--theme-card-border);
	}

	.modal-card-foot {
		padding-top: var(--space-3);
		padding-bottom: var(--space-6);
		padding-right: var(--space-6);


		font-size: var(--font-base);
		font-weight: 400;
		line-height: 20px;
		letter-spacing: 0;
		text-align: left;

		.button {
			margin-right: 0;
		}

		.is-dark {
			margin-left: var(--space-4);
			background: hsla(208, 100%, 45%, 1);
		}
	}
}

.icon-cell {
	width: 100%;
	height: 100%;
	padding: var(--space-2) var(--space-1) var(--space-1);
	box-sizing: border-box;
	justify-content: center;
	gap: 0;
}

.app-label {
	margin-top: var(--space-1);
	font-size: var(--font-xs);
	font-weight: 500;
	color: #fff;
	text-shadow: 0 1px 3px rgba(0, 0, 0, 0.85);
	line-height: 1.2;
	max-width: 84px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	// The label text sits inside a bare <a> (see template). A non-!important
	// `color: inherit` here previously lost a same-specificity tie against
	// _card.scss's `.common-card a { color: white }` depending on runtime
	// style-injection order (not the static bundle's byte order) - forcing
	// it with !important removes that ambiguity instead of relying on which
	// happens to load last.
	a {
		color: #fff !important;
		text-decoration: none;
	}
}
</style>
