// What a delete does where (GET /v1/trash/support, services/core/service/
// trash) and where a Trash item lives - shared by the delete dialog and
// the Trash view.

// deleteOutcome: mode 'trash' (NivaroOS Trash), 'provider' (the cloud
// provider's own trash, e.g. Google Drive's) or 'permanent' (with reason:
// 'chosen', 'readonly', 'no_server_move' or 'no_trash').
export function deleteOutcome(support, skipTrash) {
	const s = support || {}
	const kind = s.kind || 'disk'
	// A provider's trash can't be skipped from here: the drive does it.
	if (s.provider) return { mode: 'provider', kind, provider: s.provider }
	if (!s.supported) return { mode: 'permanent', kind, reason: s.reason || 'no_trash' }
	if (skipTrash) return { mode: 'permanent', kind, reason: 'chosen' }
	return { mode: 'trash', kind }
}

const ICONS = { phone: 'cellphone', cloud: 'cloud-outline', share: 'folder-network-outline' }

// trashPlace: icon and name of the share, cloud drive or phone an item is
// on; null for this server's own disks.
export function trashPlace(item) {
	const icon = ICONS[item && item.kind]
	return icon ? { icon, label: item.location || '' } : null
}
