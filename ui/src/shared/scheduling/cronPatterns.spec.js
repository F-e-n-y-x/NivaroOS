import { describe, test, expect } from 'vitest'
import { parsePattern, buildCron, validateCron, describeCron, nextRuns, daysExpr, defaultPattern, PATTERN_KINDS } from './cronPatterns'
import { cronSentence, localizeCronArgs } from './cronText'
import { formatRunTime } from './timeFormat'

describe('builder patterns round-trip', () => {
	const cases = [
		['*/30 * * * *', { kind: 'every_n_minutes', n: 30 }],
		['5 * * * *', { kind: 'hourly', minute: 5 }],
		['0 */6 * * *', { kind: 'every_n_hours', minute: 0, n: 6 }],
		['0 3 * * *', { kind: 'daily', minute: 0, hour: 3 }],
		['30 22 * * *', { kind: 'daily', minute: 30, hour: 22 }],
		['0 3 * * 1-5', { kind: 'weekdays', minute: 0, hour: 3, days: [1, 2, 3, 4, 5] }],
		['0 9 * * 0,6', { kind: 'weekdays', minute: 0, hour: 9, days: [0, 6] }],
		['0 9 * * 1,3,5', { kind: 'weekdays', minute: 0, hour: 9, days: [1, 3, 5] }],
		['0 4 * * 0', { kind: 'weekly', minute: 0, hour: 4, day: 0 }],
		['0 0 1 * *', { kind: 'monthly', minute: 0, hour: 0, dom: 1 }],
		['15 2 28 * *', { kind: 'monthly', minute: 15, hour: 2, dom: 28 }]
	]
	test.each(cases)('%s', (expr, pattern) => {
		expect(parsePattern(expr)).toEqual(pattern)
		expect(buildCron(pattern)).toBe(expr)
	})

	test('every default pattern builds a valid cron that parses back to itself', () => {
		for (const kind of PATTERN_KINDS.filter(k => k !== 'custom')) {
			const p = defaultPattern(kind)
			const expr = buildCron(p)
			expect(validateCron(expr)).toBeNull()
			expect(parsePattern(expr)).toEqual(p)
		}
	})

	test('switching kinds keeps the time of day', () => {
		expect(defaultPattern('weekly', { hour: 22, minute: 30 })).toEqual({ kind: 'weekly', hour: 22, minute: 30, day: 0 })
	})
})

describe('anything else goes to Custom unchanged', () => {
	const custom = [
		'0 3 * * 1,2,3', // valid, but the builder would write 1-3
		'00 3 * * *', // leading zero
		'0  3 * * *', // double space
		'0 3 * * 7', // out of range for robfig
		'0 */5 * * *', // interval the builder doesn't offer
		'*/7 * * * *',
		'0 3 1 1 *',
		'0 3 * * MON',
		'@daily',
		'@every 90m',
		'CRON_TZ=Europe/Berlin 0 3 * * *',
		'not a cron',
		''
	]
	test.each(custom)('%j', (expr) => {
		expect(parsePattern(expr)).toEqual({ kind: 'custom', expr })
	})

	test('a custom pattern builds its expression byte-for-byte', () => {
		expect(buildCron({ kind: 'custom', expr: '0  3 * * *' })).toBe('0  3 * * *')
	})
})

describe('validateCron follows robfig (5 fields + descriptors)', () => {
	test.each(['0 3 * * *', '*/15 * * * *', '0 0 1,15 * *', '0 3 * JAN-MAR MON-FRI', '0 3 ? * *', '@hourly', '@weekly', '@every 1h30m', 'TZ=UTC 0 3 * * *', '5/10 * * * *'])('valid %j', (expr) => {
		expect(validateCron(expr)).toBeNull()
	})

	test('explains invalid expressions with i18n keys', () => {
		expect(validateCron('')).toEqual({ key: 'schedule.cron_err.empty', args: {} })
		expect(validateCron('0 3 * *')).toEqual({ key: 'schedule.cron_err.fields', args: { count: 4 } })
		expect(validateCron('0 3 * * * *')).toEqual({ key: 'schedule.cron_err.fields', args: { count: 6 } })
		expect(validateCron('61 3 * * *').key).toBe('schedule.cron_err.range')
		expect(validateCron('0 24 * * *').args).toMatchObject({ field: 'hour', min: 0, max: 23 })
		expect(validateCron('0 3 * * 7').key).toBe('schedule.cron_err.range')
		expect(validateCron('0 3 0 * *').key).toBe('schedule.cron_err.range')
		expect(validateCron('x 3 * * *').key).toBe('schedule.cron_err.not_number')
		expect(validateCron('0 5-2 * * *').key).toBe('schedule.cron_err.range')
		expect(validateCron('*/0 * * * *').key).toBe('schedule.cron_err.step')
		expect(validateCron('@sometimes').key).toBe('schedule.cron_err.descriptor')
		expect(validateCron('@every soon').key).toBe('schedule.cron_err.duration')
	})
})

describe('describeCron matches the server human_key/args', () => {
	test.each([
		['* * * * *', 'backup.cron.every_minute', {}],
		['*/15 * * * *', 'backup.cron.every_n_minutes', { n: 15 }],
		['5 * * * *', 'backup.cron.hourly_at', { minute: 5 }],
		['@hourly', 'backup.cron.hourly_at', { minute: 0 }],
		['0 */6 * * *', 'backup.cron.every_n_hours', { n: 6, minute: 0 }],
		['0 3 * * *', 'backup.cron.daily_at', { time: '03:00' }],
		['@daily', 'backup.cron.daily_at', { time: '00:00' }],
		['0 3 * * 1-5', 'backup.cron.weekdays_at', { days: '1,2,3,4,5', time: '03:00' }],
		['0 3 * * 1,2,3', 'backup.cron.weekdays_at', { days: '1,2,3', time: '03:00' }],
		['0 3 * * 0-6', 'backup.cron.daily_at', { time: '03:00' }],
		['0 4 * * 0', 'backup.cron.weekly_at', { day: 0, time: '04:00' }],
		['0 3 * * MON', 'backup.cron.weekly_at', { day: 1, time: '03:00' }],
		['0 0 1 * *', 'backup.cron.monthly_at', { dom: 1, time: '00:00' }],
		['0 3 1 1 *', 'backup.cron.custom', { expr: '0 3 1 1 *' }],
		['@yearly', 'backup.cron.custom', { expr: '@yearly' }],
		['@every 2h', 'backup.cron.custom', { expr: '@every 2h' }]
	])('%s', (expr, key, args) => {
		expect(describeCron(expr)).toEqual({ key, args })
	})

	test('invalid gives null', () => {
		expect(describeCron('0 25 * * *')).toBeNull()
	})
})

describe('human summaries', () => {
	const en = {
		'backup.cron.daily_at': 'Every day at {time}',
		'backup.cron.weekdays_at': 'On {days} at {time}',
		'backup.cron.weekly_at': 'Every {day} at {time}',
		'backup.cron.hourly_at': 'Every hour at minute {minute}'
	}
	const t = (key, args) => en[key].replace(/\{(\w+)\}/g, (_, k) => args[k])

	test('times follow the 12/24 h setting', () => {
		expect(cronSentence(t, describeCron('0 3 * * *'), { locale: 'en_us', hour12: true })).toBe('Every day at 3:00 AM')
		expect(cronSentence(t, describeCron('30 22 * * *'), { locale: 'en_us', hour12: false })).toBe('Every day at 22:30')
	})

	test('weekdays are named, not numbered', () => {
		expect(cronSentence(t, describeCron('0 3 * * 0'), { locale: 'en_us', hour12: false })).toBe('Every Sunday at 03:00')
		expect(cronSentence(t, describeCron('0 9 * * 1,3,5'), { locale: 'en_us', hour12: true })).toBe('On Mon, Wed, and Fri at 9:00 AM')
	})

	test('server args (strings) are localized the same way', () => {
		expect(localizeCronArgs({ days: '0,6', time: '07:05' }, { locale: 'en_us', hour12: false })).toEqual({ days: 'Sun and Sat', time: '07:05' })
		expect(cronSentence(t, { key: 'backup.cron.hourly_at', args: { minute: 5 } })).toBe('Every hour at minute 5')
	})

	test('no summary, no sentence', () => {
		expect(cronSentence(t, null)).toBe('')
	})
})

describe('nextRuns (local time estimate)', () => {
	const from = new Date(2026, 8, 24, 14, 33) // Thu 24 Sep 2026 14:33

	test('daily', () => {
		const runs = nextRuns('0 3 * * *', from, 3)
		expect(runs.map(d => [d.getDate(), d.getHours(), d.getMinutes()])).toEqual([[25, 3, 0], [26, 3, 0], [27, 3, 0]])
	})

	test('later today when the time has not passed', () => {
		expect(nextRuns('0 23 * * *', from, 1)[0]).toEqual(new Date(2026, 8, 24, 23, 0))
		expect(nextRuns('33 14 * * *', from, 1)[0]).toEqual(new Date(2026, 8, 25, 14, 33))
	})

	test('weekly and weekdays', () => {
		expect(nextRuns('0 4 * * 0', from, 1)[0]).toEqual(new Date(2026, 8, 27, 4, 0))
		expect(nextRuns('0 9 * * 1-5', from, 2)).toEqual([new Date(2026, 8, 25, 9, 0), new Date(2026, 8, 28, 9, 0)])
	})

	test('every n hours and minutes', () => {
		expect(nextRuns('0 */6 * * *', from, 2)).toEqual([new Date(2026, 8, 24, 18, 0), new Date(2026, 8, 25, 0, 0)])
		expect(nextRuns('*/30 * * * *', from, 2)).toEqual([new Date(2026, 8, 24, 15, 0), new Date(2026, 8, 24, 15, 30)])
	})

	test('day-of-month OR day-of-week when both are set (robfig)', () => {
		// The 1st, or any Monday.
		const runs = nextRuns('0 0 1 * 1', from, 3)
		expect(runs).toEqual([new Date(2026, 8, 28), new Date(2026, 9, 1), new Date(2026, 9, 5)])
	})

	test('monthly skips months without that day', () => {
		const runs = nextRuns('0 0 31 * *', new Date(2026, 8, 1), 2)
		expect(runs).toEqual([new Date(2026, 9, 31), new Date(2026, 11, 31)])
	})

	test('@every and impossible or invalid expressions', () => {
		expect(nextRuns('@every 1h', from, 1)[0]).toEqual(new Date(from.getTime() + 3600000))
		expect(nextRuns('0 0 30 2 *', from, 1)).toEqual([])
		expect(nextRuns('nope', from, 1)).toEqual([])
	})
})

test('daysExpr writes ranges of three or more', () => {
	expect(daysExpr([5, 1, 2, 3, 4])).toBe('1-5')
	expect(daysExpr([0, 6])).toBe('0,6')
	expect(daysExpr([0, 1, 2, 4, 5])).toBe('0-2,4,5')
})

test('run times name the year only when it is not this year', () => {
	const now = new Date(Date.UTC(2026, 8, 24, 12))
	const opts = { locale: 'en_us', timeZone: 'UTC', hour12: false, now }
	expect(formatRunTime(new Date(Date.UTC(2026, 8, 25, 3)), opts)).not.toMatch(/2026/)
	expect(formatRunTime(new Date(Date.UTC(2027, 0, 1, 3)), opts)).toMatch(/2027/)
	expect(formatRunTime('not a date', opts)).toBe('')
})
