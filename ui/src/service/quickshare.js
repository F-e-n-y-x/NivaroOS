import {api} from "./service.js";

const PREFIX = "/quickshare";
const quickshare = {
	// list this instance's active Quick Share links
	list() {
		return api.get(`${PREFIX}`);
	},

	// create a Quick Share link for a single file. expiry is one of: '1h', '1d', 'never'
	create(path, expiry) {
		return api.post(`${PREFIX}`, { path, expiry });
	},

	// revoke a Quick Share link
	remove(id) {
		return api.delete(`${PREFIX}/${id}`);
	},
}
export default quickshare;
