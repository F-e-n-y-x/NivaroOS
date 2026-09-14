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

		// Files the given app names into the (possibly newly-created) auto
		// container folder in one read-modify-write, so two apps discovered
		// unfiled in the same getList() tick don't race each other's
		// getFolders()/saveFolders() pair and clobber one another.
		async autoFileContainerApps(appNames) {
			if (!appNames.length) return null
			const folders = await this.getFolders()
			let folder = folders.find(f => f.isAutoContainerFolder)
			if (!folder) {
				folder = {
					id: 'folder-' + Date.now(),
					name: AUTO_CONTAINER_FOLDER_NAME,
					icon: null,
					appNames: [],
					isAutoContainerFolder: true
				}
				folders.push(folder)
			}
			appNames.forEach(name => {
				if (!folder.appNames.includes(name)) folder.appNames.push(name)
			})
			await this.saveFolders(folders)
			return folder
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
			const excludes = await this.getContainerAutoExcludes()
			if (!excludes.includes(appName)) {
				excludes.push(appName)
				await this.$api.users.setCustomStorage(containerAutoExcludeConfig, excludes)
			}
		}
	}
}
