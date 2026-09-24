// Start-step presets of the job wizard (spec §12.4 "Start: presets"). Pure:
// visibility and the draft a preset starts with are computed from the
// GET /locations list (its folder presets say which /DATA folders, apps
// and VMs exist), so a preset that can't apply is hidden rather than
// offered and then failing.
//
// A preset only pre-fills; every value stays editable in the later steps.
// "Where" is always asked (hint says which kind of location fits).

// Folder presets on the locations: "data:<Folder>", "appdata:<app>",
// "vm:<name>" (engine FolderPreset.ID).
export function folderPresets(locations) {
	const out = []
	for (const loc of locations || []) {
		for (const p of loc.presets || []) out.push({ ...p, location: loc })
	}
	return out
}

export function presetsOfKind(locations, prefix) {
	return folderPresets(locations).filter(p => String(p.id).startsWith(prefix))
}

function dataPreset(locations, names) {
	const list = presetsOfKind(locations, 'data:')
	for (const n of names) {
		const hit = list.find(p => p.id.slice(5).toLowerCase() === n.toLowerCase())
		if (hit) return hit
	}
	return null
}

// endpointOf turns a folder preset into a source endpoint.
export function presetEndpoint(p) {
	const loc = p.location
	const ep = { kind: loc.kind, ref_id: loc.ref_id, sub_path: p.sub_path, label: p.label || loc.label, preset: p.id }
	if (loc.match) ep.match = { ...loc.match }
	return ep
}

const PHOTO_FOLDERS = ['Gallery', 'Photos', 'Pictures']

// PRESETS, in display order. `source(locations)` returns the endpoints the
// preset starts with ([] = the user picks); `visible(locations)` hides
// presets that can't apply. `destHint` names the kind of destination that
// fits (shown on the Where step).
export const PRESETS = Object.freeze([
	Object.freeze({
		id: 'photos_usb',
		icon: 'image-multiple-outline',
		type: 'mirror',
		destHint: 'usb',
		visible: () => true,
		source: locations => {
			const p = dataPreset(locations, PHOTO_FOLDERS)
			return p ? [presetEndpoint(p)] : []
		},
		triggers: () => [{ kind: 'volume_mounted', min_gap_hours: 24, catch_up: true }]
	}),
	Object.freeze({
		id: 'documents_cloud',
		icon: 'file-document-multiple-outline',
		type: 'copy',
		destHint: 'cloud',
		visible: () => true,
		source: locations => {
			const p = dataPreset(locations, ['Documents'])
			return p ? [presetEndpoint(p)] : []
		},
		triggers: () => [{ kind: 'schedule', cron: '0 2 * * *', catch_up: true }]
	}),
	Object.freeze({
		id: 'apps',
		icon: 'apps',
		// Archive toward a cloud, Mirror toward local storage: the wizard
		// switches when the destination is chosen (autoTypeForDest).
		type: 'mirror',
		autoArchiveToCloud: true,
		destHint: 'any',
		visible: locations => presetsOfKind(locations, 'appdata:').length > 0,
		source: () => [],
		pick: 'appdata:',
		triggers: () => [{ kind: 'schedule', cron: '0 3 * * *', catch_up: true }]
	}),
	Object.freeze({
		id: 'vms',
		icon: 'monitor-multiple',
		type: 'mirror',
		autoArchiveToCloud: true,
		destHint: 'any',
		visible: locations => presetsOfKind(locations, 'vm:').length > 0,
		source: () => [],
		pick: 'vm:',
		triggers: () => [{ kind: 'schedule', cron: '0 4 * * 0', catch_up: true }]
	}),
	Object.freeze({
		id: 'downloads',
		icon: 'download-multiple',
		type: 'copy',
		destHint: 'local',
		visible: locations => !!dataPreset(locations, ['Downloads']),
		source: locations => {
			const p = dataPreset(locations, ['Downloads'])
			return p ? [presetEndpoint(p)] : []
		},
		triggers: () => [{ kind: 'schedule', cron: '0 */6 * * *', catch_up: true }]
	}),
	Object.freeze({
		id: 'scratch',
		icon: 'pencil-plus-outline',
		type: 'copy',
		destHint: 'any',
		visible: () => true,
		source: () => [],
		triggers: () => [{ kind: 'schedule', cron: '0 3 * * *', catch_up: true }]
	})
])

export function visiblePresets(locations) {
	return PRESETS.filter(p => p.visible(locations || []))
}

export function presetById(id) {
	return PRESETS.find(p => p.id === id) || null
}

// isAppOrVmSource: app data and VM images need owners, modes and links
// kept, which clouds can't (spec §6.3 point 6).
export function isAppOrVmSource(ep) {
	const p = (ep && ep.preset) || ''
	return p.startsWith('appdata:') || p.startsWith('vm:')
}

// autoTypeForDest is the type a preset job switches to once the
// destination is known: Archive toward a cloud, Mirror otherwise. Only for
// presets that ask for it, and only while the user hasn't picked a type.
export function autoTypeForDest(preset, destKind) {
	if (!preset || !preset.autoArchiveToCloud) return null
	return destKind === 'cloud' ? 'archive' : 'mirror'
}

// safeTypes are the job types allowed for these sources toward a
// destination kind (the server rejects the rest with
// appdata_cloud_needs_archive).
export function safeTypes(sources, destKind) {
	const all = ['mirror', 'copy', 'archive']
	if (destKind === 'cloud' && (sources || []).some(isAppOrVmSource)) return ['archive']
	return all
}
