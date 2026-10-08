// Drive health (GET /v1/disks/health): plain helpers for DriveHealthSection.

export const VERDICT_TEXT = { good: 'Good', watch: 'Watch', failing: 'Failing', unknown: 'Unknown' }

// "17,549 h · 2 years" for hours, "38 °C", "10%", "959".
export function metricText(m) {
	if (!m) return ''
	const n = Number(m.value).toLocaleString('en-US')
	if (m.unit === 'h') {
		const years = m.value / 8766
		return years >= 1 ? `${n} h · ${years.toFixed(1)} years` : `${n} h · ${Math.round(m.value / 24)} days`
	}
	if (m.unit === '°C') return `${n} °C`
	if (m.unit === '%') return `${n}%`
	return n
}

// The counter worth charting: one that changed, else the first damage
// counter the drive reports, else temperature.
export const CHART_KEYS = ['pending', 'reallocated', 'offline_uncorrectable', 'reported_uncorrect', 'crc_errors', 'media_errors', 'wear', 'temperature']

export function chartKeys(history) {
	const seen = new Set()
	// a counter that stayed at 0 has nothing to show
	for (const s of history || []) for (const [k, v] of Object.entries(s.values || {})) if (v !== 0 || k === 'temperature') seen.add(k)
	return CHART_KEYS.filter(k => seen.has(k))
}

export function defaultChartKey(report) {
	const keys = chartKeys(report && report.history)
	const moved = keys.find(k => k !== 'temperature' && new Set((report.history || []).map(s => s.values && s.values[k])).size > 1)
	return moved || keys[0] || ''
}

// SVG polyline points for key over the history, in a w x h box.
export function chartPoints(history, key, w = 240, h = 48) {
	const pts = (history || []).filter(s => s.values && s.values[key] !== undefined).map(s => s.values[key])
	if (!pts.length) return ''
	const min = Math.min(...pts), max = Math.max(...pts)
	const span = max - min || 1
	const step = pts.length > 1 ? w / (pts.length - 1) : 0
	return pts.map((v, i) => {
		const x = pts.length > 1 ? i * step : w / 2
		const y = max === min ? h / 2 : h - ((v - min) / span) * h
		return `${Math.round(x * 10) / 10},${Math.round(y * 10) / 10}`
	}).join(' ')
}

export function selfTestText(st) {
	if (!st || !st.supported) return 'This drive does not support self-tests.'
	if (st.running) return `Running · ${st.remaining_percent}% left`
	const last = st.last && st.last[0]
	if (!last) return 'Never run'
	return `${last.type}: ${last.result} (at ${Number(last.hours).toLocaleString('en-US')} h)`
}
