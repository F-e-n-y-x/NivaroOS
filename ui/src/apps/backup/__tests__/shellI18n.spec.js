import { describe, test, expect, vi } from 'vitest'
import fs from 'fs'
import path from 'path'

// messages.js reaches service/backup.js; keep the real axios instance
// (router, store) out of the test.
vi.mock('../../../service/service.js', () => ({ instance: { request: vi.fn() } }))

import { ERROR_CODES, ACTIONS, FIELD_CODES } from '../errorCodes'
import { BACKUP_SECTIONS } from '../windows'
import { HEALTH_ORDER, STATUS_VIEW, JOB_FILTERS, JOB_SORTS, ACTIVITY_RESULTS, ACTIVITY_KINDS, ACTIVITY_RANGES } from '../state'

// The app shell's side of spec §12.12 (wizardI18n.spec.js covers the
// wizard, the Preview window, the shared pickers and Scheduled Tasks):
// every $t() key of BackupApp, its sections, the run/browse/restore
// windows and their components exists in en_US.json, including the keys
// built at runtime for every value they can take - a missing key renders
// raw, {placeholders} and all.
const UI = path.resolve(__dirname, '../../..')
const en = JSON.parse(fs.readFileSync(path.join(UI, 'assets/lang/en_US.json'), 'utf8'))

const COMPONENTS = ['ActivityList', 'AttentionList', 'BackupVsSyncExplainer', 'EmptyState', 'ErrorExplain', 'JobCard', 'JobDetailsPane', 'JobMenu',
	'RunningJobCard', 'RunProgress', 'RunSteps', 'RunSummary', 'RunLog', 'StatusHeader', 'StatusPill', 'UpcomingList', 'VersionList']
const FILES = [
	'apps/backup/BackupApp.vue',
	'apps/backup/BackupNav.vue',
	...['OverviewSection', 'JobsSection', 'RestoreSection', 'ActivitySection', 'BackupSettingsSection'].map(s => `apps/backup/sections/${s}.vue`),
	...['BackupRunWindow', 'BackupBrowseWindow', 'BackupRestoreWindow'].map(w => `apps/backup/windows/${w}.vue`),
	...COMPONENTS.map(c => `apps/backup/components/${c}.vue`),
	'apps/backup/state.js',
	'apps/backup/messages.js',
	'apps/backup/format.js',
	'apps/backup/backupMixin.js',
	'apps/backup/windows.js'
]

const RUN_KINDS = ACTIVITY_KINDS.filter(k => k !== 'all')
const TRIGGERS = ['schedule', 'volume_mounted', 'catch_up', 'manual', 'retry']
const JOB_TYPES = ['copy', 'mirror', 'archive']

// prefix -> every suffix the code can append ($t('prefix' + x ...) and
// $t(`prefix${x}...`)).
const DYNAMIC = {
	'backup.action.': ACTIONS,
	'backup.activity.range_': ACTIVITY_RANGES,
	'backup.activity.result_': ACTIVITY_RESULTS,
	'backup.ep.': ['volume', 'usb', 'merge', 'smb', 'cloud'],
	'backup.exclude.': ['caches', 'trash', 'temp', 'thumbs', 'node_modules'],
	'backup.health.': HEALTH_ORDER,
	'backup.jobs.filter_': JOB_FILTERS,
	'backup.jobs.sort_': JOB_SORTS,
	'backup.jobs.tab.': ['summary', 'history', 'versions', 'settings'],
	'backup.jobs.when_unmet.': ['skip', 'wait', 'fail'],
	'backup.kind.': RUN_KINDS,
	'backup.log.lvl_': ['info', 'warn', 'error'],
	'backup.nav.': BACKUP_SECTIONS,
	'backup.overview.state.': ['empty', 'ok', 'attention', 'problem'],
	'backup.phase.': ['precheck', 'pre_hooks', 'transfer', 'verify', 'prune', 'post_hooks'],
	'backup.restore.conflict_': ['keep_both', 'overwrite', 'skip'].flatMap(c => [c, `${c}_hint`]),
	'backup.run.count.': ['added', 'changed', 'skipped', 'errored', 'data', 'duration'],
	'backup.run.step_state.': ['pending', 'active', 'done', 'failed', 'skipped'],
	'backup.settings.': ['max_concurrent', 'default_delete_pct', 'default_change_pct', 'default_versions_days', 'log_retention_days'].flatMap(k => [k, `${k}_hint`]),
	'backup.settings.import_result.': ['imported', 'imported_unresolved', 'skipped_exists'],
	'backup.status.': Object.keys(STATUS_VIEW),
	'backup.trigger.': TRIGGERS,
	'backup.type.': JOB_TYPES.flatMap(t => [`${t}.label`, `${t}.promise`, `${t}.deleted`]),
	'backup.ver.': ['current', 'recycle', 'archive', 'files'],
	'backup.attention.': ['waiting', 'migrated_unresolved', 'partial', 'stale'].flatMap(r => [`${r}.title`, `${r}.cause`])
}
// Families whose prefix is itself built (`${base}.title`), checked by the
// dedicated tests below.
const INDIRECT = ['backup.err.', 'backup.field.']

function sources() {
	return FILES.map(f => [f, fs.readFileSync(path.join(UI, f), 'utf8')])
}

describe('Backup & Sync shell i18n', () => {
	test('literal keys exist', () => {
		const missing = []
		for (const [f, src] of sources()) {
			for (const m of src.matchAll(/\$tc?\(\s*'((?:[^'\\]|\\.)+)'\s*[,)]/g)) {
				const key = m[1].replace(/\\'/g, "'")
				if (!(key in en)) missing.push(`${f}: ${key}`)
			}
			for (const m of src.matchAll(/\b(?:label|key|title): '(backup\.[a-z0-9_.]+)'\s*[,}]/g)) {
				if (!(m[1] in en)) missing.push(`${f}: ${m[1]}`)
			}
		}
		expect(missing).toEqual([])
	})

	test('keys built at runtime are known families and exist for every value', () => {
		const used = new Set()
		for (const [, src] of sources()) {
			for (const m of src.matchAll(/\$tc?\(\s*'([a-z_.]+)'\s*\+/g)) used.add(m[1])
			for (const m of src.matchAll(/\$tc?\(\s*`([a-z_.]+)\$\{/g)) used.add(m[1])
		}
		const unknown = [...used].filter(p => !DYNAMIC[p] && !INDIRECT.includes(p))
		expect(unknown).toEqual([])
		const missing = Object.entries(DYNAMIC).flatMap(([p, xs]) => xs.map(x => p + x)).filter(k => !(k in en))
		expect(missing).toEqual([])
	})

	test('every error code is explained and every field error has a text', () => {
		const missing = []
		for (const code of Object.keys(ERROR_CODES)) for (const part of ['title', 'cause', 'fix']) if (!(`backup.err.${code}.${part}` in en)) missing.push(`backup.err.${code}.${part}`)
		// The client-side service_unavailable (module missing or stopped) has
		// its own texts (messages.js explainError); unknown codes fall back
		// to internal.
		for (const part of ['title', 'cause', 'fix']) if (!(`backup.app.unavailable.${part}` in en)) missing.push(`backup.app.unavailable.${part}`)
		if (!('backup.err.internal.title' in en)) missing.push('backup.err.internal.title')
		for (const f of FIELD_CODES) if (!(`backup.field.${f}` in en)) missing.push(`backup.field.${f}`)
		expect(missing).toEqual([])
	})

	// Entry points into Backup & Sync from other apps: only their backup.*
	// keys are this module's (the rest of those files is checked with
	// their own apps).
	test('entry points in other apps have their backup keys', () => {
		const missing = []
		for (const f of ['apps/files/ContextMenu.vue', 'apps/settings/DisksPanel.vue']) {
			const src = fs.readFileSync(path.join(UI, f), 'utf8')
			const keys = [...src.matchAll(/\$tc?\(\s*'(backup\.(?:[^'\\]|\\.)+)'\s*[,)]/g)].map(m => m[1])
			expect(keys.length, f).toBeGreaterThan(0)
			for (const k of keys) if (!(k in en)) missing.push(`${f}: ${k}`)
		}
		expect(missing).toEqual([])
	})

	test('the launcher name is translated', () => {
		expect(en['Backup & Sync']).toBeTruthy()
	})
})

describe('Backup & Sync shell rules (spec §13)', () => {
	test('no literal colours in component styles', () => {
		for (const [f, src] of sources().filter(([f]) => f.endsWith('.vue'))) {
			const style = src.split('<style').slice(1).join('')
			expect(style.match(/#[0-9a-fA-F]{3,8}\b/g), f).toBeNull()
		}
		expect(fs.readFileSync(path.join(UI, 'apps/backup/backup-common.scss'), 'utf8').match(/#[0-9a-fA-F]{3,8}\b/g)).toBeNull()
	})

	test('no Buefy modals or dialogs: every dialog is a desktop window', () => {
		for (const [f, src] of sources()) expect(/\$buefy\.(dialog|modal)|<b-modal/.test(src), f).toBe(false)
	})

	// A click handler belongs on a button, link or input - or on an element
	// that has a role and key handling (the version radios).
	test('no clickable non-interactive elements', () => {
		for (const [f, src] of sources().filter(([f]) => f.endsWith('.vue'))) {
			const tpl = src.split('<script')[0]
			const bad = (tpl.match(/<(div|span|li|p|td|tr|section|header)\b[^>]*>/g) || []).filter(tag => /@click/.test(tag) && !/\srole="/.test(tag))
			expect(bad, f).toEqual([])
		}
	})
})
