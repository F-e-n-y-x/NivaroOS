export function baseName(path) {
	const segments = path.split('/').filter(Boolean)
	return segments[segments.length - 1] || ''
}

export function parentPath(path) {
	const segments = path.split('/').filter(Boolean)
	if (segments.length <= 1) return null
	return '/' + segments.slice(0, -1).join('/')
}

export function joinPath(dir, name) {
	return dir.endsWith('/') ? `${dir}${name}` : `${dir}/${name}`
}
