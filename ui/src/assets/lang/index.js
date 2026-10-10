// English is the fallback and ships in the entry; every other locale is its
// own chunk, loaded by plugins/i18n.js setLocale() when picked.
import en_us from './en_US.json'

const key = (path) => path.replace(/(\.\/|\.json$)/g, '').toLowerCase()
const loaders = Object.fromEntries(Object.entries(import.meta.glob(['./*.json', '!./en_US.json'], { import: 'default' })).map(([p, load]) => [key(p), load]))

export const messages = { en_us }

// Resolves to the locale's messages, or undefined for an unknown one.
export async function loadLocale(lang) {
	if (!messages[lang] && loaders[lang]) messages[lang] = await loaders[lang]()
	return messages[lang]
}

