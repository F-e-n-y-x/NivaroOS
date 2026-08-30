/*
 * Which apps (besides the built-in Files/Terminal/Settings) show up as
 * launcher icons in the bottom dock - a plain list of app names, per
 * user, in per-user custom storage.
 */
const pinsConfig = 'dock_pinned_apps'

export default {
	methods: {
		async getDockPins() {
			try {
				const res = await this.$api.users.getCustomStorage(pinsConfig)
				return (res.data && Array.isArray(res.data.data)) ? res.data.data : []
			} catch (e) {
				console.error('getDockPins', e)
				return []
			}
		},

		async setDockPinned(name, pinned) {
			const pins = await this.getDockPins()
			const withoutName = pins.filter(n => n !== name)
			const next = pinned ? withoutName.concat(name) : withoutName
			return this.$api.users.setCustomStorage(pinsConfig, next)
		}
	}
}
