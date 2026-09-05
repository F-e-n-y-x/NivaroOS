// Thin REST/WebSocket client for nivaroos-vm-sidecar (port 28641), used by
// the VM Manager windowed app. Mirrors the SIDECAR_URL convention the GPU
// widget already uses for its own sidecar. Guarded against a missing
// `window` (the unit tests run under vitest's default "node" environment,
// which has no DOM globals) rather than requiring jsdom just for this.
const hostname = typeof window !== 'undefined' ? window.location.hostname : 'localhost'
const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:'
const protocol = isHttps ? 'https:' : 'http:'
const wsProtocol = isHttps ? 'wss:' : 'ws:'
const BASE_URL = `${protocol}//${hostname}:28641`

async function request(path, options = {}) {
	const res = await fetch(`${BASE_URL}${path}`, options)
	if (!res.ok) {
		let body = {}
		try {
			if (res.json) body = await res.json()
			else if (res.text) body = JSON.parse(await res.text())
		} catch (e) {}
		throw new Error(body.error || `${options.method || 'GET'} ${path} failed: ${res.status}`)
	}
	if (res.status === 204) return null
	if (res.text) {
		const text = await res.text()
		if (!text || !text.trim()) return null
		try {
			return JSON.parse(text)
		} catch (e) {
			return null
		}
	}
	if (res.json) {
		return res.json().catch(() => null)
	}
	return null
}

const jsonBody = (payload, method = 'POST') => ({
	method,
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify(payload)
})

export const vmSidecar = {
	baseUrl: BASE_URL,

	getSetupStatus: () => request('/setup/status'),
	// InstallResult carries its own success flag regardless of HTTP
	// status (500 on failure) - read the body directly instead of
	// throwing, so the caller can show the failed step/output.
	runSetupInstall: () => fetch(`${BASE_URL}/setup/install`, { method: 'POST' }).then(res => res.json()),

	listVMs: () => request('/vms'),
	getVM: name => request(`/vms/${encodeURIComponent(name)}`),
	createVM: payload => request('/vms', jsonBody(payload)),
	updateVM: (name, payload) => request(`/vms/${encodeURIComponent(name)}`, jsonBody(payload, 'PUT')),
	startVM: name => request(`/vms/${encodeURIComponent(name)}/start`, { method: 'POST' }),
	shutdownVM: name => request(`/vms/${encodeURIComponent(name)}/shutdown`, { method: 'POST' }),
	forceOffVM: name => request(`/vms/${encodeURIComponent(name)}/force-off`, { method: 'POST' }),
	resetVM: name => request(`/vms/${encodeURIComponent(name)}/reset`, { method: 'POST' }),
	deleteVM: (name, wipeDisk) =>
		request(`/vms/${encodeURIComponent(name)}${wipeDisk ? '?wipe_disk=true' : ''}`, { method: 'DELETE' }),

	listISOs: () => request('/isos'),
	uploadISO: formData => request('/isos', { method: 'POST', body: formData }),
	deleteISO: name => request(`/isos/${encodeURIComponent(name)}`, { method: 'DELETE' }),

	listNetworks: () => request('/networks'),
	listHostInterfaces: () => request('/networks/interfaces'),
	createBridge: payload => request('/networks/bridge', jsonBody(payload)),
	deleteBridge: name => request(`/networks/bridge/${encodeURIComponent(name)}`, { method: 'DELETE' }),

	// USB/PCI devices available for passthrough, plus whether IOMMU is
	// enabled at all (a hard prerequisite for PCI passthrough specifically -
	// USB passthrough works regardless).
	getHostCapabilities: () => request('/host/capabilities'),

	// Hot attach/detach - unlike updateVM (which requires the VM stopped),
	// these work on a VM in any state: live if it's running, persisted to
	// its config either way. Used by the console's own USB/Disks panels
	// so a user never has to power a VM off just to plug something in.
	attachUSBDevice: (name, spec) => request(`/vms/${encodeURIComponent(name)}/usb-devices`, jsonBody(spec)),
	detachUSBDevice: (name, vendorId, productId) =>
		request(`/vms/${encodeURIComponent(name)}/usb-devices/${encodeURIComponent(vendorId)}/${encodeURIComponent(productId)}`, { method: 'DELETE' }),
	attachDisk: (name, spec) => request(`/vms/${encodeURIComponent(name)}/disks`, jsonBody(spec)),
	detachDisk: (name, target) => request(`/vms/${encodeURIComponent(name)}/disks/${encodeURIComponent(target)}`, { method: 'DELETE' }),
	ejectCDROM: name => request(`/vms/${encodeURIComponent(name)}/cdrom/eject`, { method: 'POST' }),
	insertCDROM: (name, isoPath) => request(`/vms/${encodeURIComponent(name)}/cdrom`, jsonBody({ iso_path: isoPath })),
	setNetworkLink: (name, mac, state) =>
		request(`/vms/${encodeURIComponent(name)}/network/link`, jsonBody({ mac, state })),
	updateNetworkAdapter: (name, oldMac, nic) =>
		request(`/vms/${encodeURIComponent(name)}/network/adapter`, jsonBody({ old_mac: oldMac, nic })),

	listSharedFolders: name => request(`/vms/${encodeURIComponent(name)}/shared-folders`),
	attachSharedFolder: (name, payload) => request(`/vms/${encodeURIComponent(name)}/shared-folders`, jsonBody(payload)),
	detachSharedFolder: (name, tag) => request(`/vms/${encodeURIComponent(name)}/shared-folders/${encodeURIComponent(tag)}`, { method: 'DELETE' }),
	insertVirtioWin: name => request(`/vms/${encodeURIComponent(name)}/insert-virtio-win`, { method: 'POST' }),

	// VM Snapshots
	listSnapshots: name => request(`/vms/${encodeURIComponent(name)}/snapshots`),
	getSnapshot: (name, snapName) => request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}`),
	createSnapshot: (name, payload) => request(`/vms/${encodeURIComponent(name)}/snapshots`, jsonBody(payload || {})),
	revertSnapshot: (name, snapName) => request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}/revert`, { method: 'POST' }),
	deleteSnapshot: (name, snapName, children) =>
		request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}${children ? '?children=true' : ''}`, { method: 'DELETE' }),

	consoleUrl: name => `${wsProtocol}//${hostname}:28641/vms/${encodeURIComponent(name)}/console`,
	// A cache-busting `t` param is left for the caller to append when
	// polling (a plain <img src> won't re-fetch an unchanged URL).
	screenshotUrl: name => `${BASE_URL}/vms/${encodeURIComponent(name)}/screenshot`
}
