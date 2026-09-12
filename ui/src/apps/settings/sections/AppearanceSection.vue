<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Appearance') }}</h2>

		<h3 class="setting-card-title">{{ $t('Color Theme') }}</h3>
		<div class="setting-card">
			<div class="theme-picker-grid">
				<div
					v-for="opt in themeOptions"
					:key="opt.value"
					class="theme-card-option"
					:class="{ 'is-selected': currentThemeMode === opt.value }"
					@click="selectTheme(opt.value)"
					role="button"
					tabindex="0"
					@keydown.enter="selectTheme(opt.value)"
					@keydown.space.prevent="selectTheme(opt.value)"
				>
					<div class="theme-preview-box">
						<img :src="themeIcons[opt.value]" :alt="opt.label" class="theme-preview-img" />
					</div>

					<div class="theme-meta">
						<div class="theme-text">
							<span class="theme-title">
								<b-icon :icon="opt.icon" pack="mdi" size="is-16" class="mr-1"></b-icon>
								{{ $t(opt.label) }}
							</span>
							<span class="theme-sub">{{ $t(opt.desc) }}</span>
						</div>
						<div class="check-icon" v-if="currentThemeMode === opt.value">
							<b-icon icon="check-circle" pack="casa" size="is-20"></b-icon>
						</div>
					</div>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Wallpaper') }}</h3>
		<div class="setting-card">
			<wallpaper-modal embedded></wallpaper-modal>
		</div>

		<h3 class="setting-card-title">{{ $t('Window Transparency & Blur') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="circle-opacity" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Window transparency') }}</div>
					<div class="setting-desc">{{ $t('Background opacity for windows and panels') }}</div>
				</div>
				<div class="row-control slider-control">
					<span class="slider-hint">{{ $t('Glass') }}</span>
					<input class="pretty-range" v-model.number="backdropAlphaPct" type="range" min="40" max="100" step="1"
						:style="rangeStyle(backdropAlphaPct, 40, 100)" @input="applyBackdropAlpha" />
					<span class="slider-hint">{{ $t('Opaque') }}</span>
					<span class="slider-value">{{ backdropAlphaPct }}%</span>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="blur-radial" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Window blur') }}</div>
					<div class="setting-desc">{{ $t('Frosted glass blur effect strength') }}</div>
				</div>
				<div class="row-control slider-control">
					<span class="slider-hint">{{ $t('None') }}</span>
					<input class="pretty-range" v-model.number="backdropBlurPx" type="range" min="0" max="24" step="1"
						:style="rangeStyle(backdropBlurPx, 0, 24)" @input="applyBackdropBlur" />
					<span class="slider-hint">{{ $t('Strong') }}</span>
					<span class="slider-value">{{ backdropBlurPx }}px</span>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="restore" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Reset to Defaults') }}</div>
					<div class="setting-desc">{{ $t('Restore standard theme, transparency and blur values') }}</div>
				</div>
				<div class="row-control">
					<b-button rounded size="is-small" @click="resetToDefaults">{{ $t('Reset') }}</b-button>
				</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Widgets') }}</h3>
		<div class="setting-card">
			<widget-visibility-panel></widget-visibility-panel>
		</div>
	</section>
</template>

<script>
import WallpaperModal from '@/shell/wallpaper/WallpaperModal.vue'
import WidgetVisibilityPanel from '@/apps/settings/WidgetVisibilityPanel.vue'
import { THEME_MODES, getStoredThemeMode, applyTheme } from '@/utils/theme'
import lightIcon from '@/assets/img/theme/light.svg'
import darkIcon from '@/assets/img/theme/dark.svg'
import autoIcon from '@/assets/img/theme/auto.svg'

export const ROWS = [
	{ label: 'Color Theme' },
	{ label: 'Dark mode' },
	{ label: 'Light mode' },
	{ label: 'Auto theme' },
	{ label: 'Wallpaper' },
	{ label: 'Window transparency' },
	{ label: 'Window blur' },
	{ label: 'Widgets' }
]

export default {
	name: 'appearance-section',
	components: { WallpaperModal, WidgetVisibilityPanel },
	data() {
		return {
			currentThemeMode: getStoredThemeMode(),
			themeOptions: [
				{
					value: THEME_MODES.AUTO,
					label: 'Auto (System)',
					desc: 'Follow system appearance',
					icon: 'theme-light-dark'
				},
				{
					value: THEME_MODES.LIGHT,
					label: 'Light Mode',
					desc: 'Clean bright interface',
					icon: 'white-balance-sunny'
				},
				{
					value: THEME_MODES.DARK,
					label: 'Dark Mode',
					desc: 'Eye-friendly dark interface',
					icon: 'weather-night'
				}
			],
			backdropAlphaPct: 40,
			backdropBlurPx: 5,
			themeIcons: {
				[THEME_MODES.LIGHT]: lightIcon,
				[THEME_MODES.DARK]: darkIcon,
				[THEME_MODES.AUTO]: autoIcon
			}
		}
	},
	created() {
		this.restoreBackdropSettings()
	},
	mounted() {
		this.onThemeChangeHandler = (e) => {
			if (e && e.detail && e.detail.mode) {
				this.currentThemeMode = e.detail.mode
			}
		}
		window.addEventListener('nivaroos:theme-change', this.onThemeChangeHandler)
	},
	beforeDestroy() {
		if (this.onThemeChangeHandler) {
			window.removeEventListener('nivaroos:theme-change', this.onThemeChangeHandler)
		}
	},
	methods: {
		selectTheme(mode) {
			this.currentThemeMode = mode
			applyTheme(mode)
			this.saveAppearanceSettings()
		},
		restoreBackdropSettings() {
			this.currentThemeMode = getStoredThemeMode()
			const alpha = localStorage.getItem('uiBackdropAlpha')
			const blur = localStorage.getItem('uiBackdropBlur')
			this.backdropAlphaPct = alpha !== null ? Math.round(parseFloat(alpha) * 100) : 40
			this.backdropBlurPx = blur !== null ? parseFloat(blur) : 5

			this.$api.users.getCustomStorage('appearance').then(res => {
				if (res.data.success === 200 && res.data.data) {
					const { alpha, blur, theme } = res.data.data
					if (theme && Object.values(THEME_MODES).includes(theme)) {
						this.currentThemeMode = theme
						applyTheme(theme)
					}
					if (alpha !== undefined && alpha !== null) {
						this.backdropAlphaPct = Math.round(parseFloat(alpha) * 100)
						document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha)
						localStorage.setItem('uiBackdropAlpha', alpha)
					}
					if (blur !== undefined && blur !== null) {
						this.backdropBlurPx = parseFloat(blur)
						document.documentElement.style.setProperty('--ui-backdrop-blur', `${blur}px`)
						localStorage.setItem('uiBackdropBlur', blur)
					}
				}
			}).catch(() => {})
		},
		saveAppearanceSettings() {
			clearTimeout(this._saveTimer)
			this._saveTimer = setTimeout(() => {
				const alpha = this.backdropAlphaPct / 100
				const blur = this.backdropBlurPx
				const theme = this.currentThemeMode
				this.$api.users.setCustomStorage('appearance', { alpha, blur, theme }).catch(() => {})
			}, 300)
		},
		applyBackdropAlpha() {
			const alpha = this.backdropAlphaPct / 100
			document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha)
			localStorage.setItem('uiBackdropAlpha', alpha)
			this.saveAppearanceSettings()
		},
		applyBackdropBlur() {
			document.documentElement.style.setProperty('--ui-backdrop-blur', `${this.backdropBlurPx}px`)
			localStorage.setItem('uiBackdropBlur', this.backdropBlurPx)
			this.saveAppearanceSettings()
		},
		resetToDefaults() {
			this.backdropAlphaPct = 40
			this.backdropBlurPx = 5
			this.currentThemeMode = THEME_MODES.AUTO
			applyTheme(THEME_MODES.AUTO)
			this.applyBackdropAlpha()
			this.applyBackdropBlur()
			this.$buefy.toast.open({ message: this.$t('Appearance reset to defaults'), type: 'is-success' })
		},
		rangeStyle(value, min, max) {
			return { '--pct': `${((value - min) / (max - min)) * 100}%` }
		}
	}
}
</script>

<style lang="scss" scoped>
.theme-picker-grid {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: var(--space-4);
	padding: var(--space-5);

	@media (max-width: 680px) {
		grid-template-columns: 1fr;
		gap: var(--space-3);
		padding: var(--space-4);
	}
}

.theme-card-option {
	position: relative;
	display: flex;
	flex-direction: column;
	background: var(--theme-card-bg, #f8fafc); border-color: var(--theme-card-border, #e2e8f0);
	border: 2px solid var(--theme-card-border, #e2e8f0);
	border-radius: var(--radius-card);
	padding: var(--space-3);
	cursor: pointer;
	transition: all 0.2s ease;
	outline: none;
	user-select: none;

	&:hover {
		border-color: var(--theme-card-border, #cbd5e1);
		transform: translateY(-2px);
		box-shadow: var(--shadow-sm);
	}

	&.is-selected {
		border-color: #2563eb;
		background: rgba(37, 99, 235, 0.08);
		box-shadow: 0 0 0 1px #2563eb;
	}
}

.theme-preview-box {
	width: 100%;
	border-radius: var(--radius-control);
	overflow: hidden;
	margin-bottom: var(--space-3);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	line-height: 0;

	.theme-preview-img {
		width: 100%;
		height: auto;
		display: block;
		border-radius: var(--radius-sm);
	}
}

.theme-meta {
	display: flex;
	align-items: center;
	justify-content: space-between;

	.theme-text {
		display: flex;
		flex-direction: column;
		min-width: 0;

		.theme-title {
			display: flex;
			align-items: center;
			font-size: var(--font-base);
			font-weight: 600;
			color: var(--theme-text-primary, #1e293b);
			white-space: nowrap;
		}

		.theme-sub {
			font-size: var(--font-xs);
			color: var(--theme-text-secondary, #64748b);
			margin-top: var(--space-1);
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
	}

	.check-icon {
		color: #2563eb;
		flex-shrink: 0;
		margin-left: var(--space-2);
	}
}
</style>

