import { expect, test, describe, vi, beforeEach, afterEach } from 'vitest'
import { downloadSidecar, apiBase, sidecarOrigin, formatBytes, rootOf, GATEWAY_PREFIX } from './downloadSidecar'

const store = { access_token: 'tok' }
vi.stubGlobal('localStorage', {
	getItem: k => (k in store ? store[k] : null),
	setItem: (k, v) => {
		store[k] = String(v)
	},
	removeItem: k => {
		delete store[k]
	}
})

function setLocation(protocol, hostname) {
	vi.stubGlobal('window', { location: { protocol, hostname, origin: `${protocol}//${hostname}` } })
}

function response(body, status = 200) {
	const text = typeof body === 'string' ? body : JSON.stringify(body)
	return Promise.resolve({ ok: status >= 200 && status < 300, status, text: () => Promise.resolve(text) })
}

describe('downloadSidecar', () => {
	beforeEach(() => {
		global.fetch = vi.fn()
		setLocation('http:', 'nas.local')
	})
	afterEach(() => {
		vi.unstubAllGlobals()
		vi.stubGlobal('localStorage', {
			getItem: k => (k in store ? store[k] : null),
			setItem: () => {},
			removeItem: () => {}
		})
	})

	test('over http it goes through the same-origin gateway route, with the token', async () => {
		global.fetch.mockReturnValue(response([]))
		await downloadSidecar.listDownloads()
		expect(global.fetch).toHaveBeenCalledWith(`http://nas.local${GATEWAY_PREFIX}/downloads`, { headers: { Authorization: 'tok' } })
		expect(downloadSidecar.browserAvailable()).toBe(true)
	})

	test('over https it uses the same-origin gateway route and disables the lite browser', async () => {
		setLocation('https:', 'nas.example.com')
		expect(apiBase()).toBe(`https://nas.example.com${GATEWAY_PREFIX}`)
		expect(downloadSidecar.browserAvailable()).toBe(false)
		// The proxy origin is never the UI's own.
		expect(sidecarOrigin()).toBe('http://nas.example.com:28642')
	})

	test('a stopped sidecar behind the gateway is reported as unreachable', async () => {
		setLocation('https:', 'nas.example.com')
		global.fetch.mockReturnValue(response('Bad Gateway', 502))
		await expect(downloadSidecar.listDownloads()).rejects.toMatchObject({ code: 'unreachable' })
	})

	test('an unreachable sidecar over http is reported as unreachable', async () => {
		global.fetch.mockReturnValue(Promise.reject(new TypeError('Failed to fetch')))
		await expect(downloadSidecar.listDownloads()).rejects.toMatchObject({ code: 'unreachable' })
	})

	test('server errors surface the JSON error message and status', async () => {
		global.fetch.mockReturnValue(response({ error: 'save folder must be inside /DATA' }, 400))
		await expect(downloadSidecar.addDownload({ url: 'x' })).rejects.toMatchObject({ message: 'save folder must be inside /DATA', status: 400 })
	})

	test('JSON writes are sent as application/json', async () => {
		global.fetch.mockReturnValue(response({ path: '/DATA/New' }, 201))
		await downloadSidecar.createFolder('/DATA', 'New')
		const [url, opts] = global.fetch.mock.calls[0]
		expect(url).toBe(`http://nas.local${GATEWAY_PREFIX}/storage/folders`)
		expect(opts.method).toBe('POST')
		expect(opts.headers['Content-Type']).toBe('application/json')
		expect(JSON.parse(opts.body)).toEqual({ parent: '/DATA', name: 'New' })
	})

	test('proxyUrl maps a page URL onto the session prefix on the sidecar origin', () => {
		expect(downloadSidecar.proxyUrl('/b/abc/', 'https://example.com/a/b?q=1#h')).toBe('http://nas.local:28642/b/abc/https/example.com/a/b?q=1#h')
	})
})

describe('helpers', () => {
	test('formatBytes uses binary units, labelled as such', () => {
		expect(formatBytes(512)).toBe('512 B')
		expect(formatBytes(1024 * 1024)).toBe('1.00 MiB')
		expect(formatBytes(-1)).toBe('—')
	})

	test('rootOf finds the storage root a path is in', () => {
		const roots = ['/DATA', '/media', '/mnt']
		expect(rootOf('/DATA/Downloads', roots)).toBe('/DATA')
		expect(rootOf('/DATA', roots)).toBe('/DATA')
		expect(rootOf('/DATAX/evil', roots)).toBe(null)
		expect(rootOf('/etc', roots)).toBe(null)
	})
})
