<!-- src/shell/desktop/NotificationCenter.vue -->
<template>
	<div class="notification-center-wrap">
		<!-- Bell Button in Tray -->
		<button
			type="button"
			class="notification-pill"
			:class="{ 'is-active': menuOpen, 'has-unread': unreadCount > 0 }"
			:title="$t('Notifications & Activity')"
			@click.stop="toggleMenu"
		>
			<div class="bell-icon-wrap">
				<b-icon
					:icon="unreadCount > 0 ? 'bell-badge-outline' : 'bell-outline'"
					pack="mdi"
					custom-size="mdi-18px"
				></b-icon>
				<span v-if="unreadCount > 0" class="unread-badge">
					{{ unreadCount > 99 ? '99+' : unreadCount }}
				</span>
			</div>
		</button>

		<!-- Popover Dropdown Panel -->
		<transition name="pop-up">
			<div v-if="menuOpen" class="notification-popover" @click.stop>
				<!-- Header -->
				<div class="notif-header is-flex is-align-items-center is-justify-content-between">
					<div class="is-flex is-align-items-center">
						<b-icon icon="bell-outline" pack="mdi" custom-size="mdi-18px" class="mr-2 has-text-primary"></b-icon>
						<span class="notif-title font-weight-bold">{{ $t('Activity & Notifications') }}</span>
						<span v-if="unreadCount > 0" class="unread-pill ml-2">{{ unreadCount }} {{ $t('new') }}</span>
					</div>

					<div class="notif-header-actions is-flex is-align-items-center">
						<button
							v-if="unreadCount > 0"
							class="hdr-action-btn mr-1"
							:title="$t('Mark all as read')"
							@click="markAllRead"
						>
							<b-icon icon="check-all" pack="mdi" custom-size="mdi-16px"></b-icon>
						</button>
						<button
							v-if="activities.length"
							class="hdr-action-btn"
							:title="$t('Clear all history')"
							@click="clearAll"
						>
							<b-icon icon="trash-can-outline" pack="mdi" custom-size="mdi-16px"></b-icon>
						</button>
					</div>
				</div>

				<!-- Filter Tabs -->
				<div class="notif-filters">
					<button
						v-for="tab in filterTabs"
						:key="tab.id"
						type="button"
						class="filter-tab"
						:class="{ active: currentFilter === tab.id }"
						@click="currentFilter = tab.id"
					>
						{{ tab.label }}
						<span v-if="tab.count !== undefined" class="tab-count">({{ tab.count }})</span>
					</button>
				</div>

				<!-- Notification List -->
				<div class="notif-body scrollbars">
					<div v-if="!filteredActivities.length" class="notif-empty has-text-centered p-5">
						<b-icon icon="bell-check-outline" pack="mdi" custom-size="mdi-36px" class="empty-bell mb-2"></b-icon>
						<div class="is-size-7 font-weight-bold text-muted">{{ $t('No notifications') }}</div>
						<div class="is-size-8 text-muted mt-1">{{ $t('Your recent system events, storage alerts, and app tasks will appear here.') }}</div>
					</div>

					<div v-else class="notif-list">
						<div
							v-for="item in filteredActivities"
							:key="item.id"
							class="notif-item"
							:class="{ 'is-unread': !item.read, ['type-' + item.type]: true }"
							@click="markRead(item)"
						>
							<!-- Category Icon Avatar -->
							<div class="notif-avatar" :class="[item.type, item.status]">
								<b-icon :icon="getNotifIcon(item)" pack="mdi" custom-size="mdi-18px"></b-icon>
							</div>

							<!-- Content -->
							<div class="notif-content">
								<div class="notif-top-row is-flex is-align-items-center is-justify-content-between">
									<span class="notif-item-title font-weight-bold">{{ item.title }}</span>
									<span class="notif-time">{{ formatTimeAgo(item.timestamp) }}</span>
								</div>
								<p v-if="item.message" class="notif-msg">{{ item.message }}</p>

								<!-- Action button if attached -->
								<div v-if="item.action" class="notif-action-wrap mt-2">
									<button
										class="notif-action-btn"
										@click.stop="triggerAction(item)"
									>
										<span>{{ item.action.label || $t('View') }}</span>
										<b-icon icon="arrow-right" pack="mdi" custom-size="mdi-12px" class="ml-1"></b-icon>
									</button>
								</div>
							</div>

							<!-- Delete single button -->
							<button class="notif-del-btn" :title="$t('Dismiss')" @click.stop="removeItem(item.id)">
								<b-icon icon="close" pack="mdi" custom-size="mdi-14px"></b-icon>
							</button>

							<!-- Unread Dot Indicator -->
							<span v-if="!item.read" class="unread-dot"></span>
						</div>
					</div>
				</div>
			</div>
		</transition>
	</div>
</template>

<script>
import { activityService } from '@/service/activity'

export default {
	name: 'notification-center',
	data() {
		return {
			menuOpen: false,
			activities: [],
			currentFilter: 'all',
			unsubscribe: null
		}
	},
	computed: {
		unreadCount() {
			return this.activities.filter(a => !a.read).length
		},
		filterTabs() {
			return [
				{ id: 'all', label: this.$t('All'), count: this.activities.length },
				{ id: 'app', label: this.$t('Apps'), count: this.activities.filter(a => a.type === 'app').length },
				{ id: 'storage', label: this.$t('Storage & USB'), count: this.activities.filter(a => a.type === 'storage' || a.type === 'usb').length },
				{ id: 'system', label: this.$t('Tasks & System'), count: this.activities.filter(a => a.type === 'schedule' || a.type === 'vm' || a.type === 'system' || a.type === 'maintenance').length }
			]
		},
		filteredActivities() {
			if (this.currentFilter === 'all') return this.activities
			if (this.currentFilter === 'app') return this.activities.filter(a => a.type === 'app')
			if (this.currentFilter === 'storage') return this.activities.filter(a => a.type === 'storage' || a.type === 'usb')
			if (this.currentFilter === 'system') return this.activities.filter(a => a.type === 'schedule' || a.type === 'vm' || a.type === 'system' || a.type === 'maintenance')
			return this.activities
		}
	},
	created() {
		this.activities = activityService.getAll()
		this.unsubscribe = activityService.subscribe(list => {
			this.activities = list
		})
	},
	mounted() {
		document.addEventListener('click', this.onOutsideClick)
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
		this.$EventBus.$off('desktop:close-tray-popovers', this.onCloseTrayPopovers)
	},
	methods: {
		toggleMenu() {
			this.menuOpen = !this.menuOpen
			if (this.menuOpen) {
				this.$EventBus.$emit('desktop:close-tray-popovers', 'notification')
			}
		},
		onOutsideClick() {
			this.menuOpen = false
		},
		markRead(item) {
			activityService.markAsRead(item.id)
		},
		markAllRead() {
			activityService.markAllAsRead()
		},
		removeItem(id) {
			activityService.remove(id)
		},
		clearAll() {
			activityService.clear()
		},
		triggerAction(item) {
			this.markRead(item)
			this.menuOpen = false
			const act = item.action
			if (!act) return
			if (act.event) {
				this.$EventBus.$emit(act.event, act.path || act.data || act.payload)
			} else if (act.window) {
				this.$store.commit('OPEN_WINDOW', act.window)
			} else if (act.path) {
				this.$store.commit('SET_CURRENT_PATH', act.path)
				this.$store.commit('OPEN_WINDOW', {
					id: 'files',
					title: this.$t('Files'),
					component: 'FilesApp',
					width: 960,
					height: 620
				})
			} else if (act.url) {
				window.open(act.url, '_blank')
			}
		},
		getNotifIcon(item) {
			switch (item.type) {
				case 'usb': return 'usb'
				case 'storage': return 'harddisk'
				case 'app': return 'docker'
				case 'vm': return 'monitor'
				case 'schedule': return 'clock-outline'
				case 'maintenance': return 'wrench-outline'
				default:
					if (item.status === 'error') return 'alert-circle-outline'
					if (item.status === 'warning') return 'alert-outline'
					if (item.status === 'success') return 'check-circle-outline'
					return 'information-outline'
			}
		},
		formatTimeAgo(isoStr) {
			if (!isoStr) return ''
			const d = new Date(isoStr)
			if (isNaN(d.getTime())) return ''
			const now = new Date()
			const diffSec = Math.floor((now - d) / 1000)
			if (diffSec < 30) return this.$t('Just now')
			if (diffSec < 60) return `${diffSec}s ago`
			if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`
			if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`
			return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
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
	color: $white;
	background-color: $backDropColor;
	backdrop-filter: $backDropBlur;
	-webkit-backdrop-filter: $backDropBlur;
	border-radius: 50%;
	box-shadow: var(--shadow-lg), $backDropShadow;
	cursor: pointer;
	transition: all 0.15s ease;
	padding: 0;

	&:hover,
	&.is-active {
		filter: brightness(1.15);
		box-shadow: var(--shadow-xl), $backDropShadow;
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
	background: #2563eb;
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
	background: var(--theme-card-bg, #ffffff); border: 1px solid var(--theme-card-border, #e2e8f0); color: var(--theme-text-primary, #1e293b);
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

.notif-header {
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
}

.notif-title {
	font-size: var(--font-base);
	color: var(--theme-text-primary, #0f172a);
}

.unread-pill {
	font-size: var(--font-2xs);
	font-weight: 700;
	color: #2563eb;
	background: rgba(37, 99, 235, 0.1);
	padding: 1px var(--space-2);
	border-radius: var(--radius-pill);
}

.hdr-action-btn {
	background: transparent;
	border: none;
	color: var(--theme-text-muted, #64748b);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.15s ease;

	&:hover {
		background: var(--theme-card-hover, #f1f5f9); color: var(--theme-text-primary, #0f172a);
	}
}

.notif-filters {
	display: flex;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	background: var(--theme-card-subtle, #f8fafc); border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
	border-bottom: 1px solid var(--theme-card-border, #f1f5f9);
	overflow-x: auto;
}

.filter-tab {
	background: transparent;
	border: none;
	font-size: var(--font-2xs);
	font-weight: 500;
	color: var(--theme-text-muted, #64748b);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-sm);
	cursor: pointer;
	white-space: nowrap;
	transition: all 0.15s ease;

	&:hover {
		color: var(--theme-text-primary, #0f172a);
		background: rgba(0, 0, 0, 0.04);
	}

	&.active {
		background: var(--theme-card-bg, #ffffff); color: var(--color-primary, #2563eb);
		font-weight: 600;
		box-shadow: var(--shadow-sm);
	}

	.tab-count {
		opacity: 0.7;
		font-size: var(--font-2xs);
	}
}

.notif-body {
	max-height: 22rem;
	overflow-y: auto;
}

.notif-empty {
	color: var(--theme-text-muted, #94a3b8);

	.empty-bell {
		color: var(--theme-text-muted, #cbd5e1);
	}
}

.notif-item {
	display: flex;
	align-items: flex-start;
	padding: var(--space-2) var(--space-3);
	border-bottom: 1px solid #f8fafc;
	position: relative;
	cursor: pointer;
	transition: background 0.12s ease;

	&:hover {
		background: var(--theme-card-hover, #f8fafc);

		.notif-del-btn {
			opacity: 1;
		}
	}

	&.is-unread {
		background: rgba(239, 246, 255, 0.5);

		&:hover {
			background: rgba(59, 130, 246, 0.1);
		}
	}

	&:last-child {
		border-bottom: none;
	}
}

.notif-avatar {
	width: 32px;
	height: 32px;
	border-radius: var(--radius-control);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
	margin-right: var(--space-3);
	/* left as-is: fine icon-baseline alignment nudge, not generic spacing */
	margin-top: 2px;
	background: var(--theme-pill-bg, #f1f5f9); color: var(--theme-pill-color, #64748b);

	&.app {
		background: rgba(14, 165, 233, 0.12);
		color: #0284c7;
	}
	&.storage, &.usb {
		background: rgba(16, 185, 129, 0.12);
		color: #059669;
	}
	&.vm {
		background: rgba(139, 92, 246, 0.12);
		color: #a78bfa;
	}
	&.schedule, &.maintenance {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
	}
	&.error {
		background: rgba(239, 68, 68, 0.12);
		color: #dc2626;
	}
	&.warning {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
	}
}

.notif-content {
	flex: 1;
	min-width: 0;
	padding-right: var(--space-4);
}

.notif-top-row {
	/* left as-is: tight intentional line-spacing, rounding to --space-1 (4px) would double the visual gap */
	margin-bottom: 2px;
}

.notif-item-title {
	font-size: var(--font-xs);
	color: var(--theme-text-primary, #0f172a);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	padding-right: var(--space-2);
}

.notif-time {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #94a3b8);
	flex-shrink: 0;
}

.notif-msg {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	line-height: 1.35;
	word-break: break-word;
}

.notif-action-btn {
	background: var(--theme-card-bg, #ffffff); border: 1px solid var(--theme-card-border, #cbd5e1);
	border-radius: var(--radius-sm);
	font-size: var(--font-2xs);
	font-weight: 600;
	color: #2563eb;
	padding: var(--space-1) var(--space-2);
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	transition: all 0.15s ease;

	&:hover {
		background: #2563eb;
		color: #ffffff;
		border-color: #2563eb;
	}
}

.notif-del-btn {
	position: absolute;
	top: 8px;
	right: 8px;
	opacity: 0;
	background: transparent;
	border: none;
	color: var(--theme-text-muted, #94a3b8);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-xs);
	transition: all 0.15s ease;

	&:hover {
		color: #ef4444;
		background: rgba(239, 68, 68, 0.1);
	}
}

.unread-dot {
	position: absolute;
	bottom: 12px;
	right: 10px;
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: #2563eb;
}
</style>
