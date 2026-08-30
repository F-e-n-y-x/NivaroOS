export function formatSize(bytes) {
	if (!bytes) return '0 B'
	const units = ['B', 'KB', 'MB', 'GB', 'TB']
	let i = 0
	let size = bytes
	while (size >= 1024 && i < units.length - 1) {
		size /= 1024
		i++
	}
	return `${size.toFixed(1)} ${units[i]}`
}
