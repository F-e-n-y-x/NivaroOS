import { describe, test, expect } from 'vitest'
import { coverage, needsLastSuccess } from '../coverage'

const gallery = { kind: 'volume', ref_id: 'sys', sub_path: 'Gallery', label: 'Gallery', preset: 'data:Gallery' }
const docs = { kind: 'volume', ref_id: 'sys', sub_path: 'Documents', label: 'Data' }
const usb = { kind: 'usb', ref_id: 'u1', sub_path: 'Backups', label: 'USB' }
const usbOther = { kind: 'usb', ref_id: 'u1', sub_path: 'Old', label: 'USB' }
const cloud = { kind: 'cloud', ref_id: 'gdrive', sub_path: 'nas', label: 'Drive' }
const ok = { id: 'r1', status: 'success', kind: 'backup' }
const failed = { id: 'r2', status: 'failed', kind: 'backup' }
const job = (id, sources, dest, last_run = ok) => ({ id, name: id, sources, dest, last_run })

describe('3-2-1 coverage', () => {
	test('counts the original plus every destination that succeeded', () => {
		const [g] = coverage([job('a', [gallery], usb), job('b', [gallery], cloud)])
		expect(g).toMatchObject({ label: 'Gallery', copies: 3, media: 3, offsite: true, gaps: [] })
	})

	test('Gallery: 2 copies, no off-site copy', () => {
		const [g] = coverage([job('a', [gallery], usb)])
		expect(g).toMatchObject({ copies: 2, media: 2, offsite: false, gaps: ['copies', 'offsite'] })
	})

	test('same drive is one medium; same folder twice is one copy', () => {
		const [g] = coverage([job('a', [gallery], usb), job('b', [gallery], usbOther), job('c', [gallery], usb)])
		expect(g).toMatchObject({ copies: 3, media: 2, gaps: ['offsite'] })
		const [d] = coverage([job('d', [docs], { kind: 'volume', ref_id: 'sys', sub_path: 'Copy' })])
		expect(d.gaps).toEqual(['copies', 'media', 'offsite'])
	})

	test('a failed or never-run job is pending, unless it succeeded before', () => {
		const jobs = [job('a', [gallery], cloud, failed), job('b', [gallery], usb, null)]
		expect(coverage(jobs)[0]).toMatchObject({ copies: 1, pending: 2 })
		expect(coverage(jobs, { a: '2026-10-01T00:00:00Z' })[0]).toMatchObject({ copies: 2, offsite: true, pending: 1 })
		expect(needsLastSuccess(jobs).map(j => j.id)).toEqual(['a'])
	})

	test('a job with several sources covers each; worst gaps first', () => {
		const rows = coverage([job('a', [gallery, docs], usb), job('b', [gallery], cloud)])
		expect(rows.map(r => r.label)).toEqual(['Data › Documents', 'Gallery'])
		expect(rows[0].copies).toBe(2)
	})
})
