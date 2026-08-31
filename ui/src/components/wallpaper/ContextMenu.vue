<template>
	<div
		v-if="visible"
		ref="menu"
		class="desktop-context-menu"
		:style="{ top: y + 'px', left: x + 'px' }"
		@contextmenu.prevent.stop
	>
		<!-- 1. Creation & Organization -->
		<button class="ctx-item" @click="showCreateFolderPrompt">
			<i class="mdi mdi-folder-plus-outline ctx-icon"></i>
			<span class="ctx-label">{{ $t('New Folder') }}</span>
		</button>

		<button class="ctx-item" @click="arrangeApps">
			<i class="mdi mdi-view-grid-outline ctx-icon"></i>
			<span class="ctx-label">{{ $t('Auto-Arrange Icons') }}</span>
		</button>

		<div class="ctx-divider"></div>

		<!-- 2. Custom App & Link Installation -->
		<button class="ctx-item" @click="showCustomInstall">
			<i class="mdi mdi-docker ctx-icon"></i>
			<span class="ctx-label">{{ $t('Custom Install App / Container') }}</span>
		</button>

		<button class="ctx-item" @click="showExternalLinkPanel">
			<i class="mdi mdi-link-variant-plus ctx-icon"></i>
			<span class="ctx-label">{{ $t('Add Web Link / Shortcut') }}</span>
		</button>

		<div class="ctx-divider"></div>

		<!-- 3. System Utilities -->
		<button class="ctx-item" @click="openTerminal">
			<i class="mdi mdi-console ctx-icon"></i>
			<span class="ctx-label">{{ $t('Terminal') }}</span>
		</button>

		<button class="ctx-item" @click="openWallpaperSettings">
			<i class="mdi mdi-image-outline ctx-icon"></i>
			<span class="ctx-label">{{ $t('Change Wallpaper') }}</span>
		</button>

		<button class="ctx-item" @click="openSettings">
			<i class="mdi mdi-cog-outline ctx-icon"></i>
			<span class="ctx-label">{{ $t('System Settings') }}</span>
		</button>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'

const MENU_WIDTH = 224
const MENU_HEIGHT = 340

export default {
	name: 'desktop-context-menu',
	mixins: [mixin],
	data() {
		return {
			visible: false,
			x: 0,
			y: 0
		}
	},
	mounted() {
		this.$EventBus.$on(events.SHOW_HOME_CONTEXT_MENU, event => {
			this.open(event)
		})
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('blur', this.close)
		window.addEventListener('resize', this.close)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('blur', this.close)
		window.removeEventListener('resize', this.close)
	},
	methods: {
		open(event) {
			const target = event.target

			// Walk up from the target — if we're inside an app card, folder card,
			// or any dropdown, this is NOT a bare-canvas right-click.
			let el = target
			while (el && el !== document.body) {
				const cls = el.getAttribute ? (el.getAttribute('class') || '') : ''
				if (
					cls.includes('app-card') ||
					cls.includes('folder-card') ||
					cls.includes('common-card') ||
					cls.includes('dropdown') ||
					cls.includes('app-slot') ||
					cls.includes('installing-app-slot') ||
					cls.includes('dock-item') ||
					cls.includes('dock-context-menu') ||
					cls.includes('modal') ||
					cls.includes('window') ||
					el.tagName === 'BUTTON'
				) {
					return // Not a canvas click — let the app's own handler deal with it
				}
				el = el.parentElement
			}

			const className = target?.getAttribute ? target.getAttribute('class') || '' : ''
			const isDesktopCanvas =
				className.includes('contextmenu-canvas') ||
				className.includes('desktop-') ||
				target.tagName === 'MAIN' ||
				target.id === 'app' ||
				(target.classList && target.classList.contains('desktop-viewport'))

			if (isDesktopCanvas) {
				const maxLeft = Math.max(12, window.innerWidth - MENU_WIDTH - 16)
				const maxTop = Math.max(12, window.innerHeight - MENU_HEIGHT - 80) // Above taskbar dock

				this.x = Math.max(12, Math.min(maxLeft, event.clientX))
				this.y = Math.max(12, Math.min(maxTop, event.clientY))
				this.visible = true
			}
		},
		close() {
			this.visible = false
		},
		onOutsideClick(event) {
			if (this.visible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.close()
			}
		},
		openWallpaperSettings() {
			this.close()
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540,
				props: { section: 'appearance' }
			})
		},
		openSettings() {
			this.close()
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 760,
				height: 540
			})
		},
		openTerminal() {
			this.close()
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal',
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480
			})
		},
		openAppStore() {
			this.close()
			this.$store.commit('OPEN_WINDOW', {
				id: 'appstore',
				title: this.$t('App Store'),
				component: 'AppStoreApp',
				width: 1040,
				height: 720
			})
		},
		showCustomInstall() {
			this.close()
			this.$EventBus.$emit(events.SHOW_CUSTOM_INSTALL)
		},
		showExternalLinkPanel() {
			this.close()
			this.$EventBus.$emit(events.SHOW_EXTERNAL_LINK_PANEL)
		},
		showCreateFolderPrompt() {
			this.close()
			this.$EventBus.$emit(events.SHOW_CREATE_FOLDER_PROMPT)
		},
		arrangeApps() {
			this.close()
			this.$EventBus.$emit(events.ARRANGE_APPS)
		}
	}
}
</script>

<style lang="scss" scoped>
.desktop-context-menu {
	position: fixed;
	z-index: 10000;
	width: 224px;
	background: rgba(255, 255, 255, 0.88);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid rgba(255, 255, 255, 0.65);
	border-radius: 12px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.16), 0 2px 8px rgba(0, 0, 0, 0.08);
	padding: 0.35rem;
	user-select: none;
	animation: ctxFadeIn 0.12s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes ctxFadeIn {
	from {
		opacity: 0;
		transform: scale(0.96) translateY(-4px);
	}
	to {
		opacity: 1;
		transform: scale(1) translateY(0);
	}
}

.ctx-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	padding: 0.45rem 0.65rem;
	border-radius: 7px;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 500;
	color: #1e293b;
	transition: all 0.12s ease;
	cursor: pointer;
	border: none;
	background: transparent;
	text-align: left;

	.ctx-icon {
		font-size: 1.1rem;
		color: #475569;
		flex-shrink: 0;
		line-height: 1;
		transition: color 0.12s ease;
	}

	.ctx-label {
		flex: 1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	&:hover {
		background: #2563eb;
		color: #ffffff;

		.ctx-icon {
			color: #ffffff;
		}
	}

	&:active {
		transform: scale(0.98);
	}
}

.ctx-divider {
	height: 1px;
	margin: 0.35rem 0.4rem;
	background: rgba(0, 0, 0, 0.08);
}
</style>
