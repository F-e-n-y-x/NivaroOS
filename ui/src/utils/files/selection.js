export function toggleSelect(selection, path) {
	return selection.includes(path)
		? selection.filter((p) => p !== path)
		: [...selection, path]
}

export function selectRange(list, fromPath, toPath) {
	const paths = list.map((item) => item.path)
	let start = paths.indexOf(fromPath)
	let end = paths.indexOf(toPath)
	if (start === -1 || end === -1) return []
	if (start > end) [start, end] = [end, start]
	return paths.slice(start, end + 1)
}

export function summarize(list, selection) {
	const total = list.length
	const count = list.filter((item) => selection.includes(item.path)).length
	const state = count === 0 ? 'none' : count === total ? 'all' : 'part'
	return { count, total, state }
}
