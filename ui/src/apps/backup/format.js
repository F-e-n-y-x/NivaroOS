// Number, size, date and duration formatting for Backup & Sync (spec
// §12.12): Intl only, dates in the SERVER's time zone (capabilities
// .timezone), because schedules run in server time. Pure - no Vue - so
// the state logic and its tests share it.

// toBcp47 turns a vue-i18n locale ("en_us", "zh_cn") into a BCP 47 tag.
export function toBcp47(locale) {
	const tag = String(locale || 'en').replace(/_/g, '-')
	try {
		return Intl.getCanonicalLocales(tag)[0] || 'en'
	} catch (e) {
		return 'en'
	}
}

function validZone(tz) {
	if (!tz) return undefined
	try {
		return Intl.DateTimeFormat('en', { timeZone: tz }).resolvedOptions().timeZone ? tz : undefined
	} catch (e) {
		return undefined
	}
}

function toDate(v) {
	if (v === null || v === undefined || v === '') return null
	const d = v instanceof Date ? v : new Date(v)
	return isNaN(d.getTime()) ? null : d
}

// Decimal (SI) units, like the rest of the spec's examples ("11.9 GB").
const BYTE_UNITS = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte', 'petabyte']

// createFormatter({ locale, timeZone, hour12, t }) - hour12 follows the
// user's clock setting (store timeFormat; undefined = the locale's own);
// t is only used for the "Today"/"Tomorrow"/"Yesterday" day labels.
export function createFormatter({ locale = 'en', timeZone, hour12, t } = {}) {
	const lang = toBcp47(locale)
	const tz = validZone(timeZone)
	const cache = {}
	const clock = typeof hour12 === 'boolean' ? { hour12 } : {}
	const nf = (key, opts) => cache[key] || (cache[key] = new Intl.NumberFormat(lang, opts))
	const df = (key, opts) => cache[key] || (cache[key] = new Intl.DateTimeFormat(lang, { ...opts, ...(opts.hour ? clock : {}), timeZone: tz }))

	// Calendar day in server time. Always Latin digits (en-US), since it
	// is parsed back into numbers.
	const dayParts = new Intl.DateTimeFormat('en-US', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone: tz })
	function dayKey(d) {
		const p = dayParts.formatToParts(d)
		const get = type => (p.find(x => x.type === type) || {}).value
		return `${get('year')}-${get('month')}-${get('day')}`
	}
	const utcOfKey = k => {
		const [y, m, day] = k.split('-').map(Number)
		return Date.UTC(y, m - 1, day)
	}
	// Whole days between two dates, counted in the server's calendar.
	function dayDiff(d, now) {
		return Math.round((utcOfKey(dayKey(d)) - utcOfKey(dayKey(now))) / 86400000)
	}

	const f = {
		locale: lang,
		timeZone: tz,
		number(n) {
			return nf('num', { maximumFractionDigits: 0 }).format(Number(n) || 0)
		},
		percent(ratio) {
			return nf('pct', { style: 'percent', maximumFractionDigits: 0 }).format(Math.max(0, Math.min(1, Number(ratio) || 0)))
		},
		bytes(n) {
			let v = Math.max(0, Number(n) || 0)
			let i = 0
			while (v >= 1000 && i < BYTE_UNITS.length - 1) {
				v /= 1000
				i++
			}
			// "byte" has no short unit symbol in Intl ("0 byte"); B is universal.
			if (i === 0) return `${f.number(v)} B`
			const digits = v >= 100 ? 0 : 1
			return nf(`b${i}-${digits}`, { style: 'unit', unit: BYTE_UNITS[i], unitDisplay: 'short', maximumFractionDigits: digits }).format(v)
		},
		speed(bps) {
			return `${f.bytes(bps)}/s`
		},
		// duration in seconds -> "6 min", "1 hr 5 min", "40 sec".
		duration(sec) {
			const s = Math.max(0, Math.round(Number(sec) || 0))
			const unit = (u, v) => nf(`u-${u}`, { style: 'unit', unit: u, unitDisplay: 'short' }).format(v)
			if (s < 60) return unit('second', s)
			const m = Math.round(s / 60)
			if (m < 60) return unit('minute', m)
			const h = Math.floor(m / 60)
			const rest = m % 60
			if (h >= 48) return unit('day', Math.round(h / 24))
			return rest ? `${unit('hour', h)} ${unit('minute', rest)}` : unit('hour', h)
		},
		time(v) {
			const d = toDate(v)
			return d ? df('time', { hour: '2-digit', minute: '2-digit' }).format(d) : ''
		},
		date(v) {
			const d = toDate(v)
			return d ? df('date', { weekday: 'short', day: 'numeric', month: 'short' }).format(d) : ''
		},
		dateTime(v) {
			const d = toDate(v)
			return d ? df('dt', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }).format(d) : ''
		},
		longDate(v) {
			const d = toDate(v)
			return d ? df('long', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(d) : ''
		},
		// dayKey in server time, for grouping (YYYY-MM-DD).
		dayKey(v) {
			const d = toDate(v)
			return d ? dayKey(d) : ''
		},
		// "Today 03:00", "Tomorrow 02:00", "Mon 22 Sep 03:00".
		when(v, now = new Date()) {
			const d = toDate(v)
			if (!d) return ''
			const diff = dayDiff(d, now)
			const time = f.time(d)
			if (t && diff === 0) return t('backup.time.today_at', { time })
			if (t && diff === 1) return t('backup.time.tomorrow_at', { time })
			if (t && diff === -1) return t('backup.time.yesterday_at', { time })
			return f.dateTime(d)
		},
		// "7 hours ago", "in 3 days".
		relative(v, now = new Date()) {
			const d = toDate(v)
			if (!d) return ''
			const rtf = cache.rtf || (cache.rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' }))
			const sec = Math.round((d.getTime() - now.getTime()) / 1000)
			const abs = Math.abs(sec)
			if (abs < 60) return rtf.format(0, 'second')
			if (abs < 3600) return rtf.format(Math.round(sec / 60), 'minute')
			if (abs < 86400) return rtf.format(Math.round(sec / 3600), 'hour')
			return rtf.format(Math.round(sec / 86400), 'day')
		},
		list(items) {
			const arr = (items || []).map(String)
			try {
				return new Intl.ListFormat(lang, { style: 'long', type: 'conjunction' }).format(arr)
			} catch (e) {
				return arr.join(', ')
			}
		}
	}
	return f
}
