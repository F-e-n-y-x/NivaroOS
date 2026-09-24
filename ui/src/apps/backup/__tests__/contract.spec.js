import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import { ERROR_CODES, ERROR_CLASSES, ACTIONS, FIELD_CODES, errorInfo, errorKeys, fieldErrorKey } from '../errorCodes'
import { BACKUP_EVENTS, EVENT_PROPERTIES, JOB_CHANGE } from '../events'
import { BACKUP_APP_ID, BACKUP_SECTIONS, BACKUP_WINDOWS, backupWindow } from '../windows'

// WP-0 contract checks: the generated mirrors of the Go enums are usable
// and every key they imply exists in en_US.json (a missing key renders
// raw, {placeholders} and all). The Go side checks the keys the backend
// sends (services/backup/jobs TestI18nKeysInEnUS).
const en = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../assets/lang/en_US.json'), 'utf8'))

describe('errorCodes.js', () => {
	test('every code has a known class and title/cause/fix text', () => {
		const missing = []
		for (const [code, info] of Object.entries(ERROR_CODES)) {
			expect(ERROR_CLASSES).toContain(info.class)
			for (const part of ['title', 'cause', 'fix']) {
				if (!en[`backup.err.${code}.${part}`]) missing.push(`backup.err.${code}.${part}`)
			}
			for (const a of info.actions) expect(ACTIONS).toContain(a)
		}
		expect(missing).toEqual([])
	})

	test('every action and field code is labelled', () => {
		const missing = [...ACTIONS.map(a => `backup.action.${a}`), ...FIELD_CODES.map(f => `backup.field.${f}`)].filter(k => !en[k])
		expect(missing).toEqual([])
	})

	test('unknown codes fall back to internal', () => {
		expect(errorInfo('no_such_code')).toBe(ERROR_CODES.internal)
		expect(errorKeys('no_such_code').title).toBe('backup.err.internal.title')
		expect(errorKeys('cloud_auth').fix).toBe('backup.err.cloud_auth.fix')
		expect(fieldErrorKey('invalid_cron')).toBe('backup.field.invalid_cron')
		expect(fieldErrorKey('path_not_allowed')).toBe('backup.err.path_not_allowed.title')
	})

	test('is frozen', () => {
		expect(Object.isFrozen(ERROR_CODES)).toBe(true)
		expect(Object.isFrozen(ERROR_CODES.cloud_auth.actions)).toBe(true)
	})
})

describe('events.js', () => {
	test('names are the nivaroos:backup:* bus events with properties', () => {
		for (const name of Object.values(BACKUP_EVENTS)) {
			expect(name).toMatch(/^nivaroos:backup:[a-z-]+$/)
			expect(EVENT_PROPERTIES[name]).toBeTruthy()
		}
		expect(EVENT_PROPERTIES[BACKUP_EVENTS.RUN_PROGRESS]).toContain('bytes')
		expect(Object.values(JOB_CHANGE)).toEqual(['created', 'updated', 'deleted', 'toggled'])
	})
})

describe('windows.js', () => {
	const t = (key, args) => `${key}|${(args && args.name) || ''}`

	test('every title key, section and wizard step is in en_US.json', () => {
		const keys = [
			'backup.app.title', 'backup.nav.label',
			...Object.values(BACKUP_WINDOWS).flatMap(w => [w.titleKey, w.editTitleKey].filter(Boolean)),
			...BACKUP_SECTIONS.map(s => `backup.nav.${s}`),
			...['start', 'what', 'where', 'when', 'keep', 'review'].map(s => `backup.wizard.step.${s}`)
		]
		expect(keys.filter(k => !en[k])).toEqual([])
	})

	test('ids are stable per job/run and the app is a singleton', () => {
		expect(backupWindow(t, 'app', { section: 'jobs', jobId: 'bk_1' }).id).toBe(BACKUP_APP_ID)
		expect(backupWindow(t, 'run', { runId: 'run_1', jobName: 'Photos' }).id).toBe('backup-run-run_1')
		expect(backupWindow(t, 'preview', { runId: 'run_1' }).id).toBe('backup-preview-run_1')
		expect(backupWindow(t, 'browse', { jobId: 'bk_1', versionId: 'current' }).id).toBe('backup-browse-bk_1-current')
		expect(backupWindow(t, 'wizard', { jobId: 'bk_1' }).id).toBe('backup-wizard-bk_1')
		expect(backupWindow(t, 'wizard', {}).id).toMatch(/^backup-wizard-new-\d+$/)
	})

	test('payload carries component, size, title and props', () => {
		const w = backupWindow(t, 'run', { runId: 'run_1', jobId: 'bk_1', jobName: 'Photos' })
		expect(w).toMatchObject({ component: 'BackupRunWindow', width: 640, height: 520, title: 'backup.window.run|Photos' })
		expect(w.props).toEqual({ runId: 'run_1', jobId: 'bk_1', winId: 'backup-run-run_1' })
		const edit = backupWindow(t, 'wizard', { jobId: 'bk_1', jobName: 'Photos' })
		expect(edit.title).toBe('backup.window.wizard_edit|Photos')
		const app = backupWindow(t, 'app', { section: 'activity', runId: 'run_1' })
		expect(app.props.section).toBe('activity')
		expect(typeof app.props.requestedAt).toBe('number')
		expect(() => backupWindow(t, 'nope')).toThrow()
	})
})
