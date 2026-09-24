// Format/create a storage disk and wait for it to finish, whichever backend
// is deployed:
//
//  - new backend: POST /v1/storage answers 202 { success: 200,
//    data: { job_id, job } } right away (success 200 does NOT mean done) and
//    the work runs in the background; progress is read from
//    GET /v1/storage/jobs/:id (and nudged early by message-bus events, see
//    STORAGE_JOB_EVENTS / kickJob);
//  - old backend: POST /v1/storage blocks until the format is done and
//    answers 200 { success, message } - a big disk can take minutes, so
//    the request gets a long timeout instead of axios' default 60s.
//
// Also remembers which disks this browser is formatting right now, so the
// "drive connected" toast (views/Home.vue) stays quiet for the udev
// add/remove events our own repartitioning produces.
import { api } from '@/service/service.js'

const SYNC_TIMEOUT_MS = 30 * 60 * 1000
const JOB_TIMEOUT_MS = 6 * 60 * 60 * 1000
const POLL_MS = 1500
const MAX_POLL_FAILURES = 8
// udev keeps reporting the disk for a little while after the job ends
const FORMAT_QUIET_MS = 45 * 1000

// Message-bus events the local-storage job runner publishes (properties:
// local-storage:job_id, :job_kind, :path, :state, :step, :message,
// :mount_point). They only nudge the poller - GET /storage/jobs/:id stays
// the source of truth, so a missed event costs at most one poll interval.
export const STORAGE_JOB_EVENTS = [
	'local-storage:storage-job:progress',
	'local-storage:storage-job:end',
	'local-storage:storage-job:error'
]

// Rough progress for the job's named steps (the backend reports steps,
// not percentages).
const STEP_PROGRESS = [
	['queued', 5],
	['unmounting', 15],
	['deleting partitions', 30],
	['creating partition', 50],
	['formatting', 50],
	['waiting for the new filesystem', 75],
	['mounting', 88],
	['done', 100]
]

export function stepProgress(step) {
	const s = String(step || '').toLowerCase()
	const hit = STEP_PROGRESS.find(([name]) => s.startsWith(name))
	return hit ? hit[1] : null
}

const formatting = new Map() // device path -> expiry timestamp (0 = in progress)
const kickers = new Map() // job id -> fn that polls immediately

export function markFormatting(path) {
	if (path) formatting.set(path, 0)
}

export function unmarkFormatting(path) {
	if (path) formatting.set(path, Date.now() + FORMAT_QUIET_MS)
}

// True while `path` (e.g. /dev/sdb, or one of its partitions /dev/sdb1,
// /dev/nvme0n1p1) is being formatted from this browser, or was very recently.
export function isFormatting(path) {
	if (!path) return false
	const now = Date.now()
	for (const [dev, until] of formatting) {
		if (until && until < now) {
			formatting.delete(dev)
			continue
		}
		if (path === dev || (path.startsWith(dev) && /^p?\d+$/.test(path.slice(dev.length)))) return true
	}
	return false
}

function jobIdOf(body) {
	if (!body || typeof body !== 'object') return ''
	const d = body.data && typeof body.data === 'object' ? body.data : {}
	return String(body.job_id || body.jobId || d.job_id || d.jobId || d.id || '')
}

// Pulls a job id out of a message-bus event's properties, whatever the
// key prefix (`job_id`, `local-storage:job_id`, `job:id`...).
export function jobIdFromEvent(res) {
	const props = (res && (res.Properties || res.properties)) || {}
	for (const key of Object.keys(props)) {
		if (/job[_:]?id$/i.test(key)) return String(props[key])
	}
	return ''
}

export function kickJob(jobId) {
	const fn = jobId && kickers.get(String(jobId))
	if (fn) fn()
}

function normaliseJob(body) {
	// Accept both { success, data: job } and a bare job object.
	const j = body && body.data && typeof body.data === 'object' && !Array.isArray(body.data) ? body.data : (body || {})
	const status = String(j.status || j.state || '').toLowerCase()
	let progress = Number(j.progress !== undefined ? j.progress : j.percent)
	if (!Number.isFinite(progress)) progress = null
	else if (progress > 0 && progress <= 1 && !Number.isInteger(progress)) progress = Math.round(progress * 100)
	if (progress === null) progress = stepProgress(j.step)
	return {
		status,
		progress,
		step: j.step || j.stage || '',
		mountPoint: j.mount_point || '',
		error: j.error || (['error', 'failed', 'failure'].includes(status) ? j.message : '') || ''
	}
}

const DONE = ['done', 'success', 'succeeded', 'completed', 'complete', 'finished', 'ok']
const FAILED = ['error', 'failed', 'failure', 'cancelled', 'canceled']

function waitForJob(jobId, onProgress) {
	return new Promise((resolve, reject) => {
		const started = Date.now()
		let timer = 0
		let failures = 0
		let inFlight = false
		let finished = false

		const finish = (fn, value) => {
			if (finished) return
			finished = true
			clearTimeout(timer)
			kickers.delete(jobId)
			fn(value)
		}

		const poll = () => {
			if (finished || inFlight) return
			clearTimeout(timer)
			inFlight = true
			api.get(`/storage/jobs/${encodeURIComponent(jobId)}`).then(res => {
				failures = 0
				const job = normaliseJob(res.data)
				if (onProgress) onProgress({ jobId, progress: job.progress, step: job.step, status: job.status })
				if (DONE.includes(job.status)) return finish(resolve, { jobId, mountPoint: job.mountPoint })
				if (FAILED.includes(job.status)) return finish(reject, new Error(job.error || 'The storage job failed'))
			}).catch(e => {
				const status = e && e.response && e.response.status
				if (status === 404) return finish(reject, new Error('The server lost track of this storage job - refresh the disk list to see its state.'))
				failures++
				if (onProgress) onProgress({ jobId, reconnecting: true })
				if (failures >= MAX_POLL_FAILURES) {
					finish(reject, new Error('Lost contact with the server while the disk was being prepared. It may still finish - refresh the disk list in a minute.'))
				}
			}).finally(() => {
				inFlight = false
				if (finished) return
				if (Date.now() - started > JOB_TIMEOUT_MS) {
					return finish(reject, new Error('The storage job is taking unusually long - refresh the disk list to see its state.'))
				}
				const delay = failures ? Math.min(POLL_MS * Math.pow(2, failures), 30000) : POLL_MS
				timer = setTimeout(poll, delay)
			})
		}

		kickers.set(jobId, poll)
		poll()
	})
}

// Resolves when the disk is formatted and mounted; rejects with an Error
// whose message is the server's (plain text - escape before HTML use).
export async function createStorage(body, { onProgress } = {}) {
	markFormatting(body.path)
	try {
		let res
		try {
			res = await api.post('/storage', body, { timeout: SYNC_TIMEOUT_MS })
		} catch (e) {
			const d = e && e.response && e.response.data
			if (e && e.response && e.response.status === 202 && jobIdOf(d)) res = e.response
			else throw new Error((d && d.message) || (e && e.message) || 'Failed to add storage')
		}
		const jobId = jobIdOf(res.data)
		if (res.status === 202 || jobId) {
			if (!jobId) throw new Error('The server accepted the request but returned no job id')
			return await waitForJob(jobId, onProgress)
		}
		// Old, synchronous backend (or ?sync=1): 200 means it is done.
		if (res.data && res.data.success !== undefined && res.data.success !== 200) {
			throw new Error(res.data.message || 'Failed to add storage')
		}
		const d = res.data && res.data.data
		return { jobId: '', mountPoint: (d && d.mount_point) || '' }
	} finally {
		unmarkFormatting(body.path)
	}
}
