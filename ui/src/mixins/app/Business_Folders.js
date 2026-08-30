/*
 * Folders group existing app tiles for display only - purely a UI-level
 * concept stored in per-user custom storage (same mechanism as
 * widgets_config/app_order), never touching app-management or containers.
 *
 * Folder shape: { id, name, icon, appNames: [] }
 */
const foldersConfig = 'app_folders'

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
		}
	}
}
