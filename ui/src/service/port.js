import {api} from "./service.js";

const PREFIX = "/port"

const port = {
	// check if the port is available
	check(port, type) {
		return api.get(`${PREFIX}/state/${port}`, {
			type: type
		});
	},

	// get a able port
	get(type) {
		return api.get(`${PREFIX}`, {
			type: type
		});
	}
}

export default port;