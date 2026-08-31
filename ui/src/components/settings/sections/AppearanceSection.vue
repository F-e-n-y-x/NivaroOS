<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Appearance') }}</h2>

		<h3 class="setting-card-title">{{ $t('Wallpaper') }}</h3>
		<div class="setting-card">
			<wallpaper-modal embedded></wallpaper-modal>
		</div>

		<h3 class="setting-card-title">{{ $t('Theme & Window Style') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="palette-outline" pack="mdi" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ $t('Accent Color') }}</div>
					<div class="setting-desc">{{ $t('Primary system highlight and focus color') }}</div>
				</div>
				<div class="row-control">
					<div class="accent-swatches">
						<button
							v-for="c in accentColors"
							:key="c.name"
							type="button"
							class="accent-swatch"
							:class="{ active: currentAccent === c.value }"
							:style="{ backgroundColor: c.value }"
							:title="$t(c.name)"
							@click="selectAccent(c.value)"
						>
							<i v-if="currentAccent === c.value" class="mdi mdi-check"></i>
						</button>
					</div>
				</div>
			</div>

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
					<div class="setting-desc">{{ $t('Restore standard transparency and blur values') }}</div>
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
import WallpaperModal from '@/components/wallpaper/WallpaperModal.vue'
import WidgetVisibilityPanel from '@/components/settings/WidgetVisibilityPanel.vue'

export const ROWS = [
	{ label: 'Wallpaper' },
	{ label: 'Accent Color' },
	{ label: 'Window transparency' },
	{ label: 'Window blur' },
	{ label: 'Widgets' }
]

const ACCENT_COLORS = [
	{ name: 'Nivaro Blue', value: '#2563eb' },
	{ name: 'Emerald', value: '#10b981' },
	{ name: 'Violet', value: '#8b5cf6' },
	{ name: 'Rose', value: '#f43f5e' },
	{ name: 'Amber', value: '#f59e0b' },
	{ name: 'Slate', value: '#475569' }
]

export default {
	name: 'appearance-section',
	components: { WallpaperModal, WidgetVisibilityPanel },
	data() {
		return {
			backdropAlphaPct: 100,
			backdropBlurPx: 0,
			currentAccent: localStorage.getItem('uiAccentColor') || '#2563eb',
			accentColors: ACCENT_COLORS
		}
	},
	created() {
		this.restoreBackdropSettings()
	},
	methods: {
		restoreBackdropSettings() {
			const alpha = localStorage.getItem('uiBackdropAlpha')
			const blur = localStorage.getItem('uiBackdropBlur')
			const accent = localStorage.getItem('uiAccentColor')
			this.backdropAlphaPct = alpha !== null ? Math.round(parseFloat(alpha) * 100) : 100
			this.backdropBlurPx = blur !== null ? parseFloat(blur) : 0
			if (accent) {
				this.currentAccent = accent
				document.documentElement.style.setProperty('--primary-color', accent)
			}

			this.$api.users.getCustomStorage('appearance').then(res => {
				if (res.data.success === 200 && res.data.data) {
					const { alpha, blur, accent } = res.data.data
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
					if (accent) {
						this.currentAccent = accent
						document.documentElement.style.setProperty('--primary-color', accent)
						localStorage.setItem('uiAccentColor', accent)
					}
				}
			}).catch(() => {})
		},
		selectAccent(color) {
			this.currentAccent = color
			document.documentElement.style.setProperty('--primary-color', color)
			localStorage.setItem('uiAccentColor', color)
			this.saveAppearanceSettings()
		},
		saveAppearanceSettings() {
			clearTimeout(this._saveTimer)
			this._saveTimer = setTimeout(() => {
				const alpha = this.backdropAlphaPct / 100
				const blur = this.backdropBlurPx
				const accent = this.currentAccent
				this.$api.users.setCustomStorage('appearance', { alpha, blur, accent }).catch(() => {})
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
			this.backdropAlphaPct = 100
			this.backdropBlurPx = 0
			this.selectAccent('#2563eb')
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
.accent-swatches {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

.accent-swatch {
	width: 24px;
	height: 24px;
	border-radius: 50%;
	border: 2px solid #ffffff;
	box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.15);
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	color: #ffffff;
	font-size: 0.75rem;
	transition: all 0.15s ease;

	&:hover {
		transform: scale(1.15);
	}

	&.active {
		transform: scale(1.15);
		box-shadow: 0 0 0 2px #2563eb;
	}
}
</style>
