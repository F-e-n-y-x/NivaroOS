import { api } from "./service.js";

const PREFIX = "/schedules";

const schedules = {
	getSchedules() {
		return api.get(PREFIX);
	},

	getSchedule(id) {
		return api.get(`${PREFIX}/${id}`);
	},

	createSchedule(data) {
		return api.post(PREFIX, data);
	},

	updateSchedule(id, data) {
		return api.put(`${PREFIX}/${id}`, data);
	},

	deleteSchedule(id) {
		return api.delete(`${PREFIX}/${id}`);
	},

	// The route is POST /:id/toggle (this called PUT /:id/enable, which
	// doesn't exist - the switch could never work).
	toggleSchedule(id, enabled) {
		return api.post(`${PREFIX}/${id}/toggle`, { enabled });
	},

	runScheduleNow(id) {
		return api.post(`${PREFIX}/${id}/run`);
	},

	getTargets() {
		return api.get(`${PREFIX}/targets`);
	}
};

export default schedules;
