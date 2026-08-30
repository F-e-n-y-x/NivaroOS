<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Appearance') }}</h2>

		<h3 class="setting-card-title">{{ $t('Wallpaper') }}</h3>
		<div class="setting-card">
			<wallpaper-modal embedded></wallpaper-modal>
		</div>

		<h3 class="setting-card-title">{{ $t('Window') }}</h3>
		<div class="setting-card">
			<div class="setting-row">
				<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Window transparency') }}</div>
				<div class="row-control slider-control">
					<span class="slider-hint">{{ $t('Transparent') }}</span>
					<input class="pretty-range" v-model.number="backdropAlphaPct" type="range" min="40" max="100" step="1"
						:style="rangeStyle(backdropAlphaPct, 40, 100)" @input="applyBackdropAlpha" />
					<span class="slider-hint">{{ $t('Opaque') }}</span>
					<span class="slider-value">{{ backdropAlphaPct }}%</span>
				</div>
			</div>

			<div class="setting-row">
				<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Window blur') }}</div>
				<div class="row-control slider-control">
					<span class="slider-hint">{{ $t('None') }}</span>
					<input class="pretty-range" v-model.number="backdropBlurPx" type="range" min="0" max="24" step="1"
						:style="rangeStyle(backdropBlurPx, 0, 24)" @input="applyBackdropBlur" />
					<span class="slider-hint">{{ $t('Strong') }}</span>
					<span class="slider-value">{{ backdropBlurPx }}px</span>
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
			backdropAlphaPct: 100,
			backdropBlurPx: 0
		}
	},
	created() {
		this.restoreBackdropSettings()
	},
	methods: {
		restoreBackdropSettings() {
			const alpha = localStorage.getItem('uiBackdropAlpha')
			const blur = localStorage.getItem('uiBackdropBlur')
			this.backdropAlphaPct = alpha !== null ? Math.round(parseFloat(alpha) * 100) : 100
			this.backdropBlurPx = blur !== null ? parseFloat(blur) : 0

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
		rangeStyle(value, min, max) {
			return { '--pct': `${((value - min) / (max - min)) * 100}%` }
		}
	}
}
</script>
