// Backup & Sync is an optional module (spec §0): its launcher, dock and
// app-grid entries only exist when the service answers its
// (unauthenticated) health check. Unlike Download Station this always goes
// through the gateway's same-origin route - the service listens on
// loopback only, so there is no port to reach directly. Any failure (no
// route, service stopped, the SPA's index.html instead of JSON) means not
// installed. Cached per page load (shared by the Dock and the app grid);
// callers pass force=true on RELOAD_APP_LIST to re-check.
export const BACKUP_HEALTH_URL = '/v1/backup/health'
const TIMEOUT_MS = 4000

let backupInstalledPromise = null
export function checkBackupInstalled(force = false) {
	if (force || !backupInstalledPromise) {
		const ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null
		const timer = ctrl ? setTimeout(() => ctrl.abort(), TIMEOUT_MS) : 0
		backupInstalledPromise = fetch(BACKUP_HEALTH_URL, { cache: 'no-store', credentials: 'omit', signal: ctrl ? ctrl.signal : undefined })
			.then(res => (res.ok ? res.json() : null))
			.then(body => !!(body && body.installed === true))
			.catch(() => false)
			.finally(() => clearTimeout(timer))
	}
	return backupInstalledPromise
}

// A Scheduled Task Backup & Sync took over (core's migrated_to marker).
// Core skips it whatever else it says - enabled, a schedule, Run Now -
// and that holds whether or not the backup service answers right now.
export const MIGRATED_TO_BACKUP = 'backup'
export function isMovedToBackup(task) {
	return !!task && task.migrated_to === MIGRATED_TO_BACKUP
}

// countsAsActive: an enabled task core actually runs.
export function countsAsActive(task) {
	return !!task && !!task.enabled && !isMovedToBackup(task)
}
