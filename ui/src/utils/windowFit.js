// Lower resolutions with the same aspect ratio as a window ({w, h}), for
// Host Desktop's "Fit this window" options: they fill the window without
// black bars and stream faster over a slow link. Even numbers, and never
// below 640×480 (the smallest size the sidecar accepts).
export function windowFitSizes(target, scales = [75, 66, 50]) {
	if (!target || !target.w || !target.h) return []
	const even = (n) => n - (n % 2)
	const out = []
	for (const percent of scales) {
		const w = even(Math.round((target.w * percent) / 100))
		const h = even(Math.round((target.h * percent) / 100))
		if (w < 640 || h < 480) continue
		if (!out.some((o) => o.w === w && o.h === h)) out.push({ w, h, percent })
	}
	return out
}
