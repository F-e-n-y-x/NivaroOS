import { api } from "./service.js";

const PREFIX = "/sys"

const sys = {

	// Get websocket port
	getSocketPort() {
		return api.get(`${PREFIX}/socket-port`);
	},

	// Check if need init
	guideCheck() {
		return api.get(`${PREFIX}/state`);
	},

	// check system version
	getVersion() {
		return api.get(`${PREFIX}/version`);
	},

	// Hardware Info
	hardwareInfo() {
		return api.get(`${PREFIX}/hardware`)
	},

	// get cpu info
	getCpuInfo() {
		return api.get(`${PREFIX}/cpu`);
	},

	// get disk info
	getDiskInfo() {
		return api.get(`${PREFIX}/disk`);
	},

	// get per-mount disk usage (df-based)
	getDisksUsage() {
		return api.get(`${PREFIX}/disks-usage`);
	},

	// set the machine hostname
	setHostname(hostname) {
		return api.put(`${PREFIX}/hostname`, { hostname });
	},

	// get memory info
	getMemoryInfo() {
		return api.get(`${PREFIX}/mem`);
	},

	// get network info
	getNetworkInfo() {
		return api.get(`${PREFIX}/network`);
	},

	// get this device's real network interfaces (filtered, with IPs)
	getNetworkInterfaces() {
		return api.get(`${PREFIX}/network-interfaces`);
	},

	// get server internet speedtest
	getSpeedtest() {
		return api.get(`${PREFIX}/speedtest`);
	},

	// start the internet speedtest in the background (nearest speedtest.net
	// server); poll getSpeedtestStatus for live numbers
	startSpeedtest() {
		return api.post(`${PREFIX}/speedtest`);
	},

	getSpeedtestStatus() {
		return api.get(`${PREFIX}/speedtest/status`);
	},

	// get logs
	// Last `lines` lines of the NivaroOS log (the server caps it; the full
	// file is many MB and froze the browser).
	getLogs(lines = 300) {
		return api.get(`${PREFIX}/logs`, { line: lines });
	},

	//Get Debug Info
	getDebugInfo() {
		return api.get(`${PREFIX}/debug`);
	},

	// get system utilization
	getUtilization() {
		return api.get(`${PREFIX}/utilization`);
	},

	// proxy request
	getProxyRequestContent(url) {
		return api.get(`${PREFIX}/proxy?url=${url}`)
	},

	// get nivaroos server port
	getServerPort() {
		return api.get(`/gateway/port`);
	},

	// edit nivaroos server port
	editServerPort(data) {
		return api.put(`/gateway/port`, data);
	},

	// get usb status
	getUsbStatus() {
		return api.get(`/usb/usb-auto-mount`);
	},

	// Toggle usb auto-mount
	toggleUsbAutoMount(data) {
		return api.put(`/usb/usb-auto-mount`, data);
	},

	// update NivaroOS
	updateNivaroOS() {
		return api.post(`${PREFIX}/update`);
	},

	// Free up the server's memory (drop caches; optionally empty swap,
	// which can take minutes - hence the longer wait).
	clearMemory({ swap = false } = {}) {
		return api.post(`${PREFIX}/memory/clear`, { swap }, { timeout: swap ? 12 * 60 * 1000 : 60000 });
	},

	// stop nivaroos
	stopNivaroOS() {
		return api.post(`${PREFIX}/stop`);
	},

	//Check web ui Port
	checkUiPort(url) {
		return api.get(url);
	},

	// Get system apps
	getSystemApps() {
		return api.get(`${PREFIX}/apps-state`)
	},

	// Check ssh login
	checkSshLogin(data) {
		return api.post(`${PREFIX}/ssh-login`, data);
	},

	// power -- data:shutdown
	// power -- data:restart
	power(data) {
		return api.put(`${PREFIX}/state/${data}`);
	},

	// System (Linux) users
	getSystemUsers() {
		return api.get(`${PREFIX}/system-users`);
	},
	createSystemUser(data) {
		return api.post(`${PREFIX}/system-users`, data);
	},
	deleteSystemUser(username) {
		return api.delete(`${PREFIX}/system-users/${username}`);
	},
	setSystemUserPassword(username, password) {
		return api.put(`${PREFIX}/system-users/${username}/password`, { password });
	},
	setSystemUserGroups(username, data) {
		return api.put(`${PREFIX}/system-users/${username}/groups`, data);
	},

	// SMB users
	getSmbUsers() {
		return api.get(`${PREFIX}/smb-users`);
	},
	createSmbUser(data) {
		return api.post(`${PREFIX}/smb-users`, data);
	},
	deleteSmbUser(username) {
		return api.delete(`${PREFIX}/smb-users/${username}`);
	},
	setSmbUserPassword(username, password) {
		return api.put(`${PREFIX}/smb-users/${username}/password`, { password });
	},

	// Linux System (APT) Package Updates
	getPackageUpdates() {
		return api.get(`${PREFIX}/packages/check`);
	},
	refreshPackageUpdates() {
		return api.post(`${PREFIX}/packages/refresh`);
	},
	upgradePackages() {
		return api.post(`${PREFIX}/packages/upgrade`);
	},
	getPackageUpgradeStatus() {
		return api.get(`${PREFIX}/packages/upgrade/status`);
	},

	// APT Package Management
	searchAptPackages(query) {
		return api.get(`${PREFIX}/apt/search?q=${encodeURIComponent(query)}`);
	},
	getInstalledAptPackages(query = '') {
		return api.get(`${PREFIX}/apt/installed${query ? '?q=' + encodeURIComponent(query) : ''}`);
	},
	getUpgradableAptPackages() {
		return api.get(`${PREFIX}/apt/upgradable`);
	},
	installAptPackages(packages, reinstall = false) {
		return api.post(`${PREFIX}/apt/install`, { packages, reinstall });
	},
	uninstallAptPackages(packages, purge = false) {
		return api.post(`${PREFIX}/apt/uninstall`, { packages, purge });
	},
	upgradeAptPackages(packages = []) {
		return api.post(`${PREFIX}/apt/upgrade`, { packages });
	},
	// What uninstalling `packages` would remove in total (a simulation).
	getAptRemovePreview(packages) {
		return api.get(`${PREFIX}/apt/remove-preview`, { packages: packages.join(',') });
	},
	// The latest package operation (running or finished) with its log.
	getAptJob() {
		return api.get(`${PREFIX}/apt/job`);
	},
	updateAptRepositories() {
		return api.post(`${PREFIX}/apt/update`);
	},
	getAptSources() {
		return api.get(`${PREFIX}/apt/sources`);
	},
	addAptSource(source, file = 'custom.list') {
		return api.post(`${PREFIX}/apt/sources`, { source, file });
	},
	deleteAptSource(file, line, raw) {
		// api.delete already wraps its second argument as the request body.
		return api.delete(`${PREFIX}/apt/sources`, { file, line, raw });
	},
}
export default sys;
