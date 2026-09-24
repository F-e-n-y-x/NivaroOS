// What a job does, in plain sentences (spec §12.4 Review, §12.11): pure,
// job -> [{ key, args }], so the wizard's Review and any job card or
// notification say the same thing with the same i18n keys. Args are
// machine-shaped; the caller localizes them (JobSummary.vue):
//   schedule_cron  a cron expression -> its human sentence
//   apps           an array -> a localized list
//   source, dest   display labels (endpoint label › sub_path)

function epLabel(ep) {
	if (!ep) return ''
	const base = ep.label || ep.ref_id || ''
	return ep.sub_path ? `${base} › ${ep.sub_path}` : base
}

export function sourcesLabel(sources) {
	const list = sources || []
	if (!list.length) return ''
	if (list.length === 1) return epLabel(list[0])
	return { key: 'backup.summary.sources_more', args: { first: epLabel(list[0]), count: list.length - 1 } }
}

// The drive a volume_mounted trigger watches: its own ref, else the
// destination when that is a drive, else the source.
export function plugTarget(job, trigger) {
	if (trigger && trigger.volume) return trigger.volume
	if (job.dest && (job.dest.kind === 'usb' || job.dest.kind === 'volume')) return job.dest
	const src = (job.sources || [])[0]
	return src && (src.kind === 'usb' || src.kind === 'volume') ? src : null
}

export function jobSummary(job) {
	if (!job) return []
	const out = []
	const source = sourcesLabel(job.sources)
	const dest = epLabel(job.dest)
	const what = { mirror: 'backup.summary.mirror', copy: 'backup.summary.copy', archive: 'backup.summary.archive' }[job.type]
	if (what) out.push({ key: what, args: { source, dest } })

	const triggers = job.triggers || []
	const schedules = triggers.filter(t => t.kind === 'schedule' && t.cron)
	const plugs = triggers.filter(t => t.kind === 'volume_mounted')
	for (const s of schedules) out.push({ key: 'backup.summary.when_schedule', args: { schedule_cron: s.cron } })
	for (const p of plugs) {
		const target = plugTarget(job, p)
		const drive = target ? target.label || target.ref_id : ''
		if (p.min_gap_hours > 0) out.push({ key: 'backup.summary.when_plug_gap', args: { drive, hours: p.min_gap_hours } })
		else out.push({ key: 'backup.summary.when_plug', args: { drive } })
	}
	if (!schedules.length && !plugs.length) out.push({ key: 'backup.summary.when_manual', args: {} })
	else if (triggers.some(t => t.catch_up)) out.push({ key: 'backup.summary.catch_up', args: {} })

	const c = job.conditions || {}
	if (c.window && c.window.start && c.window.end) out.push({ key: 'backup.summary.window', args: { start: c.window.start, end: c.window.end } })
	if (schedules.length || plugs.length) {
		if (c.when_unmet === 'wait') out.push({ key: 'backup.summary.unmet_wait', args: { hours: Math.round((c.wait_max_min || 360) / 60) } })
		else if (c.when_unmet === 'fail') out.push({ key: 'backup.summary.unmet_fail', args: {} })
		else out.push({ key: 'backup.summary.unmet_skip', args: {} })
	}

	const r = job.retention || {}
	if (job.type === 'mirror') {
		out.push(r.versions_days ? { key: 'backup.keep.recycle_days', args: { days: r.versions_days } } : { key: 'backup.keep.recycle_forever', args: {} })
	} else if (job.type === 'archive') {
		out.push({ key: 'backup.keep.last_archives', args: { keep: r.keep_last || 8 } })
	} else if (job.type === 'copy') {
		out.push({ key: 'backup.keep.copy_never_deletes', args: {} })
	}

	const g = job.guards || {}
	if (job.type === 'mirror') out.push({ key: 'backup.summary.guards', args: { delete: g.delete_pct, change: g.change_pct } })
	else if (job.type === 'copy') out.push({ key: 'backup.summary.guard_change', args: { change: g.change_pct } })

	for (const h of job.hooks || []) {
		if (h.phase !== 'pre') continue
		if (h.action === 'stop_apps' && (h.apps || []).length) {
			out.push({ key: h.app_mode === 'one_at_a_time' ? 'backup.summary.stop_apps_each' : 'backup.summary.stop_apps', args: { apps: h.apps.slice() } })
		}
		if (h.action === 'shutdown_vm' && h.vm) out.push({ key: 'backup.summary.shutdown_vm', args: { vm: h.vm } })
	}

	const f = job.filters || {}
	const skipCount = (f.exclude_presets || []).length + (f.exclude || []).length + (f.max_size_bytes ? 1 : 0)
	if (skipCount) out.push({ key: 'backup.summary.skips', args: { count: skipCount } })

	const o = job.options || {}
	if (o.preview_first && job.type !== 'archive') out.push({ key: 'backup.summary.preview_first', args: {} })
	if (o.verify) out.push({ key: 'backup.summary.verify', args: {} })
	const retry = (job.retry && job.retry.max) || 0
	out.push(retry ? { key: 'backup.summary.retry', args: { n: retry } } : { key: 'backup.summary.no_retry', args: {} })
	return out
}
