import {api} from "./service.js";

const PREFIX = "/storage/fstab";
const fstab = {
	// Managed + system fstab entries: { managed: [...], system: [...] }
	list() {
		return api.get(`${PREFIX}`);
	},

	// Already-formatted drives that can be added.
	candidates() {
		return api.get(`${PREFIX}/candidates`);
	},

	create(data) {
		return api.post(`${PREFIX}`, data);
	},

	update(data) {
		return api.put(`${PREFIX}`, data);
	},

	remove(mountPoint) {
		return api.delete(`${PREFIX}?mount_point=${encodeURIComponent(mountPoint)}`);
	},

	setEnabled(mountPoint, enabled) {
		return api.put(`${PREFIX}/enabled`, { mount_point: mountPoint, enabled });
	}
}
export default fstab;
