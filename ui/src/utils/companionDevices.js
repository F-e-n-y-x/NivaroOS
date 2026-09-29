// The web's list of companion devices after a rename or removal, so it
// shows what the server now holds at once (and what a refresh a moment
// later would show) - the server is the source of truth for both.

const MAX_NAME = 100

/** Trims a device name; null when it is empty or too long for the server. */
export function cleanDeviceName(name) {
	const n = String(name ?? '').trim()
	if (!n || n.length > MAX_NAME) return null
	return n
}

/**
 * The list with device `id` updated from the rename answer (the device the
 * server saved), or with just its name when the answer has none.
 */
export function applyRename(devices, id, answer, fallbackName) {
	const saved = answer && typeof answer === 'object' && answer.id === id ? answer : null
	return devices.map((d) => {
		if (d.id !== id) return d
		return saved ? { ...d, name: saved.name, name_source: saved.name_source } : { ...d, name: fallbackName }
	})
}

/** The list without device `id`: removed on the server, gone at once. */
export function withoutDevice(devices, id) {
	return devices.filter((d) => d.id !== id)
}

/**
 * What to tell the user after removing a device, from the server's answer
 * (`data` of DELETE /v1/companion/devices/:id).
 */
export function removalMessage(data, t = (s) => s) {
	if (data && data.signed_out) {
		return t('Device removed - its app was signed out. Its backups were kept.')
	}
	return t('Device removed - its backups were kept')
}
