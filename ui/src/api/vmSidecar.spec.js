import { expect, test, describe, vi, beforeEach, afterEach } from 'vitest'
import { vmSidecar } from './vmSidecar'

function jsonResponse(body, status = 200) {
	return Promise.resolve({
		ok: status >= 200 && status < 300,
		status,
		json: () => Promise.resolve(body)
	})
}

describe('vmSidecar', () => {
	beforeEach(() => {
		global.fetch = vi.fn()
	})
	afterEach(() => {
		vi.restoreAllMocks()
	})

	test('listVMs GETs /vms and returns the parsed body', async () => {
		global.fetch.mockReturnValue(jsonResponse([{ name: 'vm1' }]))
		const vms = await vmSidecar.listVMs()
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms`, {})
		expect(vms).toEqual([{ name: 'vm1' }])
	})

	test('a non-ok response throws using the body error message', async () => {
		global.fetch.mockReturnValue(jsonResponse({ error: 'boom' }, 500))
		await expect(vmSidecar.getVM('missing')).rejects.toThrow('boom')
	})

	test('a non-ok response with no parseable body falls back to a generic message', async () => {
		global.fetch.mockReturnValue(
			Promise.resolve({ ok: false, status: 404, json: () => Promise.reject(new Error('no body')) })
		)
		await expect(vmSidecar.getVM('missing')).rejects.toThrow('/vms/missing failed: 404')
	})

	test('a 204 response resolves to null instead of parsing a body', async () => {
		global.fetch.mockReturnValue(Promise.resolve({ ok: true, status: 204, json: () => Promise.reject(new Error('should not be called')) }))
		await expect(vmSidecar.startVM('vm1')).resolves.toBeNull()
	})

	test('deleteVM appends wipe_disk=true only when requested', async () => {
		global.fetch.mockReturnValue(Promise.resolve({ ok: true, status: 204 }))
		await vmSidecar.deleteVM('vm1', true)
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/vm1?wipe_disk=true`, { method: 'DELETE' })

		await vmSidecar.deleteVM('vm1', false)
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/vm1`, { method: 'DELETE' })
	})

	test('createVM POSTs JSON with the right headers', async () => {
		global.fetch.mockReturnValue(jsonResponse({ name: 'vm1' }, 201))
		await vmSidecar.createVM({ name: 'vm1', vcpus: 2 })
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: 'vm1', vcpus: 2 })
		})
	})

	test('runSetupInstall returns the parsed InstallResult body even on a failing HTTP status', async () => {
		global.fetch.mockReturnValue(jsonResponse({ step: 'apt-get install -y ovmf', output: 'exit 100', success: false }, 500))
		const result = await vmSidecar.runSetupInstall()
		expect(result).toEqual({ step: 'apt-get install -y ovmf', output: 'exit 100', success: false })
	})

	test('listHostInterfaces GETs /networks/interfaces', async () => {
		global.fetch.mockReturnValue(jsonResponse(['enp7s0']))
		const names = await vmSidecar.listHostInterfaces()
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/networks/interfaces`, {})
		expect(names).toEqual(['enp7s0'])
	})

	test('consoleUrl builds a ws:// URL scoped to the VM name', () => {
		// window is undefined under vitest's node environment, so the
		// client falls back to "localhost" - see vmSidecar.js.
		expect(vmSidecar.consoleUrl('my-vm')).toBe('ws://localhost:28641/vms/my-vm/console')
	})

	test('sharedFolder endpoints call correct URLs and verbs', async () => {
		global.fetch.mockReturnValue(jsonResponse([{ source_dir: '/DATA/Share', target_tag: 'nivaroshare' }]))
		const shares = await vmSidecar.listSharedFolders('my-vm')
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/my-vm/shared-folders`, {})
		expect(shares).toEqual([{ source_dir: '/DATA/Share', target_tag: 'nivaroshare' }])

		global.fetch.mockReturnValue(jsonResponse({}, 201))
		await vmSidecar.attachSharedFolder('my-vm', { source_dir: '/DATA/Share', target_tag: 'nivaroshare' })
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/my-vm/shared-folders`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ source_dir: '/DATA/Share', target_tag: 'nivaroshare' })
		})

		global.fetch.mockReturnValue(Promise.resolve({ ok: true, status: 204 }))
		await vmSidecar.detachSharedFolder('my-vm', 'nivaroshare')
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/my-vm/shared-folders/nivaroshare`, { method: 'DELETE' })

		global.fetch.mockReturnValue(Promise.resolve({ ok: true, status: 204 }))
		await vmSidecar.insertVirtioWin('my-vm')
		expect(global.fetch).toHaveBeenCalledWith(`${vmSidecar.baseUrl}/vms/my-vm/insert-virtio-win`, { method: 'POST' })
	})
})
