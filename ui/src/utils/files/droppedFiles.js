// Turn a drop's DataTransfer into a flat list of Files, walking dropped
// folders and tagging every file with its path relative to the drop
// (`relativePath`, which simple-uploader.js sends and the server uses to
// recreate the folder structure).
//
// Passing `dataTransfer.files` straight to the uploader - what this app
// did before - silently loses folders: a folder shows up there as a
// zero-byte pseudo-file, and its contents are never read.
//
// Entries must be taken synchronously inside the drop handler: the
// browser empties dataTransfer.items once the event returns, so call
// takeDroppedEntries(event) first and resolve them afterwards.
export function takeDroppedEntries(dataTransfer) {
	const items = (dataTransfer && dataTransfer.items) || []
	const entries = []
	for (let i = 0; i < items.length; i++) {
		const it = items[i]
		if (it.kind !== 'file') continue
		const entry = it.webkitGetAsEntry ? it.webkitGetAsEntry() : null
		if (entry) entries.push(entry)
		else {
			const f = it.getAsFile()
			if (f) entries.push(f)
		}
	}
	// No entry API (very old browsers): plain files only.
	if (!entries.length && dataTransfer && dataTransfer.files) return Array.from(dataTransfer.files)
	return entries
}

function readAll(reader) {
	// readEntries returns results in batches (100 in Chrome) - keep calling
	// until it returns nothing, or big folders get cut off.
	return new Promise((resolve, reject) => {
		const out = []
		const next = () =>
			reader.readEntries((batch) => {
				if (!batch.length) return resolve(out)
				out.push(...batch)
				next()
			}, reject)
		next()
	})
}

function fileOf(entry) {
	return new Promise((resolve, reject) => entry.file(resolve, reject))
}

export async function resolveEntries(entries) {
	const files = []
	const walk = async (entry, prefix) => {
		if (entry instanceof File) {
			files.push(entry)
			return
		}
		if (entry.isFile) {
			const f = await fileOf(entry)
			if (prefix) {
				try {
					Object.defineProperty(f, 'relativePath', { value: prefix + f.name })
				} catch (e) {
					f.relativePath = prefix + f.name
				}
			}
			files.push(f)
		} else if (entry.isDirectory) {
			const children = await readAll(entry.createReader())
			for (const child of children) await walk(child, prefix + entry.name + '/')
		}
	}
	for (const e of entries) await walk(e, '')
	return files
}
