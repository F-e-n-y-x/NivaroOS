export function filterRows(rows, query) {
	const q = query.trim().toLowerCase()
	if (!q) return []
	return rows.filter(r => r.label.toLowerCase().includes(q))
}
