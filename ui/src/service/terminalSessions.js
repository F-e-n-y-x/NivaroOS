import { api } from './service.js'
import { createSessionApi } from '@/apps/terminal/termSessions.js'

// Persistent terminal sessions (host: core, container: app-management).
export default createSessionApi({
	get: (url, params) => api.get(url, params),
	post: (url, body) => api.post(url, body),
	put: (url, body) => api.put(url, body),
	delete: (url) => api.delete(url),
})
