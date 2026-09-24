<!-- src/shell/desktop/NotificationCenter.vue -->
<template>
	<div class="notification-center-wrap">
		<!-- Bell Button in Tray -->
		<button
			ref="bell"
			type="button"
			class="notification-pill"
			:class="{ 'is-active': menuOpen, 'has-unread': unreadCount > 0 }"
			:title="$t('Notifications & Activity')"
			:aria-label="unreadCount > 0 ? $t('Notifications ({count} unread)', { count: unreadCount }) : $t('Notifications & Activity')"
			:aria-expanded="String(menuOpen)"
			aria-haspopup="dialog"
			@click.stop="toggleMenu"
		>
			<div class="bell-icon-wrap">
				<b-icon
					:icon="unreadCount > 0 ? 'bell-badge-outline' : 'bell-outline'"
					pack="mdi"
					custom-size="mdi-18px"
				></b-icon>
				<span v-if="unreadCount > 0" class="unread-badge" aria-hidden="true">
					{{ unreadCount > 99 ? '99+' : unreadCount }}
				</span>
			</div>
		</button>

		<!-- Popover Dropdown Panel -->
		<transition name="pop-up">
			<div v-if="menuOpen" class="notification-popover" role="dialog" :aria-label="$t('Activity & Notifications')" @click.stop>
				<notification-list @close="closeMenu(true)"></notification-list>
			</div>
		</transition>
	</div>
</template>

<script>
import { activityService } from '@/service/activity'
import NotificationList from './NotificationList.vue'

export default {
	name: 'notification-center',
	components: { NotificationList },
	data() {
		return {
			menuOpen: false,
			unreadCount: 0
		}
	},
	created() {
		this.unreadCount = activityService.getUnreadCount()
		this.unsubscribe = activityService.subscribe(list => {
			this.unreadCount = list.filter(a => !a.read).length
		})
	},
	mounted() {
		document.addEventListener('click', this.onOutsideClick)
		document.addEventListener('keydown', this.onKeydown)
		this.onCloseTrayPopovers = (sender) => {
			if (sender !== 'notification') {
				this.menuOpen = false
			}
		}
		this.$EventBus.$on('desktop:close-tray-popovers', this.onCloseTrayPopovers)
	},
	beforeDestroy() {
		if (this.unsubscribe) this.unsubscribe()
		document.removeEventListener('click', this.onOutsideClick)
		document.removeEventListener('keydown', this.onKeydown)
		this.$EventBus.$off('desktop:close-tray-popovers', this.onCloseTrayPopovers)
	},
	methods: {
		toggleMenu() {
			this.menuOpen = !this.menuOpen
			if (this.menuOpen) {
				this.$EventBus.$emit('desktop:close-tray-popovers', 'notification')
			}
		},
		closeMenu(returnFocus) {
			this.menuOpen = false
			if (returnFocus && this.$refs.bell) this.$refs.bell.focus()
		},
		onOutsideClick() {
			this.menuOpen = false
		},
		onKeydown(e) {
			if (e.key === 'Escape' && this.menuOpen) this.closeMenu(true)
		}
	}
}
</script>

<style lang="scss" scoped>
.notification-center-wrap {
	position: static;
	display: flex;
	align-items: center;
}

.notification-pill {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 2.6rem;
	height: 2.6rem;
	border: $backDropBorder;
	color: var(--theme-desktop-glass-icon, #ffffff);
	background-color: $backDropColor;
	backdrop-filter: $backDropBlur;
	-webkit-backdrop-filter: $backDropBlur;
	border-radius: 50%;
	box-shadow: var(--theme-desktop-glass-shadow);
	cursor: pointer;
	transition: all 0.15s ease;
	padding: 0;

	&:hover,
	&.is-active {
		box-shadow: var(--theme-desktop-glass-hover-shadow);
		transform: translateY(-1px);
	}
}

.bell-icon-wrap {
	position: relative;
	display: flex;
	align-items: center;
	justify-content: center;
}

.unread-badge {
	position: absolute;
	top: -7px;
	right: -8px;
	background: var(--color-primary);
	color: #ffffff;
	font-size: var(--font-2xs);
	font-weight: 700;
	min-width: 16px;
	height: 16px;
	padding: 0 var(--space-1);
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	border: 1.5px solid rgba(255, 255, 255, 0.4);
	box-shadow: var(--shadow-sm);
}

.notification-popover {
	position: absolute;
	right: 0;
	bottom: calc(100% + 0.75rem);
	width: 23.5rem;
	max-width: calc(100vw - 3rem);
	background: var(--theme-card-bg, #ffffff);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-modal);
	box-shadow: var(--shadow-xl);
	user-select: none;
	display: flex;
	flex-direction: column;
	color: var(--theme-text-primary, #1e293b);
	overflow: hidden;
	z-index: 1000;
}


.notification-pill:focus-visible {
	outline: 2px solid var(--theme-focus-ring, var(--color-primary));
	outline-offset: 2px;
}
</style>
