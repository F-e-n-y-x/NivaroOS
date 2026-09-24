// The backend sends i18n keys and args, never English (spec §12.12). This
// renders them: a {key, args} message (run summary, step, log line, check,
// migration note) becomes text in the viewer's locale.
//
// Args are typed by name:
//   *_key            another i18n key, rendered with the same args
//                    (reason_key: "backup.err.cloud_auth.title")
//   bytes, *_bytes   a size          at, *_at, time-like ISO strings: a date
//   arrays           a localized list ("Immich and Blinko")
//   numbers          localized
import { errorKeys, errorInfo } from './errorCodes'
import { CLIENT_CODES } from '../../service/backup'

const ISO_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/

function formatArg(name, value, t, fmt, args) {
	if (value === null || value === undefined) return ''
	if (name.endsWith('_key') && typeof value === 'string') return value.startsWith('backup.') ? t(value, args) : value
	if (Array.isArray(value)) return fmt ? fmt.list(value) : value.join(', ')
	if (typeof value === 'number') {
		if (!fmt) return String(value)
		if (name === 'bytes' || name.endsWith('_bytes') || name.startsWith('bytes_')) return fmt.bytes(value)
		if (name === 'speed_bps') return fmt.speed(value)
		return fmt.number(value)
	}
	if (typeof value === 'string' && ISO_RE.test(value) && fmt) return fmt.dateTime(value)
	return String(value)
}

// renderArgs formats every arg of a message for t().
export function renderArgs(args, t, fmt) {
	const raw = args || {}
	const out = {}
	for (const [k, v] of Object.entries(raw)) out[k] = formatArg(k, v, t, fmt, raw)
	return out
}

// renderMessage(t, {key, args}, fmt) -> text ('' for no message).
export function renderMessage(t, msg, fmt) {
	if (!msg || !msg.key) return ''
	return t(msg.key, renderArgs(msg.args, t, fmt))
}

// errorCodeFromMessage recovers the error code a summary refers to
// ({reason_key: "backup.err.<code>.title"}); a job's last_run carries the
// summary but no error_code of its own.
export function errorCodeFromMessage(msg) {
	const k = msg && msg.args && msg.args.reason_key
	const m = typeof k === 'string' && /^backup\.err\.([a-z0-9_]+)\.title$/.exec(k)
	return m ? m[1] : ''
}

// explainError returns what the UI shows for an error code: title, cause
// and fix texts plus the one-click actions. The client-only codes
// (service_unavailable, aborted) have their own texts.
export function explainError(t, code) {
	if (code === CLIENT_CODES.UNAVAILABLE) {
		return {
			code,
			title: t('backup.app.unavailable.title'),
			cause: t('backup.app.unavailable.cause'),
			fix: t('backup.app.unavailable.fix'),
			actions: []
		}
	}
	const keys = errorKeys(code)
	return { code, title: t(keys.title), cause: t(keys.cause), fix: t(keys.fix), actions: errorInfo(code).actions.slice() }
}

// errorText is a one-line message for a failed call (toasts, inline
// notes): the code's title.
export function errorText(t, err) {
	const code = (err && err.code) || 'internal'
	if (code === CLIENT_CODES.UNAVAILABLE) return t('backup.app.unavailable.title')
	return t(errorKeys(code).title)
}
