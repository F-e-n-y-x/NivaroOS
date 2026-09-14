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

// POSIX-safe single-quoting for dropping a path into a generated shell
// command (e.g. "cd '<path>'" for Terminal's initCommand) - wraps in single
// quotes and escapes any embedded single quote as '\'' (close the quote,
// emit an escaped literal quote, reopen the quote).
export function shellQuote(str) {
	return `'${String(str).replace(/'/g, `'\\''`)}'`
}
