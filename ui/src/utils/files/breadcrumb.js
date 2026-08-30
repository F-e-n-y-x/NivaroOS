export function buildBreadcrumb(path) {
	const normalized = path === '/' ? '' : path
	const segments = normalized.split('/').filter(Boolean)
	const crumbs = [{ name: 'Root', path: '/' }]
	let acc = ''
	for (const segment of segments) {
		acc = `${acc}/${segment}`
		crumbs.push({ name: segment, path: acc })
	}
	return crumbs
}
