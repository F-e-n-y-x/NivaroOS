import { createI18n } from 'vue-i18n'
import messages from '@/assets/lang'

// Legacy (Options API) mode: components keep this.$t / this.$i18n.locale.
const i18n = createI18n({
	legacy: true,
	locale: localStorage.getItem('lang') || 'en_us',
	fallbackLocale: 'en_us',
	silentTranslationWarn: true,
	silentFallbackWarn: true,
	// Some messages carry markup for v-html (e.g. "<br/>").
	warnHtmlInMessage: 'off',
	messages,
})
export default i18n

// vue-i18n 8 gave '' for a missing key ($t(undefined), e.g. a label whose
// data hasn't loaded); 11 throws and breaks the render.
const t = i18n.global.t
i18n.global.t = function (key, ...rest) {
	return key === undefined || key === null || key === '' ? '' : t.call(this, key, ...rest)
}
