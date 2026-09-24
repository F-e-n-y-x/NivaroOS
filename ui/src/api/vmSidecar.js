// Thin REST/WebSocket client for nivaroos-vm-sidecar, used by the VM
// Manager windowed app. Everything goes through the NivaroOS gateway's
// same-origin route /v1/vm-sidecar/* (REST and the VNC console
// WebSockets), so it works wherever the dashboard itself works: on the LAN,
// over https, and behind a reverse proxy or tunnel (Cloudflare Tunnel,
// nginx) that only publishes the dashboard's port. It used to call the
// sidecar's own port (28641) directly, which none of those carry.
// Guarded against a missing `window` (the unit tests run under vitest's
// default "node" environment, which has no DOM globals).
export const GATEWAY_PREFIX = '/v1/vm-sidecar'

function loc() {
	return typeof window !== 'undefined' && window.location ? window.location : { protocol: 'http:', host: 'localhost', origin: 'http://localhost' }
}

// Same-origin base for REST calls and <img> URLs.
export function apiBase() {
	return `${loc().origin}${GATEWAY_PREFIX}`
}

// Same-origin base for WebSockets (ws:// on http pages, wss:// on https).
export function wsBase() {
	const l = loc()
	return `${l.protocol === 'https:' ? 'wss:' : 'ws:'}//${l.host}${GATEWAY_PREFIX}`
}

// The sidecar now requires the same JWT every other NivaroOS API call sends
// (it used to accept anything from the LAN with no auth at all) - read it
// the same way service.js's axios instance does, straight from localStorage,
// since this client uses fetch/XHR/WebSocket rather than that axios
// instance.
function authToken() {
	return localStorage.getItem('access_token') || ''
}

async function request(path, options = {}) {
	const headers = { ...(options.headers || {}) }
	const token = authToken()
	if (token) headers.Authorization = token
	const res = await fetch(`${apiBase()}${path}`, { ...options, headers })
	if (!res.ok) {
		let body = {}
		try {
			if (res.json) body = await res.json()
			else if (res.text) body = JSON.parse(await res.text())
		} catch (e) {}
		// Sidecar errors are {error}; the auth layer's 401 is {message}.
		const err = new Error(body.error || body.message || (res.status === 401 ? 'Your session expired - sign in again' : `${options.method || 'GET'} ${path} failed: ${res.status}`))
		err.status = res.status
		throw err
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
	get baseUrl() {
		return apiBase()
	},

	getSetupStatus: () => request('/setup/status'),
	// InstallResult carries its own success flag regardless of HTTP
	// status (500 on failure) - read the body directly instead of
	// throwing, so the caller can show the failed step/output.
	runSetupInstall: () =>
		fetch(`${apiBase()}/setup/install`, { method: 'POST', headers: { Authorization: authToken() } }).then(res => res.json()),

	listVMs: () => request('/vms'),
	getVM: name => request(`/vms/${encodeURIComponent(name)}`),
	createVM: payload => request('/vms', jsonBody(payload)),
	updateVM: (name, payload) => request(`/vms/${encodeURIComponent(name)}`, jsonBody(payload, 'PUT')),
	startVM: name => request(`/vms/${encodeURIComponent(name)}/start`, { method: 'POST' }),
	shutdownVM: name => request(`/vms/${encodeURIComponent(name)}/shutdown`, { method: 'POST' }),
	forceOffVM: name => request(`/vms/${encodeURIComponent(name)}/force-off`, { method: 'POST' }),
	resetVM: name => request(`/vms/${encodeURIComponent(name)}/reset`, { method: 'POST' }),
	pauseVM: name => request(`/vms/${encodeURIComponent(name)}/pause`, { method: 'POST' }),
	resumeVM: name => request(`/vms/${encodeURIComponent(name)}/resume`, { method: 'POST' }),
	deleteVM: (name, wipeDisk) =>
		request(`/vms/${encodeURIComponent(name)}${wipeDisk ? '?wipe_disk=true' : ''}`, { method: 'DELETE' }),

	listISOs: () => request('/isos'),
	uploadISO: formData => request('/isos', { method: 'POST', body: formData }),
	// ISOs are often several GB: XHR (unlike fetch) reports upload
	// progress, and the returned abort() lets the user cancel.
	uploadISOWithProgress(file, onProgress) {
		const xhr = new XMLHttpRequest()
		const promise = new Promise((resolve, reject) => {
			xhr.open('POST', `${apiBase()}/isos`)
			const token = authToken()
			if (token) xhr.setRequestHeader('Authorization', token)
			xhr.upload.onprogress = e => {
				if (e.lengthComputable && onProgress) onProgress(e.loaded, e.total)
			}
			xhr.onload = () => {
				if (xhr.status >= 200 && xhr.status < 300) return resolve()
				let msg = `Upload failed: ${xhr.status}`
				try {
					const body = JSON.parse(xhr.responseText)
					msg = body.error || body.message || msg
				} catch (e) {}
				reject(new Error(msg))
			}
			xhr.onerror = () => reject(new Error('Upload failed: network error'))
			xhr.onabort = () => {
				const e = new Error('Upload cancelled')
				e.cancelled = true
				reject(e)
			}
			const form = new FormData()
			form.append('iso', file)
			xhr.send(form)
		})
		return { promise, abort: () => xhr.abort() }
	},
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
	detachSharedFolder: (name, tag) => request(`/vms/${encodeURIComponent(name)}/shared-folders/${encodeURIComponent(tag)}`, { method: 'DELETE' }),
	insertVirtioWin: name => request(`/vms/${encodeURIComponent(name)}/insert-virtio-win`, { method: 'POST' }),

	// NivaroOS Guest Tools (drivers + guest agent + shared folder setup).
	// insert answers 409 while the disc is still being built on the server
	// (poll getGuestTools); autoSetup answers 409 when the VM has no guest
	// agent yet (the one manual step is then shown instead).
	getGuestTools: () => request('/guest-tools'),
	buildGuestTools: () => request('/guest-tools/build', { method: 'POST' }),
	insertGuestTools: name => request(`/vms/${encodeURIComponent(name)}/guest-tools/insert`, { method: 'POST' }),
	autoSetupGuestTools: name => request(`/vms/${encodeURIComponent(name)}/guest-tools/auto-setup`, { method: 'POST' }),

	// VM Snapshots
	listSnapshots: name => request(`/vms/${encodeURIComponent(name)}/snapshots`),
	getSnapshot: (name, snapName) => request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}`),
	createSnapshot: (name, payload) => request(`/vms/${encodeURIComponent(name)}/snapshots`, jsonBody(payload || {})),
	revertSnapshot: (name, snapName) => request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}/revert`, { method: 'POST' }),
	deleteSnapshot: (name, snapName, children) =>
		request(`/vms/${encodeURIComponent(name)}/snapshots/${encodeURIComponent(snapName)}${children ? '?children=true' : ''}`, { method: 'DELETE' }),

	// Neither a WebSocket handshake nor a plain <img src> can carry a custom
	// header, so the token has to ride as a query param here - the sidecar's
	// auth accepts either (see vm-sidecar/auth.go).
	consoleUrl: name => `${wsBase()}/vms/${encodeURIComponent(name)}/console?token=${encodeURIComponent(authToken())}`,
	// A cache-busting `t` param is left for the caller to append when
	// polling (a plain <img src> won't re-fetch an unchanged URL).
	screenshotUrl: name => `${apiBase()}/vms/${encodeURIComponent(name)}/screenshot?token=${encodeURIComponent(authToken())}`
}
