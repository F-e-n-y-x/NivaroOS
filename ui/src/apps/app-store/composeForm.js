// The App Store's container form <-> Docker Compose document.
//
// The form used to be turned into a brand-new compose file every time
// (formDataToYaml), keeping one service and a handful of fields. Opening
// "Setting" on an installed app and saving - even without changing
// anything - dropped the other services (an app's database), labels,
// healthchecks, caps, top-level volumes/networks and the x-casaos store
// metadata (so the app stopped being recognised as a store app).
//
// Now the form is only a view: docToFormState() reads the editable fields
// out of a document, and applyFormToDoc() writes the edited fields back
// into a copy of that same document, leaving everything else as it was.


function clone(v) {
	return v === undefined ? undefined : JSON.parse(JSON.stringify(v))
}

function localized(v) {
	if (!v) return ''
	if (typeof v === 'string') return v
	return v.custom || v.en_us || v['en-US'] || v.en || Object.values(v)[0] || ''
}

// "8080:80", "127.0.0.1:8080:80/udp", "80", "[::1]:8080:80", "8000-8010:8000-8010"
export function parsePortSpec(p) {
	if (p && typeof p === 'object') {
		return {
			hostIp: p.host_ip || '',
			host: p.published !== undefined && p.published !== null ? String(p.published) : '',
			container: p.target !== undefined ? String(p.target) : '',
			protocol: (p.protocol || 'tcp').toUpperCase()
		}
	}
	let str = String(p)
	let protocol = 'TCP'
	const slash = str.lastIndexOf('/')
	if (slash > -1) {
		protocol = str.slice(slash + 1).toUpperCase()
		str = str.slice(0, slash)
	}
	let hostIp = ''
	const v6 = str.match(/^\[([^\]]+)\]:(.*)$/)
	if (v6) {
		hostIp = v6[1]
		str = v6[2]
	}
	const parts = str.split(':')
	const container = parts.pop()
	const host = parts.length ? parts.pop() : ''
	if (parts.length) hostIp = parts.join(':')
	return { hostIp, host, container, protocol }
}

function formatPort(p) {
	const proto = p.protocol && p.protocol.toUpperCase() !== 'TCP' ? `/${p.protocol.toLowerCase()}` : ''
	const ip = p.hostIp ? (p.hostIp.includes(':') ? `[${p.hostIp}]:` : `${p.hostIp}:`) : ''
	if (p.host) return `${ip}${p.host}:${p.container}${proto}`
	return `${p.container}${proto}`
}

// Compose memory limits come back from the server normalised to bytes.
function memoryToMB(m) {
	if (m === undefined || m === null || m === '') return ''
	if (typeof m === 'number' || /^\d+$/.test(String(m))) {
		const bytes = Number(m)
		return bytes >= 1024 * 1024 ? String(Math.round(bytes / (1024 * 1024))) : String(bytes)
	}
	const mm = String(m).trim().match(/^(\d+(?:\.\d+)?)\s*([kKmMgG])[bB]?$/)
	if (!mm) return String(m)
	const n = parseFloat(mm[1])
	const unit = mm[2].toLowerCase()
	return String(Math.round(unit === 'g' ? n * 1024 : unit === 'k' ? n / 1024 : n))
}

// Array commands are shown shell-quoted so they round-trip unchanged.
function commandToString(c) {
	if (!c) return ''
	if (!Array.isArray(c)) return String(c)
	return c.map((a) => (/^[\w@%+=:,./-]+$/.test(a) ? a : `'${String(a).replace(/'/g, `'\\''`)}'`)).join(' ')
}

function firstNetwork(service) {
	if (service.network_mode) return service.network_mode
	const n = service.networks
	if (Array.isArray(n)) return n[0] || 'bridge'
	if (n && typeof n === 'object') return Object.keys(n)[0] || 'bridge'
	return 'bridge'
}

// The Web UI link the form describes, with serverHost for a blank host;
// '' when there is nothing to open.
export function webUILink(webUI, serverHost) {
	if (!webUI || !webUI.enabled) return ''
	const host = String(webUI.hostname || '').trim() || serverHost
	const port = String(webUI.port || '').trim()
	if (!host || (!port && !String(webUI.hostname || '').trim())) return ''
	let path = String(webUI.index || '').trim() || '/'
	if (!path.startsWith('/')) path = '/' + path
	return `${webUI.scheme || 'http'}://${host}${port ? ':' + port : ''}${path}`
}

export function mainServiceKey(doc) {
	const services = (doc && doc.services) || {}
	const main = doc && doc['x-casaos'] && doc['x-casaos'].main
	return main && services[main] ? main : Object.keys(services)[0] || 'app'
}

export function docToFormState(doc) {
	doc = doc || {}
	const x = doc['x-casaos'] || {}
	const mainKey = mainServiceKey(doc)
	const service = (doc.services || {})[mainKey] || {}

	const ports = (service.ports || []).map(parsePortSpec).filter((p) => p.container)
	const volumes = (service.volumes || [])
		.map((v) => {
			if (typeof v === 'string') {
				const parts = v.split(':')
				return { host: parts[0] || '', container: parts[1] || '', mode: parts[2] || 'rw' }
			}
			if (v && typeof v === 'object') return { host: v.source || '', container: v.target || '', mode: v.read_only ? 'ro' : 'rw', type: v.type }
			return null
		})
		.filter((v) => v && v.container)
	const envs = []
	const rawEnv = service.environment
	if (Array.isArray(rawEnv)) {
		for (const e of rawEnv) {
			const eq = String(e).indexOf('=')
			envs.push(eq > -1 ? { key: e.slice(0, eq), value: e.slice(eq + 1) } : { key: e, value: '' })
		}
	} else if (rawEnv && typeof rawEnv === 'object') {
		for (const [k, v] of Object.entries(rawEnv)) envs.push({ key: k, value: v === null || v === undefined ? '' : String(v) })
	}
	const devices = (service.devices || [])
		.map((d) => {
			if (typeof d === 'string') {
				const parts = d.split(':')
				return { host: parts[0] || '', container: parts[1] || parts[0] || '' }
			}
			if (d && typeof d === 'object') return { host: d.source || '', container: d.target || d.source || '' }
			return null
		})
		.filter((d) => d && d.host)

	const portMap = x.port_map !== undefined ? String(x.port_map) : ports[0] ? ports[0].host || ports[0].container : ''
	return {
		isEditing: false,
		appName: doc.name || mainKey,
		mainService: mainKey,
		title: localized(x.title) || doc.name || mainKey,
		icon: x.icon || '',
		category: x.category || 'Others',
		tagline: localized(x.tagline),
		description: localized(x.description),
		image: service.image || '',
		containerName: service.container_name || doc.name || mainKey,
		webUI: { enabled: Boolean(portMap || x.hostname), scheme: x.scheme || 'http', hostname: x.hostname || '', port: portMap, index: x.index || '' },
		ports,
		volumes,
		envs,
		devices,
		network: firstNetwork(service),
		restart: service.restart || 'unless-stopped',
		privileged: Boolean(service.privileged),
		command: commandToString(service.command),
		memoryLimit: memoryToMB(service.deploy && service.deploy.resources && service.deploy.resources.limits && service.deploy.resources.limits.memory)
	}
}

function setLocalized(existing, value) {
	const out = existing && typeof existing === 'object' ? { ...existing } : {}
	out.en_us = value
	if ('custom' in out || !existing) out.custom = value
	return out
}

// Write the form's fields into a copy of baseDoc. Fields the form shows
// but the user didn't change are written back in the document's own
// original form (so e.g. an array command or a byte memory limit stays).
export function applyFormToDoc(baseDoc, form) {
	const doc = clone(baseDoc) || {}
	const original = docToFormState(baseDoc || {})
	const mainKey = form.mainService || mainServiceKey(doc)
	const safeName = (form.appName || form.containerName || 'custom-app').toLowerCase().replace(/[^a-z0-9_-]/g, '-')

	doc.name = doc.name || safeName
	if (!baseDoc || !baseDoc.name) doc.name = safeName
	doc.services = doc.services || {}
	const service = doc.services[mainKey] || {}
	doc.services[mainKey] = service

	service.image = form.image || service.image || 'nginx:latest'
	if (form.containerName) service.container_name = form.containerName
	service.restart = form.restart || 'unless-stopped'

	// Network: only touch it when the form's choice differs from the doc.
	if (form.network !== original.network || !baseDoc) {
		if (form.network === 'host' || /^(container|service):/.test(form.network || '')) {
			service.network_mode = form.network
			delete service.networks
		} else if (!form.network || form.network === 'bridge') {
			delete service.network_mode
			delete service.networks
		} else {
			delete service.network_mode
			service.networks = { [form.network]: {} }
			doc.networks = doc.networks || {}
			if (!doc.networks[form.network]) doc.networks[form.network] = { external: true }
		}
	}

	const samePorts = JSON.stringify(form.ports) === JSON.stringify(original.ports)
	if (!samePorts || !baseDoc) {
		const ports = (form.ports || []).filter((p) => p.container).map(formatPort)
		if (ports.length && service.network_mode !== 'host') service.ports = ports
		else delete service.ports
	}

	const sameVolumes = JSON.stringify(form.volumes) === JSON.stringify(original.volumes)
	if (!sameVolumes || !baseDoc) {
		const vols = (form.volumes || [])
			.filter((v) => v.container)
			.map((v) => `${v.host || '/DATA/AppData/' + safeName}:${v.container}${v.mode && v.mode !== 'rw' ? ':' + v.mode : ''}`)
		if (vols.length) service.volumes = vols
		else delete service.volumes
	}

	const sameEnv = JSON.stringify(form.envs) === JSON.stringify(original.envs)
	if (!sameEnv || !baseDoc) {
		const list = (form.envs || []).filter((e) => e.key)
		if (!list.length) delete service.environment
		else if (service.environment && !Array.isArray(service.environment)) {
			service.environment = Object.fromEntries(list.map((e) => [e.key, e.value || '']))
		} else service.environment = list.map((e) => `${e.key}=${e.value || ''}`)
	}

	const sameDevices = JSON.stringify(form.devices) === JSON.stringify(original.devices)
	if (!sameDevices || !baseDoc) {
		const devs = (form.devices || []).filter((d) => d.host).map((d) => `${d.host}:${d.container || d.host}`)
		if (devs.length) service.devices = devs
		else delete service.devices
	}

	if (form.privileged) service.privileged = true
	else delete service.privileged

	if (form.command !== original.command || !baseDoc) {
		if (form.command && form.command.trim()) service.command = form.command.trim()
		else delete service.command
	}

	if (String(form.memoryLimit || '') !== String(original.memoryLimit || '') || !baseDoc) {
		const m = String(form.memoryLimit || '').trim()
		if (m && m !== '0') {
			service.deploy = service.deploy || {}
			service.deploy.resources = service.deploy.resources || {}
			service.deploy.resources.limits = service.deploy.resources.limits || {}
			service.deploy.resources.limits.memory = /^\d+$/.test(m) ? `${m}M` : m
		} else if (service.deploy && service.deploy.resources && service.deploy.resources.limits) {
			delete service.deploy.resources.limits.memory
		}
	}

	const x = doc['x-casaos'] || {}
	doc['x-casaos'] = x
	x.main = mainKey
	const changed = (k) => !baseDoc || form[k] !== original[k]
	if (!baseDoc && !x.architectures) x.architectures = ['amd64', 'arm64']
	if (changed('title')) x.title = setLocalized(x.title, form.title || safeName)
	// No icon: leave it unset and the UI shows its bundled default (the old
	// fallback pointed at CasaOS's icon CDN).
	if (changed('icon')) {
		if (form.icon) x.icon = form.icon
		else delete x.icon
	}
	if (changed('tagline')) x.tagline = setLocalized(x.tagline, form.tagline || '')
	if (changed('description')) x.description = setLocalized(x.description, form.description || '')
	if (changed('category')) x.category = form.category || 'Others'
	if (JSON.stringify(form.webUI) !== JSON.stringify(original.webUI) || !baseDoc) {
		const web = form.webUI || {}
		x.port_map = web.enabled ? String(web.port || '') : ''
		x.scheme = web.scheme || x.scheme || 'http'
		// Blank host = this server (the dashboard and the mobile app fill
		// in the server's address). A cleared host or path is cleared.
		if (web.enabled && web.hostname) x.hostname = String(web.hostname).trim()
		else if ('hostname' in x) x.hostname = ''
		x.index = web.enabled ? String(web.index || '').trim() : x.index || ''
	}
	return doc
}
