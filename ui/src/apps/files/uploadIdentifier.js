let seq = 0

// One identifier per added file (the server keys the upload by it plus the
// destination). simple-uploader's default - size + name with every
// non-ASCII character stripped - turned a second upload of the same file
// (to another folder, or again after deleting it), or of any same-size
// file whose name differed only in non-ASCII letters, into a "duplicate"
// that was silently ignored for as long as the Files window was open.
export function uploadIdentifier(file) {
	seq += 1
	return `${file.size}-${Date.now().toString(36)}-${seq}`
}
