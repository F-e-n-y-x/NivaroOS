// The message to show for a failed API call. Services put the reason in
// different places: core's `data` (a string), others' `message`; axios'
// own err.message ("Request failed with status code 400") is the last
// resort - it was what the UI usually showed.
const GENERIC = /^(ok|service error|parameters error|request failed with status code \d+)$/i

export function apiError(err, fallback = '') {
	const d = err && err.response && err.response.data
	if (d) {
		if (typeof d.data === 'string' && d.data.trim()) return d.data
		if (typeof d.message === 'string' && d.message.trim() && !GENERIC.test(d.message.trim())) return d.message
		if (typeof d.error === 'string' && d.error.trim()) return d.error
	}
	if (err && err.message === 'Network Error') return "Can't reach the server - check the connection"
	if (err && /timeout/i.test(err.message || '')) return 'The server took too long to answer'
	return fallback || (err && err.message) || 'Something went wrong'
}
