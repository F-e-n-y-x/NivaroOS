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
				<!-- Top Header: User Profile + Quick Actions -->
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
							<b-icon icon="cog-outline" pack="mdi" size="is-16"></b-icon>
						</button>
						<button type="button" class="hdr-btn is-logout" :title="$t('Sign out')" @click="logout">
							<b-icon icon="logout" pack="mdi" size="is-16"></b-icon>
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
								<b-icon icon="chevron-left" pack="mdi" size="is-16"></b-icon>
							</button>
							<button type="button" class="cal-arrow-btn" :title="$t('Next Month')" @click="nextMonth">
								<b-icon icon="chevron-right" pack="mdi" size="is-16"></b-icon>
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
						<b-icon icon="restart" pack="mdi" size="is-16"></b-icon>
						<span>{{ $t('Restart') }}</span>
					</button>
					<button type="button" class="pwr-btn is-shutdown" @click="shutdown">
						<b-icon icon="power" pack="mdi" size="is-16"></b-icon>
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
			const startingDayOfWeek = firstDayOfMonth.getDay() // 0 = Sunday

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
	width: 20rem;
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

.menu-top-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.15rem 0.25rem;
}

.user-profile-left {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	min-width: 0;
}

.user-avatar {
	width: 2rem;
	height: 2rem;
	border-radius: 50%;
	background: linear-gradient(135deg, #3b82f6, #1d4ed8);
	color: #ffffff;
	display: flex;
	align-items: center;
	justify-content: center;
	font-weight: 700;
	font-size: 0.85rem;
	box-shadow: 0 2px 5px rgba(37, 99, 235, 0.25);
	flex-shrink: 0;
}

.user-meta {
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.user-name {
	font-size: 0.85rem;
	font-weight: 600;
	color: #1e293b;
	line-height: 1.2;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.user-badge {
	font-size: 0.6875rem;
	color: #64748b;
	font-weight: 400;
}

.header-actions-right {
	display: flex;
	align-items: center;
	gap: 0.35rem;
}

.hdr-btn {
	border: 1px solid #e2e8f0;
	background: #f8fafc;
	color: #64748b;
	border-radius: 50%;
	width: 1.85rem;
	height: 1.85rem;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: #f1f5f9;
		color: #1e293b;
		border-color: #cbd5e1;
	}

	&.is-logout:hover {
		background: #fee2e2;
		color: #dc2626;
		border-color: #fca5a5;
	}
}

.clock-hero-card {
	background: #f8fafc;
	border: 1px solid #e2e8f0;
	border-radius: 12px;
	padding: 0.65rem 0.85rem;
	text-align: center;
}

.hero-time {
	font-size: 1.5rem;
	font-weight: 700;
	color: #1e293b;
	font-variant-numeric: tabular-nums;
	line-height: 1.15;
}

.hero-date {
	font-size: 0.75rem;
	font-weight: 500;
	color: #64748b;
	margin-top: 0.2rem;
	display: flex;
	align-items: center;
	justify-content: center;
}

/* Mini Calendar Styling */
.calendar-widget {
	display: flex;
	flex-direction: column;
	gap: 0.45rem;
}

.cal-nav-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0 0.2rem;
}

.cal-month-year {
	font-size: 0.85rem;
	font-weight: 700;
	color: #1e293b;
}

.cal-nav-controls {
	display: flex;
	align-items: center;
	gap: 0.25rem;
}

.cal-btn-today {
	border: 1px solid #e2e8f0;
	background: #f8fafc;
	color: #475569;
	border-radius: 6px;
	padding: 0.15rem 0.45rem;
	font-size: 0.7rem;
	font-weight: 600;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: #f1f5f9;
		color: #1e293b;
	}
}

.cal-arrow-btn {
	border: 1px solid #e2e8f0;
	background: #f8fafc;
	color: #64748b;
	border-radius: 6px;
	width: 1.5rem;
	height: 1.5rem;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		background: #f1f5f9;
		color: #1e293b;
	}
}

.cal-weekdays-grid {
	display: grid;
	grid-template-columns: repeat(7, 1fr);
	text-align: center;
}

.cal-weekday-label {
	font-size: 0.6875rem;
	font-weight: 600;
	color: #94a3b8;
	text-transform: uppercase;
	padding: 0.2rem 0;
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
	font-size: 0.775rem;
	font-weight: 500;
	color: #1e293b;
	transition: all 0.12s ease;

	&:hover {
		background: #f1f5f9;
	}

	&.is-other-month {
		color: #cbd5e1;
	}

	&.is-selected {
		background: rgba(37, 99, 235, 0.12);
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
	gap: 0.5rem;
}

.pwr-btn {
	flex: 1;
	border: none;
	padding: 0.5rem 0.75rem;
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
