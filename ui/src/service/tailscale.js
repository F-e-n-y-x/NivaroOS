import { api } from './service.js'

const PREFIX = '/tailscale'

const tailscale = {
	getStatus() {
		return api.get(`${PREFIX}/status`)
	},
	setState(state) {
		return api.put(`${PREFIX}/state/${state}`)
	},
	getPrefs() {
		return api.get(`${PREFIX}/prefs`)
	},
	setPrefs(data) {
		return api.put(`${PREFIX}/prefs`, data)
	}
}
export default tailscale
