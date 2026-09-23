import { api } from './service.js'

const PREFIX = '/trash'

// The Files app's recycle bin (services/core/service/trash).
const trash = {
	list() {
		return api.get(PREFIX)
	},
	// Does deleting in `path` go to the Trash? (No for cloud drives,
	// companion devices and network shares - those delete permanently.)
	support(path) {
		return api.get(`${PREFIX}/support`, { path })
	},
	restore(ids) {
		return api.post(`${PREFIX}/restore`, { ids })
	},
	deleteForever(ids) {
		return api.delete(PREFIX, { ids })
	},
	empty() {
		return api.delete(`${PREFIX}/all`)
	},
}

export default trash
