<!--
	The notification/activity history list. Rendered inside the desktop
	tray's popover (NotificationCenter.vue) and, on phones, as its own
	full-screen window opened from MobileTabBar - which also passes
	`showAccount` so the phone gets sign-out/restart/shutdown here (the
	desktop has those in the clock pill's menu, which phones don't show).
-->
<template>
	<div class="notif-list-root" :class="{ 'is-window': isWindow }">
		<!-- Header -->
		<div class="notif-header">
			<div class="notif-header-title">
				<b-icon icon="bell-outline" pack="mdi" custom-size="mdi-18px" class="mr-2 notif-accent"></b-icon>
				<span class="notif-title">{{ $t('Activity & Notifications') }}</span>
				<span v-if="unreadCount > 0" class="unread-pill ml-2">{{ $t('{count} new', { count: unreadCount }) }}</span>
			</div>

			<div class="notif-header-actions">
				<button v-if="unreadCount > 0" type="button" class="hdr-action-btn" :title="$t('Mark all as read')"
					:aria-label="$t('Mark all as read')" @click="markAllRead">
					<b-icon icon="check-all" pack="mdi" custom-size="mdi-16px"></b-icon>
				</button>
				<button v-if="activities.length" type="button" class="hdr-action-btn" :title="$t('Clear all history')"
					:aria-label="$t('Clear all history')" @click="clearAll">
					<b-icon icon="trash-can-outline" pack="mdi" custom-size="mdi-16px"></b-icon>
				</button>
				<button v-if="!isWindow" type="button" class="hdr-action-btn" :title="$t('Close')" :aria-label="$t('Close')" @click="$emit('close')">
					<b-icon icon="close" pack="mdi" custom-size="mdi-16px"></b-icon>
				</button>
			</div>
		</div>

		<!-- Filter Tabs -->
		<div class="notif-filters" role="toolbar" :aria-label="$t('Filter notifications')">
			<button v-for="tab in filterTabs" :key="tab.id" type="button" class="filter-tab"
				:class="{ active: currentFilter === tab.id }" :aria-pressed="String(currentFilter === tab.id)" @click="currentFilter = tab.id">
				{{ tab.label }}
				<span class="tab-count">({{ tab.count }})</span>
			</button>
		</div>

		<!-- Notification List -->
		<div class="notif-body scrollbars">
			<div v-if="!filteredActivities.length" class="notif-empty">
				<b-icon icon="bell-check-outline" pack="mdi" custom-size="mdi-36px" class="empty-bell mb-2"></b-icon>
				<div class="notif-empty-title">{{ $t('No notifications') }}</div>
				<div class="notif-empty-sub">{{ $t('Your recent system events, storage alerts, and app tasks will appear here.') }}</div>
			</div>

			<ul v-else class="notif-list">
				<li v-for="item in filteredActivities" :key="item.id" class="notif-item" :class="{ 'is-unread': !item.read }">
					<button type="button" class="notif-main" :aria-label="itemLabel(item)" @click="markRead(item)">
						<span class="notif-avatar" :class="[item.type, item.status]" aria-hidden="true">
							<img v-if="item.icon && !brokenIcons[item.id]" :src="item.icon" alt="" class="notif-app-icon" @error="$set(brokenIcons, item.id, true)">
							<b-icon v-else :icon="getNotifIcon(item)" pack="mdi" custom-size="mdi-18px"></b-icon>
						</span>
						<span class="notif-content">
							<span class="notif-top-row">
								<span class="notif-item-title">{{ item.title }}</span>
								<time class="notif-time" :datetime="item.timestamp" :title="absoluteTime(item.timestamp)">{{ formatTimeAgo(item.timestamp) }}</time>
							</span>
							<span v-if="item.message" class="notif-msg">{{ item.message }}</span>
						</span>
						<span v-if="!item.read" class="unread-dot" aria-hidden="true"></span>
					</button>

					<div v-if="item.action" class="notif-action-wrap">
						<button type="button" class="notif-action-btn" @click="triggerAction(item)">
							<span>{{ item.action.label || $t('View') }}</span>
							<b-icon icon="arrow-right" pack="mdi" custom-size="mdi-12px" class="ml-1"></b-icon>
						</button>
					</div>

					<button type="button" class="notif-del-btn" :title="$t('Dismiss')" :aria-label="$t('Dismiss notification: {title}', { title: item.title })"
						@click="removeItem(item.id)">
						<b-icon icon="close" pack="mdi" custom-size="mdi-14px"></b-icon>
					</button>
				</li>
			</ul>
		</div>

		<!-- Phone only: account + power (the desktop has these in the clock menu) -->
		<div v-if="showAccount" class="notif-account">
			<div v-if="showPowerModal" class="power-status" :class="'is-' + (powerState || 'pending')" role="status" aria-live="polite">
				<strong>{{ $t(powerTitle) }}</strong>
				<span>{{ $t(powerMessage) }}</span>
				<button type="button" class="hdr-action-btn" :title="$t('Dismiss')" :aria-label="$t('Dismiss')" @click="resetPowerModal">
					<b-icon icon="close" pack="mdi" custom-size="mdi-14px"></b-icon>
				</button>
			</div>
			<div class="notif-account-row">
				<span class="notif-account-name">
					<b-icon icon="account-circle-outline" pack="mdi" custom-size="mdi-20px"></b-icon>
					{{ userName || $t('User') }}
				</span>
				<button type="button" class="acct-btn" @click="logout">
					<b-icon icon="logout" pack="mdi" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Sign out') }}</span>
				</button>
			</div>
			<div class="notif-account-row">
				<button type="button" class="acct-btn" @click="confirmPower('Restart')">
					<b-icon icon="restart" pack="mdi" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Restart') }}</span>
				</button>
				<button type="button" class="acct-btn is-danger" @click="confirmPower('Shutdown')">
					<b-icon icon="power" pack="mdi" custom-size="mdi-18px"></b-icon>
					<span>{{ $t('Shutdown') }}</span>
				</button>
			</div>
		</div>
	</div>
</template>

<script>
import { activityService } from '@/service/activity'
import systemPower from '@/mixins/systemPower'

const SYSTEM_TYPES = ['schedule', 'vm', 'system', 'maintenance', 'backup']

export default {
	name: 'notification-list',
	mixins: [systemPower],
	props: {
		// Rendered as its own window (phone) rather than in the tray popover.
		isWindow: { type: Boolean, default: false },
		showAccount: { type: Boolean, default: false }
	},
	data() {
		return {
			activities: [],
			currentFilter: 'all',
			now: Date.now(),
			brokenIcons: {}
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
				{ id: 'system', label: this.$t('Tasks & System'), count: this.activities.filter(a => SYSTEM_TYPES.includes(a.type)).length }
			]
		},
		filteredActivities() {
			if (this.currentFilter === 'app') return this.activities.filter(a => a.type === 'app')
			if (this.currentFilter === 'storage') return this.activities.filter(a => a.type === 'storage' || a.type === 'usb')
			if (this.currentFilter === 'system') return this.activities.filter(a => SYSTEM_TYPES.includes(a.type))
			return this.activities
		},
		userName() {
			const u = this.$store.state.user
			if (u && u.username) return u.username
			try {
				const stored = JSON.parse(localStorage.getItem('user') || 'null')
				return (stored && stored.username) || ''
			} catch (e) {
				return ''
			}
		}
	},
	created() {
		this.activities = activityService.getAll()
		this.unsubscribe = activityService.subscribe(list => {
			this.activities = list
		})
	},
	mounted() {
		// "2m ago" labels only stay true if something re-renders them.
		this.ticker = setInterval(() => {
			this.now = Date.now()
		}, 30000)
	},
	beforeDestroy() {
		clearInterval(this.ticker)
		if (this.unsubscribe) this.unsubscribe()
	},
	methods: {
		itemLabel(item) {
			return [item.title, item.message, this.formatTimeAgo(item.timestamp), item.read ? '' : this.$t('Unread')].filter(Boolean).join('. ')
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
			this.confirmWindow({
				title: this.$t('Clear all history'),
				message: this.$t('Remove all {count} notifications? This cannot be undone.', { count: this.activities.length }),
				type: 'is-danger',
				confirmText: this.$t('Clear all'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => activityService.clear()
			})
		},
		triggerAction(item) {
			this.markRead(item)
			const act = item.action
			if (!act) return
			this.$emit('close')
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
				const w = window.open(act.url, '_blank', 'noopener,noreferrer')
				if (w) w.opener = null
			}
		},
		logout() {
			this.$router.push('/logout')
		},
		getNotifIcon(item) {
			switch (item.type) {
				case 'usb': return 'usb'
				case 'storage': return 'harddisk'
				case 'app': return 'docker'
				case 'vm': return 'monitor'
				case 'schedule': return 'clock-outline'
				case 'maintenance': return 'wrench-outline'
				case 'backup': return 'backup-restore'
				default:
					if (item.status === 'error') return 'alert-circle-outline'
					if (item.status === 'warning') return 'alert-outline'
					if (item.status === 'success') return 'check-circle-outline'
					return 'information-outline'
			}
		},
		absoluteTime(isoStr) {
			const d = new Date(isoStr)
			return isNaN(d.getTime()) ? '' : d.toLocaleString()
		},
		formatTimeAgo(isoStr) {
			if (!isoStr) return ''
			const d = new Date(isoStr)
			if (isNaN(d.getTime())) return ''
			const diffSec = Math.max(0, Math.floor((this.now - d.getTime()) / 1000))
			if (diffSec < 30) return this.$t('Just now')
			if (diffSec < 60) return this.$t('{n}s ago', { n: diffSec })
			if (diffSec < 3600) return this.$t('{n}m ago', { n: Math.floor(diffSec / 60) })
			if (diffSec < 86400) return this.$t('{n}h ago', { n: Math.floor(diffSec / 3600) })
			return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
		}
	}
}
</script>

<style lang="scss" scoped>
.notif-list-root {
	display: flex;
	flex-direction: column;
	min-height: 0;
	color: var(--theme-text-primary);
	background: var(--theme-card-bg);

	&.is-window {
		height: 100%;

		.notif-body {
			max-height: none;
			flex: 1 1 auto;
		}
	}
}

.notif-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--theme-card-border);
}

.notif-header-title,
.notif-header-actions {
	display: flex;
	align-items: center;
	gap: var(--space-1);
}

.notif-accent {
	color: var(--color-primary-fg, var(--color-primary));
}

.notif-title {
	font-size: var(--font-base);
	font-weight: 700;
}

.unread-pill {
	font-size: var(--font-2xs);
	font-weight: 700;
	color: var(--color-primary-fg, var(--color-primary));
	background: var(--color-primary-soft, var(--theme-info-soft));
	padding: 1px var(--space-2);
	border-radius: var(--radius-pill);
}

.hdr-action-btn {
	background: transparent;
	border: none;
	color: var(--theme-text-secondary);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-sm);
	display: flex;
	align-items: center;
	justify-content: center;
	min-width: 1.75rem;
	min-height: 1.75rem;

	&:hover {
		background: var(--theme-card-hover);
		color: var(--theme-text-primary);
	}
}

.notif-filters {
	display: flex;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-3);
	background: var(--theme-card-subtle);
	border-bottom: 1px solid var(--theme-card-border);
	overflow-x: auto;
}

.filter-tab {
	background: transparent;
	border: none;
	font-size: var(--font-2xs);
	font-weight: 500;
	color: var(--theme-text-secondary);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-sm);
	cursor: pointer;
	white-space: nowrap;

	&:hover {
		color: var(--theme-text-primary);
		background: var(--theme-card-hover);
	}

	&.active {
		background: var(--theme-card-bg);
		color: var(--color-primary-fg, var(--color-primary));
		font-weight: 600;
		box-shadow: var(--shadow-sm);
	}

	.tab-count {
		font-size: var(--font-2xs);
	}
}

.notif-body {
	max-height: 22rem;
	overflow-y: auto;
}

.notif-empty {
	text-align: center;
	padding: var(--space-5);
	color: var(--theme-text-secondary);

	.empty-bell {
		color: var(--theme-text-muted);
	}
}

.notif-empty-title {
	font-size: var(--font-xs);
	font-weight: 700;
}

.notif-empty-sub {
	font-size: var(--font-2xs);
	margin-top: var(--space-1);
}

.notif-list {
	list-style: none;
	margin: 0;
	padding: 0;
}

.notif-item {
	position: relative;
	border-bottom: 1px solid var(--theme-table-divider, var(--theme-card-border));

	&:last-child {
		border-bottom: none;
	}

	&.is-unread {
		background: var(--theme-info-soft, var(--theme-card-subtle));
	}

	&:hover,
	&:focus-within {
		.notif-del-btn {
			opacity: 1;
		}
	}
}

.notif-main {
	display: flex;
	align-items: flex-start;
	width: 100%;
	padding: var(--space-2) var(--space-3);
	padding-right: calc(var(--space-3) + 1.75rem);
	background: transparent;
	border: none;
	text-align: left;
	color: inherit;
	font: inherit;
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover);
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
	margin-top: 2px;
	background: var(--theme-pill-bg);
	color: var(--theme-pill-color);
	overflow: hidden;

	&.app {
		background: var(--color-info-soft, var(--theme-info-soft));
		color: var(--color-info-fg);
	}
	&.storage, &.usb, &.success {
		background: var(--color-success-soft, var(--theme-success-soft));
		color: var(--color-success-fg);
	}
	&.schedule, &.maintenance, &.warning {
		background: var(--color-warning-soft, var(--theme-warning-soft));
		color: var(--color-warning-fg);
	}
	&.error {
		background: var(--color-danger-soft, var(--theme-danger-soft));
		color: var(--color-danger-fg);
	}
}

.notif-app-icon {
	width: 100%;
	height: 100%;
	object-fit: cover;
}

.notif-content {
	flex: 1;
	min-width: 0;
	display: flex;
	flex-direction: column;
}

.notif-top-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 2px;
}

.notif-item-title {
	font-size: var(--font-xs);
	font-weight: 700;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	padding-right: var(--space-2);
}

.notif-time {
	font-size: var(--font-2xs);
	color: var(--theme-text-secondary);
	flex-shrink: 0;
}

.notif-msg {
	font-size: var(--font-2xs);
	color: var(--theme-text-secondary);
	line-height: 1.35;
	word-break: break-word;
}

.notif-action-wrap {
	padding: 0 var(--space-3) var(--space-2) calc(var(--space-3) + 32px + var(--space-3));
}

.notif-action-btn {
	background: var(--theme-card-bg);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);
	font-size: var(--font-2xs);
	font-weight: 600;
	color: var(--color-primary-fg, var(--color-primary));
	padding: var(--space-1) var(--space-2);
	cursor: pointer;
	display: inline-flex;
	align-items: center;

	&:hover {
		background: var(--theme-card-hover);
		border-color: var(--color-primary);
	}
}

.notif-del-btn {
	position: absolute;
	top: 6px;
	right: 6px;
	opacity: 0;
	background: transparent;
	border: none;
	color: var(--theme-text-secondary);
	cursor: pointer;
	padding: var(--space-1);
	border-radius: var(--radius-xs);
	min-width: 1.75rem;
	min-height: 1.75rem;

	&:hover {
		color: var(--color-danger-fg);
		background: var(--color-danger-soft, var(--theme-danger-soft));
	}

	&:focus {
		opacity: 1;
	}

	// Touch screens have no hover: always show it there.
	@media (hover: none) {
		opacity: 1;
	}
}

.unread-dot {
	position: absolute;
	bottom: 12px;
	right: 12px;
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: var(--color-primary);
}

.notif-account {
	flex-shrink: 0;
	border-top: 1px solid var(--theme-card-border);
	padding: var(--space-3) var(--space-4) calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	background: var(--theme-card-subtle);
}

.notif-account-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
}

.notif-account-name {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	font-weight: 600;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
}

.acct-btn {
	flex: 1 1 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	min-height: 2.75rem;
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-control);
	background: var(--theme-card-bg);
	color: var(--theme-text-primary);
	font: inherit;
	font-size: var(--font-sm);
	cursor: pointer;

	&.is-danger {
		color: var(--color-danger-fg);
	}

	.notif-account-name + & {
		flex: 0 0 auto;
	}
}

.power-status {
	display: grid;
	grid-template-columns: 1fr auto;
	gap: var(--space-1) var(--space-2);
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-control);
	background: var(--theme-card-bg);
	font-size: var(--font-xs);

	strong {
		grid-column: 1;
	}

	span {
		grid-column: 1;
		color: var(--theme-text-secondary);
	}

	.hdr-action-btn {
		grid-column: 2;
		grid-row: 1 / span 2;
	}

	&.is-error {
		border-color: var(--color-danger);
	}
}

.hdr-action-btn,
.filter-tab,
.notif-main,
.notif-action-btn,
.notif-del-btn,
.acct-btn {
	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: -2px;
	}
}
</style>
