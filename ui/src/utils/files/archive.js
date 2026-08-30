// Extensions the backend's archiver library (mholt/archiver v3) can
// extract - checked longest-first so "tar.gz" matches before the plain
// "gz" suffix would.
const ARCHIVE_EXTENSIONS = [
	'tar.gz', 'tar.bz2', 'tar.xz', 'tar.lz4', 'tar.sz', 'tar.zst', 'tar.br',
	'zip', 'tar', 'rar', 'gz', 'bz2', 'xz', 'lz4', 'sz', 'zst', 'br',
]

export function isArchive(item) {
	if (!item || item.is_dir) return false
	const lower = item.name.toLowerCase()
	return ARCHIVE_EXTENSIONS.some((ext) => lower.endsWith('.' + ext))
}
