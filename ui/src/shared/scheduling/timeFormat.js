// Locale-aware time helpers for schedules (spec §12.12: Intl with the
// server's time zone). vue-i18n locales here are "en_us"-style; Intl
// wants BCP 47 ("en-US").

export function intlLocale(locale) {
	const raw = String(locale || '').replace('_', '-')
	if (!raw) return undefined
	const [lang, region] = raw.split('-')
	const tag = region ? `${lang.toLowerCase()}-${region.toUpperCase()}` : lang.toLowerCase()
	try {
		return Intl.getCanonicalLocales(tag)[0]
	} catch (e) {
		return undefined
	}
}

// The browser's IANA zone ("Europe/Berlin"), or '' when unknown.
export function browserTimeZone() {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
	} catch (e) {
		return ''
	}
}

// A zone Intl accepts, else undefined (= this device's zone).
export function safeTimeZone(tz) {
	if (!tz) return undefined
	try {
		new Intl.DateTimeFormat('en-US', { timeZone: tz })
		return tz
	} catch (e) {
		return undefined
	}
}

// formatClock turns "HH:MM" into the user's clock format ("3:00 AM" or
// "03:00"). hour12 follows the NivaroOS time-format setting when given.
export function formatClock(hhmm, locale, hour12) {
	const m = /^(\d{1,2}):(\d{2})$/.exec(String(hhmm || ''))
	if (!m) return String(hhmm || '')
	const d = new Date(2000, 0, 1, parseInt(m[1], 10), parseInt(m[2], 10))
	const opts = { hour: 'numeric', minute: '2-digit' }
	if (typeof hour12 === 'boolean') opts.hour12 = hour12
	try {
		return new Intl.DateTimeFormat(intlLocale(locale), opts).format(d)
	} catch (e) {
		return hhmm
	}
}

// weekdayName: 0 = Sunday. style is 'long' or 'short'.
export function weekdayName(day, locale, style = 'long') {
	// 2023-01-01 was a Sunday.
	const d = new Date(2023, 0, 1 + (Number(day) % 7))
	try {
		return new Intl.DateTimeFormat(intlLocale(locale), { weekday: style }).format(d)
	} catch (e) {
		return ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][Number(day) % 7]
	}
}

// joinList joins names the locale's way ("Mon, Wed and Fri").
export function joinList(items, locale) {
	try {
		return new Intl.ListFormat(intlLocale(locale), { style: 'long', type: 'conjunction' }).format(items)
	} catch (e) {
		return items.join(', ')
	}
}

// yearIn is the calendar year of `date` in `tz` (this device's zone
// when tz is unknown).
function yearIn(date, tz) {
	try {
		return new Intl.DateTimeFormat('en-US', { year: 'numeric', timeZone: tz }).format(date)
	} catch (e) {
		return String(date.getFullYear())
	}
}

// formatRunTime formats an instant as "Thu 25 Sep, 03:00" in a zone. A
// run in another year than `now` names the year too ("Fri 1 Jan 2027"),
// so a yearly schedule's next runs don't all read the same.
export function formatRunTime(date, { locale, timeZone, hour12, now = new Date() } = {}) {
	const d = date instanceof Date ? date : new Date(date)
	if (isNaN(d.getTime())) return ''
	const zone = safeTimeZone(timeZone)
	const opts = { weekday: 'short', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit', timeZone: zone }
	if (yearIn(d, zone) !== yearIn(now, zone)) opts.year = 'numeric'
	if (typeof hour12 === 'boolean') opts.hour12 = hour12
	try {
		return new Intl.DateTimeFormat(intlLocale(locale), opts).format(d)
	} catch (e) {
		return d.toLocaleString()
	}
}

// Offset in minutes east of UTC that `tz` has at `date` (Intl-based, so
// it works for any zone, not only the browser's).
export function zoneOffsetMinutes(date, tz) {
	const zone = safeTimeZone(tz)
	if (!zone) return -date.getTimezoneOffset()
	try {
		const parts = new Intl.DateTimeFormat('en-US', {
			timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric'
		}).formatToParts(date)
		const get = type => parseInt(parts.find(p => p.type === type).value, 10)
		const asUTC = Date.UTC(get('year'), get('month') - 1, get('day'), get('hour') % 24, get('minute'))
		return Math.round((asUTC - Math.floor(date.getTime() / 60000) * 60000) / 60000)
	} catch (e) {
		return -date.getTimezoneOffset()
	}
}

// "UTC+2", "UTC-5:30", "UTC".
export function formatUtcOffset(minutes) {
	if (!minutes) return 'UTC'
	const sign = minutes > 0 ? '+' : '-'
	const abs = Math.abs(minutes)
	const h = Math.floor(abs / 60)
	const m = abs % 60
	return `UTC${sign}${h}${m ? ':' + String(m).padStart(2, '0') : ''}`
}

// Parse "+02:00" / "-05:30" into minutes east of UTC.
export function parseUtcOffset(text) {
	const m = /^([+-])(\d{2}):?(\d{2})$/.exec(String(text || ''))
	if (!m) return null
	const v = parseInt(m[2], 10) * 60 + parseInt(m[3], 10)
	return m[1] === '-' ? -v : v
}
