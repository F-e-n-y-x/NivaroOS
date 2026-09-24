// Cron expressions for the shared ScheduleBuilder (Backup & Sync and
// Scheduled Tasks). Both backends parse with robfig/cron v3 configured as
// Minute | Hour | Dom | Month | Dow | Descriptor (5 fields, or @daily-style
// descriptors), in the server's local time. This file mirrors that parser
// closely enough to validate as you type, describe a schedule in words and
// estimate the next runs when no server preview is available.
//
// The builder never rewrites a schedule it didn't produce: parsePattern()
// only maps an expression to a builder pattern when buildCron() of that
// pattern gives back the exact same string. Anything else is "custom" and
// is kept byte-for-byte.

const MONTH_NAMES = { jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6, jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12 }
const DOW_NAMES = { sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6 }

// Field order and bounds are robfig's (Dow is 0-6; 7 is out of range).
export const CRON_FIELDS = Object.freeze([
	Object.freeze({ name: 'minute', min: 0, max: 59 }),
	Object.freeze({ name: 'hour', min: 0, max: 23 }),
	Object.freeze({ name: 'dom', min: 1, max: 31 }),
	Object.freeze({ name: 'month', min: 1, max: 12, names: MONTH_NAMES }),
	Object.freeze({ name: 'dow', min: 0, max: 6, names: DOW_NAMES })
])

const DESCRIPTORS = Object.freeze({
	'@yearly': '0 0 1 1 *',
	'@annually': '0 0 1 1 *',
	'@monthly': '0 0 1 * *',
	'@weekly': '0 0 * * 0',
	'@daily': '0 0 * * *',
	'@midnight': '0 0 * * *',
	'@hourly': '0 * * * *'
})

// Intervals the builder offers. Anything else is still valid as Custom.
export const HOUR_INTERVALS = Object.freeze([2, 3, 4, 6, 8, 12])
export const MINUTE_INTERVALS = Object.freeze([5, 10, 15, 20, 30])

// Builder pattern kinds, in the order the builder lists them.
// every_n_minutes is only offered where sub-hour schedules make sense
// (Scheduled Tasks); Backup jobs start at hourly.
export const PATTERN_KINDS = Object.freeze(['every_n_minutes', 'hourly', 'every_n_hours', 'daily', 'weekdays', 'weekly', 'monthly', 'custom'])

// CronError carries an i18n key + args (schedule.cron_err.*) so the
// builder can explain the problem in the user's language.
export class CronError extends Error {
	constructor(key, args = {}) {
		super(`${key} ${JSON.stringify(args)}`)
		this.key = key
		this.args = args
	}
}

function parseNumber(token, field) {
	if (field.names && Object.prototype.hasOwnProperty.call(field.names, token.toLowerCase())) {
		return field.names[token.toLowerCase()]
	}
	if (!/^\d+$/.test(token)) throw new CronError('schedule.cron_err.not_number', { field: field.name, value: token })
	return parseInt(token, 10)
}

// parseField returns the set of values one field matches plus robfig's
// "star" flag (the field was * or ?), which changes how day-of-month and
// day-of-week combine.
export function parseField(expr, field) {
	const values = new Set()
	let star = false
	if (expr === '') throw new CronError('schedule.cron_err.empty_field', { field: field.name })
	for (const part of expr.split(',')) {
		const rangeAndStep = part.split('/')
		if (rangeAndStep.length > 2) throw new CronError('schedule.cron_err.syntax', { field: field.name, value: part })
		const lowAndHigh = rangeAndStep[0].split('-')
		let start
		let end
		let partStar = false
		if (lowAndHigh[0] === '*' || lowAndHigh[0] === '?') {
			if (lowAndHigh.length > 1) throw new CronError('schedule.cron_err.syntax', { field: field.name, value: part })
			start = field.min
			end = field.max
			partStar = true
		} else {
			if (lowAndHigh.length > 2) throw new CronError('schedule.cron_err.syntax', { field: field.name, value: part })
			start = parseNumber(lowAndHigh[0], field)
			end = lowAndHigh.length === 2 ? parseNumber(lowAndHigh[1], field) : start
		}
		let step = 1
		if (rangeAndStep.length === 2) {
			if (!/^\d+$/.test(rangeAndStep[1])) throw new CronError('schedule.cron_err.not_number', { field: field.name, value: rangeAndStep[1] })
			step = parseInt(rangeAndStep[1], 10)
			// robfig: "N/step" means "N-max/step".
			if (lowAndHigh.length === 1) end = field.max
			if (step > 1) partStar = false
		}
		if (start < field.min || end > field.max) {
			throw new CronError('schedule.cron_err.range', { field: field.name, value: part, min: field.min, max: field.max })
		}
		if (start > end) throw new CronError('schedule.cron_err.range', { field: field.name, value: part, min: field.min, max: field.max })
		if (step === 0) throw new CronError('schedule.cron_err.step', { field: field.name, value: part })
		for (let v = start; v <= end; v += step) values.add(v)
		if (partStar) star = true
	}
	return { values, star }
}

// Go duration ("1h30m", "90s") for @every.
const DURATION_RE = /^(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))+$/
const DURATION_UNIT_MS = { ns: 1e-6, us: 1e-3, 'µs': 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 }

function parseDurationMs(text) {
	if (!DURATION_RE.test(text)) return null
	let ms = 0
	const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
	let m
	while ((m = re.exec(text))) ms += parseFloat(m[1]) * DURATION_UNIT_MS[m[2]]
	return ms
}

// parseCron parses an expression the way the servers do. It returns
// { every: ms } for @every, or { fields: [minute, hour, dom, month, dow] }
// (each from parseField), and throws CronError when the server would
// reject it.
export function parseCron(expr) {
	let spec = typeof expr === 'string' ? expr.trim() : ''
	if (!spec) throw new CronError('schedule.cron_err.empty')
	// robfig accepts a leading TZ=/CRON_TZ= zone. Next-run estimates here
	// stay in local time; the server preview is authoritative.
	if (/^(CRON_)?TZ=/.test(spec)) {
		const i = spec.indexOf(' ')
		if (i < 0) throw new CronError('schedule.cron_err.fields', { count: 0 })
		spec = spec.slice(i + 1).trim()
	}
	if (spec.startsWith('@')) {
		if (spec.startsWith('@every ')) {
			const ms = parseDurationMs(spec.slice(7).trim())
			if (ms === null || ms <= 0) throw new CronError('schedule.cron_err.duration', { value: spec.slice(7).trim() })
			// robfig rounds to whole seconds, minimum 1 s.
			return { every: Math.max(1000, Math.round(ms / 1000) * 1000) }
		}
		if (!DESCRIPTORS[spec]) throw new CronError('schedule.cron_err.descriptor', { value: spec })
		spec = DESCRIPTORS[spec]
	}
	const parts = spec.split(/\s+/)
	if (parts.length !== 5) throw new CronError('schedule.cron_err.fields', { count: parts.length })
	return { fields: parts.map((p, i) => parseField(p, CRON_FIELDS[i])) }
}

// validateCron returns null when valid, else { key, args } explaining why.
export function validateCron(expr) {
	try {
		parseCron(expr)
		return null
	} catch (e) {
		if (e instanceof CronError) return { key: e.key, args: e.args }
		return { key: 'schedule.cron_err.syntax', args: { field: '', value: String(expr) } }
	}
}

// robfig dayMatches: when either day field is a star they must both
// match, otherwise either may.
function dayMatches(fields, date) {
	const dom = fields[2]
	const dow = fields[4]
	const domMatch = dom.values.has(date.getDate())
	const dowMatch = dow.values.has(date.getDay())
	if (dom.star || dow.star) return domMatch && dowMatch
	return domMatch || dowMatch
}

// nextRuns lists up to count run times after `from`, in this device's
// local time (the server preview gives server time instead). Times that
// don't exist because of a DST jump are skipped, as robfig does. Returns
// [] for an invalid expression.
export function nextRuns(expr, from = new Date(), count = 5) {
	let parsed
	try {
		parsed = parseCron(expr)
	} catch (e) {
		return []
	}
	const out = []
	if (parsed.every) {
		let t = from.getTime()
		for (let i = 0; i < count; i++) {
			t += parsed.every
			out.push(new Date(t))
		}
		return out
	}
	const [minutes, hours, , months] = parsed.fields
	const hourList = [...hours.values].sort((a, b) => a - b)
	const minuteList = [...minutes.values].sort((a, b) => a - b)
	const start = new Date(from.getTime())
	start.setSeconds(0, 0)
	const day = new Date(start.getFullYear(), start.getMonth(), start.getDate())
	// Five years covers every valid pattern (Feb 29 on a given weekday is
	// the rarest); an impossible one (Feb 30) just yields nothing.
	for (let i = 0; i < 366 * 5 && out.length < count; i++) {
		const d = new Date(day.getFullYear(), day.getMonth(), day.getDate() + i)
		if (!months.values.has(d.getMonth() + 1) || !dayMatches(parsed.fields, d)) continue
		for (const h of hourList) {
			for (const m of minuteList) {
				const t = new Date(d.getFullYear(), d.getMonth(), d.getDate(), h, m)
				if (t.getHours() !== h || t.getMinutes() !== m) continue // DST gap
				if (t <= from) continue
				out.push(t)
				if (out.length >= count) return out
			}
		}
	}
	return out
}

function pad2(n) {
	return String(n).padStart(2, '0')
}

export function formatHHMM(hour, minute) {
	return `${pad2(hour)}:${pad2(minute)}`
}

// daysExpr writes weekdays the canonical way: sorted, runs of three or
// more as a range ("1-5"), the rest comma separated ("0,6").
export function daysExpr(days) {
	const sorted = [...new Set(days)].sort((a, b) => a - b)
	const parts = []
	for (let i = 0; i < sorted.length; ) {
		let j = i
		while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++
		if (j - i >= 2) parts.push(`${sorted[i]}-${sorted[j]}`)
		else for (let k = i; k <= j; k++) parts.push(String(sorted[k]))
		i = j + 1
	}
	return parts.join(',')
}

// buildCron turns a builder pattern into its expression.
export function buildCron(p) {
	const m = p.minute
	const h = p.hour
	switch (p.kind) {
		case 'every_n_minutes': return `*/${p.n} * * * *`
		case 'hourly': return `${m} * * * *`
		case 'every_n_hours': return `${m} */${p.n} * * *`
		case 'daily': return `${m} ${h} * * *`
		case 'weekdays': return `${m} ${h} * * ${daysExpr(p.days)}`
		case 'weekly': return `${m} ${h} * * ${p.day}`
		case 'monthly': return `${m} ${h} ${p.dom} * *`
		case 'custom': return p.expr
		default: throw new Error(`unknown schedule pattern: ${p.kind}`)
	}
}

const NUM = '(\\d{1,2})'
const PATTERN_MATCHERS = [
	[new RegExp(`^\\*/${NUM} \\* \\* \\* \\*$`), g => ({ kind: 'every_n_minutes', n: +g[1] })],
	[new RegExp(`^${NUM} \\* \\* \\* \\*$`), g => ({ kind: 'hourly', minute: +g[1] })],
	[new RegExp(`^${NUM} \\*/${NUM} \\* \\* \\*$`), g => ({ kind: 'every_n_hours', minute: +g[1], n: +g[2] })],
	[new RegExp(`^${NUM} ${NUM} \\* \\* \\*$`), g => ({ kind: 'daily', minute: +g[1], hour: +g[2] })],
	[new RegExp(`^${NUM} ${NUM} \\* \\* ([0-6])$`), g => ({ kind: 'weekly', minute: +g[1], hour: +g[2], day: +g[3] })],
	[new RegExp(`^${NUM} ${NUM} \\* \\* ([0-6](?:[-,][0-6])+)$`), (g) => {
		const dow = parseField(g[3], CRON_FIELDS[4])
		return { kind: 'weekdays', minute: +g[1], hour: +g[2], days: [...dow.values].sort((a, b) => a - b) }
	}],
	[new RegExp(`^${NUM} ${NUM} ${NUM} \\* \\*$`), g => ({ kind: 'monthly', minute: +g[1], hour: +g[2], dom: +g[3] })]
]

function patternInRange(p) {
	if (p.minute !== undefined && (p.minute < 0 || p.minute > 59)) return false
	if (p.hour !== undefined && (p.hour < 0 || p.hour > 23)) return false
	if (p.kind === 'every_n_hours' && !HOUR_INTERVALS.includes(p.n)) return false
	if (p.kind === 'every_n_minutes' && !MINUTE_INTERVALS.includes(p.n)) return false
	if (p.kind === 'monthly' && (p.dom < 1 || p.dom > 31)) return false
	if (p.kind === 'weekdays' && p.days.length < 2) return false
	return true
}

// parsePattern maps an expression to a builder pattern, or to
// { kind: 'custom', expr } (unchanged) when the builder can't show it
// exactly - including every invalid expression.
export function parsePattern(expr) {
	const text = typeof expr === 'string' ? expr : ''
	for (const [re, make] of PATTERN_MATCHERS) {
		const g = re.exec(text)
		if (!g) continue
		let p
		try {
			p = make(g)
		} catch (e) {
			break
		}
		if (patternInRange(p) && buildCron(p) === text) return p
		break
	}
	return { kind: 'custom', expr: text }
}

// defaultPattern is what switching the builder to a kind starts with,
// keeping the time of day (and minute) the previous pattern had.
export function defaultPattern(kind, from = {}) {
	const hour = Number.isInteger(from.hour) ? from.hour : 3
	const minute = Number.isInteger(from.minute) ? from.minute : 0
	switch (kind) {
		case 'every_n_minutes': return { kind, n: 30 }
		case 'hourly': return { kind, minute }
		case 'every_n_hours': return { kind, n: 6, minute }
		case 'daily': return { kind, hour, minute }
		case 'weekdays': return { kind, hour, minute, days: [1, 2, 3, 4, 5] }
		case 'weekly': return { kind, hour, minute, day: Number.isInteger(from.day) ? from.day : 0 }
		case 'monthly': return { kind, hour, minute, dom: 1 }
		case 'custom': return { kind, expr: from.expr || '' }
		default: throw new Error(`unknown schedule pattern: ${kind}`)
	}
}

function single(field) {
	return !field.star && field.values.size === 1 ? [...field.values][0] : null
}

// stepOf returns n when the token is exactly "*/n".
function stepOf(token) {
	const m = /^\*\/(\d+)$/.exec(token)
	return m ? parseInt(m[1], 10) : null
}

// describeCron returns the same { key, args } the server's /cron/preview
// sends as human_key/args (backup.cron.*): "days" is weekday numbers
// joined by "," (0 = Sunday), "time" is "HH:MM". null when invalid.
export function describeCron(expr) {
	let parsed
	try {
		parsed = parseCron(expr)
	} catch (e) {
		return null
	}
	const text = expr.trim()
	if (parsed.every) return { key: 'backup.cron.custom', args: { expr: text } }
	let spec = text.replace(/^(CRON_)?TZ=\S+\s+/, '')
	if (DESCRIPTORS[spec]) {
		if (spec === '@yearly' || spec === '@annually') return { key: 'backup.cron.custom', args: { expr: text } }
		spec = DESCRIPTORS[spec]
	}
	const tokens = spec.split(/\s+/)
	const [mi, ho, dom, mon, dow] = parsed.fields
	const minute = single(mi)
	const hour = single(ho)
	const dayStar = dom.star && mon.star
	if (dayStar && dow.star) {
		if (mi.star && ho.star) return { key: 'backup.cron.every_minute', args: {} }
		const n = stepOf(tokens[0])
		if (n && ho.star) return { key: 'backup.cron.every_n_minutes', args: { n } }
		if (minute !== null && ho.star) return { key: 'backup.cron.hourly_at', args: { minute } }
		const nh = stepOf(tokens[1])
		if (minute !== null && nh) return { key: 'backup.cron.every_n_hours', args: { n: nh, minute } }
		if (minute !== null && hour !== null) return { key: 'backup.cron.daily_at', args: { time: formatHHMM(hour, minute) } }
	}
	if (minute !== null && hour !== null) {
		const time = formatHHMM(hour, minute)
		if (dayStar && !dow.star) {
			const days = [...dow.values].sort((a, b) => a - b)
			if (days.length === 1) return { key: 'backup.cron.weekly_at', args: { day: days[0], time } }
			if (days.length === 7) return { key: 'backup.cron.daily_at', args: { time } }
			return { key: 'backup.cron.weekdays_at', args: { days: days.join(','), time } }
		}
		const d = single(dom)
		if (d !== null && mon.star && dow.star) return { key: 'backup.cron.monthly_at', args: { dom: d, time } }
	}
	return { key: 'backup.cron.custom', args: { expr: text } }
}
