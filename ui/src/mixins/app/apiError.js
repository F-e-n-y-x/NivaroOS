import { escapeHtml } from '@/utils/escapeHtml'
import { apiError } from '@/utils/apiError'

// Text for a failed API call - see utils/apiError (handles network errors,
// `message` vs `data` bodies, and never throws).
export function apiErrorText(e, fallback = '') {
	return apiError(e, fallback)
}

// Same text, escaped for Buefy toasts / confirm windows, which render their
// message as HTML.
export function apiErrorHtml(e, fallback = '') {
	return escapeHtml(apiError(e, fallback))
}

export default apiErrorText
