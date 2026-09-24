// Turns a schedule description ({ key, args } from describeCron() or the
// server's /cron/preview human_key + args) into a sentence in the user's
// language. Args arrive machine-shaped ("time": "03:00", "days": "1,3,5",
// 0 = Sunday) and are formatted here, never parsed from English.
import { describeCron } from './cronPatterns'
import { formatClock, weekdayName, joinList } from './timeFormat'

export function localizeCronArgs(args = {}, { locale, hour12 } = {}) {
	const out = { ...args }
	if (out.time !== undefined) out.time = formatClock(out.time, locale, hour12)
	if (out.day !== undefined && out.day !== '') out.day = weekdayName(out.day, locale, 'long')
	if (typeof out.days === 'string' || Array.isArray(out.days)) {
		const list = Array.isArray(out.days) ? out.days : out.days.split(',').filter(s => s !== '')
		out.days = joinList(list.map(d => weekdayName(d, locale, 'short')), locale)
	}
	return out
}

// cronSentence(t, summary) with t = a bound $t. A missing summary (an
// invalid expression) gives ''.
export function cronSentence(t, summary, opts = {}) {
	if (!summary || !summary.key) return ''
	return t(summary.key, localizeCronArgs(summary.args || {}, opts))
}

// describeCronText(t, expr) is the offline sentence for an expression.
export function describeCronText(t, expr, opts = {}) {
	return cronSentence(t, describeCron(expr), opts)
}
