// Server self-updater (GET /v1/sys/update/status, docs/guides/server-updates.md):
// the words Settings > Updates shows for its state. t is vue-i18n's $t.

export const BUSY_STATES = ['waiting', 'downloading', 'verifying', 'applying', 'probation', 'rolling_back']

export function isBusy(status) {
	return !!status && (!!status.running || BUSY_STATES.includes(status.state && status.state.state))
}

// The step in progress, in words ("" when idle).
export function stepLabel(t, st) {
	if (!st) return ''
	switch (st.state) {
		case 'waiting':
			return t('Waiting for running backups to finish')
		case 'downloading':
			return st.progress ? t('Downloading ({pct}%)', { pct: st.progress }) : t('Downloading')
		case 'verifying':
			return t('Checking the signature')
		case 'probation':
			return t('Installed - watching it for 10 minutes')
		case 'rolling_back':
			return t('Putting the previous version back')
		case 'applying':
			return (
				{
					verify: t('Checking the files'),
					stopping: t('Stopping the services that changed'),
					migrating: t('Updating settings'),
					installing: t('Installing'),
					health: t('Checking that everything runs')
				}[st.step] || t('Installing')
			)
	}
	return ''
}

// The last update's outcome in one sentence.
export function resultLabel(t, r) {
	if (!r) return ''
	const from = r.from === 'source' ? t('the build from source') : r.from
	switch (r.result) {
		case 'installed':
			return t('Updated to {to}', { to: r.to })
		case 'rolled_back':
			return r.reason
				? t('{to} was rolled back to {from}: {reason}', { to: r.to, from, reason: r.reason })
				: t('{to} was rolled back to {from}', { to: r.to, from })
		case 'stuck':
			return t('{to} failed and the previous version is not healthy either - see nivaroos-recover status', { to: r.to })
		default:
			return t('{to} was not installed: {reason}', { to: r.to, reason: r.reason || '' })
	}
}

export function formatSize(bytes) {
	const mb = (bytes || 0) / 1048576
	return mb >= 1024 ? (mb / 1024).toFixed(1) + ' GB' : Math.round(mb) + ' MB'
}
