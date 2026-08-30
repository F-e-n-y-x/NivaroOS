import dateFormat from 'dateformat'

export const DATE_STYLE_OPTIONS = {
	long: { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' },
	medium: { year: 'numeric', month: 'short', day: 'numeric' },
	short: { year: 'numeric', month: 'numeric', day: 'numeric' }
}

// timeFormat is the base dateformat mask ('HH:MM' or 'h:MM TT') - seconds
// are spliced in as their own mask token rather than stored as a third
// format string, so the seconds toggle works the same for either base.
export function buildTimeMask(timeFormat, showSeconds) {
	if (!showSeconds) return timeFormat
	return timeFormat.replace('MM', 'MM:ss')
}

export function formatTime(date, timeFormat, showSeconds) {
	return dateFormat(date, buildTimeMask(timeFormat, showSeconds))
}

export function formatDate(date, lang, dateFormatStyle) {
	return date.toLocaleDateString(lang, DATE_STYLE_OPTIONS[dateFormatStyle] || DATE_STYLE_OPTIONS.long)
}

const WEEKDAYS_LONG = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const WEEKDAYS_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const MONTHS_LONG = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
const MONTHS_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function pad(n) {
	return String(n).padStart(2, '0')
}

// A small strftime-subset - covers the tokens people actually paste in
// from elsewhere (`date +%F`, log timestamp formats, etc), not the full
// POSIX spec.
const STRFTIME_TOKENS = {
	Y: d => String(d.getFullYear()),
	y: d => pad(d.getFullYear() % 100),
	m: d => pad(d.getMonth() + 1),
	d: d => pad(d.getDate()),
	H: d => pad(d.getHours()),
	I: d => pad(((d.getHours() + 11) % 12) + 1),
	M: d => pad(d.getMinutes()),
	S: d => pad(d.getSeconds()),
	p: d => (d.getHours() < 12 ? 'AM' : 'PM'),
	A: d => WEEKDAYS_LONG[d.getDay()],
	a: d => WEEKDAYS_SHORT[d.getDay()],
	B: d => MONTHS_LONG[d.getMonth()],
	b: d => MONTHS_SHORT[d.getMonth()]
}

export const STRFTIME_TOKEN_LIST = ['%Y', '%m', '%d', '%H', '%M', '%S', '%p', '%A', '%a', '%B', '%b']
export const STRFTIME_SHORTCUTS = [
	{ token: '%F', meaning: '%Y-%m-%d' },
	{ token: '%T', meaning: '%H:%M:%S' },
	{ token: '%D', meaning: '%m/%d/%y' }
]

export function formatStrftime(date, pattern) {
	const expanded = pattern
		.replace(/%F/g, '%Y-%m-%d')
		.replace(/%T/g, '%H:%M:%S')
		.replace(/%D/g, '%m/%d/%y')
	return expanded.replace(/%(.)/g, (match, token) => {
		if (token === '%') return '%'
		const fn = STRFTIME_TOKENS[token]
		return fn ? fn(date) : match
	})
}
