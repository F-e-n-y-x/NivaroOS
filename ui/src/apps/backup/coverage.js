// 3-2-1 coverage (Overview): for every source a job protects, how many
// copies exist - the original plus each destination with a successful run
// - on how many different drives/locations, and whether one is off-site.
// Pure, from the job list (+ last_success of jobs whose latest run failed),
// unit-tested in __tests__/coverage.spec.js.
import { sourcesLabel } from './summaries'

// ponytail: only cloud remotes count as off-site; an SMB share may be in
// another building, but nothing here says so. Add a per-location
// "off-site" flag if people ask.
const OFFSITE = ['cloud']

const place = ep => `${ep.kind}:${ep.ref_id}:${ep.sub_path || ''}`
const medium = ep => `${ep.kind}:${ep.ref_id}`
const succeeded = r => !!r && (r.status === 'success' || r.status === 'partial') && r.kind !== 'restore'

// jobs: GET /jobs; lastSuccess: { jobId: iso } for jobs whose last_run is
// not a success but that succeeded before (their copy still exists).
// ponytail: sources match by exact folder; a job backing up a parent
// folder doesn't count for its subfolders.
export function coverage(jobs, lastSuccess = {}) {
	const rows = new Map()
	for (const job of jobs || []) {
		if (!job.dest) continue
		const done = succeeded(job.last_run) || !!lastSuccess[job.id]
		for (const src of job.sources || []) {
			const k = place(src)
			if (!rows.has(k)) rows.set(k, { source: src, dests: new Map(), pending: 0 })
			const row = rows.get(k)
			// Two jobs writing the same folder are still one copy.
			if (done) row.dests.set(place(job.dest), job.dest)
			else row.pending++
		}
	}
	return [...rows.values()].map(({ source, dests, pending }) => {
		const list = [...dests.values()]
		const copies = 1 + list.length
		const media = new Set([source, ...list].map(medium)).size
		const offsite = list.some(d => OFFSITE.includes(d.kind))
		const gaps = []
		if (copies < 3) gaps.push('copies')
		if (media < 2) gaps.push('media')
		if (!offsite) gaps.push('offsite')
		// A folder preset's label already names the folder ("Gallery").
		return { key: place(source), source, label: source.preset ? source.label : sourcesLabel([source]), copies, media, offsite, gaps, pending }
	}).sort((a, b) => b.gaps.length - a.gaps.length || a.label.localeCompare(b.label))
}

// Jobs whose copy may exist although the latest run didn't succeed: the
// ones to ask GET /jobs/:id (stats.last_success) about.
export function needsLastSuccess(jobs) {
	return (jobs || []).filter(j => j.dest && j.last_run && !succeeded(j.last_run))
}
