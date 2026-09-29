import { describe, test, expect, vi } from 'vitest'
import {
	createSessionApi, mergeSessions, sessionSubtitle, isIdleShell, shortCwd, relativeTime, exitSummary, attachUrl,
	saveWindowLayout, loadWindowLayout, forgetWindowLayout, saveLastClosed, takeLastClosed,
	saveContainerSession, loadContainerSession, pickContainerSession, restorableTabs,
} from '../termSessions'

function memStorage() {
	const m = new Map()
	return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k), m }
}

const ok = (data) => Promise.resolve({ data: { success: 200, message: 'OK', data } })
const host = (id, extra = {}) => ({ id, kind: 'host', title: 'Terminal ' + id, state: 'running', shell: '/bin/bash', command: 'bash', user: 'alice', last_activity_at: '2026-09-29T10:00:00Z', clients: 0, ...extra })
const ctr = (id, extra = {}) => ({ id, kind: 'container', container: 'jellyfin', title: 'jellyfin (bash)', state: 'running', shell: '/bin/bash', last_activity_at: '2026-09-29T09:00:00Z', clients: 0, ...extra })

describe('REST client', () => {
	test('lists both families and merges them; one failing family is reported, not fatal', async () => {
		const http = {
			get: vi.fn((url) => (url === '/sys/terminal-sessions'
				? ok({ sessions: [host('a')], running: 1, limits: { max_sessions: 12 } })
				: Promise.reject(Object.assign(new Error('down'), { response: { status: 502 } })))),
		}
		const api = createSessionApi(http)
		const r = await api.listAll()
		expect(r.sessions.map((s) => s.id)).toEqual(['a'])
		expect(Object.keys(r.errors)).toEqual(['container'])
		expect(r.limits.max_sessions).toBe(12)
	})

	test('routes: create / rename (PUT) / kill / container filter', async () => {
		const http = { get: vi.fn(() => ok({ sessions: [] })), post: vi.fn(() => ok(host('n'))), put: vi.fn(() => ok(host('a', { title: 'x' }))), delete: vi.fn(() => ok(null)) }
		const api = createSessionApi(http)
		await api.create('container', { container: 'jellyfin', cols: 80, rows: 24 })
		expect(http.post).toHaveBeenCalledWith('/container/terminal-sessions', { container: 'jellyfin', cols: 80, rows: 24 })
		await api.rename(host('a'), 'x')
		expect(http.put).toHaveBeenCalledWith('/sys/terminal-sessions/a', { title: 'x' })
		await api.kill(ctr('c'))
		expect(http.delete).toHaveBeenCalledWith('/container/terminal-sessions/c')
		await api.listContainer('jellyfin')
		expect(http.get).toHaveBeenCalledWith('/container/terminal-sessions', { container: 'jellyfin' })
	})

	test('probe tells gone (404) from unreachable', async () => {
		const mk = (impl) => createSessionApi({ get: impl })
		await expect(mk(() => ok(host('a'))).probe('host', 'a')).resolves.toBe('alive')
		await expect(mk(() => Promise.reject({ response: { status: 404 } })).probe('host', 'a')).resolves.toBe('gone')
		await expect(mk(() => Promise.reject(new Error('Network Error'))).probe('host', 'a')).resolves.toBe('unknown')
	})
})

describe('list helpers', () => {
	test('running first, newest activity first, duplicates dropped', () => {
		const list = mergeSessions([
			[host('old', { last_activity_at: '2026-09-29T08:00:00Z' }), host('x', { state: 'exited', last_activity_at: '2026-09-29T12:00:00Z' })],
			[ctr('c'), host('old')],
			[host('new', { last_activity_at: '2026-09-29T11:00:00Z' })],
		])
		expect(list.map((s) => s.id)).toEqual(['new', 'c', 'old', 'x'])
	})

	test('a host id and a container id can be equal without clashing', () => {
		expect(mergeSessions([[host('same')], [ctr('same')]])).toHaveLength(2)
	})

	test('subtitle shows the foreground job and a ~ cwd', () => {
		expect(sessionSubtitle(host('a', { command: 'vim', cwd: '/home/alice/src' }))).toBe('vim · ~/src')
		expect(sessionSubtitle(host('a', { command: 'bash', cwd: '/home/alice' }))).toBe('~')
		expect(sessionSubtitle(host('a', { user: 'root', command: '-bash', cwd: '/root/x' }))).toBe('~/x')
		expect(sessionSubtitle(ctr('c', { command: '', cwd: '/config' }))).toBe('/config')
		expect(sessionSubtitle(ctr('c', { command: '', cwd: '' }))).toBe('bash')
		expect(shortCwd(host('a', { cwd: '/home/alicex' }))).toBe('/home/alicex')
	})

	test('idle shell detection (used to skip the "end it?" question)', () => {
		expect(isIdleShell(host('a', { command: 'bash' }))).toBe(true)
		expect(isIdleShell(host('a', { command: '/bin/zsh', shell: '/bin/bash' }))).toBe(true)
		expect(isIdleShell(host('a', { command: 'htop' }))).toBe(false)
		expect(isIdleShell(host('a', { command: undefined }))).toBe(true)
	})

	test('relative time and exit summaries', () => {
		const now = Date.parse('2026-09-29T10:00:00Z')
		expect(relativeTime('2026-09-29T09:59:50Z', now)).toBe('just now')
		expect(relativeTime('2026-09-29T09:50:00Z', now)).toBe('10 min ago')
		expect(relativeTime('2026-09-29T07:00:00Z', now)).toBe('3 h ago')
		expect(relativeTime('', now)).toBe('')
		expect(exitSummary({ code: 7, reason: 'exited' })).toBe('Shell exited with code {code}')
		expect(exitSummary({ code: -1, reason: 'killed' })).toBe('This session was ended')
		expect(exitSummary({ code: -1, reason: 'timeout' })).toMatch(/idle/)
	})

	test('attach url uses the server-provided path', () => {
		expect(attachUrl(host('a', { attach_path: '/v1/sys/terminal-sessions/a/attach' }), { wsBase: 'ws://10.0.0.2', token: 't k', cols: 90, rows: 30 }))
			.toBe('ws://10.0.0.2/v1/sys/terminal-sessions/a/attach?token=t+k&cols=90&rows=30')
		expect(attachUrl({ id: 'c', kind: 'container' }, { wsBase: 'wss://x', token: 't' })).toBe('wss://x/v1/container/terminal-sessions/c/attach?token=t')
	})
})

describe('window memory', () => {
	test('per-window layout round-trips and ignores junk', () => {
		const st = memStorage()
		saveWindowLayout('terminal', { tabs: [{ id: 'a', kind: 'host' }, { id: 'c', kind: 'container' }, { nope: 1 }], active: 'c' }, st)
		expect(loadWindowLayout('terminal', st)).toEqual({ tabs: [{ id: 'a', kind: 'host' }, { id: 'c', kind: 'container' }], active: 'c' })
		forgetWindowLayout('terminal', st)
		expect(loadWindowLayout('terminal', st)).toBeNull()
		st.setItem('nvos_term_window_w', '{not json')
		expect(loadWindowLayout('w', st)).toBeNull()
		saveWindowLayout('empty', { tabs: [] }, st)
		expect(loadWindowLayout('empty', st)).toBeNull()
	})

	test('last closed is taken once, and expires', () => {
		const st = memStorage()
		saveLastClosed({ tabs: [{ id: 'a', kind: 'host' }], active: 'a' }, st, 1000)
		expect(takeLastClosed(st, 2000)).toEqual({ tabs: [{ id: 'a', kind: 'host' }], active: 'a' })
		expect(takeLastClosed(st, 2000)).toBeNull()
		saveLastClosed({ tabs: [{ id: 'a', kind: 'host' }] }, st, 0)
		expect(takeLastClosed(st, 8 * 24 * 3600 * 1000)).toBeNull()
	})

	test('works without storage (private mode)', () => {
		const broken = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') }, removeItem: () => { throw new Error('blocked') } }
		expect(() => saveWindowLayout('w', { tabs: [{ id: 'a', kind: 'host' }] }, broken)).not.toThrow()
		expect(loadWindowLayout('w', broken)).toBeNull()
		expect(takeLastClosed(broken)).toBeNull()
	})

	test('restorable tabs keep only sessions still running, in saved order', () => {
		const layout = { tabs: [{ id: 'b', kind: 'host' }, { id: 'gone', kind: 'host' }, { id: 'a', kind: 'host' }, { id: 'x', kind: 'host' }], active: 'gone' }
		const r = restorableTabs(layout, [host('a'), host('b'), host('x', { state: 'exited' })])
		expect(r.tabs.map((s) => s.id)).toEqual(['b', 'a'])
		expect(r.active).toBe('b')
		expect(restorableTabs(null, [])).toEqual({ tabs: [], active: null })
	})

	test('container console picks its last session, else a detached one, else none', () => {
		const st = memStorage()
		saveContainerSession('jellyfin', 'c2', st)
		expect(loadContainerSession('jellyfin', st)).toBe('c2')
		const list = [ctr('c1', { clients: 1 }), ctr('c2', { clients: 1 }), ctr('c3')]
		expect(pickContainerSession(list, 'c2').id).toBe('c2')
		expect(pickContainerSession(list, 'zz').id).toBe('c3')
		expect(pickContainerSession([ctr('c1', { clients: 1 })], '')).toBeNull()
		expect(pickContainerSession([ctr('c9', { state: 'exited' })], 'c9')).toBeNull()
	})
})
