import {api} from "./service.js";

const PREFIX = "/samba";
const samba = {
	//  Connections
	// get the list of samba connections
	getConnections() {
		return api.get(`${PREFIX}/connections`);
	},

	// create a connection
	createConnection(data) {
		return api.post(`${PREFIX}/connections`, data);
	},

	// Delete a connection
	deleteConnection(id) {
		return api.delete(`${PREFIX}/connections/${id}`);
	},

	// Shares
	// get share list
	getShares() {
		return api.get(`${PREFIX}/shares`);
	},

	// create a share
	createShare(data) {
		return api.post(`${PREFIX}/shares`, data);
	},

	// edit an existing share's name/read-only/guest-access settings
	updateShare(id, data) {
		return api.put(`${PREFIX}/shares/${id}`, data);
	},

	// delete a share
	deleteShare(id) {
		return api.delete(`${PREFIX}/shares/${id}`);
	},
}
export default samba;
