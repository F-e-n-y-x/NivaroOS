<template>
	<div class="datetime-pill-wrap">
		<button type="button" class="datetime-pill" :class="{ 'is-active': menuOpen }" @click.stop="toggleMenu">
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
				<!-- Top Header: User Profile + System Actions -->
				<div class="menu-top-header">
					<div class="user-profile-left">
						<div class="user-avatar">{{ userInitial }}</div>
						<div class="user-meta">
							<span class="user-name">{{ userName }}</span>
							<span class="user-badge">{{ $t('Signed in') }}</span>
						</div>
					</div>
					<div class="header-actions-right">
						<button type="button" class="hdr-btn" :title="$t('Settings')" @click="openSettings('system')">
							<i class="mdi mdi-cog-outline"></i>
						</button>
						<button type="button" class="hdr-btn is-logout" :title="$t('Sign out')" @click="logout">
							<i class="mdi mdi-logout"></i>
						</button>
					</div>
				</div>

				<div class="menu-divider"></div>

				<!-- Digital Clock Glance -->
				<div class="clock-hero-card">
					<div class="hero-time">{{ timeText }}</div>
					<div class="hero-date">
						<i class="mdi mdi-calendar-blank-outline mr-1"></i>{{ fullDateText }}
					</div>
				</div>

				<div class="menu-divider"></div>

				<!-- Mini Interactive Calendar -->
				<div class="calendar-widget">
					<!-- Calendar Nav Header -->
					<div class="cal-nav-header">
						<span class="cal-month-year">{{ calendarMonthYear }}</span>
						<div class="cal-nav-controls">
							<button type="button" class="cal-btn-today" @click="goToToday">
								{{ $t('Today') }}
							</button>
							<button type="button" class="cal-arrow-btn" :title="$t('Previous Month')" @click="prevMonth">
								<i class="mdi mdi-chevron-left"></i>
							</button>
							<button type="button" class="cal-arrow-btn" :title="$t('Next Month')" @click="nextMonth">
								<i class="mdi mdi-chevron-right"></i>
							</button>
						</div>
					</div>

					<!-- Weekday Header -->
					<div class="cal-weekdays-grid">
						<span v-for="(dayName, idx) in weekdays" :key="idx" class="cal-weekday-label">
							{{ dayName }}
						</span>
					</div>

					<!-- Days Grid (7x6) -->
					<div class="cal-days-grid">
						<button
							v-for="(day, idx) in calendarDays"
							:key="idx"
							type="button"
							class="cal-day-cell"
							:class="{
								'is-today': day.isToday,
								'is-selected': day.isSelected && !day.isToday,
								'is-other-month': !day.isCurrentMonth
							}"
							@click="selectDay(day)"
						>
							<span class="day-num">{{ day.dayNumber }}</span>
						</button>
					</div>
				</div>

				<div class="menu-divider"></div>

				<!-- Power Actions Footer -->
				<div class="power-actions-footer">
					<button type="button" class="pwr-btn is-restart" @click="restart">
						<i class="mdi mdi-restart pwr-icon"></i>
						<span>{{ $t('Restart') }}</span>
					</button>
					<button type="button" class="pwr-btn is-shutdown" @click="shutdown">
						<i class="mdi mdi-power pwr-icon"></i>
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
			menuOpen: false,
			currentViewingDate: new Date(),
			selectedDate: new Date()
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
		},
		calendarMonthYear() {
			const options = { month: 'long', year: 'numeric' }
			try {
				return this.currentViewingDate.toLocaleDateString(this.lang, options)
			} catch (e) {
				return `${this.currentViewingDate.toLocaleString('default', { month: 'long' })} ${this.currentViewingDate.getFullYear()}`
			}
		},
		weekdays() {
			return ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']
		},
		calendarDays() {
			const year = this.currentViewingDate.getFullYear()
			const month = this.currentViewingDate.getMonth()

			const firstDayOfMonth = new Date(year, month, 1)
			const startingDayOfWeek = firstDayOfMonth.getDay()

			const lastDayOfMonth = new Date(year, month + 1, 0)
			const totalDaysInMonth = lastDayOfMonth.getDate()

			const lastDayOfPrevMonth = new Date(year, month, 0).getDate()

			const days = []
			const today = new Date()
			const isSameDay = (d1, d2) =>
				d1 && d2 &&
				d1.getFullYear() === d2.getFullYear() &&
				d1.getMonth() === d2.getMonth() &&
				d1.getDate() === d2.getDate()

			// Previous month padding days
			for (let i = startingDayOfWeek - 1; i >= 0; i--) {
				const d = new Date(year, month - 1, lastDayOfPrevMonth - i)
				days.push({
					dayNumber: lastDayOfPrevMonth - i,
					date: d,
					isCurrentMonth: false,
					isToday: isSameDay(d, today),
					isSelected: isSameDay(d, this.selectedDate)
				})
			}

			// Current month days
			for (let i = 1; i <= totalDaysInMonth; i++) {
				const d = new Date(year, month, i)
				days.push({
					dayNumber: i,
					date: d,
					isCurrentMonth: true,
					isToday: isSameDay(d, today),
					isSelected: isSameDay(d, this.selectedDate)
				})
			}

			// Next month padding days to fill 42 cells (6 rows x 7 cols)
			const remainingCells = 42 - days.length
			for (let i = 1; i <= remainingCells; i++) {
				const d = new Date(year, month + 1, i)
				days.push({
					dayNumber: i,
					date: d,
					isCurrentMonth: false,
					isToday: isSameDay(d, today),
					isSelected: isSameDay(d, this.selectedDate)
				})
			}

			return days
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
		this.onCloseTrayPopovers = (sender) => {
			if (sender !== 'datetime') {
				this.menuOpen = false
			}
		}
		this.$EventBus.$on('desktop:close-tray-popovers', this.onCloseTrayPopovers)
	},
	beforeDestroy() {
		clearInterval(this.timer)
		document.removeEventListener('click', this.closeMenu)
		this.$EventBus.$off('desktop:close-tray-popovers', this.onCloseTrayPopovers)
	},
	methods: {
		toggleMenu() {
			this.menuOpen = !this.menuOpen
			if (this.menuOpen) {
				this.$EventBus.$emit('desktop:close-tray-popovers', 'datetime')
			}
		},
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
		prevMonth() {
			const d = new Date(this.currentViewingDate)
			d.setMonth(d.getMonth() - 1)
			this.currentViewingDate = d
		},
		nextMonth() {
			const d = new Date(this.currentViewingDate)
			d.setMonth(d.getMonth() + 1)
			this.currentViewingDate = d
		},
		goToToday() {
			this.currentViewingDate = new Date()
			this.selectedDate = new Date()
		},
		selectDay(day) {
			this.selectedDate = day.date
			if (!day.isCurrentMonth) {
				this.currentViewingDate = new Date(day.date)
			}
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
	position: static;
	display: flex;
	align-items: center;
}

.datetime-pill {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-4);
	border: $backDropBorder;
	color: $white;
	background-color: $backDropColor;
	backdrop-filter: $backDropBlur;
	-webkit-backdrop-filter: $backDropBlur;
	border-radius: var(--radius-pill);
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25), $backDropShadow;
	white-space: nowrap;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover,
	&.is-active {
		filter: brightness(1.15);
		box-shadow: 0 12px 32px rgba(0, 0, 0, 0.3), $backDropShadow;
		transform: translateY(-1px);
	}
}

.datetime-text {
	display: flex;
	align-items: baseline;
	gap: var(--space-2);
	font-size: var(--font-sm);
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
	width: 20rem;
	max-width: calc(100vw - 3rem);
	background: var(--theme-card-bg, #ffffff); border: 1px solid var(--theme-card-border, #e2e8f0); color: var(--theme-text-primary, #1e293b);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-modal);
	box-shadow: var(--shadow-lg);
	padding: var(--space-4);
	user-select: none;
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	color: var(--theme-text-primary, #1e293b);
	z-index: 1000;
}

.menu-top-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.1rem 0.15rem;
}

.user-profile-left {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	min-width: 0;
}

.user-avatar {
	width: 2.1rem;
	height: 2.1rem;
	border-radius: 50%;
	background: linear-gradient(135deg, #3b82f6, #1d4ed8);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-weight: 700;
	font-size: var(--font-base);
	box-shadow: 0 2px 5px rgba(37, 99, 235, 0.25);
	flex-shrink: 0;
}

.user-meta {
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.user-name {
	font-size: var(--font-base);
	font-weight: 600;
	color: var(--theme-text-primary, #1e293b);
	line-height: 1.2;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.user-badge {
	font-size: var(--font-2xs);
	color: var(--theme-text-muted, #64748b);
	font-weight: 400;
}

.header-actions-right {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.hdr-btn {
	border: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-card-subtle, #f8fafc); border: 1px solid var(--theme-card-border, #e2e8f0); color: var(--theme-text-secondary, #64748b);
	color: var(--theme-text-muted, #64748b);
	border-radius: 50%;
	width: 2rem;
	height: 2rem;
	min-width: 2rem;
	min-height: 2rem;
	padding: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	transition: all 0.15s ease;
	outline: none;

	i {
		font-size: var(--font-md);
		line-height: 1;
		display: inline-block;
	}

	&:hover {
		background: var(--theme-card-hover, #f1f5f9); color: var(--theme-text-primary, #1e293b);
		border-color: var(--theme-card-border, #cbd5e1);
		transform: translateY(-1px);
	}

	&.is-logout:hover {
		background: rgba(239, 68, 68, 0.1);
		color: #dc2626;
		border-color: #fca5a5;
	}
}

.clock-hero-card {
	background: var(--theme-card-subtle, #f8fafc); border: 1px solid var(--theme-card-border, #e2e8f0);
	border: 1px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	padding: var(--space-3) var(--space-3);
	text-align: center;
}

.hero-time {
	font-size: var(--font-2xl);
	font-weight: 700;
	color: var(--theme-text-primary, #1e293b);
	font-variant-numeric: tabular-nums;
	line-height: 1.15;
}

.hero-date {
	font-size: var(--font-xs);
	font-weight: 500;
	color: var(--theme-text-muted, #64748b);
	margin-top: var(--space-1);
	display: flex;
	align-items: center;
	justify-content: center;
}

/* Mini Calendar Styling */
.calendar-widget {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.cal-nav-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0 var(--space-1);
}

.cal-month-year {
	font-size: var(--font-base);
	font-weight: 700;
	color: var(--theme-text-primary, #1e293b);
}

.cal-nav-controls {
	display: flex;
	align-items: center;
	gap: var(--space-1);
}

.cal-btn-today {
	border: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-card-subtle, #f8fafc); border: 1px solid var(--theme-card-border, #e2e8f0); color: var(--theme-text-secondary, #475569);
	color: var(--theme-text-secondary, #475569);
	border-radius: var(--radius-sm);
	padding: var(--space-1) var(--space-2);
	font-size: var(--font-2xs);
	font-weight: 600;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		background: var(--theme-card-hover, #f1f5f9); color: var(--theme-text-primary, #1e293b);
		border-color: var(--theme-card-border, #cbd5e1);
	}
}

.cal-arrow-btn {
	border: 1px solid var(--theme-card-border, #e2e8f0);
	background: var(--theme-card-subtle, #f8fafc); border: 1px solid var(--theme-card-border, #e2e8f0); color: var(--theme-text-secondary, #64748b);
	color: var(--theme-text-muted, #64748b);
	border-radius: var(--radius-sm);
	width: 1.6rem;
	height: 1.6rem;
	min-width: 1.6rem;
	min-height: 1.6rem;
	padding: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	transition: all 0.15s ease;
	outline: none;

	i {
		font-size: var(--font-lg);
		line-height: 1;
		display: inline-block;
	}

	&:hover {
		background: var(--theme-card-hover, #f1f5f9); color: var(--theme-text-primary, #1e293b);
		border-color: var(--theme-card-border, #cbd5e1);
	}
}

.cal-weekdays-grid {
	display: grid;
	grid-template-columns: repeat(7, 1fr);
	text-align: center;
}

.cal-weekday-label {
	font-size: var(--font-2xs);
	font-weight: 600;
	color: var(--theme-text-muted, #94a3b8);
	text-transform: uppercase;
	padding: var(--space-1) 0;
}

.cal-days-grid {
	display: grid;
	grid-template-columns: repeat(7, 1fr);
	gap: 0.15rem;
}

.cal-day-cell {
	border: none;
	background: transparent;
	border-radius: 50%;
	aspect-ratio: 1;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	font-size: var(--font-xs);
	font-weight: 500;
	color: var(--theme-text-primary, #1e293b);
	transition: all 0.12s ease;

	&:hover {
		background: var(--theme-card-hover, #f1f5f9);
	}

	&.is-other-month {
		color: var(--theme-text-muted, #cbd5e1);
	}

	&.is-selected {
		background: rgba(37, 99, 235, 0.12);
		border: 1px solid rgba(37, 99, 235, 0.4);
		color: #2563eb;
		font-weight: 600;
	}

	&.is-today {
		background: #2563eb;
		color: #ffffff;
		font-weight: 700;
		box-shadow: 0 2px 6px rgba(37, 99, 235, 0.35);
	}
}

.power-actions-footer {
	display: flex;
	gap: var(--space-2);
}

.pwr-btn {
	flex: 1;
	border: 1px solid var(--theme-card-border, #e2e8f0);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-card);
	font-size: var(--font-xs);
	font-weight: 600;
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	transition: all 0.15s ease;
	line-height: 1;

	.pwr-icon {
		font-size: var(--font-md);
		line-height: 1;
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}

	span {
		line-height: 1;
	}

	&.is-restart {
		background: var(--theme-card-subtle, #f8fafc);
		color: var(--theme-text-primary, #1e293b);

		&:hover {
			background: var(--theme-card-hover, #f1f5f9);
			border-color: var(--theme-card-border, #cbd5e1);
		}
	}

	&.is-shutdown {
		background: rgba(239, 68, 68, 0.1);
		border-color: rgba(239, 68, 68, 0.4);
		color: #dc2626;

		&:hover {
			background: rgba(252, 165, 165, 0.3);
			color: #f87171;
		}
	}
}

.menu-divider {
	height: 1px;
	background: var(--theme-card-border, #f1f5f9);
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
