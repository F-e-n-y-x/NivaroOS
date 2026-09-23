import appCategories from './appCategories.js';
import apps from './apps.js';
import batch from './batch.js';
import container from './container.js';
import disks from './disks.js';
import fstab from './fstab.js';
import file from './file.js';
import folder from './folder.js';
import image from './image.js';
import port from './port.js';
import sys from './sys.js';
import storage from './storage.js';
import samba from './samba.js';
import quickshare from './quickshare.js';
import tailscale from './tailscale.js';
import users from "./users.js";
import trash from "./trash.js";
import local_storage from "./local_storage.js";
import driver from './driver.js';
import cloud from './cloud.js';
import schedules from './schedules.js';
import companion from './companion.js';

// Getters, not values: service.js imports the router, whose views import
// this file - so while a service module (e.g. batch.js) is still loading,
// this object can be built. A plain value would capture `undefined`
// forever ($api.batch.delete -> "Cannot read properties of undefined");
// a getter reads the module once it has finished loading.
export default {
	get trash() { return trash },
	// Apps
	get appCategories() { return appCategories },
	get apps() { return apps },
	get container() { return container },
	// Files
	get file() { return file },
	get folder() { return folder },
	get image() { return image },
	get batch() { return batch },
	// Devices
	get disks() { return disks },
	get fstab() { return fstab },
	get storage() { return storage },
	get samba() { return samba },
	get quickshare() { return quickshare },
	get tailscale() { return tailscale },
	get companion() { return companion },
	get driver() { return driver },
	get cloud() { return cloud },
	// System
	get sys() { return sys },
	get port() { return port },
	get schedules() { return schedules },
	// User
	get users() { return users },
	get local_storage() { return local_storage },
}
