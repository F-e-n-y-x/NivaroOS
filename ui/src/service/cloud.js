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

	// quick real upload/download throughput check against the account
	speedTest(name) {
		return api.post(`${PREFIX}/accounts/${name}/speedtest`, {})
	}
}
export default cloud;
