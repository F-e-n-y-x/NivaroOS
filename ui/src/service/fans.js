// REST client for fan control (nivaroos-fans, reached through the gateway
// at /v1/fans - never its port). Reads need the normal token; changes need
// an administrator. Every call resolves to the service's JSON (the new
// status after a change) or throws a FansError with the service's message.
import { instance } from './service.js'

export const FANS_BASE = '/v1/fans'

export class FansError extends Error {
	constructor(message, status = 0, unavailable = false) {
		super(message)
		this.name = 'FansError'
		this.status = status
		this.unavailable = unavailable
	}
}

export function toFansError(err) {
	if (err instanceof FansError) return err
	const res = err && err.response
	if (!res) return new FansError('The fan service could not be reached.', 0, true)
	if (res.status === 502 || res.status === 503 || res.status === 504 || res.status === 404) {
		const msg = res.data && res.data.error
		if (res.status !== 404 || !msg || msg === 'not found') return new FansError('Fan control is not installed or not running on this server.', res.status, true)
	}
	const msg = (res.data && (res.data.error || res.data.message)) || `Request failed (${res.status})`
	return new FansError(msg, res.status)
}

export function createFansClient(transport = instance) {
	const call = async (method, path, body) => {
		try {
			const res = method === 'get' ? await transport.get(FANS_BASE + path) : await transport.post(FANS_BASE + path, body || {})
			return res.data
		} catch (err) {
			throw toFansError(err)
		}
	}
	return {
		status: () => call('get', '/status'),
		updateFan: (id, changes) => call('post', '/fan', { id, ...changes }),
		applyPreset: (preset) => call('post', '/preset', { preset }),
		setCritical: (cpu, gpu) => {
			const body = {}
			if (cpu !== undefined && cpu !== null) body.critical_cpu_c = cpu
			if (gpu !== undefined && gpu !== null) body.critical_gpu_c = gpu
			return call('post', '/settings', body)
		},
		identify: (id) => call('post', '/identify', { id }),
		allAuto: () => call('post', '/auto'),
		detect: () => call('post', '/detect')
	}
}

export default createFansClient()
