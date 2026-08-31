<template>
	<div class="datetime-pill-wrap">
		<button type="button" class="datetime-pill" :class="{ 'is-active': menuOpen }" @click.stop="menuOpen = !menuOpen">
			<div class="datetime-text">
				<template v-if="customFormat">
					<span class="pill-time">{{ customText }}</span>
				</template>
				<template v-else>
					<span class="pill-time">{{ timeText }}</span>
					<span class="pill-sep">&middot;</span>
					<span class="pill-date">{{ dateText }}</span>
				</template>
			</div>
		</button>

		<transition name="pop-up">
			<div v-if="menuOpen" class="quick-control-menu" @click.stop>
				<!-- User Profile Header -->
				<div class="menu-user-card">
					<div class="user-avatar-wrap">
						<div class="user-avatar-fallback">{{ userInitial }}</div>
					</div>
					<div class="user-info">
						<span class="user-name">{{ userName }}</span>
						<span class="user-status">{{ $t('Signed in') }}</span>
					</div>
					<button type="button" class="signout-btn" :title="$t('Sign out')" @click="logout">
						<b-icon icon="logout" pack="mdi" size="is-16"></b-icon>
						<span>{{ $t('Sign out') }}</span>
					</button>
				</div>

				<div class="menu-divider"></div>

				<!-- Clock & Calendar Glance -->
				<div class="menu-clock-glance">
					<div class="glance-time">{{ timeText }}</div>
					<div class="glance-date">
						<i class="mdi mdi-calendar-blank-outline mr-1"></i>{{ fullDateText }}
					</div>
				</div>

				<div class="menu-divider"></div>

				<!-- Quick App Shortcuts -->
				<div class="shortcuts-grid">
					<button type="button" class="shortcut-tile" @click="openSettings('system')">
						<div class="shortcut-icon-box is-blue">
							<b-icon icon="cog-outline" pack="mdi" size="is-20"></b-icon>
						</div>
						<span class="shortcut-name">{{ $t('Settings') }}</span>
					</button>

					<button type="button" class="shortcut-tile" @click="openSettings('packages')">
						<div class="shortcut-icon-box is-indigo">
							<b-icon icon="package-variant-closed" pack="mdi" size="is-20"></b-icon>
						</div>
						<span class="shortcut-name">{{ $t('Packages') }}</span>
					</button>

					<button type="button" class="shortcut-tile" @click="openTerminal">
						<div class="shortcut-icon-box is-slate">
							<b-icon icon="console" pack="mdi" size="is-20"></b-icon>
						</div>
						<span class="shortcut-name">{{ $t('Terminal') }}</span>
					</button>

					<button type="button" class="shortcut-tile" @click="openFiles">
						<div class="shortcut-icon-box is-amber">
							<b-icon icon="folder-outline" pack="mdi" size="is-20"></b-icon>
						</div>
						<span class="shortcut-name">{{ $t('Files') }}</span>
					</button>
				</div>

				<div class="menu-divider"></div>

				<!-- Power Actions -->
				<div class="power-actions-row">
					<button type="button" class="power-btn is-restart" @click="restart">
						<b-icon icon="restart" pack="mdi" size="is-18"></b-icon>
						<span>{{ $t('Restart') }}</span>
					</button>
					<button type="button" class="power-btn is-shutdown" @click="shutdown">
						<b-icon icon="power" pack="mdi" size="is-18"></b-icon>
						<span>{{ $t('Shutdown') }}</span>
					</button>
				</div>
			</div>
		</transition>

		<b-modal v-model="showPowerModal" :can-cancel="false" scroll="clip" width="20rem">
			<b-message @close="resetPowerModal">
				<template #header>
					{{ $t(powerTitle) }}
				</template>
				<div>{{ $t(powerMessage) }}</div>
			</b-message>
		</b-modal>
	</div>
</template>

<script>
import { formatTime, formatDate, formatStrftime } from '@/utils/dateTimeFormat'
import systemPower from '@/mixins/systemPower'

export default {
	name: 'date-time-pill',
	mixins: [systemPower],
	data() {
		return {
			timer: 0,
			timeText: '',
			dateText: '',
			customText: '',
			menuOpen: false
		}
	},
	computed: {
		lang() {
			return this.$i18n.locale.replace('_', '-')
		},
		timeFormat() {
			return this.$store.state.timeFormat
		},
		dateFormatStyle() {
			return this.$store.state.dateFormatStyle
		},
		showSeconds() {
			return this.$store.state.showSeconds
		},
		customFormat() {
			return this.$store.state.customDateTimeFormat
		},
		userName() {
			return (this.$store.state.user && this.$store.state.user.username) || 'ayush'
		},
		userInitial() {
			return (this.userName || 'U').charAt(0).toUpperCase()
		},
		fullDateText() {
			const today = new Date()
			const options = { weekday: 'long', year: 'numeric', month: 'short', day: 'numeric' }
			try {
				return today.toLocaleDateString(this.lang, options)
			} catch (e) {
				return this.dateText
			}
		}
	},
	watch: {
		timeFormat() {
			this.updateClock()
		},
		dateFormatStyle() {
			this.updateClock()
		},
		showSeconds() {
			this.updateClock()
		},
		customFormat() {
			this.updateClock()
		},
		lang() {
			this.updateClock()
		}
	},
	mounted() {
		this.updateClock()
		this.timer = setInterval(this.updateClock, 1000)
		document.addEventListener('click', this.closeMenu)
	},
	beforeDestroy() {
		clearInterval(this.timer)
		document.removeEventListener('click', this.closeMenu)
	},
	methods: {
		updateClock() {
			const today = new Date()
			if (this.customFormat) {
				this.customText = formatStrftime(today, this.customFormat)
				return
			}
			this.timeText = formatTime(today, this.timeFormat, this.showSeconds)
			this.dateText = formatDate(today, this.lang, this.dateFormatStyle)
		},
		closeMenu() {
			this.menuOpen = false
		},
		openSettings(section = 'system') {
			this.menuOpen = false
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings',
				title: this.$t('Settings'),
				component: 'SettingsApp',
				width: 780,
				height: 560,
				props: { section }
			})
		},
		openTerminal() {
			this.menuOpen = false
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal',
				title: this.$t('Terminal'),
				component: 'TerminalApp',
				width: 800,
				height: 500
			})
		},
		openFiles() {
			this.menuOpen = false
			this.$store.commit('OPEN_WINDOW', {
				id: 'files',
				title: this.$t('Files'),
				component: 'FilesApp',
				width: 900,
				height: 580
			})
		},
		logout() {
			this.menuOpen = false
			this.$messageBus('account_setting_logout')
			this.$router.push('/logout')
		},
		restart() {
			this.menuOpen = false
			this.confirmPower('Restart')
		},
		shutdown() {
			this.menuOpen = false
			this.confirmPower('Shutdown')
		}
	}
}
</script>

<style lang="scss" scoped>
.datetime-pill-wrap {
	position: fixed;
	right: 1.5rem;
	bottom: 0.9rem;
	z-index: 500;
}

.datetime-pill {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	padding: 0.6rem 1.1rem;
	border: 1px solid rgba(255, 255, 255, 0.14);
	color: #ffffff;
	background: rgba(15, 23, 42, 0.65);
	backdrop-filter: blur(20px) saturate(180%);
	border-radius: 999px;
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
	white-space: nowrap;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover,
	&.is-active {
		background: rgba(15, 23, 42, 0.85);
		border-color: rgba(255, 255, 255, 0.25);
		transform: translateY(-1px);
	}
}

.datetime-text {
	display: flex;
	align-items: baseline;
	gap: 0.5rem;
	font-size: 0.8rem;
}

.pill-time {
	font-weight: 700;
}

.pill-sep {
	opacity: 0.5;
}

.pill-date {
	opacity: 0.85;
}

.quick-control-menu {
	position: absolute;
	right: 0;
	bottom: calc(100% + 0.75rem);
	width: 19rem;
	background: rgba(255, 255, 255, 0.96);
	backdrop-filter: blur(30px) saturate(190%);
	border: 1px solid rgba(226, 232, 240, 0.95);
	border-radius: 18px;
	box-shadow: 0 20px 40px -10px rgba(0, 0, 0, 0.25), 0 0 0 1px rgba(255, 255, 255, 0.8) inset;
	padding: 0.9rem;
	user-select: none;
	display: flex;
	flex-direction: column;
	gap: 0.65rem;
}

.menu-user-card {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	padding: 0.15rem 0.25rem;
}

.user-avatar-wrap {
	flex-shrink: 0;
}

.user-avatar-fallback {
	width: 2.25rem;
	height: 2.25rem;
	border-radius: 50%;
	background: linear-gradient(135deg, #3b82f6, #1d4ed8);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-weight: 700;
	font-size: 0.9rem;
	box-shadow: 0 2px 6px rgba(37, 99, 235, 0.25);
}

.user-info {
	display: flex;
	flex-direction: column;
	min-width: 0;
	flex: 1;
}

.user-name {
	font-size: 0.875rem;
	font-weight: 600;
	color: #1e293b;
	line-height: 1.2;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.user-status {
	font-size: 0.7rem;
	color: #64748b;
	font-weight: 400;
}

.signout-btn {
	margin-left: auto;
	border: 1px solid #fee2e2;
	background: #fff5f5;
	color: #ef4444;
	border-radius: 999px;
	padding: 0.25rem 0.65rem;
	font-size: 0.725rem;
	font-weight: 600;
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	transition: all 0.12s ease;

	&:hover {
		background: #fee2e2;
		color: #dc2626;
	}
}

.menu-clock-glance {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 0.65rem 0.85rem;
	text-align: center;
}

.glance-time {
	font-size: 1.5rem;
	font-weight: 700;
	color: #1e293b;
	font-variant-numeric: tabular-nums;
	line-height: 1.15;
}

.glance-date {
	font-size: 0.75rem;
	font-weight: 500;
	color: #64748b;
	margin-top: 0.2rem;
	display: flex;
	align-items: center;
	justify-content: center;
}

.shortcuts-grid {
	display: grid;
	grid-template-columns: repeat(4, 1fr);
	gap: 0.45rem;
}

.shortcut-tile {
	border: 1px solid #f1f5f9;
	background: #f8fafc;
	border-radius: 10px;
	padding: 0.55rem 0.25rem;
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.35rem;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: #ffffff;
		border-color: #cbd5e1;
		transform: translateY(-1px);
		box-shadow: 0 2px 4px rgba(0, 0, 0, 0.04);
	}
}

.shortcut-icon-box {
	width: 2rem;
	height: 2rem;
	border-radius: 8px;
	display: flex;
	align-items: center;
	justify-content: center;

	&.is-blue {
		background: rgba(37, 99, 235, 0.1);
		color: #2563eb;
	}
	&.is-indigo {
		background: rgba(99, 102, 241, 0.1);
		color: #6366f1;
	}
	&.is-slate {
		background: rgba(15, 23, 42, 0.08);
		color: #1e293b;
	}
	&.is-amber {
		background: rgba(245, 158, 11, 0.12);
		color: #d97706;
	}
}

.shortcut-name {
	font-size: 0.6875rem;
	font-weight: 600;
	color: #475569;
	white-space: nowrap;
}

.power-actions-row {
	display: flex;
	gap: 0.5rem;
}

.power-btn {
	flex: 1;
	border: none;
	padding: 0.55rem 0.75rem;
	border-radius: 10px;
	font-size: 0.775rem;
	font-weight: 600;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 0.4rem;
	transition: all 0.12s ease;

	&.is-restart {
		background: #f1f5f9;
		color: #1e293b;

		&:hover {
			background: #e2e8f0;
		}
	}

	&.is-shutdown {
		background: #fee2e2;
		color: #dc2626;

		&:hover {
			background: #fecaca;
		}
	}
}

.menu-divider {
	height: 1px;
	background: #f1f5f9;
	margin: 0 0.15rem;
}

.pop-up-enter-active {
	transition: all 0.18s cubic-bezier(0.16, 1, 0.3, 1);
}

.pop-up-leave-active {
	transition: all 0.12s ease-in;
}

.pop-up-enter {
	opacity: 0;
	transform: translateY(8px) scale(0.96);
}

.pop-up-leave-to {
	opacity: 0;
	transform: translateY(6px) scale(0.97);
}
</style>
