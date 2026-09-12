<!--
	The mobile "app shell" equivalent of WindowManager's desktop-window
	loop: instead of every open window floating/overlapping at once, only
	the single topmost non-minimized one is shown at all, fullscreen, with
	no titlebar/drag/resize chrome - real mobile apps aren't freely
	resizable windows, they're fullscreen screens you navigate between.

	This deliberately reuses the SAME state.windows/OPEN_WINDOW/
	CLOSE_WINDOW/TOGGLE_MINIMIZE_WINDOW model the desktop uses, just
	rendered differently: "back" from a primary tab (VMs/Settings) minimizes
	it (matching what tapping its own Minimize button would do on desktop -
	the window keeps its state, just stops being the visible one), while
	"back" from anything else (a sub-screen pushed on top, e.g. an image
	viewer or a VM's edit dialog) closes it outright, since those aren't
	meant to persist in the background the way a primary tab's app is.
	When nothing is open at all, the "Apps" home grid shows instead - see
	MobileTabBar.vue for the other half of this navigation model.
-->
<template>
	<div class="mobile-screen-host">
		<div v-if="activeWindow" class="mobile-screen" :class="{ 'is-dark': isDarkWindow, 'is-immersive': isImmersive }">
			<div v-if="!hasOwnTitlebar" class="mobile-screen-bar">
				<button type="button" class="mobile-back-btn" @click="goBack">
					<b-icon icon="chevron-left" custom-size="mdi-24px"></b-icon>
				</button>
				<b-icon v-if="isConsoleWindow" icon="monitor" custom-size="mdi-16px" class="mobile-screen-icon"></b-icon>
				<span class="mobile-screen-title one-line">{{ activeWindow.title }}</span>
				<span v-if="isConsoleWindow && consoleStatus" class="mobile-screen-status" :class="'is-' + consoleStatus">{{ consoleStatusText }}</span>
			</div>
			<div class="mobile-screen-content">
				<component
					:is="resolvedComponent"
					:key="activeWindow.id"
					ref="content"
					v-bind="activeWindow.props"
					@close="closeActive"
					@minimize="minimizeActive"
					@drag-start="noop"
					@status-change="onConsoleStatusChange"
				></component>
			</div>
		</div>
		<div v-else class="mobile-home-screen">
			<app-section ref="apps"></app-section>
		</div>
	</div>
</template>

<script>
import AppSection from '@/components/Apps/AppSection.vue'
import { resolveComponent, OWN_TITLEBAR_COMPONENTS, DARK_WINDOW_COMPONENTS, NO_SCROLL_COMPONENTS } from '@/utils/desktop/windowRegistry'

// Tapping "back" on one of these minimizes rather than closes - matches
// what tapping their own Minimize button would do on desktop, since
// they're meant to keep running in the background (see MobileTabBar.vue,
// which drives these same three ids).
const PRIMARY_TAB_WINDOW_IDS = ['vms', 'settings']

export default {
	name: 'mobile-screen-host',
	components: { AppSection },
	data() {
		return {
			consoleStatus: null
		}
	},
	computed: {
		windows() {
			return this.$store.state.windows
		},
		activeWindow() {
			let top = null
			for (const w of this.windows) {
				if (w.minimized) continue
				if (!top || w.zIndex > top.zIndex) top = w
			}
			return top
		},
		resolvedComponent() {
			return this.activeWindow ? resolveComponent(this.activeWindow.component) : null
		},
		hasOwnTitlebar() {
			return this.activeWindow && OWN_TITLEBAR_COMPONENTS.includes(this.activeWindow.component)
		},
		isDarkWindow() {
			return this.activeWindow && DARK_WINDOW_COMPONENTS.includes(this.activeWindow.component)
		},
		isImmersive() {
			return this.activeWindow && NO_SCROLL_COMPONENTS.includes(this.activeWindow.component)
		},
		isConsoleWindow() {
			return this.activeWindow && this.activeWindow.component === 'VmConsolePanel'
		},
		consoleStatusText() {
			return (
				{
					connecting: this.$t('Connecting...'),
					connected: this.$t('Connected'),
					disconnected: this.$t('Disconnected'),
				}[this.consoleStatus] || this.consoleStatus
			)
		}
	},
	methods: {
		noop() {},
		onConsoleStatusChange(status) {
			this.consoleStatus = status
		},
		goBack() {
			if (!this.activeWindow) return
			if (PRIMARY_TAB_WINDOW_IDS.includes(this.activeWindow.id)) {
				this.minimizeActive()
			} else {
				this.closeActive()
			}
		},
		closeActive() {
			const content = this.$refs.content
			if (content && typeof content.requestClose === 'function') {
				content.requestClose()
				return
			}
			this.$store.commit('CLOSE_WINDOW', this.activeWindow.id)
		},
		minimizeActive() {
			this.$store.commit('TOGGLE_MINIMIZE_WINDOW', this.activeWindow.id)
		}
	}
}
</script>

<style lang="scss" scoped>
.mobile-screen-host {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
}

.mobile-screen {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	background: var(--theme-bg-window, #ffffff);

	&.is-dark {
		background: #1e1e1e;

		.mobile-screen-bar {
			background: #262626;
			border-bottom-color: rgba(255, 255, 255, 0.08);
		}

		.mobile-screen-title {
			color: rgba(255, 255, 255, 0.85);
		}

		.mobile-back-btn {
			color: rgba(255, 255, 255, 0.75);
		}
	}
}

.mobile-screen-bar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	height: 2.75rem;
	padding: 0 0.5rem;
	background: var(--theme-bg-window, #fff);
	border-bottom: 1px solid var(--theme-card-border, rgb(228 233 237));
	// This is the mobile equivalent of a titlebar's drag handle - it isn't
	// draggable (no window to drag), but a stray touch-scroll gesture
	// starting here shouldn't fight with tapping the back button.
	touch-action: none;
}

.mobile-back-btn {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 2.25rem;
	height: 2.25rem;
	border: none;
	background: transparent;
	color: var(--theme-text-primary, #2c3e50);
	cursor: pointer;
	border-radius: 50%;

	&:active {
		background: rgba(0, 0, 0, 0.06);
	}
}

.mobile-screen-icon {
	flex-shrink: 0;
	margin-right: 0.4rem;
	color: rgba(255, 255, 255, 0.6);
}

.mobile-screen-title {
	flex: 1 1 auto;
	min-width: 0;
	color: var(--theme-text-primary, #2c3e50);
	font-size: 0.95rem;
	font-weight: 500;
}

.mobile-screen-status {
	flex-shrink: 0;
	margin-left: 0.6rem;
	font-size: 0.68rem;
	padding: 0.1rem 0.5rem;
	border-radius: 999px;
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.7);

	&.is-connected {
		background: rgba(72, 199, 116, 0.2);
		color: #48c774;
	}
	&.is-connecting {
		background: rgba(255, 221, 87, 0.15);
		color: #ffdd57;
	}
	&.is-disconnected {
		background: rgba(255, 56, 96, 0.15);
		color: #ff3860;
	}
}

.mobile-screen-content {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	position: relative;
}

.is-immersive .mobile-screen-content {
	overflow: hidden;
}

.mobile-home-screen {
	position: absolute;
	inset: 0;
	overflow: hidden;
}
</style>
