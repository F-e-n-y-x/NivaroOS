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

	toggleSchedule(id, enabled) {
		return api.put(`${PREFIX}/${id}/enable`, { enabled });
	},

	runScheduleNow(id) {
		return api.post(`${PREFIX}/${id}/run`);
	},

	getTargets() {
		return api.get(`${PREFIX}/targets`);
	}
};

export default schedules;
