import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'

// Every $t() key of the job wizard, the Preview window, the shared
// schedule builder and storage pickers, and the Scheduled Tasks files
// exists in en_US.json (a missing key renders raw, {placeholders} and all
// - spec §12.12). Keys built at runtime ($t('prefix.' + x)) are checked
// for every value they can take.
const UI = path.resolve(__dirname, '../../..')
const en = JSON.parse(fs.readFileSync(path.join(UI, 'assets/lang/en_US.json'), 'utf8'))

const FILES = [
	'shared/scheduling/ScheduleBuilder.vue',
	'shared/scheduling/CronSummary.vue',
	'shared/scheduling/cronText.js',
	'shared/scheduling/cronPatterns.js',
	'shared/storage/StoragePickerWindow.vue',
	'shared/storage/FolderPickerWindow.vue',
	'shared/storage/LocationRow.vue',
	'apps/backup/windows/BackupJobWizardWindow.vue',
	'apps/backup/windows/BackupPreviewWindow.vue',
	'apps/backup/summaries.js',
	...['WizardStepper', 'PresetGrid', 'JobTypePicker', 'SourcePicker', 'ExcludeEditor', 'AppConsistencyOptions', 'LocationCard', 'SpaceEstimate',
		'TriggerEditor', 'ConditionEditor', 'RetryPolicy', 'KeepEditor', 'AdvancedOptions', 'JobSummary', 'FieldError'].map(c => `apps/backup/components/${c}.vue`),
	'apps/settings/ScheduledTaskWindow.vue',
	'apps/settings/ScheduledTaskLogWindow.vue',
	'apps/settings/sections/ScheduledTasksSection.vue'
]

// prefix -> every suffix the code can append.
const DYNAMIC = {
	'backup.ep.': ['volume', 'usb', 'merge', 'smb', 'cloud'],
	'backup.exclude.': ['caches', 'trash', 'temp', 'thumbs', 'node_modules'],
	'backup.preset.': ['photos_usb', 'documents_cloud', 'apps', 'vms', 'downloads', 'scratch'].flatMap(p => [`${p}.title`, `${p}.desc`]),
	'backup.preview.guard.': ['delete', 'change', 'empty_source'],
	'backup.preview.op.': ['add', 'update', 'delete'],
	'backup.preview.tab.': ['add', 'update', 'delete'],
	'backup.preview.title_': ['mirror', 'copy', 'archive'],
	'backup.quirk.': ['case_insensitive', 'ntfs_chars', 'mtime_2s', 'max_file_4g', 'no_modtime', 'no_hash', 'no_metadata', 'per_branch_free', 'daily_quota'],
	'backup.warn.': ['limited_change_detection', 'system_disk', 'exported_share', 'world_writable'],
	'backup.type.': ['mirror', 'copy', 'archive'].flatMap(t => [`${t}.label`, `${t}.promise`, `${t}.deleted`]),
	'backup.wizard.check_status.': ['pass', 'warn', 'fail', 'skip'],
	'backup.wizard.step.': ['start', 'what', 'where', 'when', 'keep', 'review'],
	'backup.wizard.what.quick.': ['apps', 'vms', 'folders'],
	'backup.wizard.when.unmet_hint.': ['skip', 'wait', 'fail'],
	'backup.wizard.where.hint_': ['usb', 'cloud', 'local'],
	'backup.health.': ['problem', 'offline', 'warning', 'ok', 'disabled'],
	'schedule.builder.kind.': ['every_n_minutes', 'hourly', 'every_n_hours', 'daily', 'weekdays', 'weekly', 'monthly', 'custom'],
	'schedule.cron_field.': ['minute', 'hour', 'dom', 'month', 'dow'],
	'schedule.status.': ['done', 'success', 'running', 'error', 'interrupted']
}

function sources() {
	return FILES.map(f => [f, fs.readFileSync(path.join(UI, f), 'utf8')])
}

describe('wizard, pickers and Scheduled Tasks i18n', () => {
	test('literal keys exist', () => {
		const missing = []
		for (const [f, src] of sources()) {
			for (const m of src.matchAll(/(?:\$t|\bt)\(\s*'((?:[^'\\]|\\.)+)'\s*[,)]/g)) {
				const key = m[1].replace(/\\'/g, "'")
				if (!(key in en)) missing.push(`${f}: ${key}`)
			}
			for (const m of src.matchAll(/\bkey: '((?:backup|schedule)\.[a-z0-9_.]+)'\s*[,}]/g)) {
				if (!(m[1] in en)) missing.push(`${f}: ${m[1]}`)
			}
		}
		expect(missing).toEqual([])
	})

	test('keys built at runtime exist for every value', () => {
		const used = new Set()
		for (const [, src] of sources()) for (const m of src.matchAll(/\$t\(\s*'([a-z_.]+)'\s*\+/g)) used.add(m[1])
		const unknown = [...used].filter(p => !DYNAMIC[p] && !p.startsWith('backup.wizard.field.'))
		expect(unknown).toEqual([])
		const missing = Object.entries(DYNAMIC).flatMap(([p, xs]) => xs.map(x => p + x)).filter(k => !(k in en))
		expect(missing).toEqual([])
	})

	test('every wizard field has a label for the error summary', async () => {
		const { FIELD_STEP } = await import('../wizard/draft')
		const missing = Object.keys(FIELD_STEP).map(f => 'backup.wizard.field.' + f.replace(/\./g, '_')).filter(k => !(k in en))
		expect(missing).toEqual([])
	})

	test('no literal colours in the wizard and preview styles (spec §13)', () => {
		for (const [f, src] of sources().filter(([f]) => f.startsWith('apps/backup/'))) {
			const style = (src.split('<style')[1] || '')
			expect(style.match(/#[0-9a-fA-F]{3,6}\b/g), f).toBeNull()
		}
		const scss = fs.readFileSync(path.join(UI, 'apps/backup/components/wizard-form.scss'), 'utf8') + fs.readFileSync(path.join(UI, 'shared/storage/picker-common.scss'), 'utf8')
		expect(scss.match(/#[0-9a-fA-F]{3,6}\b/g)).toBeNull()
	})

	test('no Buefy modals or dialogs: every dialog is a desktop window', () => {
		for (const [f, src] of sources().filter(([f]) => !f.startsWith('apps/settings/'))) {
			expect(/\$buefy\.dialog|<b-modal/.test(src), f).toBe(false)
		}
	})
})
