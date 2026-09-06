import { api } from './service.js'

const PREFIX = '/companion'

const companion = {
	getDevices() {
		return api.get(`${PREFIX}/devices`)
	},
	registerDevice(data) {
		return api.post(`${PREFIX}/register`, data)
	},
	updateDevice(id, data) {
		return api.put(`${PREFIX}/devices/${id}`, data)
	},
	deleteDevice(id) {
		return api.delete(`${PREFIX}/devices/${id}`)
	},
	getDeviceStorage(id) {
		return api.get(`${PREFIX}/devices/${id}/storage`)
	},
	getDeviceFiles(id, path) {
		return api.get(`${PREFIX}/devices/${id}/files`, { params: { path } })
	}
}

export default companion
