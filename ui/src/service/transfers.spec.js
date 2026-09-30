import { describe, test, expect, beforeEach, vi } from 'vitest'

const dismiss = vi.fn(() => Promise.resolve({ data: { success: 200 } }))
vi.mock('./batch', () => ({ default: { dismiss: (...a) => dismiss(...a), list: vi.fn(), retry: vi.fn(), cancel: vi.fn() } }))

function storage() {
	const data = {}
	return { getItem: k => (k in data ? data[k] : null), setItem: (k, v) => { data[k] = String(v) }, removeItem: k => { delete data[k] }, data }
}

const failedJob = { id: 'iso', kind: 'copy', state: 'done_with_errors', files_done: 1, files_failed: 1, finished_at: new Date().toISOString(), sources: ['/DATA/x.iso'], dest: '/mnt/terabox' }

describe('transfers: a dismissed job never comes back', () => {
	let store
	beforeEach(() => {
		vi.resetModules()
		dismiss.mockClear()
		store = storage()
		globalThis.localStorage = store
	})

	test('dismiss tells the server and is remembered across reloads', async () => {
		let t = await import('./transfers.js')
		t.track(failedJob)
		expect(t.default.visibleJobs().map(j => j.id)).toEqual(['iso'])
		t.hide('iso')
		expect(dismiss).toHaveBeenCalledWith('iso')
		expect(t.default.visibleJobs()).toHaveLength(0)
		// A reload: the server (say its dismiss failed) still lists the job.
		vi.resetModules()
		t = await import('./transfers.js')
		t.track(failedJob)
		expect(t.default.visibleJobs()).toHaveLength(0)
	})

	test('a clean success fading out stays in the server history', async () => {
		const t = await import('./transfers.js')
		t.track({ ...failedJob, id: 'ok', state: 'done', files_failed: 0 })
		t.fade('ok')
		expect(dismiss).not.toHaveBeenCalled()
		expect(t.default.visibleJobs()).toHaveLength(0)
	})
})
