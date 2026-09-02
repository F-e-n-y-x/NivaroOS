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
	{ label: 'Window transparency' },
	{ label: 'Window blur' },
	{ label: 'Widgets' }
]

export default {
	name: 'appearance-section',
	components: { WallpaperModal, WidgetVisibilityPanel },
	data() {
		return {
			backdropAlphaPct: 40,
			backdropBlurPx: 5
		}
	},
	created() {
		this.restoreBackdropSettings()
	},
	methods: {
		restoreBackdropSettings() {
			const alpha = localStorage.getItem('uiBackdropAlpha')
			const blur = localStorage.getItem('uiBackdropBlur')
			this.backdropAlphaPct = alpha !== null ? Math.round(parseFloat(alpha) * 100) : 40
			this.backdropBlurPx = blur !== null ? parseFloat(blur) : 5

			this.$api.users.getCustomStorage('appearance').then(res => {
				if (res.data.success === 200 && res.data.data) {
					const { alpha, blur } = res.data.data
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
				this.$api.users.setCustomStorage('appearance', { alpha, blur }).catch(() => {})
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
</style>
