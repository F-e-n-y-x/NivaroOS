/*
 * @Author: JerryK
 * @Date: 2021-09-18 21:32:13
 * @LastEditors: Jerryk jerry@icewhale.org
 * @LastEditTime: 2022-08-11 17:16:42
 * @Description: Disk API
 * @FilePath: \CasaOS-UI\src\service\disks.js
 */
import {api} from "./service.js";

const PREFIX = "/disks";
const disks = {

	// get disk list
	getDiskList(data) {
		return api.get(`${PREFIX}`, data);
	},

	umount(data) {
		return api.delete(`${PREFIX}`, data);
	},

	// Get usbs
	getUsbs() {
		return api.get(`${PREFIX}/usb`);
	},

	// Umount usb
	umountUsb(data) {
		return api.delete(`${PREFIX}/usb`, data);
	},

	// Full (uncached) SMART info for a drive - "Show info" and self-test polling
	getSmartInfo(path) {
		return api.get(`${PREFIX}/smart`, { path });
	},

	// Start a SMART self-test ("short" or "long") on a drive
	startSmartTest(path, type) {
		return api.post(`${PREFIX}/smart-test`, { path, type });
	},

	// Get a drive's configured standby/spindown timer, in minutes
	getStandby(path) {
		return api.get(`${PREFIX}/standby`, { path });
	},

	// Set a drive's standby/spindown timer, in minutes (0 disables it)
	setStandby(path, minutes) {
		return api.put(`${PREFIX}/standby`, { path, minutes });
	}
}
export default disks;
