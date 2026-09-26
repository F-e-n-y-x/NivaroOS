import { describe, expect, test } from 'vitest'
import { applyFormToDoc, docToFormState, isValidWebUIHost, parsePortSpec, webUILink } from './composeForm'

// A store app the way the server returns it: two services, labels,
// healthcheck, an array command, a byte memory limit, store metadata.
const storeDoc = () => ({
	name: 'immich',
	services: {
		server: {
			image: 'ghcr.io/immich-app/immich-server:v1.120',
			container_name: 'immich-server',
			restart: 'always',
			command: ['sh', '-c', 'start.sh --flag "a b"'],
			ports: ['127.0.0.1:2283:2283', { target: 3001, published: '3001', protocol: 'tcp', host_ip: '0.0.0.0' }],
			environment: { DB_HOST: 'db', TZ: 'UTC' },
			labels: { icon: 'x.png' },
			healthcheck: { test: ['CMD', 'curl', 'localhost'] },
			depends_on: ['db'],
			deploy: { resources: { limits: { memory: 536870912 } } },
			networks: { immich_default: {} }
		},
		db: { image: 'postgres:16', volumes: ['/DATA/AppData/immich/pg:/var/lib/postgresql/data'] }
	},
	networks: { immich_default: {} },
	'x-casaos': { main: 'server', store_app_id: 'immich', author: 'Immich', title: { en_us: 'Immich', zh_cn: '图' }, tips: { before_install: { en_us: 'hi' } }, port_map: '2283', architectures: ['amd64'] }
})

describe('App Store form <-> compose', () => {
	test('saving an unchanged form leaves the document exactly as it was', () => {
		const doc = storeDoc()
		expect(applyFormToDoc(doc, docToFormState(doc))).toEqual(storeDoc())
	})

	test('editing one field changes only that field', () => {
		const doc = storeDoc()
		const form = docToFormState(doc)
		form.image = 'ghcr.io/immich-app/immich-server:v1.121'
		const out = applyFormToDoc(doc, form)
		expect(out.services.server.image).toBe('ghcr.io/immich-app/immich-server:v1.121')
		const expected = storeDoc()
		expected.services.server.image = out.services.server.image
		expect(out).toEqual(expected)
	})

	test('reads the values the old parser corrupted', () => {
		const f = docToFormState(storeDoc())
		expect(f.ports[0]).toEqual({ hostIp: '127.0.0.1', host: '2283', container: '2283', protocol: 'TCP' })
		expect(f.memoryLimit).toBe('512')
		expect(f.network).toBe('immich_default')
		expect(f.command).toBe(`sh -c 'start.sh --flag "a b"'`)
		expect(f.mainService).toBe('server')
	})

	test('a changed port list keeps the host IP', () => {
		const doc = storeDoc()
		const form = docToFormState(doc)
		form.ports = form.ports.concat([{ hostIp: '', host: '9000', container: '90', protocol: 'UDP' }])
		expect(applyFormToDoc(doc, form).services.server.ports).toEqual(['127.0.0.1:2283:2283', '0.0.0.0:3001:3001', '9000:90/udp'])
	})

	test('port specs', () => {
		expect(parsePortSpec('80')).toEqual({ hostIp: '', host: '', container: '80', protocol: 'TCP' })
		expect(parsePortSpec('8080:80/udp')).toEqual({ hostIp: '', host: '8080', container: '80', protocol: 'UDP' })
		expect(parsePortSpec('[::1]:8080:80')).toEqual({ hostIp: '::1', host: '8080', container: '80', protocol: 'TCP' })
	})

	test('a brand new app gets a complete document', () => {
		const out = applyFormToDoc(null, { appName: 'My App', mainService: 'app', title: 'My App', image: 'nginx', containerName: 'my-app', ports: [{ host: '8080', container: '80', protocol: 'TCP' }], volumes: [], envs: [], devices: [], network: 'bridge', webUI: { enabled: true, scheme: 'http', port: '8080', index: '' } })
		expect(out.name).toBe('my-app')
		expect(out.services.app.ports).toEqual(['8080:80'])
		expect(out['x-casaos'].port_map).toBe('8080')
		expect(out['x-casaos'].main).toBe('app')
	})

	test('the Web UI link: host, port and path, and clearing them', () => {
		const doc = storeDoc()
		doc['x-casaos'].hostname = ''
		doc['x-casaos'].index = '/admin'
		const form = docToFormState(doc)
		expect(form.webUI).toEqual({ enabled: true, scheme: 'http', hostname: '', port: '2283', index: '/admin' })
		expect(applyFormToDoc(doc, form)).toEqual(doc)
		expect(webUILink(form.webUI, 'nas.local')).toBe('http://nas.local:2283/admin')

		form.webUI = { enabled: true, scheme: 'https', hostname: 'photos.example.com', port: '', index: '' }
		const x = applyFormToDoc(doc, form)['x-casaos']
		expect([x.scheme, x.hostname, x.port_map, x.index]).toEqual(['https', 'photos.example.com', '', ''])
		expect(x.store_app_id).toBe('immich')
		expect(webUILink(form.webUI, 'nas.local')).toBe('https://photos.example.com/')

		const back = docToFormState(applyFormToDoc(doc, form))
		back.webUI.hostname = ''
		back.webUI.port = '2283'
		expect(applyFormToDoc(doc, back)['x-casaos'].hostname).toBe('')
		expect(webUILink({ enabled: true, scheme: 'http', hostname: '', port: '', index: '' }, 'nas.local')).toBe('')
	})

	test('a literal $ in a variable shows once and is written escaped', () => {
		const doc = storeDoc()
		doc.services.server.environment = { PASS: 'pa$$word', HASH: '$$2y$$10$$abc', TZ: 'UTC' }
		const form = docToFormState(doc)
		expect(form.envs.find((e) => e.key === 'PASS').value).toBe('pa$word')
		expect(form.envs.find((e) => e.key === 'HASH').value).toBe('$2y$10$abc')
		// Untouched: exactly as it came.
		expect(applyFormToDoc(doc, docToFormState(doc)).services.server.environment).toEqual(doc.services.server.environment)
		// Another variable changed: the others keep their escapes, a new $ is escaped.
		form.envs.find((e) => e.key === 'TZ').value = 'a$b'
		expect(applyFormToDoc(doc, form).services.server.environment).toEqual({ PASS: 'pa$$word', HASH: '$$2y$$10$$abc', TZ: 'a$$b' })
	})

	test('the Web UI host takes a name or an address, not a port', () => {
		for (const ok of ['', 'nas', 'media.example.com', '192.168.1.20', '[fd00::1]']) expect(isValidWebUIHost(ok)).toBe(true)
		for (const bad of ['nas:8080', 'http://nas', 'nas/web', 'fd00::1', 'a b']) expect(isValidWebUIHost(bad)).toBe(false)
	})
})
