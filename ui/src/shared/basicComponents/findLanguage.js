// File name/path or language name -> @codemirror/language-data entry (null = plain text).
import { LanguageDescription } from '@codemirror/language'
import { languages } from '@codemirror/language-data'

export function findLanguage(name) {
	const base = name.split('/').pop()
	const byName = (n) => LanguageDescription.matchLanguageName(languages, n, false)
	return LanguageDescription.matchFilename(languages, base)
		|| (/\.conf$/i.test(base) && /nginx/i.test(name) ? byName('nginx') : null)
		|| (/\.(?:conf|cfg|env|gitconfig)$|^\.env/i.test(base) ? byName('properties') : null)
		|| byName(name)
}
