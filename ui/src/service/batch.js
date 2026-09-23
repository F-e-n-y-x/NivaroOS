import {api} from "./service.js";

const PREFIX = "/batch"

const batch = {
	// download
	download(format, files) {
		return api.get(`${PREFIX}`, {
			format: format,
			files: files
		});
	},

	// register a multi-file / folder download; returns a ticket id
	downloadTicket(files, format = 'zip') {
		return api.post(`${PREFIX}/download`, { files, format });
	},

	// File operate task TODO:wait for the api
	task(data) {
		return api.post(`${PREFIX}/task`, data);
	},

	// cancel a copy/move/delete job ("0" = all)
	deleteTask(id) {
		return api.delete(`${PREFIX}/${id}/task`);
	},

	// every recent job, active and finished (see service/transfers.js)
	tasks() {
		return api.get(`${PREFIX}/tasks`);
	},

	// re-run what didn't complete in a finished job
	retry(id) {
		return api.post(`${PREFIX}/${id}/retry`);
	},

	// drop a finished job from the history ("0" = all finished)
	dismiss(id) {
		return api.delete(`${PREFIX}/${id}/history`);
	},

	// delete file or folder
	delete(files) {
		return api.delete(`${PREFIX}`, files);
	},


}

export default batch;