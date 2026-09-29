// Pure logic of the Phones section (sections/PhonesSection.vue): phone
// backups pushed by the NivaroOS app (docs/specs/2026-09-30-phone-backup-
// device-api.md). No Vue here, so __tests__/phones.spec.js covers it.

// Display order and kind of every category (jobs/phone_model.go).
export const PHONE_CATEGORIES = Object.freeze(['media', 'files', 'contacts', 'calendar', 'sms', 'calllog', 'apps', 'apks', 'settings'])
export const FILE_CATEGORIES = Object.freeze(['media', 'files', 'apks'])
export const INCREMENTAL_CATEGORIES = Object.freeze(['sms', 'calllog'])

export const CATEGORY_ICONS = Object.freeze({
	media: 'image-multiple-outline',
	files: 'folder-outline',
	contacts: 'account-box-multiple-outline',
	calendar: 'calendar-month-outline',
	sms: 'message-text-outline',
	calllog: 'phone-log-outline',
	apps: 'apps',
	apks: 'package-variant-closed',
	settings: 'cog-outline'
})

export function isFileCategory(c) {
	return FILE_CATEGORIES.includes(c)
}

// deviceTone is a phone's status dot: problem (revoked, last backup
// failed, location unusable), warning (partial, stale, offline drive),
// ok, or muted (never backed up).
export function deviceTone(device, { destination = null, staleDays = 7, now = Date.now() } = {}) {
	if (!device) return 'muted'
	if (device.revoked_at) return 'danger'
	if (destination && destination.error_code && destination.error_code !== 'dest_offline') return 'danger'
	if (device.last_status === 'failed') return 'danger'
	if (destination && !destination.online) return 'warn'
	if (!device.last_backup_at) return 'muted'
	if (device.last_status === 'partial' || device.last_status === 'interrupted') return 'warn'
	if (staleDays > 0 && now - new Date(device.last_backup_at).getTime() > staleDays * 86400000) return 'warn'
	return 'ok'
}

// deviceStateKey is the i18n key (backup.phones.state.*) of that status.
export function deviceStateKey(device, opts = {}) {
	if (!device) return 'backup.phones.state.never'
	if (device.revoked_at) return 'backup.phones.state.revoked'
	if (device.moving) return 'backup.phones.state.moving'
	const d = opts.destination
	if (d && d.error_code && d.error_code !== 'dest_offline') return 'backup.phones.state.dest_problem'
	if (d && !d.online) return 'backup.phones.state.dest_offline'
	if (!device.last_backup_at) return 'backup.phones.state.never'
	if (device.last_status === 'failed') return 'backup.phones.state.failed'
	if (device.last_status === 'partial') return 'backup.phones.state.partial'
	const tone = deviceTone(device, opts)
	return tone === 'warn' ? 'backup.phones.state.stale' : 'backup.phones.state.ok'
}

// crumbs splits a browse path into breadcrumb steps: [{name, path}].
export function crumbs(path) {
	const out = []
	let acc = ''
	for (const seg of String(path || '').split('/').filter(Boolean)) {
		acc = acc ? `${acc}/${seg}` : seg
		out.push({ name: seg, path: acc })
	}
	return out
}

export function parentPath(path) {
	const parts = String(path || '').split('/').filter(Boolean)
	parts.pop()
	return parts.join('/')
}

// sortEntries: folders first, then by name (the server's order is by
// path, which puts "B" before "a").
export function sortEntries(entries, collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })) {
	return (entries || []).slice().sort((a, b) => {
		if (!!a.dir !== !!b.dir) return a.dir ? -1 : 1
		return collator.compare(a.name, b.name)
	})
}

// locationText describes where a phone's backups go.
export function locationText(t, dest) {
	if (!dest) return ''
	if (!dest.location) return t('backup.phones.location_default', { path: dest.path })
	const where = dest.location.label || dest.location.ref_id
	return dest.location.sub_path ? `${where} › ${dest.location.sub_path}` : where
}

// moveProgress is 0..1 of a location change in progress (null unknown).
export function moveProgress(move) {
	if (!move || move.state !== 'moving') return null
	if (move.total_bytes > 0) return Math.min(1, move.bytes / move.total_bytes)
	if (move.total_files > 0) return Math.min(1, move.files / move.total_files)
	return null
}

// categoryCount is the "what is kept" figure of a category row.
export function categoryCount(t, fmt, c) {
	if (!c) return ''
	if (isFileCategory(c.category)) {
		const files = t('backup.phones.count_files', { count: fmt.number(c.files || 0) })
		return c.deleted_files ? `${files} · ${t('backup.phones.count_deleted', { count: fmt.number(c.deleted_files) })}` : files
	}
	if (!c.exports) return t('backup.phones.count_none')
	const key = INCREMENTAL_CATEGORIES.includes(c.category) ? 'backup.phones.count_items_all' : 'backup.phones.count_items'
	return t(key, { count: fmt.number(c.items || 0) })
}

// Conditions the app reports (PhoneCategorySettings.conditions).
export const PHONE_CONDITIONS = Object.freeze(['wifi', 'unmetered', 'charging', 'battery_not_low', 'idle'])

// phoneSchedule is what the phone reports for a category ('' unknown).
export function phoneSchedule(t, phone, category) {
	const cs = phone && phone.categories && phone.categories[category]
	if (!cs) return ''
	if (!cs.enabled) return t('backup.phones.schedule_off')
	const parts = [cs.schedule || t('backup.phones.schedule_on')]
	for (const c of cs.conditions || []) parts.push(PHONE_CONDITIONS.includes(c) ? t('backup.phones.condition.' + c) : c)
	return parts.join(' · ')
}

// Retention form limits (jobs/phone_admin.go validDeviceSettings).
export const SETTINGS_LIMITS = Object.freeze({
	keep_last: { min: 1, max: 1000 },
	keep_days: { min: 0, max: 3650 },
	deleted_purge_days: { min: 0, max: 3650 },
	stale_days: { min: 0, max: 365 }
})

// settingsErrors checks the retention form: { field: 'out_of_range' }.
export function settingsErrors(form) {
	const out = {}
	for (const [k, { min, max }] of Object.entries(SETTINGS_LIMITS)) {
		const v = form ? form[k] : undefined
		if (!Number.isInteger(v) || v < min || v > max) out[k] = 'out_of_range'
	}
	return out
}

// needsMode: changing the location of a phone that has backups asks
// whether to move them or start fresh.
export function needsMode(detail) {
	if (!detail) return false
	return (detail.categories || []).some(c => (c.files || 0) + (c.deleted_files || 0) + (c.exports || 0) > 0) || (detail.snapshots || 0) > 0
}

// snapshotLabel names a snapshot in the picker.
export function snapshotLabel(t, fmt, snap) {
	if (!snap || snap === 'latest') return t('backup.phones.snapshot_latest')
	const when = fmt.dateTime(snap.taken_at)
	return snap.status === 'partial' ? t('backup.phones.snapshot_partial', { when }) : when
}
