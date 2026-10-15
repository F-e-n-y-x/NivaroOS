// Tells the server a file's upload was cancelled, so its temp file goes
// now instead of after the 6-hour idle cleanup. Same fields as a chunk
// (they key the upload server-side) plus cancel=1. Best effort: if it
// fails, the idle cleanup still removes the temp file.
export function cancelUpload(target, headers, file) {
	const form = new FormData()
	const fields = {
		cancel: '1',
		path: file.targetPath,
		relativePath: file.relativePath,
		filename: file.name,
		identifier: file.uniqueIdentifier,
		totalSize: file.size,
	}
	for (const [k, v] of Object.entries(fields)) form.append(k, v)
	return fetch(target, { method: 'POST', headers, body: form }).catch(() => {})
}
