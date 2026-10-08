import { describe, expect, test } from 'vitest'
import { createI18n } from 'vue-i18n'
import messages from './index'

// vue-i18n 9+ compiles every message: a bare "@", "|" or a broken {placeholder}
// throws (or picks a plural form) where vue-i18n 8 printed it as is.
describe('locale messages', () => {
	test('every message compiles', () => {
		const errors = []
		const i18n = createI18n({ legacy: false, locale: 'en_us', messages, warnHtmlMessage: false, missingWarn: false, fallbackWarn: false, messageCompiler: undefined })
		for (const [locale, msgs] of Object.entries(messages)) {
			const walk = (o, prefix) => {
				for (const [k, v] of Object.entries(o)) {
					const key = prefix + k
					if (v && typeof v === 'object') walk(v, key + '.')
					else {
						try {
							i18n.global.t(key, {}, { locale })
						} catch (e) {
							errors.push(`${locale} ${key}: ${e.message}`)
						}
					}
				}
			}
			walk(msgs, '')
		}
		expect(errors).toEqual([])
	})
})
