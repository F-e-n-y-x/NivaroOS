/*
 * Folders group existing app tiles for display only - purely a UI-level
 * concept stored in per-user custom storage (same mechanism as
 * widgets_config/app_order), never touching app-management or containers.
 *
 * Folder shape: { id, name, icon, appNames: [], isAutoContainerFolder? }
 */
const foldersConfig = 'app_folders'

// Containers deployed outside the App Store (e.g. `docker compose up` via
// Portainer/CLI - app_type: "container" from GetAppGrid) get auto-filed
// into this folder instead of littering the desktop. `isAutoContainerFolder`
// marks it so it survives a rename and so getList() can find it without
// depending on a fixed id/name.
export const AUTO_CONTAINER_FOLDER_NAME = 'Other Containers'
const containerAutoExcludeConfig = 'container_auto_folder_excludes'

// Best-effort project detection from Docker Compose's own default container
// naming convention - `<project>-<service>-<index>` (Compose v2, the
// default today) or `<project>_<service>_<index>` (Compose v1 /
// COMPOSE_COMPATIBILITY mode). There's no backend field for this (would
// need a container-label plumbed through app-management's API, which is
// blocked on an unrelated broken dependency chain in this build - see
// project notes), so this is a heuristic: it assumes the service name
// itself has no dash/underscore, which holds for typical single-word
// service names (web, db, redis, nginx...) but can mis-split a service
// name that itself contains one. A container started with a custom
// `container_name` (bypassing the convention entirely) returns null and
// falls back to the generic "Other Containers" folder, same as before.
function parseComposeProject(rawName) {
	if (!rawName) return null
	const name = rawName.replace(/^\//, '')
	const m = name.match(/^(.+)[-_][^-_]+[-_]\d+$/)
	return m ? m[1] : null
}
export { parseComposeProject }

function makeFolderId() {
	return 'folder-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8)
}

export default {
	methods: {
		async getFolders() {
			try {
				const res = await this.$api.users.getCustomStorage(foldersConfig)
				return (res.data && res.data.data) || []
			} catch (e) {
				console.error('getFolders', e)
				return []
			}
		},

		saveFolders(folders) {
			return this.$api.users.setCustomStorage(foldersConfig, folders)
		},

		async createFolder(name) {
			const folders = await this.getFolders()
			const folder = {
				id: 'folder-' + Date.now(),
				name,
				icon: null,
				appNames: []
			}
			folders.push(folder)
			await this.saveFolders(folders)
			return folder
		},

		async addAppToFolder(appName, folderId) {
			const folders = await this.getFolders()
			folders.forEach(f => {
				f.appNames = f.appNames.filter(n => n !== appName)
			})
			const folder = folders.find(f => f.id === folderId)
			if (folder) {
				folder.appNames.push(appName)
			}
			await this.saveFolders(folders)
			return folders
		},

		async removeAppFromFolder(appName, folderId) {
			const folders = await this.getFolders()
			const folder = folders.find(f => f.id === folderId)
			if (folder) {
				folder.appNames = folder.appNames.filter(n => n !== appName)
			}
			await this.saveFolders(folders)
			return folders
		},

		// Same as removeAppFromFolder but for several apps at once, in a
		// single read-modify-write - calling removeAppFromFolder in a loop
		// would race itself (each call's getFolders() can miss the previous
		// call's not-yet-saved removal), silently leaving some apps behind.
		async removeAppsFromFolder(appNames, folderId) {
			const folders = await this.getFolders()
			const folder = folders.find(f => f.id === folderId)
			if (folder) {
				folder.appNames = folder.appNames.filter(n => !appNames.includes(n))
			}
			await this.saveFolders(folders)
			return folders
		},

		async deleteFolder(folderId) {
			const folders = await this.getFolders()
			const remaining = folders.filter(f => f.id !== folderId)
			await this.saveFolders(remaining)
			return remaining
		},

		async renameFolder(folderId, name) {
			const folders = await this.getFolders()
			const folder = folders.find(f => f.id === folderId)
			if (folder) {
				folder.name = name
			}
			await this.saveFolders(folders)
			return folders
		},

		async setFolderIcon(folderId, icon, iconRadius) {
			const folders = await this.getFolders()
			const folder = folders.find(f => f.id === folderId)
			if (folder) {
				if (icon) folder.icon = icon
				folder.iconRadius = iconRadius
			}
			await this.saveFolders(folders)
			return folders
		},

		// Files each group's app names into its own compose-project folder
		// (creating it on first sight) - or, for containers with no detected
		// project, the shared "Other Containers" folder - in one
		// read-modify-write so several groups discovered unfiled in the same
		// getList() tick don't race each other's getFolders()/saveFolders()
		// pair and clobber one another.
		//
		// groups: [{ project: string|null, appNames: string[] }]
		async autoFileContainerApps(groups) {
			if (!groups.length) return
			const folders = await this.getFolders()
			groups.forEach(({ project, appNames }) => {
				if (!appNames.length) return
				let folder = project
					? folders.find(f => f.composeProject === project)
					: folders.find(f => f.isAutoContainerFolder && !f.composeProject)
				if (!folder) {
					folder = {
						id: makeFolderId(),
						name: project || AUTO_CONTAINER_FOLDER_NAME,
						icon: null,
						appNames: [],
						isAutoContainerFolder: true
					}
					if (project) folder.composeProject = project
					folders.push(folder)
				}
				appNames.forEach(name => {
					if (!folder.appNames.includes(name)) folder.appNames.push(name)
				})
			})
			await this.saveFolders(folders)
		},

		async getContainerAutoExcludes() {
			try {
				const res = await this.$api.users.getCustomStorage(containerAutoExcludeConfig)
				return (res.data && res.data.data) || []
			} catch (e) {
				console.error('getContainerAutoExcludes', e)
				return []
			}
		},

		// Called when the user removes an app from the auto container folder
		// (moves it back out) - remembered permanently so getList() doesn't
		// re-file it right back in on its next 8s refresh.
		async addContainerAutoExclude(appName) {
			return this.addContainerAutoExcludes([appName])
		},

		// Batched version - see removeAppsFromFolder for why a loop of single
		// read-modify-write calls would race itself.
		async addContainerAutoExcludes(appNames) {
			const excludes = await this.getContainerAutoExcludes()
			let changed = false
			appNames.forEach(name => {
				if (!excludes.includes(name)) {
					excludes.push(name)
					changed = true
				}
			})
			if (changed) {
				await this.$api.users.setCustomStorage(containerAutoExcludeConfig, excludes)
			}
		}
	}
}
