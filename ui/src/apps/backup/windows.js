// Window contract for Backup & Sync (spec §12, frozen by WP-0): the
// component names, window ids, default sizes, title keys and props every
// backup window is opened with. Open them only through backupWindow(), so
// ids stay stable - a second OPEN_WINDOW with the same id focuses the
// existing window and merges its props (BackupApp watches requestedAt).
//
//   this.$store.commit('OPEN_WINDOW', backupWindow(this.$t.bind(this), 'run', { runId, jobName }))
//
// Registration (windowRegistry.js COMPONENT_REGISTRY, NO_SCROLL_COMPONENTS
// and store/mutations.js PERSISTABLE_COMPONENTS) is done by the app shell;
// NO_SCROLL and PERSISTABLE below say which list each component belongs in.

export const BACKUP_APP_ID = 'backup'

// Sections of BackupApp, in nav order (props.section).
export const BACKUP_SECTIONS = Object.freeze(['overview', 'jobs', 'restore', 'activity', 'settings'])

export const BACKUP_WINDOWS = Object.freeze({
	// props: { section, jobId, runId, wizard, preset, sourcePath, destRef, destPath, requestedAt }
	//   section     one of BACKUP_SECTIONS (default 'overview')
	//   jobId/runId select a job (Jobs) or run (Activity)
	//   wizard      true opens the new-job wizard on arrival, with preset
	//               (presets.js id), sourcePath (absolute, resolved via
	//               /locations/resolve-path), destRef ({kind, ref_id}) or
	//               destPath (a drive's mount point, resolved the same way)
	//   requestedAt Date.now() of the latest request (set by backupWindow)
	app: Object.freeze({
		component: 'BackupApp', width: 1040, height: 680, titleKey: 'backup.app.title',
		persistable: true, noScroll: false,
		id: () => BACKUP_APP_ID
	}),
	// props: { winId, jobId (null for new), preset, sourceEndpoint, destEndpoint, startStep }
	//   startStep  'start' | 'what' | 'where' | 'when' | 'keep' | 'review'
	//              (editing an existing job opens on 'review')
	wizard: Object.freeze({
		component: 'BackupJobWizardWindow', width: 720, height: 620, titleKey: 'backup.window.wizard_new', editTitleKey: 'backup.window.wizard_edit',
		persistable: false, noScroll: false,
		id: ({ jobId } = {}) => (jobId ? `backup-wizard-${jobId}` : `backup-wizard-new-${Date.now()}`)
	}),
	// props: { winId, runId, jobId } - read-only when the run is final
	run: Object.freeze({
		component: 'BackupRunWindow', width: 640, height: 520, titleKey: 'backup.window.run',
		persistable: false, noScroll: false,
		id: ({ runId }) => `backup-run-${runId}`
	}),
	// props: { winId, runId, jobId } - a preview run or a waiting_user run;
	// Continue calls POST /runs/:id/decide
	preview: Object.freeze({
		component: 'BackupPreviewWindow', width: 760, height: 560, titleKey: 'backup.window.preview',
		persistable: false, noScroll: true,
		id: ({ runId }) => `backup-preview-${runId}`
	}),
	// props: { winId, jobId, versionId }
	browse: Object.freeze({
		component: 'BackupBrowseWindow', width: 900, height: 600, titleKey: 'backup.window.browse',
		persistable: false, noScroll: true,
		id: ({ jobId, versionId }) => `backup-browse-${jobId}-${versionId}`
	}),
	// props: { winId, jobId, versionId, paths }
	restore: Object.freeze({
		component: 'BackupRestoreWindow', width: 560, height: 520, titleKey: 'backup.window.restore',
		persistable: false, noScroll: false,
		id: ({ jobId }) => `backup-restore-${jobId}-${Date.now()}`
	}),
	// Shared picker (ui/src/shared/storage/), also used outside Backup.
	// props: { winId, role: 'source' | 'dest', selected: Endpoint|null, sourceEndpoint, onSelect(location) }
	storagePicker: Object.freeze({
		component: 'StoragePickerWindow', width: 600, height: 620, titleKey: 'backup.window.storage_picker',
		persistable: false, noScroll: true,
		id: () => `storage-picker-${Date.now()}`
	}),
	// Shared picker. props: { winId, endpoint, startPath, allowCreate, onSelect(endpointWithSubPath) }
	folderPicker: Object.freeze({
		component: 'FolderPickerWindow', width: 480, height: 520, titleKey: 'backup.window.folder_picker',
		persistable: false, noScroll: true,
		id: () => `folder-picker-${Date.now()}`
	})
})

// backupWindow builds the OPEN_WINDOW payload for one of BACKUP_WINDOWS.
// t is the component's bound $t; params are the window's props plus
// jobName/name for titles that show it. winId is always set to the id.
export function backupWindow(t, kind, params = {}) {
	const def = BACKUP_WINDOWS[kind]
	if (!def) throw new Error(`unknown backup window: ${kind}`)
	const id = def.id(params)
	const { jobName, name, ...props } = params
	const titleKey = kind === 'wizard' && params.jobId ? def.editTitleKey : def.titleKey
	const title = t(titleKey, { name: jobName || name || '' })
	if (kind === 'app') props.requestedAt = Date.now()
	else props.winId = id
	return { id, component: def.component, title, props, width: def.width, height: def.height }
}
