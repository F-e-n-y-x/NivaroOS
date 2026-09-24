// Pure helpers for the shared storage pickers (StoragePickerWindow,
// FolderPickerWindow) over GET /v1/backup/locations (spec §6.1): grouping,
// status (always icon + text, never colour alone), free space, endpoint
// building and the sub-path rules the backend enforces (§6.2).

// Picker groups, in display order.
export const LOCATION_GROUPS = Object.freeze([
	Object.freeze({ id: 'internal', kinds: Object.freeze(['volume', 'merge']), titleKey: 'backup.loc.group.internal', icon: 'harddisk' }),
	Object.freeze({ id: 'usb', kinds: Object.freeze(['usb']), titleKey: 'backup.loc.group.usb', icon: 'usb-flash-drive-outline' }),
	Object.freeze({ id: 'network', kinds: Object.freeze(['smb']), titleKey: 'backup.loc.group.network', icon: 'folder-network-outline' }),
	Object.freeze({ id: 'cloud', kinds: Object.freeze(['cloud']), titleKey: 'backup.loc.group.cloud', icon: 'cloud-outline' })
])

const KIND_ICON = { volume: 'harddisk', merge: 'database-outline', usb: 'usb-flash-drive-outline', smb: 'folder-network-outline', cloud: 'cloud-outline' }

export function locationIcon(loc) {
	if (loc && loc.system_disk) return 'server'
	return KIND_ICON[loc && loc.kind] || 'folder-outline'
}

export function locationKey(loc) {
	return loc ? `${loc.kind}:${loc.ref_id}` : ''
}

// Does this location hold the endpoint (same kind and ref)?
export function locationMatches(loc, ep) {
	return !!(loc && ep && loc.kind === ep.kind && loc.ref_id === ep.ref_id)
}

export function findLocation(list, ep) {
	return (list || []).find(l => locationMatches(l, ep)) || null
}

// groupLocations filters by a search text (label, filesystem, provider,
// mount point) and splits into LOCATION_GROUPS, dropping empty groups.
export function groupLocations(list, query = '') {
	const q = String(query || '').trim().toLowerCase()
	const hit = l => !q || [l.label, l.fstype, l.provider, l.mount_point, l.ref_id].some(v => v && String(v).toLowerCase().includes(q))
	return LOCATION_GROUPS
		.map(g => ({ ...g, items: (list || []).filter(l => g.kinds.includes(l.kind) && hit(l)) }))
		.filter(g => g.items.length)
}

// locationStatus is { tone, icon, key, args } for the status pill. tone
// maps to --status-<tone>-{bg,fg}.
export function locationStatus(loc, role = 'dest') {
	if (!loc) return null
	const warnings = loc.warnings || []
	if (!loc.online) {
		if (loc.kind === 'usb') {
			return loc.last_seen
				? { tone: 'muted', icon: 'power-plug-off-outline', key: 'backup.loc.status.not_connected_seen', args: { at: loc.last_seen } }
				: { tone: 'muted', icon: 'power-plug-off-outline', key: 'backup.loc.status.not_connected', args: {} }
		}
		if (loc.kind === 'cloud') return { tone: 'danger', icon: 'cloud-off-outline', key: 'backup.loc.status.cloud_offline', args: {} }
		return { tone: 'danger', icon: 'lan-disconnect', key: 'backup.loc.status.offline', args: {} }
	}
	if (role === 'dest' && loc.system_disk) return { tone: 'warn', icon: 'alert-outline', key: 'backup.loc.status.not_recommended', args: {} }
	if (warnings.includes('limited_change_detection')) return { tone: 'warn', icon: 'alert-outline', key: 'backup.loc.status.limited', args: {} }
	if (loc.kind === 'usb') return { tone: 'ok', icon: 'check-circle-outline', key: 'backup.loc.status.plugged_in', args: {} }
	if (loc.kind === 'smb') return { tone: 'ok', icon: 'check-circle-outline', key: 'backup.loc.status.online', args: {} }
	if (loc.kind === 'cloud') return { tone: 'ok', icon: 'check-circle-outline', key: 'backup.loc.status.connected', args: {} }
	return { tone: 'ok', icon: 'check-circle-outline', key: 'backup.loc.status.ready', args: {} }
}

// spaceInfo is null when free space is unknown (clouds without About).
export function spaceInfo(loc) {
	if (!loc || typeof loc.free !== 'number') return null
	const total = typeof loc.total === 'number' && loc.total > 0 ? loc.total : null
	const used = total ? Math.min(1, Math.max(0, (total - loc.free) / total)) : null
	return { free: loc.free, total, usedRatio: used }
}

// A location can be chosen as a source only while online; as a
// destination a remembered USB drive may be offline (the job waits for it).
export function selectable(loc, role = 'dest') {
	if (!loc) return false
	if (loc.online) return true
	return role === 'dest' && loc.kind === 'usb'
}

// Destinations that need an extra "use it anyway" confirmation: the
// system disk, or the same physical disk as the source (§12.4).
export function riskyDestination(loc, sourceLoc) {
	if (!loc) return null
	if (sourceLoc && loc.physical_disk && sourceLoc.physical_disk && loc.physical_disk === sourceLoc.physical_disk) return 'same_disk'
	if (loc.system_disk) return 'system_disk'
	return null
}

// ---------------------------------------------------------------------
// Sub-paths (§6.2): relative, slash separated, cleaned, no "..", no NUL,
// no leading "/". "" is the location's root.

// cleanSubPath returns { path, error } where error is a field code
// ('invalid') or ''.
export function cleanSubPath(input) {
	const raw = String(input === undefined || input === null ? '' : input)
	if (raw.includes('\0')) return { path: '', error: 'invalid' }
	if (raw.startsWith('/')) return { path: '', error: 'invalid' }
	const parts = raw.split('/').map(s => s.trim()).filter(s => s !== '' && s !== '.')
	if (parts.includes('..')) return { path: '', error: 'invalid' }
	return { path: parts.join('/'), error: '' }
}

export function joinSubPath(...parts) {
	return cleanSubPath(parts.filter(p => p !== undefined && p !== null && p !== '').join('/')).path
}

export function parentSubPath(p) {
	const clean = cleanSubPath(p).path
	const i = clean.lastIndexOf('/')
	return i < 0 ? '' : clean.slice(0, i)
}

export function lastSegment(p) {
	const clean = cleanSubPath(p).path
	return clean.slice(clean.lastIndexOf('/') + 1)
}

// pickerTarget is the folder the folder picker's "Use" button takes: a
// new folder being added, else the folder selected in the list, else the
// folder being browsed.
export function pickerTarget(path, selectedName = '', pendingNew = '') {
	if (pendingNew) return joinSubPath(path, pendingNew)
	if (selectedName) return joinSubPath(path, selectedName)
	return cleanSubPath(path).path
}

// A folder name typed by the user (new folder, job folder): one segment.
export function validFolderName(name) {
	const n = String(name || '').trim()
	return !!n && n !== '.' && n !== '..' && !n.includes('/') && !n.includes('\0') && n.length <= 200
}

// isSameOrInside: is sub-path b equal to or inside sub-path a?
export function isSameOrInside(a, b) {
	const pa = cleanSubPath(a).path
	const pb = cleanSubPath(b).path
	return pa === '' || pb === pa || pb.startsWith(pa + '/')
}

// endpointFromLocation builds an Endpoint (by identity, never by path).
export function endpointFromLocation(loc, subPath = '', preset = '') {
	const ep = { kind: loc.kind, ref_id: loc.ref_id, sub_path: cleanSubPath(subPath).path, label: loc.label || loc.ref_id }
	if (loc.match) ep.match = { ...loc.match }
	if (preset) ep.preset = preset
	return ep
}

// displayPath is "tower › photos/2024" style text for an endpoint.
export function displayPath(ep) {
	if (!ep) return ''
	return ep.sub_path ? `${ep.label || ep.ref_id} › ${ep.sub_path}` : (ep.label || ep.ref_id)
}
