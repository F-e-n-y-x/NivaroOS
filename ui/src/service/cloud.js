import {api} from "./service.js";

const PREFIX = "/cloud";
const cloud = {
	// get storage list
	list(data) {
		return api.get(`${PREFIX}`, data)
	},

	// delete storage
	umount(data) {
		return api.delete(`${PREFIX}`, data);
	},

	// supported online-account providers
	providers() {
		return api.get(`${PREFIX}/providers`)
	},

	// rclone's own config-field metadata for a provider type
	providerOptions(type) {
		return api.get(`${PREFIX}/providers/${type}/options`)
	},

	// add a non-interactive account (form-based providers, or an
	// OAuth provider via a pasted `rclone authorize` token)
	createAccount(data) {
		return api.post(`${PREFIX}/accounts`, data)
	},

	// iCloud's interactive Apple ID + 2FA flow (pass `name` too to reconnect
	// an existing account in place instead of creating a new one)
	icloudStart(data) {
		return api.post(`${PREFIX}/accounts/icloud/start`, data)
	},
	icloudVerify(data) {
		return api.post(`${PREFIX}/accounts/icloud/verify`, data)
	},

	// rename an account's display label
	rename(name, label) {
		return api.put(`${PREFIX}/accounts/${name}`, { label })
	},

	// replace credentials on an existing account (fresh token, or updated
	// server details) without losing its name/mount point
	reconnect(name, params) {
		return api.post(`${PREFIX}/accounts/${name}/reconnect`, { params })
	},

	// real upload/download throughput check against the account: POST
	// starts a background run, GET polls it, DELETE stops it
	speedTest(name) {
		return api.post(`${PREFIX}/accounts/${name}/speedtest`, {})
	},
	speedTestStatus(name) {
		return api.get(`${PREFIX}/accounts/${name}/speedtest`)
	},
	speedTestCancel(name) {
		return api.delete(`${PREFIX}/accounts/${name}/speedtest`)
	},

	// Settings > Online storage > Cache: settings, per-account usage and
	// uploads that can't finish
	cacheGet() {
		return api.get(`${PREFIX}/cache`)
	},
	// Saving remounts the online drives; it can take a while when a cache
	// with uploads waiting moves to another disk.
	cacheSave(settings, dryRun = false) {
		return api.put(`${PREFIX}/cache${dryRun ? '?dry_run=1' : ''}`, settings, { timeout: 30 * 60 * 1000 })
	},
	cacheClear(name) {
		return api.post(`${PREFIX}/cache/clear`, { name: name || '' }, { timeout: 5 * 60 * 1000 })
	},
	// action: save | retry | discard; data: {account, file, folder?}
	cacheStuck(action, data) {
		return api.post(`${PREFIX}/cache/stuck/${action}`, data, { timeout: 30 * 60 * 1000 })
	}
}
export default cloud;
