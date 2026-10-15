import DOMPurify from 'dompurify'
import { purifyConfig } from '@/utils/purifyConfig'

// Buefy's toast, snackbar, notification and dialog render `message` as
// HTML, and many callers put server text or names in it (a file, container
// or task named <img onerror=...> ran script - with the root token in
// localStorage). Every message is sanitized on the way in, as
// v-dompurify-html does: markup callers mean (<b>, links) and text they
// escaped themselves stay as they are.
export function sanitizeBuefyMessages(buefy) {
	const clean = (o) => {
		if (typeof o === 'string') return DOMPurify.sanitize(o, purifyConfig.default)
		if (o && typeof o.message === 'string') return { ...o, message: DOMPurify.sanitize(o.message, purifyConfig.default) }
		return o
	}
	const methods = { toast: ['open'], snackbar: ['open'], notification: ['open'], dialog: ['alert', 'confirm', 'prompt'] }
	for (const [name, fns] of Object.entries(methods)) {
		const p = buefy && buefy[name]
		if (!p) continue
		for (const fn of fns) {
			const orig = p[fn]
			if (typeof orig === 'function') p[fn] = (o) => orig.call(p, clean(o))
		}
	}
}
