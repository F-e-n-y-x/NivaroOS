// Single source of truth for copy / move / delete jobs in the UI.
//
// Before this, three components each parsed the nivaroos:file:operate event
// on their own (two progress widgets with different math, plus every
// ContentView reloading on *any* finished job), paste gave no feedback until
// the next event, and nothing could show which files failed. Now:
//   - submit() returns the server's job immediately (instant "Queued" row),
//     refuses a double-submit of the same paste, and surfaces API errors;
//   - live events, a resync on (re)connect and a quiet-period poll keep one
//     job list current, so a missed event can't leave the UI stale;
//   - finished jobs announce themselves once on `bus`, with the exact
//     folders they changed, so listings refresh precisely.
import Vue from 'vue'
import batch from './batch'

const state = Vue.observable({
	jobs: {}, // id -> job (server snapshot, see service/transfer.Job)
	order: [], // ids, oldest first
	lastEventAt: 0,
	lastSyncAt: 0,
	// ids the user closed in the panel (kept locally; history stays on the
	// server until "Clear finished")
	hidden: {},
})

// Emits: 'finished' (job) once per job reaching a terminal state.
export const bus = new Vue()

const TERMINAL = ['done', 'done_with_errors', 'failed', 'cancelled', 'interrupted']
export const isTerminal = (job) => TERMINAL.includes(job && job.state)
export const isActive = (job) => job && !isTerminal(job)

const announced = new Set()

function normalize(raw) {
	if (!raw || !raw.id) return null
	return {
		...raw,
		sources: raw.sources || [],
		failures: raw.failures || [],
		affected_dirs: raw.affected_dirs || [],
		// Events from a server that predates the new engine only carry
		// the legacy fields - map them so the panel still works.
		state: raw.state || legacyState(raw),
		bytes_done: raw.bytes_done != null ? raw.bytes_done : raw.processed_size,
		bytes_total: raw.bytes_total != null ? raw.bytes_total : raw.total_size,
		kind: raw.kind || raw.type,
		dest: raw.dest || raw.to,
	}
}

function legacyState(raw) {
	if (raw.cancelled) return 'cancelled'
	if (raw.finished) return 'done'
	return raw.status === 'CALCULATING' ? 'scanning' : 'running'
}

function upsert(raw) {
	const job = normalize(raw)
	if (!job) return
	const prev = state.jobs[job.id]
	if (!prev) state.order.push(job.id)
	Vue.set(state.jobs, job.id, job)
	if (isTerminal(job) && !announced.has(job.id)) {
		announced.add(job.id)
		// Only announce jobs that finished recently - a page load that
		// receives a day-old history shouldn't reload every window.
		const finishedAt = job.finished_at ? Date.parse(job.finished_at) : Date.now()
		if (prev || Date.now() - finishedAt < 60 * 1000) bus.$emit('finished', job)
	}
}

// Apply the live event payload (res.Properties.file_operate).
export function applyEvent(res) {
	let payload
	try {
		payload = JSON.parse(res.Properties.file_operate)
	} catch (e) {
		return
	}
	state.lastEventAt = Date.now()
	for (const raw of payload.data || []) upsert(raw)
}

// Full resync from the server (new tab, reconnect, missed events).
export async function sync() {
	try {
		const res = await batch.tasks()
		if (res.data && res.data.success === 200) {
			const list = res.data.data || []
			const seen = new Set()
			for (const raw of list) {
				upsert(raw)
				seen.add(raw.id)
			}
			// Drop jobs the server no longer has (dismissed elsewhere).
			for (const id of state.order.slice()) {
				if (!seen.has(id)) remove(id)
			}
			state.lastSyncAt = Date.now()
		}
	} catch (e) {
		// Offline / older server: live events still work.
	}
}

function remove(id) {
	Vue.delete(state.jobs, id)
	const i = state.order.indexOf(id)
	if (i >= 0) state.order.splice(i, 1)
}

// In-flight / just-submitted pastes, keyed by what they'd do - clicking
// Paste twice (or Ctrl+V held down) must not start two overwrite jobs.
const inflight = new Map()
const DEDUPE_MS = 4000

/**
 * Start a copy or move. Returns the job, or throws an Error with a
 * user-facing message.
 * @param {{type: 'copy'|'move', from: string[], to: string, style?: string}} op
 */
export async function submit({ type, from, to, style = 'overwrite' }) {
	if (!from || !from.length || !to) throw new Error('Nothing to paste')
	const key = [type, to, ...from].join('\u0000')
	const existing = inflight.get(key)
	if (existing && Date.now() - existing.at < DEDUPE_MS) {
		return existing.promise
	}
	const promise = (async () => {
		let res
		try {
			res = await batch.task({ type, item: from.map((f) => ({ from: f })), to, style })
		} catch (e) {
			throw new Error(networkMessage(e))
		}
		if (!res.data || res.data.success !== 200) {
			throw new Error((res.data && res.data.message) || 'The server refused the request')
		}
		const job = res.data.data
		if (job && job.id) upsert(job)
		return state.jobs[job && job.id] || null
	})()
	inflight.set(key, { at: Date.now(), promise })
	promise.catch(() => inflight.delete(key))
	setTimeout(() => {
		if (inflight.get(key) && inflight.get(key).promise === promise) inflight.delete(key)
	}, DEDUPE_MS)
	return promise
}

export async function cancel(id) {
	await batch.deleteTask(id)
	sync()
}

export async function retry(id) {
	const res = await batch.retry(id)
	if (!res.data || res.data.success !== 200) throw new Error((res.data && res.data.message) || 'Retry failed')
	hide(id)
	upsert(res.data.data)
	return state.jobs[res.data.data.id]
}

export function hide(id) {
	Vue.set(state.hidden, id, true)
}

export async function clearFinished() {
	for (const id of state.order.slice()) {
		if (isTerminal(state.jobs[id])) {
			hide(id)
		}
	}
	try {
		await batch.dismiss('0')
	} catch (e) {}
}

// Track a job the server started on our behalf (e.g. a big delete that
// continues in the background).
export function track(raw) {
	upsert(raw)
}

export function networkMessage(e) {
	if (e && e.response && e.response.data && e.response.data.message) return e.response.data.message
	if (e && e.message === 'Network Error') return "Can't reach the server - check the connection and try again"
	return (e && e.message) || 'Something went wrong'
}

export function visibleJobs() {
	return state.order.map((id) => state.jobs[id]).filter((j) => j && !state.hidden[j.id])
}

export default { state, bus, submit, cancel, retry, hide, clearFinished, sync, applyEvent, track, visibleJobs, isTerminal, isActive }
