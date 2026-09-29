// Fan curve helpers for Settings -> Fans. The rules mirror the fan
// service (services/fans/curve.go) so the editor can never produce a curve
// the service would refuse: 2-8 points, temperatures rising within the
// limits, speeds never falling as it gets hotter, every speed between the
// fan's minimum and 100%.

export const DEFAULT_LIMITS = Object.freeze({
	min_pct: 20,
	min_points: 2,
	max_points: 8,
	min_temp_c: 20,
	max_temp_c: 100,
	max_hysteresis_c: 10,
	min_critical_c: 60,
	max_critical_cpu_c: 95,
	max_critical_gpu_c: 90
})

export const PRESETS = Object.freeze([
	{ id: 'auto', label: 'Auto', desc: 'BIOS and drivers control every fan' },
	{ id: 'quiet', label: 'Quiet', desc: 'Slow and silent until it gets warm' },
	{ id: 'balanced', label: 'Balanced', desc: 'A sensible middle ground' },
	{ id: 'performance', label: 'Performance', desc: 'Cooler, and louder' }
])

// evalCurve: linear between points, flat before the first and after the
// last (the service's Curve.eval).
export function evalCurve(points, t) {
	if (!points || !points.length) return 100
	if (t <= points[0].t) return points[0].p
	const last = points[points.length - 1]
	if (t >= last.t) return last.p
	for (let i = 1; i < points.length; i++) {
		const a = points[i - 1]
		const b = points[i]
		if (t <= b.t) return a.p + ((t - a.t) / (b.t - a.t)) * (b.p - a.p)
	}
	return last.p
}

// validateCurve returns '' or the reason the service would give.
export function validateCurve(points, minPct, limits = DEFAULT_LIMITS) {
	if (!Array.isArray(points) || points.length < limits.min_points || points.length > limits.max_points) {
		return `A curve needs ${limits.min_points} to ${limits.max_points} points`
	}
	for (let i = 0; i < points.length; i++) {
		const { t, p } = points[i]
		if (!(t >= limits.min_temp_c && t <= limits.max_temp_c)) return `Temperatures must be between ${limits.min_temp_c} and ${limits.max_temp_c} °C`
		if (!(p >= minPct && p <= 100)) return `Speeds must be between ${minPct}% and 100%`
		if (i > 0 && t <= points[i - 1].t) return 'Temperatures must rise from point to point'
		if (i > 0 && p < points[i - 1].p) return 'A fan curve may not slow the fan down as it gets hotter'
	}
	return ''
}

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v))

// movePoint returns a new curve with point i moved as close to (t, p) as
// the rules allow: at least 1 °C from its neighbours, speed between the
// neighbours' speeds (so the curve never dips), within the fan's limits.
// Temperatures snap to whole degrees, speeds to whole percent.
export function movePoint(points, i, t, p, minPct, limits = DEFAULT_LIMITS) {
	const out = points.map((x) => ({ ...x }))
	const prev = out[i - 1]
	const next = out[i + 1]
	const tLo = prev ? prev.t + 1 : limits.min_temp_c
	const tHi = next ? next.t - 1 : limits.max_temp_c
	const pLo = prev ? Math.max(prev.p, minPct) : minPct
	const pHi = next ? next.p : 100
	out[i] = { t: clamp(Math.round(t), tLo, tHi), p: clamp(Math.round(p), pLo, pHi) }
	return out
}

// addPoint inserts a point on the curve at temperature t (halfway into the
// widest gap when t is not given). Returns the curve unchanged when full
// or when there is no room.
export function addPoint(points, t, limits = DEFAULT_LIMITS) {
	if (points.length >= limits.max_points) return points
	let at = t
	if (at === undefined || at === null) {
		let best = -1
		let gap = 0
		for (let i = 1; i < points.length; i++) {
			if (points[i].t - points[i - 1].t > gap) {
				gap = points[i].t - points[i - 1].t
				best = i
			}
		}
		if (best < 0 || gap < 2) return points
		at = (points[best - 1].t + points[best].t) / 2
	}
	at = Math.round(at)
	if (points.some((x) => x.t === at)) return points
	if (at < limits.min_temp_c || at > limits.max_temp_c) return points
	const p = Math.round(evalCurve(points, at))
	const out = [...points.map((x) => ({ ...x })), { t: at, p }]
	out.sort((a, b) => a.t - b.t)
	return out
}

export function removePoint(points, i, limits = DEFAULT_LIMITS) {
	if (points.length <= limits.min_points) return points
	return points.filter((_, k) => k !== i).map((x) => ({ ...x }))
}

// fitCurve lifts every point to at least minPct (a raised minimum).
export function fitCurve(points, minPct) {
	return points.map((x) => ({ t: x.t, p: Math.max(x.p, minPct) }))
}

// Chart geometry: x = temperature, y = speed %, inside a padded box.
export function makeScale({ width, height, pad = 32, tMin = 20, tMax = 100 }) {
	const w = width - pad * 1.5
	const h = height - pad * 1.5
	return {
		x: (t) => pad + ((t - tMin) / (tMax - tMin)) * w,
		y: (p) => pad * 0.5 + (1 - p / 100) * h,
		t: (x) => tMin + ((x - pad) / w) * (tMax - tMin),
		p: (y) => (1 - (y - pad * 0.5) / h) * 100
	}
}

export function curvePath(points, scale, tMin = 20, tMax = 100) {
	if (!points.length) return ''
	const pts = [{ t: tMin, p: points[0].p }, ...points, { t: tMax, p: points[points.length - 1].p }]
	return pts.map((pt, k) => `${k ? 'L' : 'M'}${scale.x(pt.t).toFixed(1)},${scale.y(pt.p).toFixed(1)}`).join(' ')
}

// What a fan is doing, in words (the state the service reports).
export function fanStateLabel(fan) {
	switch (fan && fan.state) {
		case 'emergency':
			return 'Emergency: full speed'
		case 'manual':
			return fan.mode === 'curve' ? 'Following its curve' : 'Fixed speed'
		case 'identifying':
			return 'Identifying...'
		case 'readonly':
			return 'Read-only'
		default:
			return 'Auto (BIOS/driver)'
	}
}

export function formatRpm(rpm) {
	if (rpm === null || rpm === undefined) return ''
	return `${Math.round(rpm).toLocaleString('en-US')} RPM`
}

export function tempTone(c, crit) {
	if (c === null || c === undefined) return 'muted'
	if (c >= crit) return 'danger'
	if (c >= crit - 10) return 'warn'
	return 'ok'
}
