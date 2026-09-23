// Matches a query against a row's label, its translation (users of other
// languages type in their language) and its keywords - every word of the
// query must appear somewhere.
export function filterRows(rows, query, translate = (x) => x) {
	const q = query.trim().toLowerCase()
	if (!q) return []
	const words = q.split(/\s+/)
	return rows.filter((r) => {
		const hay = [r.label, translate(r.label), r.keywords || '', r.sectionLabel ? translate(r.sectionLabel) : ''].join(' ').toLowerCase()
		return words.every((w) => hay.includes(w))
	})
}
