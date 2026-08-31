/*
 * Which apps (built-ins and third-party apps) show up as launcher icons
 * in the bottom dock - a plain list of app names in per-user custom storage.
 * Default: Files and App Store only.
 */
const pinsConfig = 'dock_pinned_apps'

export const DEFAULT_PINS = ['Files', 'App Store']

export const SYSTEM_NAME_MAP = {
	files: 'Files',
	appstore: 'App Store',
	terminal: 'Terminal',
	vms: 'VMs',
	settings: 'Settings',
	'Files': 'Files',
	'App Store': 'App Store',
	'Terminal': 'Terminal',
	'VMs': 'VMs',
	'Settings': 'Settings'
}

export default {
	methods: {
		async getDockPins() {
			try {
				const res = await this.$api.users.getCustomStorage(pinsConfig)
				if (res.data && Array.isArray(res.data.data)) {
					return res.data.data
				}
				return [...DEFAULT_PINS]
			} catch (e) {
				return [...DEFAULT_PINS]
			}
		},

		async setDockPinned(name, pinned) {
			const normalizedName = SYSTEM_NAME_MAP[name] || name
			const currentPins = await this.getDockPins()
			const withoutName = currentPins.filter(n => {
				const norm = SYSTEM_NAME_MAP[n] || n
				return norm !== normalizedName && n !== name
			})
			const next = pinned ? withoutName.concat(normalizedName) : withoutName
			return this.$api.users.setCustomStorage(pinsConfig, next)
		}
	}
}
