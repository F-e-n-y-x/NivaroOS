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
			<button v-if="isV2App && !item.is_uncontrolled" class="ctx-item" :disabled="isCheckThenUpdate || isUpdating" @click="updateAppConfirm">
				<i class="mdi mdi-update ctx-icon"></i>
				<span class="ctx-label">{{ $t('Update') }}</span>
				<i v-if="isCheckThenUpdate || isUpdating" class="mdi mdi-loading mdi-spin ctx-spinner"></i>
			</button>
			<button v-if="isV1App" class="ctx-item" @click="exportYAML(item)">
				<i class="mdi mdi-file-export-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Export as Compose') }}</span>
			</button>
			<button v-if="isV1App" class="ctx-item" :disabled="isRebuilding" @click="rebuildConfirm(item)">
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
				<button class="ctx-item is-danger" :disabled="isUninstalling" @click="deleteLinkConfirm">
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
		<div class="cards-content" role="button" tabindex="0" :aria-label="cardAriaLabel" aria-haspopup="menu"
			@click="handleCardClick(item, $event)" @dblclick="handleCardDblClick(item, $event)"
			@keydown.enter.self.prevent="openApp(item)" @keydown.space.self.prevent="openApp(item)"
			@keydown.self="handleCardKeydown">
			<!-- Card Content Start -->
			<b-tooltip :always="isActiveTooltip" :animated="true" :label="tooltipLabel" :triggers="tooltipTriger"
				animation="fade1" class="in-card" type="is-white">

				<div class="has-text-centered is-flex is-justify-content-center is-flex-direction-column icon-cell">
					<div class="is-flex is-justify-content-center">
						<div class="is-relative">
							<b-image :class="dotClass(item.status, isLoading)" :alt="i18n(item.title) || item.name || ''"
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
							<img :src="require('@/assets/img/loading/waiting-white.svg')" :alt="$t('Loading')" class="is-20x20" />
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
import YAML from "yaml";
import commonI18n, { ice_i18n } from "@/mixins/base/common-i18n";
import FileSaver from 'file-saver';
import { confirmWindowMixin } from '@/mixins/confirmWindow';
import { apiErrorHtml, apiErrorText } from '@/mixins/app/apiError';

const REBUILD_DONE_EVENT = 'APP_CARD_REBUILD_DONE'
const REBUILD_WATCH_TIMEOUT = 30 * 60 * 1000

function saveYamlFile(yaml, baseName) {
	const blob = new Blob([yaml], { type: 'application/yaml' })
	const base = String(baseName || 'app').replace(/[^A-Za-z0-9._-]+/g, '_')
	const fileName = `${base}.yaml`
	FileSaver.saveAs(blob, fileName)
	return fileName
}

// A rebuild archives the legacy container, so its AppCard is usually
// destroyed (the app drops off the grid) before the reinstall's
// app:install-end/-error arrives. The outcome is therefore tracked here,
// on the raw socket, independently of any component instance.
const pendingRebuilds = {}

function watchRebuild(vm, name, yaml) {
	const client = vm.$socket && vm.$socket.client
	const i18n = vm.$i18n
	const t = (key, values) => (i18n ? i18n.t(key, values) : key)
	const buefy = vm.$buefy
	const bus = vm.$EventBus
	const title = vm.displayTitle
	const entry = { failed: false, timers: [] }
	pendingRebuilds[name] = entry

	const matches = res => {
		const props = (res && res.Properties) || {}
		return (props['app:name'] || props['dry_run.name'] || props['name']) === name
	}
	const cleanup = () => {
		if (client) {
			client.off('app:install-end', onEnd)
			client.off('app:install-error', onError)
		}
		entry.timers.forEach(clearTimeout)
		if (pendingRebuilds[name] === entry) delete pendingRebuilds[name]
	}
	const fail = reason => {
		if (entry.failed) return
		entry.failed = true
		cleanup()
		let fileName = ''
		try {
			fileName = saveYamlFile(yaml, name)
		} catch (e) {
			console.error('rebuild: saving the exported compose failed', e)
		}
		buefy.toast.open({
			message: escapeHtml(fileName
				? t('Rebuild of {name} failed: {reason}. The exported compose file was saved as {file} - install it from App Store > Custom Install.', { name: title, reason, file: fileName })
				: t('Rebuild of {name} failed: {reason}', { name: title, reason })),
			type: 'is-danger',
			position: 'is-top',
			duration: 12000,
			queue: false
		})
		if (bus) bus.$emit(REBUILD_DONE_EVENT, { name, ok: false })
	}
	const onError = res => {
		if (!matches(res)) return
		fail((res.Properties && res.Properties['message']) || t('Rebuild error'))
	}
	// install-error is published from a goroutine and install-end from a
	// defer, so the error can arrive just after the end: wait a moment
	// before calling it a success.
	const onEnd = res => {
		if (!matches(res)) return
		entry.timers.push(setTimeout(() => {
			if (entry.failed) return
			cleanup()
			buefy.toast.open({
				message: t(`{title} rebuild completed`, { title: escapeHtml(title) }),
				type: 'is-success'
			})
			if (bus) bus.$emit(REBUILD_DONE_EVENT, { name, ok: true })
		}, 800))
	}
	if (client) {
		client.on('app:install-end', onEnd)
		client.on('app:install-error', onError)
	}
	entry.timers.push(setTimeout(cleanup, REBUILD_WATCH_TIMEOUT))
	entry.fail = fail
	return entry
}

export default {
	name: "app-card",
	components: {
		cTooltip,
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
			// Set by a socket *-error for this app so the matching *-end (the
			// backend publishes it from a defer, racing the error) doesn't
			// also claim success.
			updateFailed: false,
			imageWasUpdated: false,
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
				return this.item.status === 'running' ? this.$t('Stopping...') : this.$t('Starting...');
			} else if (this.isRebuilding) {
				return this.$t('Rebuilding');
			} else if (this.isCheckThenUpdate) {
				return this.$t('Checking for updates...');
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
		displayTitle() {
			return ice_i18n(this.item.title) || this.item.name || ''
		},
		cardAriaLabel() {
			return this.tooltipLabel ? `${this.displayTitle} - ${this.tooltipLabel}` : this.displayTitle
		},

	},

	created() {
		this.checkPinStatus()
		this.$EventBus.$on(events.RELOAD_APP_LIST, this.checkPinStatus)
		this.$EventBus.$on(REBUILD_DONE_EVENT, this.onRebuildDone)
		if (pendingRebuilds[this.item.name]) this.isRebuilding = true
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
		this.$EventBus.$off(REBUILD_DONE_EVENT, this.onRebuildDone)
		this.$EventBus.$off('CLOSE_ALL_CONTEXT_MENUS', this.handleCloseOtherMenus)
		clearTimeout(this.updateEndTimer)
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
			this.getDockPinsCached().then(pins => {
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

		handleCardKeydown(event) {
			// Shift+F10 / the ContextMenu key open the menu from the keyboard.
			if ((event.key === 'F10' && event.shiftKey) || event.key === 'ContextMenu') {
				event.preventDefault()
				event.stopPropagation()
				const rect = event.currentTarget.getBoundingClientRect()
				this.handleCardContextMenu({
					clientX: rect.left + rect.width / 2,
					clientY: rect.top + rect.height / 2,
					fromKeyboard: true,
					preventDefault() {},
					stopPropagation() {}
				})
			}
		},

		handleCardContextMenu(event) {
			if (event) {
				event.preventDefault()
				event.stopPropagation()
			}
			if (this.isUninstalling) return
			this.menuOpenedByKeyboard = !!(event && event.fromKeyboard)

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
				if (this.menuOpenedByKeyboard) {
					const first = this.$refs.menu.querySelector('.ctx-item:not([disabled])')
					if (first) first.focus()
				}
			})
		},

		closeMenu() {
			if (!this.menuVisible) return
			this.menuVisible = false
			this.removeEventListeners()
			if (this.menuOpenedByKeyboard) {
				this.menuOpenedByKeyboard = false
				const card = this.$el && this.$el.querySelector && this.$el.querySelector('.cards-content')
				if (card) card.focus()
			}
		},

		toastError(err, fallback) {
			this.$buefy.toast.open({
				message: apiErrorHtml(err, fallback || this.$t('Something went wrong')),
				type: 'is-danger',
				position: 'is-top',
				duration: 5000
			})
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
				return
			}
			// Arrow-key navigation between menu items.
			if (this.menuVisible && this.$refs.menu && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
				const items = Array.from(this.$refs.menu.querySelectorAll('.ctx-item:not([disabled])'))
				if (!items.length) return
				event.preventDefault()
				const idx = items.indexOf(document.activeElement)
				const next = event.key === 'ArrowDown'
					? items[(idx + 1) % items.length]
					: items[(idx - 1 + items.length) % items.length]
				next.focus()
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
			this.setDockPinned(name, next).catch(err => {
				this.isPinned = !next
				this.toastError(err)
				return Promise.reject(err)
			}).then(() => {
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
			}).catch(() => {})
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
				case "Download Station":
					this.$store.commit('OPEN_WINDOW', {
						id: 'download-station', title: this.$t('Download Station'), component: 'DownloadStationApp', width: 980, height: 640
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
				this.toastError(err)
			}).finally(() => {
				this.isRestarting = false;
			})
		},

		restartAppV2() {
			this.$openAPI.appManagement.compose.setComposeAppStatus(this.item.name, "restart").then((res) => {
				this.updateState()
			}).catch((err) => {
				this.toastError(err)
			}).finally(() => {
				this.isRestarting = false;
			})
		},

		/**
		 * @description: Confirm before uninstall
		 * @return {*} void
		 */
		uninstallConfirm() {
			this.$messageBus('apps_uninstall', this.item.name);
			this.closeMenu();
			const dataPath = `/DATA/AppData/${this.item.name}`
			this.confirmWindow({
				title: this.$t('Uninstall app'),
				message: this.$t('Uninstall {name}?', { name: `<b>${escapeHtml(this.displayTitle)}</b>` }),
				type: 'is-danger',
				confirmText: this.$t('Uninstall'),
				cancelText: this.$t('Cancel'),
				checkbox: {
					label: this.$t('Also delete app data'),
					checked: false,
					checkedIsDanger: true,
					uncheckedHint: this.$t('Data is kept in {path}.', { path: dataPath }),
					checkedHint: this.$t('Data in {path} will be deleted and cannot be recovered.', { path: dataPath })
				},
				onConfirm: (deleteData) => {
					this.uninstallApp(!!deleteData)
				}
			})
		},

		deleteLinkConfirm() {
			this.closeMenu();
			this.confirmWindow({
				title: this.$t('Delete web link'),
				message: this.$t('Delete the web link {name}? The website itself is not affected.', { name: `<b>${escapeHtml(this.displayTitle)}</b>` }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.uninstallApp(true)
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
				return this.deleteLinkAppByName(this.item.name).then(res => {
					if (res.data.success === 200) {
						this.$EventBus.$emit(events.RELOAD_APP_LIST);
					}
				}).catch((err) => {
					this.toastError(err, this.$t('Unable to delete the web link'))
				}).finally(() => {
					this.isUninstalling = false
				})
			} else if (this.isV2App) {
				this.$openAPI.appManagement.compose.uninstallComposeApp(this.item.name, checkDelConfig).then((res) => {
					if (res.status === 200) {
						this.$EventBus.$emit(events.UPDATE_SYNC_STATUS);
					}
				}).catch((err) => {
					this.isUninstalling = false
					this.toastError(err, this.$t('Unable to uninstall the app'))
				})
			} else {
				// former app uninstall
				this.$api.container.uninstall(this.item.name, { 'delete_config_folder': checkDelConfig }).then((res) => {
					if (res.data.success === 200) {
						this.$EventBus.$emit(events.UPDATE_SYNC_STATUS);
					} else {
						this.isUninstalling = false
					}
				}).catch((err) => {
					this.isUninstalling = false
					this.toastError(err, this.$t('Unable to uninstall the app'))
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
				this.$store.commit('OPEN_WINDOW', {
					id: 'tip-editor-' + name,
					title: this.$t('Tips'),
					component: 'TipEditorModal',
					props: {
						isDialog: true,
						composeData: YAML.parse(ret),
						name
					},
					width: 440,
					height: 480
				})
			} catch (e) {
				console.error('openTips Error:', e)
				this.toastError(e, this.$t('Unable to load the tips'))
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
					this.$buefy.toast.open({
						message: escapeHtml(res.data.message || (typeof res.data.data === 'string' ? res.data.data : '') || this.$t('Something went wrong')),
						type: 'is-danger',
						position: 'is-top',
						duration: 5000
					})
				}
			}).catch((err) => {
				this.toastError(err)
			}).finally(() => {
				this.isStarting = false
			})
		},

		toggleAppV2(item, status) {
			this.$openAPI.appManagement.compose.setComposeAppStatus(item.name, status).then((res) => {
				this.updateState()
				item.status = status
			}).catch((err) => {
				this.toastError(err)
			}).finally(() => {
				this.isStarting = false
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
						this.toastError(err)
					}).then(() => {
						this.isCloning = false;
						this.closeMenu();
					})
				}
			}).catch(() => {
				this.isCloning = false;
				this.$buefy.toast.open({
					message: this.$t(`There was an error loading the data, please try again!`),
					type: 'is-danger'
				})
			})
		},

		exportYAML(item) {
			this.closeMenu();
			this.$api.container.exportAsCompose(item.name).then(res => {
				this.saveYamlFile(res.data, item)
			}).catch((err) => {
				this.toastError(err, this.$t('Unable to export the compose file'))
			})
		},

		saveYamlFile(yaml, item) {
			return saveYamlFile(yaml, item && (item.name || item.image))
		},

		rebuildConfirm(app) {
			this.closeMenu();
			this.confirmWindow({
				title: this.$t('Rebuild app'),
				message: this.$t('Rebuild {name} as a compose app? Its current container is archived (removed) and the app is reinstalled from an exported compose file. It is unavailable until the reinstall finishes.', { name: `<b>${escapeHtml(this.displayTitle)}</b>` }),
				type: 'is-warning',
				confirmText: this.$t('Rebuild'),
				cancelText: this.$t('Cancel'),
				height: 250,
				onConfirm: () => this.rebuild(app)
			})
		},

		async rebuild(app) {
			this.closeMenu();
			if (pendingRebuilds[app.name]) return
			this.isRebuilding = true;
			// 1. export yaml - nothing has been changed yet if this fails
			let file
			try {
				file = await this.$api.container.exportAsCompose(app.name).then(res => res.data)
				if (!file) throw new Error(this.$t('The exported compose file is empty'))
				// 2. validate it before removing anything (dry run, the old
				// container still holds its ports so skip the port check)
				await this.$openAPI.appManagement.compose.installComposeApp(file, true, false)
			} catch (e) {
				this.isRebuilding = false;
				console.error('rebuild Error:', e)
				this.toastError(e, this.$t('Rebuild error'))
				return
			}
			// 3. archive (removes the legacy container)
			try {
				await this.$api.container.archive(app.name)
			} catch (e) {
				this.isRebuilding = false;
				console.error('rebuild archive Error:', e)
				this.toastError(e, this.$t('Rebuild error'))
				return
			}
			// 4. install compose - the result arrives via app:install-end/-error,
			// watched outside this component (see watchRebuild).
			const watch = watchRebuild(this, app.name, file)
			try {
				await this.$openAPI.appManagement.compose.installComposeApp(file, false, true)
			} catch (e) {
				console.error('rebuild install Error:', e)
				watch.fail(apiErrorText(e, this.$t('Rebuild error')))
			}
		},

		onRebuildDone({ name }) {
			if (name === this.item.name) this.isRebuilding = false
		},

		updateAppConfirm() {
			this.closeMenu();
			this.confirmWindow({
				title: this.$t('Update app'),
				message: this.$t('Update {name} to the latest image? It restarts briefly.', { name: `<b>${escapeHtml(this.displayTitle)}</b>` }),
				confirmText: this.$t('Update'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.checkAppVersion(this.item.name)
			})
		},

		checkAppVersion(name) {
			this.closeMenu();
			this.isCheckThenUpdate = true;
			this.updateFailed = false;
			this.imageWasUpdated = false;
			this.$openAPI.appManagement.compose.updateComposeApp(name).then(resp => {
				// 200:
				if (resp.status === 200) {
					// messageBus :: apps_checkThenUpdate
					this.$messageBus('apps_checkupdate', this.item.name.toString());
					this.$buefy.toast.open({
						// value is `In the process of asynchronous updating.` or `compose app `app Name` is up to date`
						message: escapeHtml(resp.data.message || this.$t('Updating')),
						type: 'is-success'
					})

				} else {
					this.$buefy.toast.open({
						message: this.$t(`No updates are currently available for the application.`),
						type: 'is-success'
					})
				}
			}).catch((err) => {
				this.toastError(err, this.$t(`Unable to update at the moment!`))
			}).finally(() => {
				this.closeMenu();
				this.isCheckThenUpdate = false;
			})
		},
		// Socket events are broadcast to every card - only react to our own.
		isOwnEvent(res) {
			const props = (res && res.Properties) || {}
			const name = props['app:name'] || props['dry_run.name'] || props['name']
			return !!name && name === this.item.name
		},

		toastSocketError(res, fallback) {
			const msg = (res && res.Properties && res.Properties['message']) || fallback || this.$t('Something went wrong')
			this.$buefy.toast.open({
				message: escapeHtml(msg),
				duration: 5000,
				type: 'is-danger',
				position: 'is-top'
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
			if (!this.isOwnEvent(res)) return
			this.isRestarting = false
			this.isStarting = false
			this.toastSocketError(res)
		},
		"app:start-end"(res) {
			if (this.isOwnEvent(res)) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:stop-error"(res) {
			if (!this.isOwnEvent(res)) return
			this.isRestarting = false
			this.isStarting = false
			this.toastSocketError(res)
		},
		"app:stop-end"(res) {
			if (this.isOwnEvent(res)) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:restart-error"(res) {
			if (!this.isOwnEvent(res)) return
			this.isRestarting = false
			this.isStarting = false
			this.toastSocketError(res)
		},
		"app:restart-end"(res) {
			if (this.isOwnEvent(res)) {
				this.isRestarting = false
				this.isStarting = false
			}
		},
		"app:apply-changes-begin"(res) {
			if (this.isOwnEvent(res)) {
				this.isSaving = true
			}
		},
		"app:apply-changes-error"(res) {
			if (!this.isOwnEvent(res)) return
			this.isSaving = false
			this.toastSocketError(res)
		},
		"app:apply-changes-end"(res) {
			if (this.isOwnEvent(res)) {
				this.isRestarting = false
				this.isStarting = false
				this.isSaving = false
			}
		},

		'docker:image:pull-end'(data) {
			if (this.isOwnEvent(data)) {
				if (data.Properties['docker:image:updated'] === 'true') {
					this.imageWasUpdated = true;
					this.isUpdating = true;
				}
				this.isCheckThenUpdate = false;
			}
		},

		'docker:image:pull-error'(data) {
			if (this.isOwnEvent(data)) {
				this.isCheckThenUpdate = false;
			}
		},

		'app:update-error'(data) {
			if (!this.isOwnEvent(data)) return
			this.updateFailed = true;
			this.isUpdating = false;
			this.isCheckThenUpdate = false;
			this.$buefy.toast.open({
				message: escapeHtml(this.$t('Update of {name} failed: {reason}', {
					name: this.displayTitle,
					reason: (data.Properties && data.Properties['message']) || this.$t('Something went wrong')
				})),
				type: 'is-danger',
				position: 'is-top',
				duration: 8000
			})
		},

		/**
		 * @description: Update App Version
		 * @param {Object} data
		 * @return {void}
		 */
		'app:update-end'(data) {
			if (!this.isOwnEvent(data)) return
			this.isUpdating = false;
			this.isCheckThenUpdate = false;
			const updated = this.imageWasUpdated || data.Properties['docker:image:updated'] === 'true'
			this.imageWasUpdated = false
			// The backend publishes update-error from a goroutine and
			// update-end from a defer, so the error can land just after the
			// end - wait a moment before claiming success.
			clearTimeout(this.updateEndTimer)
			this.updateEndTimer = setTimeout(() => {
				if (this.updateFailed) {
					this.updateFailed = false
					return
				}
				// displayTitle comes from the installed app's manifest -
				// escape before it hits toast.open's v-html message.
				this.$buefy.toast.open({
					message: updated
						? this.$t('{appName} was updated', { appName: escapeHtml(this.displayTitle) })
						: this.$t(`{appName} is the latest version!`, { appName: escapeHtml(this.displayTitle) }),
					type: 'is-success',
					duration: 5000
				})
			}, 800)
		},
		"app:uninstall-error"(res) {
			if (!this.isOwnEvent(res)) return
			this.isUninstalling = false;
			this.toastSocketError(res, this.$t('Unable to uninstall the app'))
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

// Keyboard access for the tile (role=button, tabindex=0).
.app-card > .cards-content:focus {
	outline: none;
}

.app-card > .cards-content:focus-visible {
	outline: 2px solid var(--color-primary-fg);
	outline-offset: 2px;
	border-radius: var(--radius-card);
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
