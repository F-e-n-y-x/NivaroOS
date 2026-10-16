// Save a Blob as a file download (replaces file-saver): <a download> + an object URL,
// revoked after the click has handed it to the browser.
export function saveBlob(blob, fileName) {
	const url = URL.createObjectURL(blob)
	const a = document.createElement('a')
	a.href = url
	a.download = fileName
	document.body.appendChild(a)
	a.click()
	a.remove()
	setTimeout(() => URL.revokeObjectURL(url), 1000)
}
