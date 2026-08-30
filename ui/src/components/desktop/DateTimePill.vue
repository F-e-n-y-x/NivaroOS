<template>
	<div class="datetime-pill-wrap">
		<button type="button" class="datetime-pill" @click.stop="menuOpen = !menuOpen">
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

		<div v-if="menuOpen" class="pill-menu" @click.stop>
			<button type="button" class="pill-menu-item" @click="openSettings">
				<b-icon icon="system-outline" pack="casa" size="is-20"></b-icon>
				{{ $t('Open Settings') }}
			</button>
			<div class="pill-menu-sep"></div>
			<button type="button" class="pill-menu-item" @click="restart">
				<b-icon icon="restart-outline" pack="casa" size="is-20"></b-icon>
				{{ $t('Restart') }}
			</button>
			<button type="button" class="pill-menu-item is-danger" @click="shutdown">
				<b-icon icon="shutdown-outline" pack="casa" size="is-20"></b-icon>
				{{ $t('Shutdown') }}
			</button>
		</div>

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
		openSettings() {
			this.menuOpen = false
			this.$store.commit('OPEN_WINDOW', {
				id: 'settings', title: this.$t('Settings'), component: 'SettingsApp', width: 760, height: 540,
				props: { section: 'system' }
			})
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
	border: $backDropBorder;
	color: $white;
	background: $backDropColor;
	backdrop-filter: $backDropBlur;
	border-radius: 999px;
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25), $backDropShadow;
	white-space: nowrap;
	cursor: pointer;
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

.pill-menu {
	position: absolute;
	right: 0;
	bottom: calc(100% + 0.6rem);
	min-width: 11rem;
	padding: 0.4rem;
	color: $white;
	background: $backDropColor;
	backdrop-filter: $backDropBlur;
	border: $backDropBorder;
	border-radius: 14px;
	box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25), $backDropShadow;
}

.pill-menu-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	border: none;
	background: transparent;
	color: inherit;
	padding: 0.55rem 0.65rem;
	border-radius: 9px;
	font-size: 0.8rem;
	text-align: left;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.12);
	}

	&.is-danger:hover {
		background: hsla(0, 70%, 55%, 0.25);
	}
}

.pill-menu-sep {
	height: 1px;
	margin: 0.3rem 0.3rem;
	background: rgba(255, 255, 255, 0.15);
}
</style>
